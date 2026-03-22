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

// newScheme returns a minimal scheme that knows about HTTPRoute and HTTPRouteList.
func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	gvk := schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"}
	listGVK := schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRouteList"}
	s.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})
	return s
}

func newDriver() store.Driver {
	return store.Driver{
		PodName:     "my-driver-pod",
		CreatedAt:   time.Now(),
		AppSelector: "spark-abc123",
		AppName:     "my-spark-job",
	}
}

func newManager(client *dynamicfake.FakeDynamicClient) *httproute.Manager {
	cfg := config.HTTPRouteConfig{
		Enabled:          true,
		Hostname:         "spark.example.com",
		GatewayName:      "main-gateway",
		GatewayNamespace: "gateway-ns",
	}
	return httproute.New(client, "default", cfg)
}

func TestEnsureCreatesRoute(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()
	d := newDriver()

	mgr.Ensure(ctx, d)

	route, err := client.Resource(httpRouteGVR).Namespace("default").Get(ctx, d.AppSelector+"-ui-route", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected HTTPRoute to exist: %v", err)
	}
	if route.GetName() != d.AppSelector+"-ui-route" {
		t.Errorf("unexpected route name: %s", route.GetName())
	}
}

func TestEnsureIdempotent(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()
	d := newDriver()

	mgr.Ensure(ctx, d)
	// Second call should not error or create a duplicate.
	mgr.Ensure(ctx, d)

	routes, err := client.Resource(httpRouteGVR).Namespace("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(routes.Items) != 1 {
		t.Errorf("expected 1 route, got %d", len(routes.Items))
	}
}

func TestDeleteRoute(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	mgr := newManager(client)
	ctx := context.Background()
	d := newDriver()

	mgr.Ensure(ctx, d)
	mgr.Delete(ctx, d.AppSelector)

	routes, err := client.Resource(httpRouteGVR).Namespace("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(routes.Items) != 0 {
		t.Errorf("expected 0 routes after delete, got %d", len(routes.Items))
	}
}
