package httproute_test

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/unaiur/k8s-spark-ui-assist/internal/config"
	"github.com/unaiur/k8s-spark-ui-assist/internal/httproute"
	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

var httpRouteGVR = schema.GroupVersionResource{
	Group:    "gateway.networking.k8s.io",
	Version:  "v1",
	Resource: "httproutes",
}

const namespace = "default"

// testCfg is a minimal HTTPRouteConfig used in route tests.
var testCfg = config.HTTPRouteConfig{
	Hostname:         "spark.example.com",
	GatewayName:      "main-gateway",
	GatewayNamespace: "gateway-ns",
}

// newScheme returns a minimal scheme that knows about HTTPRoute and HTTPRouteList.
func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRouteList"}, &unstructured.UnstructuredList{})
	return s
}

// newDriver is a test helper for building a store.Driver.
func newDriver(appSelector, appName string) store.Driver {
	return store.Driver{
		PodName:     appSelector + "-pod",
		CreatedAt:   time.Now(),
		AppSelector: appSelector,
		AppName:     appName,
	}
}

// newManager creates a Manager wired to the fake client.
func newManager(client *dynamicfake.FakeDynamicClient) *httproute.Manager {
	return httproute.New(context.Background(), client, namespace, testCfg)
}

// routeExists checks whether an HTTPRoute with the given name exists.
func routeExists(t *testing.T, client *dynamicfake.FakeDynamicClient, name string) bool {
	t.Helper()
	_, err := client.Resource(httpRouteGVR).Namespace(namespace).Get(
		context.Background(), name, metav1.GetOptions{},
	)
	return err == nil
}

// listRoutes returns all HTTPRoutes in the namespace.
func listRoutes(t *testing.T, client *dynamicfake.FakeDynamicClient) []unstructured.Unstructured {
	t.Helper()
	list, err := client.Resource(httpRouteGVR).Namespace(namespace).List(
		context.Background(), metav1.ListOptions{},
	)
	if err != nil {
		t.Fatalf("listRoutes: %v", err)
	}
	return list.Items
}

// ---- Manager high-level behaviour tests -------------------------------------

// TestEnsureCreatesRoute verifies that Ensure creates an HTTPRoute for a driver.
func TestEnsureCreatesRoute(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-abc123", "my-spark-job"))

	if !routeExists(t, client, "spark-abc123-ui-route") {
		t.Fatal("expected HTTPRoute spark-abc123-ui-route to exist")
	}
}

// TestEnsureIdempotent verifies that calling Ensure twice for the same driver
// does not create a duplicate route.
func TestEnsureIdempotent(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()
	d := newDriver("spark-abc123", "my-spark-job")

	mgr.Ensure(ctx, d)
	mgr.Ensure(ctx, d)

	if routes := listRoutes(t, client); len(routes) != 1 {
		t.Errorf("expected 1 HTTPRoute after idempotent Ensure, got %d", len(routes))
	}
}

// TestEnsureMultipleDrivers verifies that two drivers produce two separate HTTPRoutes.
func TestEnsureMultipleDrivers(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-aaa", "job-a"))
	mgr.Ensure(ctx, newDriver("spark-bbb", "job-b"))

	routes := listRoutes(t, client)
	if len(routes) != 2 {
		t.Fatalf("expected 2 HTTPRoutes, got %d", len(routes))
	}
	if !routeExists(t, client, "spark-aaa-ui-route") {
		t.Error("expected HTTPRoute spark-aaa-ui-route to exist")
	}
	if !routeExists(t, client, "spark-bbb-ui-route") {
		t.Error("expected HTTPRoute spark-bbb-ui-route to exist")
	}
}

// TestDeleteRoute verifies that Delete removes the driver's HTTPRoute.
func TestDeleteRoute(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-aaa", "job-a"))
	mgr.Ensure(ctx, newDriver("spark-bbb", "job-b"))
	mgr.Delete(ctx, "spark-aaa")

	routes := listRoutes(t, client)
	if len(routes) != 1 {
		t.Fatalf("expected 1 HTTPRoute after delete, got %d", len(routes))
	}
	if routes[0].GetName() != "spark-bbb-ui-route" {
		t.Errorf("expected spark-bbb-ui-route to survive, got %q", routes[0].GetName())
	}
}

