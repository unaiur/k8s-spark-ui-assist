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

const (
	routeName   = "my-release-spark-ui-assist"
	dashSvcName = "my-release-spark-ui-assist"
	namespace   = "default"
)

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
		RouteName:        routeName,
		Hostname:         "spark.example.com",
		GatewayName:      "main-gateway",
		GatewayNamespace: "gateway-ns",
	}
	return httproute.New(client, namespace, cfg)
}

// catchAllRule mimics the static dashboard rule created by the Helm chart.
func catchAllRule() interface{} {
	return map[string]interface{}{
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
				"name": dashSvcName,
				"port": int64(80),
			},
		},
	}
}

// preCreateRoute simulates the Helm chart by creating the shared HTTPRoute with
// just the catch-all dashboard rule already present.
func preCreateRoute(t *testing.T, client *dynamicfake.FakeDynamicClient) {
	t.Helper()
	route := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "HTTPRoute",
			"metadata": map[string]interface{}{
				"name":      routeName,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"rules": []interface{}{catchAllRule()},
			},
		},
	}
	_, err := client.Resource(httpRouteGVR).Namespace(namespace).Create(
		context.Background(), route, metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("preCreateRoute: %v", err)
	}
}

// getRules retrieves spec.rules from the shared HTTPRoute.
func getRules(t *testing.T, client *dynamicfake.FakeDynamicClient) []interface{} {
	t.Helper()
	route, err := client.Resource(httpRouteGVR).Namespace(namespace).Get(
		context.Background(), routeName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("getRules: could not get HTTPRoute: %v", err)
	}
	rules, found, err := unstructured.NestedSlice(route.Object, "spec", "rules")
	if err != nil {
		t.Fatalf("getRules: spec.rules has unexpected type: %v", err)
	}
	if !found {
		t.Fatalf("getRules: spec.rules not found on HTTPRoute")
	}
	return rules
}

// driverRules returns only the driver (non-catch-all) rules.
func driverRules(rules []interface{}) []interface{} {
	var out []interface{}
	for _, r := range rules {
		rm, ok := r.(map[string]interface{})
		if !ok {
			continue
		}
		matches, _, _ := unstructured.NestedSlice(rm, "matches")
		for _, m := range matches {
			mMap, ok := m.(map[string]interface{})
			if !ok {
				continue
			}
			val, _, _ := unstructured.NestedString(mMap, "path", "value")
			if len(val) > 6 && val[:6] == "/live/" {
				out = append(out, r)
			}
		}
	}
	return out
}

// TestEnsureAddsDriverRule verifies that Ensure adds exactly one driver rule
// before the catch-all rule.
func TestEnsureAddsDriverRule(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	preCreateRoute(t, client)
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-abc123", "my-spark-job"))

	rules := getRules(t, client)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules (1 driver + catch-all), got %d", len(rules))
	}
	// Driver rule must come before the catch-all.
	first := rules[0].(map[string]interface{})
	matches, _, _ := unstructured.NestedSlice(first, "matches")
	pathVal, _, _ := unstructured.NestedString(matches[0].(map[string]interface{}), "path", "value")
	if pathVal != "/live/spark-abc123" {
		t.Errorf("expected driver rule first, got path %q", pathVal)
	}
}

// TestEnsureAddsRuleForSecondDriver verifies two drivers produce two driver
// rules, still with the catch-all last.
func TestEnsureAddsRuleForSecondDriver(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	preCreateRoute(t, client)
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-aaa", "job-a"))
	mgr.Ensure(ctx, newDriver("spark-bbb", "job-b"))

	rules := getRules(t, client)
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules (2 drivers + catch-all), got %d", len(rules))
	}
	// Last rule must still be catch-all.
	last := rules[len(rules)-1].(map[string]interface{})
	matches, _, _ := unstructured.NestedSlice(last, "matches")
	pathVal, _, _ := unstructured.NestedString(matches[0].(map[string]interface{}), "path", "value")
	if pathVal != "/" {
		t.Errorf("expected catch-all rule last, got path %q", pathVal)
	}
}

// TestEnsureIdempotent verifies that calling Ensure twice for the same driver
// does not duplicate the rule.
func TestEnsureIdempotent(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	preCreateRoute(t, client)
	mgr := newManager(client)
	ctx := context.Background()
	d := newDriver("spark-abc123", "my-spark-job")

	mgr.Ensure(ctx, d)
	mgr.Ensure(ctx, d)

	rules := getRules(t, client)
	if len(rules) != 2 {
		t.Errorf("expected 2 rules after idempotent Ensure, got %d", len(rules))
	}
}

