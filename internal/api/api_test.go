package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/unaiur/k8s-spark-ui-assist/internal/api"
)

const namespace = "default"

var podGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

// newScheme returns a scheme that knows about Pod and PodList.
func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	gvk := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}
	listGVK := schema.GroupVersionKind{Group: "", Version: "v1", Kind: "PodList"}
	s.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})
	return s
}

// newHandler creates an API handler wired to a fake dynamic client.
func newHandler(client *dynamicfake.FakeDynamicClient) http.Handler {
	return api.Handler(client, namespace)
}

// makePendingPod creates a fake pod that has no container statuses yet (i.e. the
// pod is still being scheduled or initialised).  If condReason is non-empty a
// PodScheduled=False condition with that reason is added (e.g. "Unschedulable").
func makePendingPod(appID string, condReason string) *unstructured.Unstructured {
	labels := map[string]interface{}{
		"app.kubernetes.io/instance": "spark-job",
		"spark-role":                 "driver",
		"spark-app-selector":         appID,
		"spark-app-name":             "my-job",
	}

	pod := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      appID + "-driver",
				"namespace": namespace,
				"labels":    labels,
			},
			"status": map[string]interface{}{},
		},
	}
	pod.SetCreationTimestamp(metav1.Now())

	if condReason != "" {
		_ = unstructured.SetNestedSlice(pod.Object, []interface{}{
			map[string]interface{}{
				"type":   "PodScheduled",
				"status": "False",
				"reason": condReason,
			},
		}, "status", "conditions")
	}

	return pod
}

// makePod creates a fake pod with the given appID and containerStatus state.
// containerState should be one of "waiting", "running", "terminated", or "" (no status yet).
func makePod(appID string, containerState string, reason string, exitCode int64) *unstructured.Unstructured {
	labels := map[string]interface{}{
		"app.kubernetes.io/instance": "spark-job",
		"spark-role":                 "driver",
		"spark-app-selector":         appID,
		"spark-app-name":             "my-job",
	}

	pod := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      appID + "-driver",
				"namespace": namespace,
				"labels":    labels,
			},
			"status": map[string]interface{}{},
		},
	}
	pod.SetCreationTimestamp(metav1.Now())

	if containerState != "" {
		stateMap := map[string]interface{}{}
		switch containerState {
		case "waiting":
			waitingMap := map[string]interface{}{}
			if reason != "" {
				waitingMap["reason"] = reason
			}
			stateMap["waiting"] = waitingMap
		case "running":
			stateMap["running"] = map[string]interface{}{
				"startedAt": "2026-01-01T00:00:00Z",
			}
		case "terminated":
			terminatedMap := map[string]interface{}{
				"exitCode": exitCode,
			}
			if reason != "" {
				terminatedMap["reason"] = reason
			}
			stateMap["terminated"] = terminatedMap
		}
		_ = unstructured.SetNestedSlice(pod.Object, []interface{}{
			map[string]interface{}{
				"name":  "spark-kubernetes-driver",
				"state": stateMap,
			},
		}, "status", "containerStatuses")
	}

	return pod
}

// addPod creates a pod in the fake client.
func addPod(t *testing.T, client *dynamicfake.FakeDynamicClient, pod *unstructured.Unstructured) {
	t.Helper()
	_, err := client.Resource(podGVR).Namespace(namespace).Create(
		httptest.NewRequest(http.MethodGet, "/", nil).Context(), pod, metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("addPod: %v", err)
	}
}

// getJSON performs a GET against the handler and decodes the JSON response.
func getJSON(t *testing.T, h http.Handler, path string) (int, map[string]string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("getJSON: decode: %v (body: %s)", err, rec.Body.String())
	}
	return rec.Code, body
}

// TestGetRunningDriver verifies that a running driver pod returns state "Running".
func TestGetRunningDriver(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePod("spark-abc", "running", "", 0))
	h := newHandler(client)

	code, body := getJSON(t, h, "/proxy/api/spark-abc")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if body["state"] != "Running" {
		t.Errorf("expected state Running, got %q", body["state"])
	}
	if body["appID"] != "spark-abc" {
		t.Errorf("expected appID spark-abc, got %q", body["appID"])
	}
}