// TestDeleteNonExistentRouteIsNoop verifies that deleting a route that is not
// present is safe.
func TestDeleteNonExistentRouteIsNoop(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-aaa", "job-a"))
	mgr.Delete(ctx, "spark-unknown") // should not panic or error

	if routes := listRoutes(t, client); len(routes) != 1 {
		t.Errorf("expected 1 HTTPRoute unchanged, got %d", len(routes))
	}
}

// TestReconcileRemovesStaleRoutes verifies that Reconcile deletes HTTPRoutes
// whose drivers are no longer active.
func TestReconcileRemovesStaleRoutes(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-old1", "job-old1"))
	mgr.Ensure(ctx, newDriver("spark-old2", "job-old2"))

	if err := mgr.Reconcile(ctx, []store.Driver{newDriver("spark-old2", "job-old2")}); err != nil {
		t.Fatalf("Reconcile returned unexpected error: %v", err)
	}

	routes := listRoutes(t, client)
	if len(routes) != 1 {
		t.Fatalf("expected 1 HTTPRoute after reconcile, got %d", len(routes))
	}
	if routes[0].GetName() != "spark-old2-ui-route" {
		t.Errorf("expected spark-old2-ui-route to survive, got %q", routes[0].GetName())
	}
}

// TestReconcileCreatesMissingRoutes verifies that Reconcile creates HTTPRoutes
// for active drivers that don't yet have a route.
func TestReconcileCreatesMissingRoutes(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-aaa", "job-a"))

	if err := mgr.Reconcile(ctx, []store.Driver{
		newDriver("spark-aaa", "job-a"),
		newDriver("spark-bbb", "job-b"),
	}); err != nil {
		t.Fatalf("Reconcile returned unexpected error: %v", err)
	}

	if !routeExists(t, client, "spark-aaa-ui-route") {
		t.Error("expected spark-aaa-ui-route to still exist")
	}
	if !routeExists(t, client, "spark-bbb-ui-route") {
		t.Error("expected spark-bbb-ui-route to be created by Reconcile")
	}
}

// TestReconcileNoopWhenUpToDate verifies that Reconcile does not perform
// create or delete calls when nothing needs changing.
func TestReconcileNoopWhenUpToDate(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	d := newDriver("spark-aaa", "job-a")
	mgr.Ensure(ctx, d)

	actionsBefore := len(client.Actions())
	if err := mgr.Reconcile(ctx, []store.Driver{d}); err != nil {
		t.Fatalf("Reconcile returned unexpected error: %v", err)
	}

	for _, a := range client.Actions()[actionsBefore:] {
		if a.GetVerb() == "create" || a.GetVerb() == "delete" {
			t.Errorf("unexpected %s action during no-op reconcile", a.GetVerb())
		}
	}
}

// ---- Route CRUD unit tests (previously in k8s_svc_test.go) ------------------

func TestCreateAndGetRoute(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()
	d := newDriver("spark-abc", "my-job")

	mgr.Ensure(ctx, d)

	if !routeExists(t, client, d.RouteName()) {
		t.Fatalf("expected route %q to exist after Ensure", d.RouteName())
	}
}

func TestGetRouteNotFound(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	_ = newManager(client)

	_, err := client.Resource(httpRouteGVR).Namespace(namespace).Get(
		context.Background(), "does-not-exist-ui-route", metav1.GetOptions{},
	)
	if err == nil {
		t.Fatal("expected error for missing route, got nil")
	}
}

func TestDeleteRouteUnit(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()
	d := newDriver("spark-abc", "my-job")

	mgr.Ensure(ctx, d)
	mgr.Delete(ctx, d.AppSelector)

	if routeExists(t, client, d.RouteName()) {
		t.Fatal("expected route to be gone after Delete")
	}
}

func TestDeleteRouteNotFound(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	// Delete on a non-existent route should be a no-op (Manager swallows NotFound).
	mgr.Delete(ctx, "ghost")
	// No panic or error expected; simply verify no routes were created.
	if routes := listRoutes(t, client); len(routes) != 0 {
		t.Errorf("expected 0 routes, got %d", len(routes))
	}
}

