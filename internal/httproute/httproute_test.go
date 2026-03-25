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

// newScheme returns a minimal scheme that knows about HTTPRoute and HTTPRouteList.
func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	gvk := schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"}
	listGVK := schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRouteList"}
	s.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})
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
	cfg := config.HTTPRouteConfig{
		Hostname:         "spark.example.com",
		GatewayName:      "main-gateway",
		GatewayNamespace: "gateway-ns",
	}
	return httproute.New(client, namespace, cfg)
}

// routeExists checks whether an HTTPRoute with the given name exists.
func routeExists(t *testing.T, client *dynamicfake.FakeDynamicClient, name string) bool {
	t.Helper()
	_, err := client.Resource(httpRouteGVR).Namespace(namespace).Get(
		context.Background(), name, metav1.GetOptions{},
	)
	if err == nil {
		return true
	}
	return false
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

// getPathValue returns the path value from the first rule of an HTTPRoute.
func getPathValue(t *testing.T, route unstructured.Unstructured) string {
	t.Helper()
	rules, _, _ := unstructured.NestedSlice(route.Object, "spec", "rules")
	if len(rules) == 0 {
		t.Fatal("getPathValue: no rules found")
	}
	rule := rules[0].(map[string]interface{})
	matches, _, _ := unstructured.NestedSlice(rule, "matches")
	if len(matches) == 0 {
		t.Fatal("getPathValue: no matches found")
	}
	val, _, _ := unstructured.NestedString(matches[0].(map[string]interface{}), "path", "value")
	return val
}

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

// TestEnsureRouteHasCorrectPath verifies that the created HTTPRoute has a rule
// matching the expected /proxy/<appSelector> path.
func TestEnsureRouteHasCorrectPath(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-abc123", "my-spark-job"))

	routes := listRoutes(t, client)
	if len(routes) != 1 {
		t.Fatalf("expected 1 HTTPRoute, got %d", len(routes))
	}
	pathVal := getPathValue(t, routes[0])
	if pathVal != "/proxy/spark-abc123" {
		t.Errorf("expected path /proxy/spark-abc123, got %q", pathVal)
	}
}

// TestEnsureRouteHasRedirectRules verifies that the created HTTPRoute has
// exactly the three expected rules in the correct order:
//  1. Exact /proxy/<id>         → 302 redirect to /proxy/<id>/jobs/
//  2. Exact /proxy/<id>/        → 302 redirect to /proxy/<id>/jobs/
//  3. PathPrefix /proxy/<id>    → URLRewrite forward to driver service
func TestEnsureRouteHasRedirectRules(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-abc123", "my-spark-job"))

	routes := listRoutes(t, client)
	if len(routes) != 1 {
		t.Fatalf("expected 1 HTTPRoute, got %d", len(routes))
	}
	route := routes[0]
	rules, _, _ := unstructured.NestedSlice(route.Object, "spec", "rules")
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}

	// Helper: extract path type and value from rule at index i.
	rulePathTypeValue := func(i int) (string, string) {
		t.Helper()
		rule := rules[i].(map[string]interface{})
		matches, _, _ := unstructured.NestedSlice(rule, "matches")
		m := matches[0].(map[string]interface{})
		typ, _, _ := unstructured.NestedString(m, "path", "type")
		val, _, _ := unstructured.NestedString(m, "path", "value")
		return typ, val
	}
	// Helper: extract redirect target from rule at index i.
	ruleRedirectTarget := func(i int) string {
		t.Helper()
		rule := rules[i].(map[string]interface{})
		filters, _, _ := unstructured.NestedSlice(rule, "filters")
		f := filters[0].(map[string]interface{})
		target, _, _ := unstructured.NestedString(f, "requestRedirect", "path", "replaceFullPath")
		return target
	}

	// Rule 0: Exact /proxy/spark-abc123 → redirect to /proxy/spark-abc123/jobs/
	typ0, val0 := rulePathTypeValue(0)
	if typ0 != "Exact" || val0 != "/proxy/spark-abc123" {
		t.Errorf("rule 0: expected Exact /proxy/spark-abc123, got %s %s", typ0, val0)
	}
	if target := ruleRedirectTarget(0); target != "/proxy/spark-abc123/jobs/" {
		t.Errorf("rule 0: expected redirect to /proxy/spark-abc123/jobs/, got %q", target)
	}

	// Rule 1: Exact /proxy/spark-abc123/ → redirect to /proxy/spark-abc123/jobs/
	typ1, val1 := rulePathTypeValue(1)
	if typ1 != "Exact" || val1 != "/proxy/spark-abc123/" {
		t.Errorf("rule 1: expected Exact /proxy/spark-abc123/, got %s %s", typ1, val1)
	}
	if target := ruleRedirectTarget(1); target != "/proxy/spark-abc123/jobs/" {
		t.Errorf("rule 1: expected redirect to /proxy/spark-abc123/jobs/, got %q", target)
	}

	// Rule 2: PathPrefix /proxy/spark-abc123 → forward (no redirect filter)
	typ2, val2 := rulePathTypeValue(2)
	if typ2 != "PathPrefix" || val2 != "/proxy/spark-abc123" {
		t.Errorf("rule 2: expected PathPrefix /proxy/spark-abc123, got %s %s", typ2, val2)
	}
	rule2 := rules[2].(map[string]interface{})
	if _, ok := rule2["backendRefs"]; !ok {
		t.Error("rule 2: expected backendRefs to be present")
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

	routes := listRoutes(t, client)
	if len(routes) != 1 {
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

	routes := listRoutes(t, client)
	if len(routes) != 1 {
		t.Errorf("expected 1 HTTPRoute unchanged, got %d", len(routes))
	}
}

// TestReconcileRemovesStaleRoutes verifies that Reconcile deletes HTTPRoutes
// whose drivers are no longer active.
func TestReconcileRemovesStaleRoutes(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	// Simulate two routes left over from a previous instance.
	mgr.Ensure(ctx, newDriver("spark-old1", "job-old1"))
	mgr.Ensure(ctx, newDriver("spark-old2", "job-old2"))

	// Only spark-old2 is still running.
	active := []store.Driver{newDriver("spark-old2", "job-old2")}
	mgr.Reconcile(ctx, active)

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

	// spark-aaa already has a route; spark-bbb does not.
	mgr.Ensure(ctx, newDriver("spark-aaa", "job-a"))

	active := []store.Driver{
		newDriver("spark-aaa", "job-a"),
		newDriver("spark-bbb", "job-b"),
	}
	mgr.Reconcile(ctx, active)

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
	mgr.Reconcile(ctx, []store.Driver{d})

	for _, a := range client.Actions()[actionsBefore:] {
		if a.GetVerb() == "create" || a.GetVerb() == "delete" {
			t.Errorf("unexpected %s action during no-op reconcile", a.GetVerb())
		}
	}
}
