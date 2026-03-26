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
	k8ssvc "github.com/unaiur/k8s-spark-ui-assist/internal/k8s"
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
	cfg := config.HTTPRouteConfig{
		Hostname:         "spark.example.com",
		GatewayName:      "main-gateway",
		GatewayNamespace: "gateway-ns",
	}
	svc := k8ssvc.New(context.Background(), client, namespace)
	return httproute.New(svc, cfg)
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

	mgr.Reconcile(ctx, []store.Driver{newDriver("spark-old2", "job-old2")})

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

	mgr.Reconcile(ctx, []store.Driver{
		newDriver("spark-aaa", "job-a"),
		newDriver("spark-bbb", "job-b"),
	})

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
