// Package httproute manages per-driver Gateway API HTTPRoutes.
//
// For each active Spark driver the Manager creates a dedicated HTTPRoute named
// "<appSelector>-ui-route". When the driver stops the route is deleted. The
// Manager uses the label "app.kubernetes.io/managed-by: spark-ui-assist" so
// that Reconcile can list all routes it owns and clean up any that belong to
// drivers that are no longer active.
//
// Reconcile() should be called once the pod informer has fully synced so that
// stale routes left over from a previous instance of the service are cleaned up.
package httproute

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/unaiur/k8s-spark-ui-assist/internal/config"
	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

var httpRouteGVR = schema.GroupVersionResource{
	Group:    "gateway.networking.k8s.io",
	Version:  "v1",
	Resource: "httproutes",
}

// managedByLabel is applied to every HTTPRoute created by this Manager so that
// Reconcile can list them all with a single API call.
const managedByLabel = "app.kubernetes.io/managed-by"
const managedByValue = "spark-ui-assist"

// driverPathPrefix is the fixed URL path prefix for per-driver HTTPRoute rules.
// Spark UI requires this to be "/proxy/" to resolve its internal asset paths correctly.
const driverPathPrefix = "/proxy/"

// Manager creates and deletes per-driver HTTPRoutes.
type Manager struct {
	client    dynamic.Interface
	namespace string
	cfg       config.HTTPRouteConfig
}

// New creates a new Manager.
func New(client dynamic.Interface, namespace string, cfg config.HTTPRouteConfig) *Manager {
	return &Manager{client: client, namespace: namespace, cfg: cfg}
}

// invalidDNSChars matches any character not allowed in a DNS-1123 label.
var invalidDNSChars = regexp.MustCompile(`[^a-z0-9-]`)

// routeName converts appSelector into a valid DNS-1123 subdomain name and
// appends "-ui-route". Kubernetes label values allow uppercase letters, dots,
// underscores, and up to 63 characters, none of which are universally valid in
// resource names. The sanitisation steps are:
//  1. Lowercase the selector.
//  2. Replace any character that is not [a-z0-9-] with "-".
//  3. If the result (plus the "-ui-route" suffix) exceeds 253 characters,
//     truncate and append an 8-hex-character hash of the original selector so
//     the name remains unique and deterministic.
func routeName(appSelector string) string {
	const suffix = "-ui-route"
	const maxLen = 253

	sanitized := invalidDNSChars.ReplaceAllString(strings.ToLower(appSelector), "-")

	candidate := sanitized + suffix
	if len(candidate) <= maxLen {
		return candidate
	}

	// Hash the original selector to preserve uniqueness.
	h := sha256.Sum256([]byte(appSelector))
	hash := fmt.Sprintf("%x", h[:4]) // 8 hex chars
	// Truncate sanitized so that sanitized + "-" + hash + suffix fits in maxLen.
	maxSanitized := maxLen - len(suffix) - 1 - len(hash)
	return sanitized[:maxSanitized] + "-" + hash + suffix
}

// Ensure creates the per-driver HTTPRoute if it does not already exist.
// It is a no-op when the route is already present.
func (m *Manager) Ensure(ctx context.Context, d store.Driver) {
	rc := m.client.Resource(httpRouteGVR).Namespace(m.namespace)
	name := routeName(d.AppSelector)

	_, err := rc.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return // already exists
	}
	if !errors.IsNotFound(err) {
		log.Printf("httproute: ensure %s: failed to check existence: %v", name, err)
		return
	}

	route := buildRoute(d, m.cfg, m.namespace, driverPathPrefix)
	if _, err := rc.Create(ctx, route, metav1.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) {
			return // created by a concurrent Ensure call; desired state is already met
		}
		log.Printf("httproute: ensure %s: failed to create: %v", name, err)
		return
	}
	log.Printf("httproute: created HTTPRoute %s", name)
}

// Delete removes the per-driver HTTPRoute for appSelector.
// It is a no-op when the route does not exist.
func (m *Manager) Delete(ctx context.Context, appSelector string) {
	rc := m.client.Resource(httpRouteGVR).Namespace(m.namespace)
	name := routeName(appSelector)

	err := rc.Delete(ctx, name, metav1.DeleteOptions{})
	if errors.IsNotFound(err) {
		return // already gone
	}
	if err != nil {
		log.Printf("httproute: delete %s: failed: %v", name, err)
		return
	}
	log.Printf("httproute: deleted HTTPRoute %s", name)
}