func TestListRoutesWithLabelSelector(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-aaa", "job-a"))
	mgr.Ensure(ctx, newDriver("spark-bbb", "job-b"))

	routes := listRoutes(t, client)
	if len(routes) != 2 {
		t.Errorf("expected 2 routes, got %d", len(routes))
	}
}

func TestListRoutesEmptyResult(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	_ = newManager(client)

	routes := listRoutes(t, client)
	if len(routes) != 0 {
		t.Errorf("expected 0 routes, got %d", len(routes))
	}
}

// testCfgSHS is an HTTPRouteConfig with SHS integration enabled.
var testCfgSHS = config.HTTPRouteConfig{
	Hostname:         "spark.example.com",
	GatewayName:      "main-gateway",
	GatewayNamespace: "gateway-ns",
	SelfService:      "spark-ui-assist",
	SHSService:       "spark-history-server",
}

// newManagerSHS creates a Manager with SHS config wired to the fake client.
func newManagerSHS(client *dynamicfake.FakeDynamicClient) *httproute.Manager {
	return httproute.New(context.Background(), client, namespace, testCfgSHS)
}

// ---- SHS root route tests ---------------------------------------------------

// TestEnsureSHSRouteCreatesSHSRoute verifies that EnsureSHSRoute creates a root
// HTTPRoute pointing to the SHS service.
func TestEnsureSHSRouteCreatesSHSRoute(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManagerSHS(client)
	ctx := context.Background()

	mgr.EnsureSHSRoute(ctx)

	routeName := "spark-ui-assist-root-route"
	if !routeExists(t, client, routeName) {
		t.Fatalf("expected HTTPRoute %s to exist after EnsureSHSRoute", routeName)
	}

	route, _ := client.Resource(httpRouteGVR).Namespace(namespace).Get(ctx, routeName, metav1.GetOptions{})
	rules, _, _ := unstructured.NestedSlice(route.Object, "spec", "rules")
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule in SHS root route, got %d", len(rules))
	}
	backends, _, _ := unstructured.NestedSlice(rules[0].(map[string]interface{}), "backendRefs")
	backendName, _, _ := unstructured.NestedString(backends[0].(map[string]interface{}), "name")
	if backendName != testCfgSHS.SHSService {
		t.Errorf("expected backend %q, got %q", testCfgSHS.SHSService, backendName)
	}
}

// TestEnsureFallbackRootRouteCreatesFallback verifies that EnsureFallbackRootRoute
// creates a root HTTPRoute pointing to the self service.
func TestEnsureFallbackRootRouteCreatesFallback(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManagerSHS(client)
	ctx := context.Background()

	mgr.EnsureFallbackRootRoute(ctx)

	routeName := "spark-ui-assist-root-route"
	if !routeExists(t, client, routeName) {
		t.Fatalf("expected HTTPRoute %s to exist after EnsureFallbackRootRoute", routeName)
	}

	route, _ := client.Resource(httpRouteGVR).Namespace(namespace).Get(ctx, routeName, metav1.GetOptions{})
	rules, _, _ := unstructured.NestedSlice(route.Object, "spec", "rules")
	backends, _, _ := unstructured.NestedSlice(rules[0].(map[string]interface{}), "backendRefs")
	backendName, _, _ := unstructured.NestedString(backends[0].(map[string]interface{}), "name")
	if backendName != testCfgSHS.SelfService {
		t.Errorf("expected backend %q, got %q", testCfgSHS.SelfService, backendName)
	}
}

// TestSHSRouteTransitionSHSToFallback verifies that switching from the SHS route
// to the fallback replaces the route rather than leaving both.
func TestSHSRouteTransitionSHSToFallback(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManagerSHS(client)
	ctx := context.Background()

	mgr.EnsureSHSRoute(ctx)
	mgr.EnsureFallbackRootRoute(ctx) // SHS went down

	routes := listRoutes(t, client)
	if len(routes) != 1 {
		t.Fatalf("expected exactly 1 root route after transition, got %d", len(routes))
	}
	route := routes[0]
	rules, _, _ := unstructured.NestedSlice(route.Object, "spec", "rules")
	backends, _, _ := unstructured.NestedSlice(rules[0].(map[string]interface{}), "backendRefs")
	backendName, _, _ := unstructured.NestedString(backends[0].(map[string]interface{}), "name")
	if backendName != testCfgSHS.SelfService {
		t.Errorf("after transition to fallback, expected backend %q, got %q", testCfgSHS.SelfService, backendName)
	}
}

