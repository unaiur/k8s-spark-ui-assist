package api_test

import (
	"context"
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

// newHandler creates an API handler wired to a fake dynamic client.
func newHandler(client *dynamicfake.FakeDynamicClient) http.Handler {
	svc := k8ssvc.New(context.Background(), client, namespace)
	return api.Handler(svc)
}

// addRunningPod creates a minimal running driver pod in the fake client so that
// handler tests that need a real pod response don't have to set up full state.
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

// get performs a GET request against the handler and returns the status code.
func get(h http.Handler, path string) int {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// TestHandlerReturns200ForKnownDriver verifies the happy path: a GET for a
// known driver returns 200. State derivation details are tested in k8s_svc_test.go.
func TestHandlerReturns200ForKnownDriver(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addRunningPod(t, client, "spark-abc")
	h := newHandler(client)

	if code := get(h, "/proxy/api/state/spark-abc"); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
}

// TestHandlerDriverNotFound verifies that an unknown appID returns 404.
func TestHandlerDriverNotFound(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client)

	if code := get(h, "/proxy/api/state/unknown-app"); code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
}

// TestHandlerMethodNotAllowed verifies that non-GET requests return 405.
func TestHandlerMethodNotAllowed(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client)

	req := httptest.NewRequest(http.MethodPost, "/proxy/api/state/spark-abc", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// TestHandlerEmptyAppIDReturnsNotFound verifies that /proxy/api/state/ (no
// appID segment) returns 404.
func TestHandlerEmptyAppIDReturnsNotFound(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client)

	if code := get(h, "/proxy/api/state/"); code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
}

// TestHandlerInvalidAppIDReturnsBadRequest verifies that an appID containing
// characters invalid in Kubernetes label values returns 400.
func TestHandlerInvalidAppIDReturnsBadRequest(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client)

	if code := get(h, "/proxy/api/state/bad,app=id"); code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", code)
	}
}
