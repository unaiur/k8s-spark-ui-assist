// Package api implements the /proxy/api HTTP endpoints.
//
// Currently provided endpoints:
//
//	GET /proxy/api/{appID}
//	  Returns a JSON object describing the state of the Spark driver pod
//	  identified by the spark-app-selector={appID} label.  The same label
//	  selector used by the watcher is applied so that only genuine Spark driver
//	  pods are matched.
//
//	  Responses:
//	    200  {"appID":"…","state":"…"}
//	    400  {"error":"invalid appID"}           – appID is not a valid label value
//	    404  {"error":"driver not found"}        – no matching pod
//	    405  {"error":"method not allowed"}      – non-GET request
//	    500  {"error":"…"}                       – Kubernetes API error
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"

	"github.com/unaiur/k8s-spark-ui-assist/internal/labels"
)

// apiPrefix is the URL path prefix under which all API endpoints are mounted.
const apiPrefix = "/proxy/api/"

var podGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

// Handler returns an http.Handler that serves all /proxy/api/ endpoints.
// namespace is the Kubernetes namespace to query for driver pods.
func Handler(client dynamic.Interface, namespace string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract the appID from the path: /proxy/api/{appID}
		appID := strings.TrimPrefix(r.URL.Path, apiPrefix)
		// Reject empty appID or nested paths (e.g. /proxy/api/foo/bar).
		if appID == "" || strings.Contains(appID, "/") {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}

		// Validate appID as a Kubernetes label value to prevent injection into
		// the label selector and to return a meaningful 400 instead of a 500.
		if errs := validation.IsValidLabelValue(appID); len(errs) > 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid appID"})
			return
		}

		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
			return
		}

		state, err := driverState(r.Context(), client, namespace, appID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if state == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "driver not found"})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"appID": appID, "state": state})
	})
}

// driverState queries Kubernetes for the driver pod matching appID and returns
// its state string. Returns ("", nil) when no matching pod exists.
//
// State derivation rules (in priority order):
//  1. If any container status is present, inspect the first container:
//     - waiting  → waiting.reason (e.g. "ContainerCreating", "CrashLoopBackOff")
//     - running  → "Running"
//     - terminated → terminated.reason if non-empty, else terminated.exitCode as
//     "Error" (non-zero) or "Completed" (zero)
//  2. No container status yet (pod not yet started): inspect pod conditions for a
//     False PodScheduled condition and return its reason (e.g. "Unschedulable");
//     otherwise return "Pending".
func driverState(ctx context.Context, client dynamic.Interface, namespace, appID string) (string, error) {
	list, err := client.Resource(podGVR).Namespace(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.DriverSelectorForApp(appID),
	})
	if err != nil {
		return "", err
	}
	if len(list.Items) == 0 {
		return "", nil
	}

	// If multiple pods exist (e.g. a restart), use the most recently created one.
	pod := mostRecent(list.Items)
	return stateFromPod(pod), nil
}

// mostRecent returns the pod with the latest creation timestamp.
func mostRecent(pods []unstructured.Unstructured) unstructured.Unstructured {
	best := pods[0]
	for _, p := range pods[1:] {
		if p.GetCreationTimestamp().After(best.GetCreationTimestamp().Time) {
			best = p
		}
	}
	return best
}

// stateFromPod derives a human-readable state string from a pod object.
func stateFromPod(pod unstructured.Unstructured) string {
	containerStatuses, _, _ := unstructured.NestedSlice(pod.Object, "status", "containerStatuses")
	if len(containerStatuses) > 0 {
		if cs, ok := containerStatuses[0].(map[string]interface{}); ok {
			if state := containerStateString(cs); state != "" {
				return state
			}
		}
	}

	// No container status yet — pod has not started.  Check conditions for a
	// more specific reason (e.g. scheduling failure) before falling back to
	// "Pending".
	return pendingReason(pod)
}

// pendingReason returns a waiting-style reason for a pod that has no container
// statuses yet.  It looks for a PodScheduled condition with status "False" and
// returns its Reason field (e.g. "Unschedulable"); otherwise it returns
// "Pending".
func pendingReason(pod unstructured.Unstructured) string {
	conditions, _, _ := unstructured.NestedSlice(pod.Object, "status", "conditions")
	for _, raw := range conditions {
		cond, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _ := cond["type"].(string)
		condStatus, _ := cond["status"].(string)
		if condType == "PodScheduled" && condStatus == "False" {
			if reason, _ := cond["reason"].(string); reason != "" {
				return reason
			}
		}
	}
	return "Pending"
}

// containerStateString extracts a state string from a containerStatus map.
func containerStateString(cs map[string]interface{}) string {
	stateMap, _, _ := unstructured.NestedMap(cs, "state")
	if stateMap == nil {
		return ""
	}

	if waiting, ok := stateMap["waiting"].(map[string]interface{}); ok {
		if reason, _ := waiting["reason"].(string); reason != "" {
			return reason
		}
		return "Waiting"
	}

	if _, ok := stateMap["running"]; ok {
		return "Running"
	}

	if terminated, ok := stateMap["terminated"].(map[string]interface{}); ok {
		if reason, _ := terminated["reason"].(string); reason != "" {
			return reason
		}
		// exitCode may be int64 (from fake client) or float64 (from real JSON
		// decoding), so handle both rather than relying on a single type assertion.
		if isZeroExitCode(terminated) {
			return "Completed"
		}
		return "Error"
	}

	return ""
}

// isZeroExitCode reports whether the exitCode field in a terminated container
// status map is zero.  The Kubernetes API returns JSON numbers which are decoded
// as float64 by the unstructured machinery; test fixtures may use int64.  Both
// are handled explicitly.
func isZeroExitCode(terminated map[string]interface{}) bool {
	val, ok := terminated["exitCode"]
	if !ok {
		return false
	}
	switch v := val.(type) {
	case int64:
		return v == 0
	case float64:
		return v == 0
	case int:
		return v == 0
	default:
		return false
	}
}

// writeJSON writes a JSON-encoded body with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
