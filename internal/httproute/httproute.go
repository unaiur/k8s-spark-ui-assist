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
	"log"

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

// managedByLabel / managedByValue are the labels applied to every HTTPRoute
// created by this service so that listing and reconciliation can be scoped.
const (
	managedByLabel   = "app.kubernetes.io/managed-by"
	managedByValue   = "spark-ui-assist"
	driverPathPrefix = "/proxy/"
)

// ManagedBySelector returns the label selector string used to list all
// HTTPRoutes owned by this manager.
func ManagedBySelector() string {
	return managedByLabel + "=" + managedByValue
}

// Manager creates and deletes per-driver HTTPRoutes.
type Manager struct {
	ctx       context.Context
	client    dynamic.Interface
	namespace string
	cfg       config.HTTPRouteConfig
}

// New creates a new Manager.
func New(ctx context.Context, client dynamic.Interface, namespace string, cfg config.HTTPRouteConfig) *Manager {
	return &Manager{ctx: ctx, client: client, namespace: namespace, cfg: cfg}
}

// getRoute fetches the HTTPRoute with the given name.
func (m *Manager) getRoute(name string) (*unstructured.Unstructured, error) {
	return m.client.Resource(httpRouteGVR).Namespace(m.namespace).Get(m.ctx, name, metav1.GetOptions{})
}

// createDriverRoute creates an HTTPRoute for the given driver.
func (m *Manager) createDriverRoute(d store.Driver) error {
	route := buildRoute(d, m.cfg, m.namespace, driverPathPrefix)
	_, err := m.client.Resource(httpRouteGVR).Namespace(m.namespace).Create(m.ctx, route, metav1.CreateOptions{})
	return err
}

// deleteRoute deletes the HTTPRoute with the given name.
func (m *Manager) deleteRoute(name string) error {
	return m.client.Resource(httpRouteGVR).Namespace(m.namespace).Delete(m.ctx, name, metav1.DeleteOptions{})
}

// listRoutesWithLabelSelector returns all HTTPRoutes matching selector.
func (m *Manager) listRoutesWithLabelSelector(selector string) ([]unstructured.Unstructured, error) {
	list, err := m.client.Resource(httpRouteGVR).Namespace(m.namespace).List(m.ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// Ensure creates the per-driver HTTPRoute if it does not already exist.
// It is a no-op when the route is already present.
func (m *Manager) Ensure(ctx context.Context, d store.Driver) {
	name := d.RouteName()

	_, err := m.getRoute(name)
	if err == nil {
		return // already exists
	}
	if !errors.IsNotFound(err) {
		log.Printf("httproute: ensure %s: failed to check existence: %v", name, err)
		return
	}

	if err := m.createDriverRoute(d); err != nil {
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
	d := store.Driver{AppSelector: appSelector}
	name := d.RouteName()

	err := m.deleteRoute(name)
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
// Returns the first error encountered, if any; all routes are still attempted.
func (m *Manager) Reconcile(ctx context.Context, active []store.Driver) error {
	// Build a set of expected route names.
	wantedByName := make(map[string]store.Driver, len(active))
	for _, d := range active {
		wantedByName[d.RouteName()] = d
	}

	// List all HTTPRoutes owned by this manager.
	items, err := m.listRoutesWithLabelSelector(ManagedBySelector())
	if err != nil {
		log.Printf("httproute: reconcile: failed to list managed HTTPRoutes: %v", err)
		return err
	}

	var firstErr error
	presentNames := make(map[string]bool, len(items))
	for _, item := range items {
		name := item.GetName()
		presentNames[name] = true
		if _, ok := wantedByName[name]; !ok {
			log.Printf("httproute: reconcile: deleting stale HTTPRoute %s", name)
			if delErr := m.deleteRoute(name); delErr != nil && !errors.IsNotFound(delErr) {
				log.Printf("httproute: reconcile: failed to delete %s: %v", name, delErr)
				if firstErr == nil {
					firstErr = delErr
				}
			}
		}
	}

	for name, d := range wantedByName {
		if presentNames[name] {
			continue
		}
		log.Printf("httproute: reconcile: creating missing HTTPRoute %s", name)
		if createErr := m.createDriverRoute(d); createErr != nil && !errors.IsAlreadyExists(createErr) {
			log.Printf("httproute: reconcile: failed to create %s: %v", name, createErr)
			if firstErr == nil {
				firstErr = createErr
			}
		}
	}
	return firstErr
}

// ---- HTTPRoute build helper --------------------------------------------------

func buildRoute(d store.Driver, cfg config.HTTPRouteConfig, namespace string, prefix string) *unstructured.Unstructured {
	pathPrefix := prefix + d.AppSelector
	jobsRedirectTarget := pathPrefix + "/jobs/"
	svcName := d.AppName + "-ui-svc"
	name := d.RouteName()

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
					redirectRule(pathPrefix),
					redirectRule(pathPrefix + "/"),
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
