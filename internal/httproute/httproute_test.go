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

const sharedRouteName = "spark-ui-routes"

// newScheme returns a minimal scheme that knows about HTTPRoute and HTTPRouteList.
func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	gvk := schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"}
	listGVK := schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRouteList"}
	s.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})
	return s
}

func newDriver(appSelector, appName string) store.Driver {
	return store.Driver{
		PodName:     appSelector + "-pod",
		CreatedAt:   time.Now(),
		AppSelector: appSelector,
		AppName:     appName,
	}
}

func newManager(client *dynamicfake.FakeDynamicClient) *httproute.Manager {
	cfg := config.HTTPRouteConfig{
		Hostname:         "spark.example.com",
		GatewayName:      "main-gateway",
		GatewayNamespace: "gateway-ns",
	}
	return httproute.New(client, "default", cfg)
}

// getRules is a helper that retrieves spec.rules from the shared HTTPRoute.
func getRules(t *testing.T, client *dynamicfake.FakeDynamicClient) []interface{} {
	t.Helper()
	route, err := client.Resource(httpRouteGVR).Namespace("default").Get(
		context.Background(), sharedRouteName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("could not get shared HTTPRoute: %v", err)
	}
	rules, _, _ := unstructured.NestedSlice(route.Object, "spec", "rules")
	return rules
}

// routeExists reports whether the shared HTTPRoute is present.
func routeExists(t *testing.T, client *dynamicfake.FakeDynamicClient) bool {
	t.Helper()
	_, err := client.Resource(httpRouteGVR).Namespace("default").Get(
		context.Background(), sharedRouteName, metav1.GetOptions{},
	)
	if err == nil {
		return true
	}
	return false
}

// TestEnsureCreatesSharedRoute verifies that the first Ensure call creates the
// shared HTTPRoute with exactly one rule.
func TestEnsureCreatesSharedRoute(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-abc123", "my-spark-job"))

	if !routeExists(t, client) {
		t.Fatal("expected shared HTTPRoute to exist after first Ensure")
	}
	rules := getRules(t, client)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
}

// TestEnsureAddsRuleForSecondDriver verifies that a second distinct driver
// appends a second rule instead of creating a new HTTPRoute.
func TestEnsureAddsRuleForSecondDriver(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-aaa", "job-a"))
	mgr.Ensure(ctx, newDriver("spark-bbb", "job-b"))

	// Still only one HTTPRoute.
	routes, err := client.Resource(httpRouteGVR).Namespace("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(routes.Items) != 1 {
		t.Fatalf("expected 1 HTTPRoute object, got %d", len(routes.Items))
	}

	rules := getRules(t, client)
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
}

// TestEnsureIdempotent verifies that calling Ensure twice for the same driver
// does not duplicate the rule.
func TestEnsureIdempotent(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()
	d := newDriver("spark-abc123", "my-spark-job")

	mgr.Ensure(ctx, d)
	mgr.Ensure(ctx, d) // second call for same driver

	rules := getRules(t, client)
	if len(rules) != 1 {
		t.Errorf("expected 1 rule after idempotent Ensure, got %d", len(rules))
	}
}

// TestDeleteRemovesRuleButKeepsRoute verifies that deleting one of two drivers
// removes its rule while leaving the shared HTTPRoute and the other rule intact.
func TestDeleteRemovesRuleButKeepsRoute(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-aaa", "job-a"))
	mgr.Ensure(ctx, newDriver("spark-bbb", "job-b"))
	mgr.Delete(ctx, "spark-aaa")

	if !routeExists(t, client) {
		t.Fatal("expected shared HTTPRoute to still exist after partial delete")
	}
	rules := getRules(t, client)
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule remaining, got %d", len(rules))
	}
	// Confirm the surviving rule belongs to spark-bbb.
	rule := rules[0].(map[string]interface{})
	matches, _, _ := unstructured.NestedSlice(rule, "matches")
	pathVal, _, _ := unstructured.NestedString(matches[0].(map[string]interface{}), "path", "value")
	if pathVal != "/live/spark-bbb" {
		t.Errorf("expected remaining rule path /live/spark-bbb, got %s", pathVal)
	}
}

// TestDeleteLastRuleDeletesRoute verifies that deleting the last driver removes
// the shared HTTPRoute entirely.
func TestDeleteLastRuleDeletesRoute(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-abc123", "my-spark-job"))
	mgr.Delete(ctx, "spark-abc123")

	if routeExists(t, client) {
		t.Error("expected shared HTTPRoute to be deleted when last rule is removed")
	}
}

// TestDeleteNonExistentIsNoop verifies that deleting a driver that was never
// added is a safe no-op (no panic, no error logged that would fail the test).
func TestDeleteNonExistentIsNoop(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	// Should not panic or crash even though the route doesn't exist.
	mgr.Delete(ctx, "spark-unknown")
}

// TestDeleteNonExistentRuleFromExistingRouteIsNoop verifies that deleting a
// driver whose rule is not in the existing shared route is a safe no-op.
func TestDeleteNonExistentRuleFromExistingRouteIsNoop(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()

	mgr.Ensure(ctx, newDriver("spark-aaa", "job-a"))
	mgr.Delete(ctx, "spark-unknown") // rule not present

	if !routeExists(t, client) {
		t.Fatal("HTTPRoute should still exist; only one real driver was added")
	}
	rules := getRules(t, client)
	if len(rules) != 1 {
		t.Errorf("expected 1 rule still present, got %d", len(rules))
	}
}
