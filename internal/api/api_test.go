package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/unaiur/k8s-spark-ui-assist/internal/api"
	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

// stubReconciler is a test double for api.Reconciler.
type stubReconciler struct {
	err error
}

func (r *stubReconciler) Reconcile(_ context.Context, _ []store.Driver) error {
	return r.err
}

// newHandler creates an API handler wired to a store and reconciler.
func newHandler(s *store.Store, rec api.Reconciler) http.Handler {
	return api.Handler(s, rec)
}

// get performs a GET request and returns the status code.
func get(h http.Handler, path string) int {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// getBody performs a GET request and returns the status code and parsed JSON body.
func getBody(h http.Handler, path string) (int, map[string]string) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	return rec.Code, body
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
	s := store.New()
	s.Add(store.Driver{PodName: "pod-1", AppSelector: "spark-abc", State: store.StateRunning})
	h := newHandler(s, nil)

	if code := get(h, "/proxy/api/state/spark-abc"); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
}

// TestHandlerStateResponseContainsFields verifies that the 200 response body
// contains "appID", "state", and "reason" fields.
func TestHandlerStateResponseContainsFields(t *testing.T) {
	s := store.New()
	s.Add(store.Driver{
		PodName:     "pod-1",
		AppSelector: "spark-abc",
		State:       store.StatePending,
		Reason:      "Cannot be scheduled",
	})
	h := newHandler(s, nil)

	code, body := getBody(h, "/proxy/api/state/spark-abc")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if body["appID"] != "spark-abc" {
		t.Errorf("expected appID spark-abc, got %q", body["appID"])
	}
	if body["state"] != "Pending" {
		t.Errorf("expected state Pending, got %q", body["state"])
	}
	if body["reason"] != "Cannot be scheduled" {
		t.Errorf("expected reason %q, got %q", "Cannot be scheduled", body["reason"])
	}
}

// TestHandlerStateResponseReasonEmptyWhenNotSet verifies that the reason field
// is present but empty when the driver has no reason set.
func TestHandlerStateResponseReasonEmptyWhenNotSet(t *testing.T) {
	s := store.New()
	s.Add(store.Driver{PodName: "pod-1", AppSelector: "spark-abc", State: store.StateRunning})
	h := newHandler(s, nil)

	code, body := getBody(h, "/proxy/api/state/spark-abc")
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
	if reason, ok := body["reason"]; !ok || reason != "" {
		t.Errorf("expected reason field to be present and empty, got %q (ok=%v)", reason, ok)
	}
}

// TestHandlerDriverNotFound verifies that an unknown appID returns 404.
func TestHandlerDriverNotFound(t *testing.T) {
	h := newHandler(store.New(), nil)

	if code := get(h, "/proxy/api/state/unknown-app"); code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
}

// TestHandlerMethodNotAllowed verifies that non-GET requests to the state
// endpoint return 405.
func TestHandlerMethodNotAllowed(t *testing.T) {
	h := newHandler(store.New(), nil)

	if code := post(h, "/proxy/api/state/spark-abc"); code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", code)
	}
}

// TestHandlerEmptyAppIDReturnsNotFound verifies that /proxy/api/state/ (no
// appID segment) returns 404.
func TestHandlerEmptyAppIDReturnsNotFound(t *testing.T) {
	h := newHandler(store.New(), nil)

	if code := get(h, "/proxy/api/state/"); code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
}

// TestHandlerInvalidAppIDReturnsBadRequest verifies that an appID containing
// characters invalid in Kubernetes label values returns 400.
func TestHandlerInvalidAppIDReturnsBadRequest(t *testing.T) {
	h := newHandler(store.New(), nil)

	if code := get(h, "/proxy/api/state/bad,app=id"); code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", code)
	}
}

// ---- /proxy/api/reconcile tests ---------------------------------------------

// TestReconcileReturns200OnSuccess verifies that a GET to /proxy/api/reconcile
// returns 200 when the reconciler succeeds.
func TestReconcileReturns200OnSuccess(t *testing.T) {
	h := newHandler(store.New(), &stubReconciler{})

	if code := get(h, "/proxy/api/reconcile"); code != http.StatusOK {
		t.Fatalf("expected 200, got %d", code)
	}
}

// TestReconcileReturns500OnError verifies that a reconciler error propagates as 500.
func TestReconcileReturns500OnError(t *testing.T) {
	h := newHandler(store.New(), &stubReconciler{err: errors.New("k8s error")})

	if code := get(h, "/proxy/api/reconcile"); code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", code)
	}
}

// TestReconcileMethodNotAllowed verifies that a non-GET request returns 405.
func TestReconcileMethodNotAllowed(t *testing.T) {
	h := newHandler(store.New(), &stubReconciler{})

	if code := post(h, "/proxy/api/reconcile"); code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", code)
	}
}

// TestReconcileNilReconcilerReturns501 verifies that passing nil for the
// reconciler (httproute management disabled) returns 501.
func TestReconcileNilReconcilerReturns501(t *testing.T) {
	h := newHandler(store.New(), nil)

	if code := get(h, "/proxy/api/reconcile"); code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d", code)
	}
}

// TestReconcileSetsNoCacheHeader verifies that a successful reconcile response
// carries Cache-Control: no-store to prevent caching by proxies.
func TestReconcileSetsNoCacheHeader(t *testing.T) {
	h := newHandler(store.New(), &stubReconciler{})

	req := httptest.NewRequest(http.MethodGet, "/proxy/api/reconcile", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("expected Cache-Control: no-store, got %q", cc)
	}
}

// TestReconcilePassesOnlyRunningDriversToReconciler verifies that only Running
// drivers from the store are forwarded to the reconciler. Pending/Unknown
// drivers must not receive an HTTPRoute.
func TestReconcilePassesOnlyRunningDriversToReconciler(t *testing.T) {
	s := store.New()
	s.Add(store.Driver{PodName: "driver-running", AppSelector: "spark-abc", AppName: "job", State: store.StateRunning})
	s.Add(store.Driver{PodName: "driver-pending", AppSelector: "spark-xyz", AppName: "job", State: store.StatePending})

	rec := &captureReconciler{}
	h := newHandler(s, rec)

	get(h, "/proxy/api/reconcile")

	if len(rec.drivers) != 1 {
		t.Fatalf("expected 1 driver forwarded to reconciler, got %d: %v", len(rec.drivers), rec.drivers)
	}
	if rec.drivers[0].AppSelector != "spark-abc" {
		t.Errorf("expected running driver spark-abc to be forwarded, got %v", rec.drivers)
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
