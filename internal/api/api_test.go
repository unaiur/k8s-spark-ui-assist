package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/unaiur/k8s-spark-ui-assist/internal/api"
	k8ssvc "github.com/unaiur/k8s-spark-ui-assist/internal/k8s"
	"github.com/unaiur/k8s-spark-ui-assist/internal/labels"
	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

const namespace = "default"

var podGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

// newScheme returns a scheme that knows about Pod and PodList.
func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "PodList"}, &unstructured.UnstructuredList{})
	return s
}

// stubReconciler is a test double for api.Reconciler.
type stubReconciler struct {
	err error
}

func (r *stubReconciler) Reconcile(_ context.Context, _ []store.Driver) error {
	return r.err
}

// newHandler creates an API handler wired to a fake dynamic client and store.
func newHandler(client *dynamicfake.FakeDynamicClient, s *store.Store, rec api.Reconciler) http.Handler {
	svc := k8ssvc.New(context.Background(), client, namespace)
	return api.Handler(svc, s, rec)
}

// addRunningPod creates a minimal running driver pod in the fake client.
func addRunningPod(t *testing.T, client *dynamicfake.FakeDynamicClient, appID string) {
	t.Helper()
	pod := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      appID + "-driver",
				"namespace": namespace,
				"labels": map[string]interface{}{
					labels.LabelInstance: labels.InstanceValue,
					labels.LabelRole:     labels.RoleValue,
					labels.LabelSelector: appID,
					labels.LabelAppName:  "my-job",
				},
			},
			"status": map[string]interface{}{
				"containerStatuses": []interface{}{
					map[string]interface{}{
						"name":  "spark-kubernetes-driver",
						"state": map[string]interface{}{"running": map[string]interface{}{}},
					},
				},
			},
		},
	}
	pod.SetCreationTimestamp(metav1.Now())
	_, err := client.Resource(podGVR).Namespace(namespace).Create(
		context.Background(), pod, metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("addRunningPod: %v", err)
	}
}

// get performs a GET request and returns the status code.
func get(h http.Handler, path string) int {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// post performs a POST request and returns the status code.
func post(h http.Handler, path string) int {
	req := httptest.NewRequest(http.MethodPost, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// ---- /proxy/api/state tests -------------------------------------------------

// TestHandlerReturns200ForKnownDriver verifies the happy path: a GET for a
// known driver returns 200.
func TestHandlerReturns200ForKnownDriver(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addRunningPod(t, client, "spark-abc")
	h := newHandler(client, store.New(), nil)

	if code := get(h, "/proxy/api/state/spark-abc"); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
}

// TestHandlerDriverNotFound verifies that an unknown appID returns 404.
func TestHandlerDriverNotFound(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client, store.New(), nil)

	if code := get(h, "/proxy/api/state/unknown-app"); code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
}

// TestHandlerMethodNotAllowed verifies that non-GET requests to the state
// endpoint return 405.
func TestHandlerMethodNotAllowed(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client, store.New(), nil)

	if code := post(h, "/proxy/api/state/spark-abc"); code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", code)
	}
}

// TestHandlerEmptyAppIDReturnsNotFound verifies that /proxy/api/state/ (no
// appID segment) returns 404.
func TestHandlerEmptyAppIDReturnsNotFound(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client, store.New(), nil)

	if code := get(h, "/proxy/api/state/"); code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
}

// TestHandlerInvalidAppIDReturnsBadRequest verifies that an appID containing
// characters invalid in Kubernetes label values returns 400.
func TestHandlerInvalidAppIDReturnsBadRequest(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client, store.New(), nil)

	if code := get(h, "/proxy/api/state/bad,app=id"); code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", code)
	}
}

// ---- /proxy/api/reconcile tests ---------------------------------------------

// TestReconcileReturns200OnSuccess verifies that a GET to /proxy/api/reconcile
// returns 200 when the reconciler succeeds.
func TestReconcileReturns200OnSuccess(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client, store.New(), &stubReconciler{})

	if code := get(h, "/proxy/api/reconcile"); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
}

// TestReconcileReturns500OnError verifies that a reconciler error propagates as 500.
func TestReconcileReturns500OnError(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client, store.New(), &stubReconciler{err: errors.New("k8s error")})

	if code := get(h, "/proxy/api/reconcile"); code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", code)
	}
}

// TestReconcileMethodNotAllowed verifies that a non-GET request returns 405.
func TestReconcileMethodNotAllowed(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client, store.New(), &stubReconciler{})

	if code := post(h, "/proxy/api/reconcile"); code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", code)
	}
}

// TestReconcileNilReconcilerReturns501 verifies that passing nil for the
// reconciler (httproute management disabled) returns 501.
func TestReconcileNilReconcilerReturns501(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client, store.New(), nil)

	if code := get(h, "/proxy/api/reconcile"); code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", code)
	}
}

// TestReconcileSetsNoCacheHeader verifies that a successful reconcile response
// carries Cache-Control: no-store to prevent caching by proxies.
func TestReconcileSetsNoCacheHeader(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client, store.New(), &stubReconciler{})

	req := httptest.NewRequest(http.MethodGet, "/proxy/api/reconcile", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("expected Cache-Control: no-store, got %q", cc)
	}
}

// TestReconcilePassesActiveDriversToReconciler verifies that the active drivers
// from the store are forwarded to the reconciler.
func TestReconcilePassesActiveDriversToReconciler(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	s := store.New()
	s.Add(store.Driver{PodName: "driver-1", AppSelector: "spark-abc", AppName: "job"})

	var gotDrivers []store.Driver
	rec := &captureReconciler{}
	h := newHandler(client, s, rec)

	get(h, "/proxy/api/reconcile")

	gotDrivers = rec.drivers
	if len(gotDrivers) != 1 || gotDrivers[0].AppSelector != "spark-abc" {
		t.Errorf("expected reconciler called with spark-abc driver, got %v", gotDrivers)
	}
}

// captureReconciler records the drivers passed to Reconcile.
type captureReconciler struct {
	drivers []store.Driver
}

func (r *captureReconciler) Reconcile(_ context.Context, active []store.Driver) error {
	r.drivers = active
	return nil
}
