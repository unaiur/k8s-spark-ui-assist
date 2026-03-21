package httproute_test

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakegw "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"

	"github.com/unaiur/k8s-spark-ui-assist/internal/config"
	"github.com/unaiur/k8s-spark-ui-assist/internal/httproute"
	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

func newDriver() store.Driver {
	return store.Driver{
		PodName:     "my-driver-pod",
		CreatedAt:   time.Now(),
		AppSelector: "spark-abc123",
		AppName:     "my-spark-job",
	}
}

func newManager(client *fakegw.Clientset) *httproute.Manager {
	cfg := config.HTTPRouteConfig{
		Enabled:          true,
		Hostname:         "spark.example.com",
		GatewayName:      "main-gateway",
		GatewayNamespace: "gateway-ns",
	}
	return httproute.New(client, "default", cfg)
}

func TestEnsureCreatesRoute(t *testing.T) {
	client := fakegw.NewSimpleClientset()
	mgr := newManager(client)
	ctx := context.Background()
	d := newDriver()

	mgr.Ensure(ctx, d)

	route, err := client.GatewayV1().HTTPRoutes("default").Get(ctx, d.AppSelector+"-ui-route", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected HTTPRoute to exist: %v", err)
	}
	if route.Name != d.AppSelector+"-ui-route" {
		t.Errorf("unexpected route name: %s", route.Name)
	}
}

func TestEnsureIdempotent(t *testing.T) {
	client := fakegw.NewSimpleClientset()
	mgr := newManager(client)
	ctx := context.Background()
	d := newDriver()

	mgr.Ensure(ctx, d)
	// Second call should not error or create a duplicate.
	mgr.Ensure(ctx, d)

	routes, err := client.GatewayV1().HTTPRoutes("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(routes.Items) != 1 {
		t.Errorf("expected 1 route, got %d", len(routes.Items))
	}
}

func TestDeleteRoute(t *testing.T) {
	client := fakegw.NewSimpleClientset()
	mgr := newManager(client)
	ctx := context.Background()
	d := newDriver()

	mgr.Ensure(ctx, d)
	mgr.Delete(ctx, d.AppSelector)

	routes, err := client.GatewayV1().HTTPRoutes("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(routes.Items) != 0 {
		t.Errorf("expected 0 routes after delete, got %d", len(routes.Items))
	}
}
