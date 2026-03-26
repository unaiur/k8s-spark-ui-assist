// Package api implements the /proxy/api HTTP endpoints.
//
// Currently provided endpoints:
//
//	GET /proxy/api/state/{appID}
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
	"encoding/json"
	"net/http"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	k8ssvc "github.com/unaiur/k8s-spark-ui-assist/internal/k8s"
)

// statePrefix is the URL path prefix for the state endpoint.
const statePrefix = "/proxy/api/state/"

// Handler returns an http.Handler that serves all /proxy/api/ endpoints.
// svc is used to query Kubernetes for driver pod state.
func Handler(svc *k8ssvc.KubernetesSvc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract the appID from the path: /proxy/api/state/{appID}
		appID := strings.TrimPrefix(r.URL.Path, statePrefix)
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

		state, err := svc.SparkDriverState(appID)
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

// writeJSON writes a JSON-encoded body with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