// Reconcile synchronises the set of managed HTTPRoutes against the provided
// list of currently active drivers. It deletes routes for drivers that are no
// longer active and creates routes for drivers that are missing one.
// Call this once after the pod informer has fully synced.
func (m *Manager) Reconcile(ctx context.Context, active []store.Driver) {
	rc := m.client.Resource(httpRouteGVR).Namespace(m.namespace)

	// Build a set of expected route names.
	wantedByName := make(map[string]store.Driver, len(active))
	for _, d := range active {
		wantedByName[routeName(d.AppSelector)] = d
	}

	// List all HTTPRoutes owned by this manager.
	list, err := rc.List(ctx, metav1.ListOptions{
		LabelSelector: managedByLabel + "=" + managedByValue,
	})
	if err != nil {
		log.Printf("httproute: reconcile: failed to list managed HTTPRoutes: %v", err)
		return
	}

	presentNames := make(map[string]bool, len(list.Items))
	for _, item := range list.Items {
		name := item.GetName()
		presentNames[name] = true
		if _, ok := wantedByName[name]; !ok {
			log.Printf("httproute: reconcile: deleting stale HTTPRoute %s", name)
			if delErr := rc.Delete(ctx, name, metav1.DeleteOptions{}); delErr != nil && !errors.IsNotFound(delErr) {
				log.Printf("httproute: reconcile: failed to delete %s: %v", name, delErr)
			}
		}
	}

	for name, d := range wantedByName {
		if presentNames[name] {
			continue
		}
		log.Printf("httproute: reconcile: creating missing HTTPRoute %s", name)
		route := buildRoute(d, m.cfg, m.namespace, driverPathPrefix)
		if _, createErr := rc.Create(ctx, route, metav1.CreateOptions{}); createErr != nil && !errors.IsAlreadyExists(createErr) {
			log.Printf("httproute: reconcile: failed to create %s: %v", name, createErr)
		}
	}
}

// buildRoute returns a complete HTTPRoute object for the given driver.
// namespace is the Kubernetes namespace in which the route will be created.
//
// The route contains three rules in priority order:
//  1. Exact match on "<prefix><appSelector>" (no trailing slash) → 302 redirect to
//     "<prefix><appSelector>/jobs/".
//  2. Exact match on "<prefix><appSelector>/" (trailing slash) → 302 redirect to
//     "<prefix><appSelector>/jobs/".
//  3. PathPrefix match on "<prefix><appSelector>" → URLRewrite (strip prefix) and
//     forward to the driver service at port 4040.
//
// Rules 1 and 2 implement the Spark documentation requirement to redirect bare
// prefix requests to the jobs page so that relative links resolve correctly.
// Rule 3 handles all other requests under the prefix.
func buildRoute(d store.Driver, cfg config.HTTPRouteConfig, namespace string, prefix string) *unstructured.Unstructured {
	pathPrefix := prefix + d.AppSelector
	jobsRedirectTarget := pathPrefix + "/jobs/"
	svcName := d.AppName + "-ui-svc"
	name := routeName(d.AppSelector)

	// redirectRule builds a rule that issues a 302 redirect to jobsRedirectTarget
	// for requests that exactly match the given path.
	redirectRule := func(exactPath string) interface{} {
		return map[string]interface{}{
			"matches": []interface{}{
				map[string]interface{}{
					"path": map[string]interface{}{
						"type":  "Exact",
						"value": exactPath,
					},
				},
			},
			"filters": []interface{}{
				map[string]interface{}{
					"type": "RequestRedirect",
					"requestRedirect": map[string]interface{}{
						"path": map[string]interface{}{
							"type":            "ReplaceFullPath",
							"replaceFullPath": jobsRedirectTarget,
						},
						"statusCode": int64(302),
					},
				},
			},
		}
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "HTTPRoute",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"labels": map[string]interface{}{
					managedByLabel: managedByValue,
				},
			},
			"spec": map[string]interface{}{
				"parentRefs": []interface{}{
					map[string]interface{}{
						"name":      cfg.GatewayName,
						"namespace": cfg.GatewayNamespace,
					},
				},
				"hostnames": []interface{}{cfg.Hostname},
				"rules": []interface{}{
					// Rule 1: /proxy/<id> (no trailing slash) → redirect to /proxy/<id>/jobs/
					redirectRule(pathPrefix),
					// Rule 2: /proxy/<id>/ (trailing slash) → redirect to /proxy/<id>/jobs/
					redirectRule(pathPrefix + "/"),
					// Rule 3: /proxy/<id>/... → strip prefix, forward to driver service
					map[string]interface{}{
						"matches": []interface{}{
							map[string]interface{}{
								"path": map[string]interface{}{
									"type":  "PathPrefix",
									"value": pathPrefix,
								},
							},
						},
						"filters": []interface{}{
							map[string]interface{}{
								"type": "URLRewrite",
								"urlRewrite": map[string]interface{}{
									"path": map[string]interface{}{
										"type":               "ReplacePrefixMatch",
										"replacePrefixMatch": "/",
									},
								},
							},
						},
						"backendRefs": []interface{}{
							map[string]interface{}{
								"name": svcName,
								"port": int64(4040),
							},
						},
					},
				},
			},
		},
	}
}
