// Package api implements the /proxy/api HTTP endpoints.
//
// Provided endpoints:
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
//
//	GET /proxy/api/reconcile
//	  Triggers an immediate HTTPRoute reconciliation against the current set of
//	  active drivers in the store. Uses GET because the operation is idempotent
//	  and has no side-effects beyond correcting drift; Cache-Control: no-store
//	  is set on the response to prevent clients and proxies from caching it.
//
//	  Responses:
//	    200  {"status":"ok"}                     – reconciliation succeeded
//	    405  {"error":"method not allowed"}      – non-GET request
//	    501  {"error":"…"}                       – httproute management not configured
//	    500  {"error":"…"}                       – reconciliation error
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	k8ssvc "github.com/unaiur/k8s-spark-ui-assist/internal/k8s"
	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

// Reconciler is implemented by httproute.Manager. It is defined here as an
// interface so that the api package does not import httproute.
type Reconciler interface {
	Reconcile(ctx context.Context, active []store.Driver) error
}

// statePrefix is the URL path prefix for the state endpoint.
const statePrefix = "/proxy/api/state/"

// reconcilePath is the exact URL path for the reconcile endpoint.
const reconcilePath = "/proxy/api/reconcile"

// Handler returns an http.Handler that serves all /proxy/api/ endpoints.
// svc is used to query Kubernetes for driver pod state.
// s is the driver store used to supply the active driver list to Reconcile.
// rec is the HTTPRoute reconciler; if nil the /proxy/api/reconcile endpoint
// returns 501 Not Implemented.
func Handler(svc *k8ssvc.KubernetesSvc, s *store.Store, rec Reconciler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == reconcilePath {
			handleReconcile(w, r, s, rec)
			return
		}
		handleState(w, r, svc)
	})
}

// handleState serves GET /proxy/api/state/{appID}.
func handleState(w http.ResponseWriter, r *http.Request, svc *k8ssvc.KubernetesSvc) {
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
}

// handleReconcile serves GET /proxy/api/reconcile.
func handleReconcile(w http.ResponseWriter, r *http.Request, s *store.Store, rec Reconciler) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if rec == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "httproute management not configured"})
		return
	}
	if err := rec.Reconcile(r.Context(), s.ListRunning()); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeJSON writes a JSON-encoded body with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