// TestGetWaitingDriverWithReason verifies that a waiting container returns its reason.
func TestGetWaitingDriverWithReason(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePod("spark-abc", "waiting", "ContainerCreating", 0))
	h := newHandler(client)

	code, body := getJSON(t, h, "/proxy/api/spark-abc")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if body["state"] != "ContainerCreating" {
		t.Errorf("expected state ContainerCreating, got %q", body["state"])
	}
}

// TestGetTerminatedDriverCompleted verifies exit code 0 yields "Completed".
func TestGetTerminatedDriverCompleted(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePod("spark-abc", "terminated", "", 0))
	h := newHandler(client)

	code, body := getJSON(t, h, "/proxy/api/spark-abc")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if body["state"] != "Completed" {
		t.Errorf("expected state Completed, got %q", body["state"])
	}
}

// TestGetTerminatedDriverError verifies non-zero exit code yields "Error".
func TestGetTerminatedDriverError(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePod("spark-abc", "terminated", "", 1))
	h := newHandler(client)

	code, body := getJSON(t, h, "/proxy/api/spark-abc")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if body["state"] != "Error" {
		t.Errorf("expected state Error, got %q", body["state"])
	}
}

// TestGetTerminatedDriverWithReason verifies a terminated container with a reason
// string returns that reason.
func TestGetTerminatedDriverWithReason(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePod("spark-abc", "terminated", "OOMKilled", 137))
	h := newHandler(client)

	code, body := getJSON(t, h, "/proxy/api/spark-abc")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if body["state"] != "OOMKilled" {
		t.Errorf("expected state OOMKilled, got %q", body["state"])
	}
}

// TestGetDriverNotFound verifies that an unknown appID returns 404.
func TestGetDriverNotFound(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client)

	code, body := getJSON(t, h, "/proxy/api/unknown-app")
	if code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
	if body["error"] == "" {
		t.Error("expected non-empty error field")
	}
}

// TestMethodNotAllowed verifies that non-GET requests return 405.
func TestMethodNotAllowed(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client)

	req := httptest.NewRequest(http.MethodPost, "/proxy/api/spark-abc", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

// TestEmptyAppIDReturnsNotFound verifies that /proxy/api/ (no appID) returns 404.
func TestEmptyAppIDReturnsNotFound(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	h := newHandler(client)

	code, _ := getJSON(t, h, "/proxy/api/")
	if code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
}

// TestOnlyDriverPodsMatched verifies that pods without the correct Spark driver
// labels are not returned even if they share the same spark-app-selector.
func TestOnlyDriverPodsMatched(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())

	// Pod without spark-role=driver label.
	nonDriver := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      "spark-abc-executor",
				"namespace": namespace,
				"labels": map[string]interface{}{
					"app.kubernetes.io/instance": "spark-job",
					"spark-app-selector":         "spark-abc",
					// no spark-role=driver
				},
			},
			"status": map[string]interface{}{},
		},
	}
	addPod(t, client, nonDriver)
	h := newHandler(client)

	code, body := getJSON(t, h, "/proxy/api/spark-abc")
	if code != http.StatusNotFound {
		t.Errorf("expected 404 (non-driver pod should not match), got %d; body: %v", code, body)
	}
}

// TestPendingDriverNoConditions verifies that a pod with no container statuses and
// no scheduling conditions returns state "Pending".
func TestPendingDriverNoConditions(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePendingPod("spark-abc", ""))
	h := newHandler(client)

	code, body := getJSON(t, h, "/proxy/api/spark-abc")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if body["state"] != "Pending" {
		t.Errorf("expected state Pending, got %q", body["state"])
	}
}

// TestPendingDriverUnschedulable verifies that a pod with a PodScheduled=False
// condition with reason "Unschedulable" returns that reason as the state.
func TestPendingDriverUnschedulable(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePendingPod("spark-abc", "Unschedulable"))
	h := newHandler(client)

	code, body := getJSON(t, h, "/proxy/api/spark-abc")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if body["state"] != "Unschedulable" {
		t.Errorf("expected state Unschedulable, got %q", body["state"])
	}
}