// TestDeleteRemovesDriverRule verifies that Delete removes the driver rule but
// leaves the catch-all rule intact.
func TestDeleteRemovesDriverRule(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	preCreateRoute(t, client)
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-aaa", "job-a"))
	mgr.Ensure(ctx, newDriver("spark-bbb", "job-b"))
	mgr.Delete(ctx, "spark-aaa")

	rules := getRules(t, client)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules (1 driver + catch-all) after delete, got %d", len(rules))
	}
	dr := driverRules(rules)
	if len(dr) != 1 {
		t.Fatalf("expected 1 driver rule remaining, got %d", len(dr))
	}
	rm := dr[0].(map[string]interface{})
	matches, _, _ := unstructured.NestedSlice(rm, "matches")
	pathVal, _, _ := unstructured.NestedString(matches[0].(map[string]interface{}), "path", "value")
	if pathVal != "/live/spark-bbb" {
		t.Errorf("expected surviving rule for spark-bbb, got %q", pathVal)
	}
}

// TestDeleteLastDriverRuleKeepsCatchAll verifies that removing the last driver
// does NOT delete the HTTPRoute — the catch-all rule must survive.
func TestDeleteLastDriverRuleKeepsCatchAll(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	preCreateRoute(t, client)
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-abc123", "my-spark-job"))
	mgr.Delete(ctx, "spark-abc123")

	rules := getRules(t, client) // will fail if route was deleted
	if len(rules) != 1 {
		t.Fatalf("expected only the catch-all rule remaining, got %d rules", len(rules))
	}
	rm := rules[0].(map[string]interface{})
	matches, _, _ := unstructured.NestedSlice(rm, "matches")
	pathVal, _, _ := unstructured.NestedString(matches[0].(map[string]interface{}), "path", "value")
	if pathVal != "/" {
		t.Errorf("expected catch-all rule, got path %q", pathVal)
	}
}

// TestDeleteNonExistentRuleIsNoop verifies that deleting a rule that is not
// present is safe.
func TestDeleteNonExistentRuleIsNoop(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	preCreateRoute(t, client)
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-aaa", "job-a"))
	mgr.Delete(ctx, "spark-unknown")

	rules := getRules(t, client)
	if len(rules) != 2 {
		t.Errorf("expected 2 rules unchanged, got %d", len(rules))
	}
}

// TestReconcileRemovesStaleRules verifies that Reconcile removes driver rules
// whose drivers are no longer active.
func TestReconcileRemovesStaleRules(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	preCreateRoute(t, client)
	mgr := newManager(client)
	ctx := context.Background()

	// Simulate two rules left over from a previous instance.
	mgr.Ensure(ctx, newDriver("spark-old1", "job-old1"))
	mgr.Ensure(ctx, newDriver("spark-old2", "job-old2"))

	// Only spark-old2 is still running.
	active := []store.Driver{newDriver("spark-old2", "job-old2")}
	mgr.Reconcile(ctx, active)

	rules := getRules(t, client)
	dr := driverRules(rules)
	if len(dr) != 1 {
		t.Fatalf("expected 1 driver rule after reconcile, got %d", len(dr))
	}
	rm := dr[0].(map[string]interface{})
	matches, _, _ := unstructured.NestedSlice(rm, "matches")
	pathVal, _, _ := unstructured.NestedString(matches[0].(map[string]interface{}), "path", "value")
	if pathVal != "/live/spark-old2" {
		t.Errorf("expected rule for spark-old2 to survive, got %q", pathVal)
	}
}

// TestReconcileAddsMissingRules verifies that Reconcile adds rules for active
// drivers that don't yet have a rule.
func TestReconcileAddsMissingRules(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	preCreateRoute(t, client)
	mgr := newManager(client)
	ctx := context.Background()

	// spark-aaa already has a rule; spark-bbb does not.
	mgr.Ensure(ctx, newDriver("spark-aaa", "job-a"))

	active := []store.Driver{
		newDriver("spark-aaa", "job-a"),
		newDriver("spark-bbb", "job-b"),
	}
	mgr.Reconcile(ctx, active)

	rules := getRules(t, client)
	dr := driverRules(rules)
	if len(dr) != 2 {
		t.Fatalf("expected 2 driver rules after reconcile, got %d", len(dr))
	}
}

// TestReconcileNoopWhenUpToDate verifies that Reconcile does not update the
// route when nothing needs changing.
func TestReconcileNoopWhenUpToDate(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	preCreateRoute(t, client)
	mgr := newManager(client)
	ctx := context.Background()

	d := newDriver("spark-aaa", "job-a")
	mgr.Ensure(ctx, d)

	// Count update calls before reconcile.
	actionsBefore := len(client.Actions())
	mgr.Reconcile(ctx, []store.Driver{d})

	actionsAfter := len(client.Actions())
	// Reconcile should have issued a Get but no Update.
	for _, a := range client.Actions()[actionsBefore:] {
		if a.GetVerb() == "update" {
			t.Errorf("unexpected update action during no-op reconcile: %v", a)
		}
	}
	_ = actionsAfter
}
