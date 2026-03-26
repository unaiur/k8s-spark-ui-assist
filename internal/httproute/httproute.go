// Package httproute manages per-driver Gateway API HTTPRoutes and the optional
// Spark History Server (SHS) root HTTPRoute.
//
// # Per-driver routes
//
// For each active Spark driver the Manager creates a dedicated HTTPRoute named
// "<appSelector>-ui-route" under the /proxy/<appSelector> path prefix. When
// the driver stops the route is deleted. The Manager uses the label
// "app.kubernetes.io/managed-by: spark-ui-assist" so that Reconcile can list
// all routes it owns and clean up any that belong to drivers that are no longer
// active.
//
// # SHS root route
//
// When cfg.SHSService is set the Manager also manages a single HTTPRoute named
// "<selfService>-root-route" for the "/" path:
//
//   - EnsureSHSRoute: creates (or replaces) the root route pointing to the SHS
//     service. This is called when the SHS Endpoints transitions to ≥1 ready
//     address.
//   - EnsureFallbackRootRoute: creates (or replaces) the root route pointing to
//     this service itself (cfg.SelfService). This is called when SHS goes down so
//     that "/" still works and shows the dashboard.
//   - DeleteRootRoute: deletes the root route entirely. Called on shutdown or
//     when SHS integration is disabled.
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

// Manager creates and deletes per-driver HTTPRoutes and the SHS root route.
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

// rootRouteName returns the name of the managed root HTTPRoute.
// It is derived from the self-service name so it is stable across restarts.
func (m *Manager) rootRouteName() string {
	return m.cfg.SelfService + "-root-route"
}

// getRoute fetches the HTTPRoute with the given name.
func (m *Manager) getRoute(name string) (*unstructured.Unstructured, error) {
	return m.client.Resource(httpRouteGVR).Namespace(m.namespace).Get(m.ctx, name, metav1.GetOptions{})
}

// createDriverRoute creates an HTTPRoute for the given driver.
func (m *Manager) createDriverRoute(d store.Driver) error {
	route := buildDriverRoute(d, m.cfg, m.namespace)
	_, err := m.client.Resource(httpRouteGVR).Namespace(m.namespace).Create(m.ctx, route, metav1.CreateOptions{})
	return err
}

// applyRoute creates or replaces the HTTPRoute (delete-then-create semantics to
// handle spec changes without needing strategic-merge-patch on an unstructured
// object).
func (m *Manager) applyRoute(route *unstructured.Unstructured) error {
	name := route.GetName()
	_, getErr := m.getRoute(name)
	if getErr == nil {
		// Already exists — delete first so we can recreate with the new spec.
		if delErr := m.deleteRoute(name); delErr != nil && !errors.IsNotFound(delErr) {
			return delErr
		}
	} else if !errors.IsNotFound(getErr) {
		return getErr
	}
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

// EnsureSHSRoute creates (or replaces) the root HTTPRoute pointing "/" to the
// Spark History Server. Call this when SHS transitions to having ≥1 ready pod.
// No-op when SHSService is not configured.
func (m *Manager) EnsureSHSRoute(ctx context.Context) {
	if m.cfg.SHSService == "" {
		return
	}
	name := m.rootRouteName()
	route := buildRootRoute(name, m.cfg, m.namespace, m.cfg.SHSService, 80)
	if err := m.applyRoute(route); err != nil {
		log.Printf("httproute: EnsureSHSRoute %s: %v", name, err)
		return
	}
	log.Printf("httproute: applied SHS root HTTPRoute %s → service %s", name, m.cfg.SHSService)
}

// EnsureFallbackRootRoute creates (or replaces) the root HTTPRoute pointing "/"
// to this service itself. Call this when SHS transitions to having 0 ready pods
// so the dashboard remains accessible at "/". No-op when SHSService is not
// configured (the static Helm chart HTTPRoute already handles "/" in that case).
func (m *Manager) EnsureFallbackRootRoute(ctx context.Context) {
	if m.cfg.SHSService == "" || m.cfg.SelfService == "" {
		return
	}
	name := m.rootRouteName()
	route := buildRootRoute(name, m.cfg, m.namespace, m.cfg.SelfService, 80)
	if err := m.applyRoute(route); err != nil {
		log.Printf("httproute: EnsureFallbackRootRoute %s: %v", name, err)
		return
	}
	log.Printf("httproute: applied fallback root HTTPRoute %s → service %s", name, m.cfg.SelfService)
}

// DeleteRootRoute deletes the managed root HTTPRoute. It is a no-op when the
// route does not exist or SHSService is not configured.
func (m *Manager) DeleteRootRoute(ctx context.Context) {
	if m.cfg.SHSService == "" {
		return
	}
	name := m.rootRouteName()
	err := m.deleteRoute(name)
	if err == nil {
		log.Printf("httproute: deleted root HTTPRoute %s", name)
		return
	}
	if errors.IsNotFound(err) {
		return
	}
	log.Printf("httproute: DeleteRootRoute %s: %v", name, err)
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

	// The root route (if any) is managed separately; exclude it from driver reconciliation.
	rootName := ""
	if m.cfg.SHSService != "" {
		rootName = m.rootRouteName()
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
		if name == rootName {
			presentNames[name] = true
			continue // root route lifecycle managed by SHS watcher, not here
		}
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

// ---- HTTPRoute build helpers -------------------------------------------------

func buildDriverRoute(d store.Driver, cfg config.HTTPRouteConfig, namespace string) *unstructured.Unstructured {
	pathPrefix := driverPathPrefix + d.AppSelector
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

// buildRootRoute builds an HTTPRoute that forwards all traffic at "/" to the
// given backend service on the given port.
func buildRootRoute(name string, cfg config.HTTPRouteConfig, namespace, backendService string, backendPort int64) *unstructured.Unstructured {
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
					map[string]interface{}{
						"matches": []interface{}{
							map[string]interface{}{
								"path": map[string]interface{}{
									"type":  "PathPrefix",
									"value": "/",
								},
							},
						},
						"backendRefs": []interface{}{
							map[string]interface{}{
								"name": backendService,
								"port": backendPort,
							},
						},
					},
				},
			},
		},
	}
}