// TestDeleteRootRouteRemovesRoute verifies that DeleteRootRoute removes the
// managed root HTTPRoute.
func TestDeleteRootRouteRemovesRoute(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManagerSHS(client)
	ctx := context.Background()

	mgr.EnsureSHSRoute(ctx)
	mgr.DeleteRootRoute(ctx)

	if routeExists(t, client, "spark-ui-assist-root-route") {
		t.Fatal("expected root route to be deleted")
	}
}

// TestEnsureSHSRouteNoopWhenNotConfigured verifies that EnsureSHSRoute is a
// no-op when SHSService is not set.
func TestEnsureSHSRouteNoopWhenNotConfigured(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client) // uses testCfg with no SHSService
	ctx := context.Background()

	mgr.EnsureSHSRoute(ctx)

	if routes := listRoutes(t, client); len(routes) != 0 {
		t.Errorf("expected 0 routes, got %d", len(routes))
	}
}

// TestReconcileDoesNotDeleteRootRoute verifies that Reconcile leaves the managed
// root route untouched (it is lifecycle-managed by the SHS watcher, not Reconcile).
func TestReconcileDoesNotDeleteRootRoute(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManagerSHS(client)
	ctx := context.Background()

	mgr.EnsureSHSRoute(ctx)

	// Reconcile with empty active list — should NOT delete the root route.
	if err := mgr.Reconcile(ctx, nil); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if !routeExists(t, client, "spark-ui-assist-root-route") {
		t.Fatal("Reconcile must not delete the managed root route")
	}
}

// contains the expected path prefix, hostname, gateway ref, and backend.
func TestCreateRouteHasCorrectStructure(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()
	d := newDriver("spark-abc123", "my-spark-job")

	mgr.Ensure(ctx, d)

	route, err := client.Resource(httpRouteGVR).Namespace(namespace).Get(
		ctx, d.RouteName(), metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("Get route: %v", err)
	}

	// Hostname
	hostnames, _, _ := unstructured.NestedStringSlice(route.Object, "spec", "hostnames")
	if len(hostnames) == 0 || hostnames[0] != testCfg.Hostname {
		t.Errorf("expected hostname %q, got %v", testCfg.Hostname, hostnames)
	}

	// Gateway parentRef name
	parentRefs, _, _ := unstructured.NestedSlice(route.Object, "spec", "parentRefs")
	if len(parentRefs) == 0 {
		t.Fatal("expected parentRefs, got none")
	}
	gwName, _, _ := unstructured.NestedString(parentRefs[0].(map[string]interface{}), "name")
	if gwName != testCfg.GatewayName {
		t.Errorf("expected gateway %q, got %q", testCfg.GatewayName, gwName)
	}

	// Three rules
	rules, _, _ := unstructured.NestedSlice(route.Object, "spec", "rules")
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}

	// Rule 0: Exact /proxy/spark-abc123
	rule0 := rules[0].(map[string]interface{})
	matches0, _, _ := unstructured.NestedSlice(rule0, "matches")
	pathType0, _, _ := unstructured.NestedString(matches0[0].(map[string]interface{}), "path", "type")
	pathVal0, _, _ := unstructured.NestedString(matches0[0].(map[string]interface{}), "path", "value")
	if pathType0 != "Exact" || pathVal0 != "/proxy/spark-abc123" {
		t.Errorf("rule 0: expected Exact /proxy/spark-abc123, got %s %s", pathType0, pathVal0)
	}

	// Rule 2: PathPrefix /proxy/spark-abc123 with backendRef to <appName>-ui-svc
	rule2 := rules[2].(map[string]interface{})
	backends, _, _ := unstructured.NestedSlice(rule2, "backendRefs")
	if len(backends) == 0 {
		t.Fatal("rule 2: expected backendRefs")
	}
	backendName, _, _ := unstructured.NestedString(backends[0].(map[string]interface{}), "name")
	if backendName != "my-spark-job-ui-svc" {
		t.Errorf("rule 2: expected backend my-spark-job-ui-svc, got %q", backendName)
	}
}
