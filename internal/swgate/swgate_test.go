package swgate_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/unaiur/k8s-spark-ui-assist/internal/swgate"
)

// sentinel handler that records whether it was called.
type sentinelHandler struct{ called bool }

func (h *sentinelHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.called = true
	w.WriteHeader(http.StatusOK)
}

// --- GateMiddleware tests ---------------------------------------------------

func TestGateMiddleware_DashboardTriggersGate(t *testing.T) {
	next := &sentinelHandler{}
	h := swgate.GateMiddleware(swgate.Config{}, next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxy/", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "/proxy/api/sw-gate?return=") {
		t.Fatalf("unexpected Location: %s", loc)
	}
	if next.called {
		t.Fatal("next handler should not have been called")
	}
}

func TestGateMiddleware_HTMLPathTriggersGate(t *testing.T) {
	for _, path := range []string{"/", "/proxy/myapp/", "/proxy/myapp/jobs/",
		"/some/page.html", "/some/page.htm"} {
		t.Run(path, func(t *testing.T) {
			next := &sentinelHandler{}
			h := swgate.GateMiddleware(swgate.Config{}, next)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusFound {
				t.Fatalf("path %q: expected 302, got %d", path, rec.Code)
			}
		})
	}
}

func TestGateMiddleware_BypassParamSkipsGate(t *testing.T) {
	next := &sentinelHandler{}
	h := swgate.GateMiddleware(swgate.Config{}, next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxy/?_sw=1", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !next.called {
		t.Fatal("next handler should have been called")
	}
}

func TestGateMiddleware_APIPathSkipsGate(t *testing.T) {
	for _, path := range []string{
		"/proxy/api/state/foo",
		"/proxy/api/sw-gate",
		"/proxy/api/sw.js",
		"/proxy/api/spark-inject.js",
		"/proxy/api/reconcile",
	} {
		t.Run(path, func(t *testing.T) {
			next := &sentinelHandler{}
			h := swgate.GateMiddleware(swgate.Config{}, next)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusFound {
				t.Fatalf("path %q: gate should not trigger for API paths", path)
			}
			if !next.called {
				t.Fatalf("path %q: next handler should have been called", path)
			}
		})
	}
}

func TestGateMiddleware_NonGetSkipsGate(t *testing.T) {
	next := &sentinelHandler{}
	h := swgate.GateMiddleware(swgate.Config{}, next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/proxy/", nil)
	h.ServeHTTP(rec, req)

	if rec.Code == http.StatusFound {
		t.Fatal("POST should not trigger gate")
	}
	if !next.called {
		t.Fatal("next handler should have been called")
	}
}

func TestGateMiddleware_NonHTMLPathSkipsGate(t *testing.T) {
	for _, path := range []string{"/favicon.ico", "/proxy/myapp/static/app.js", "/proxy/myapp/api/jobs"} {
		t.Run(path, func(t *testing.T) {
			next := &sentinelHandler{}
			h := swgate.GateMiddleware(swgate.Config{}, next)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			h.ServeHTTP(rec, req)
			if rec.Code == http.StatusFound {
				t.Fatalf("path %q: non-HTML path should not trigger gate", path)
			}
			if !next.called {
				t.Fatalf("path %q: next handler should have been called", path)
			}
		})
	}
}

func TestGateMiddleware_ReturnURLPreservesPath(t *testing.T) {
	next := &sentinelHandler{}
	h := swgate.GateMiddleware(swgate.Config{}, next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxy/myapp/jobs/", nil)
	h.ServeHTTP(rec, req)

	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "%2Fproxy%2Fmyapp%2Fjobs%2F") &&
		!strings.Contains(loc, "/proxy/myapp/jobs/") {
		t.Fatalf("Location %q does not encode the original path", loc)
	}
}

func TestGateMiddleware_QueryStringRoundTrip(t *testing.T) {
	// The return parameter must survive a query string with &, =, # chars
	// without being truncated or split into extra query params.
	next := &sentinelHandler{}
	h := swgate.GateMiddleware(swgate.Config{}, next)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxy/app/jobs/?a=1&b=2", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")

	// Parse Location as a URL so we can inspect its query parameters cleanly.
	locURL, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("Location %q is not a valid URL: %v", loc, err)
	}
	// There must be exactly one query param on the gate URL: "return".
	// If & / = in the original query leaked unescaped, there would be extra params.
	q := locURL.Query()
	if _, ok := q["return"]; !ok {
		t.Fatalf("Location %q missing 'return' query param", loc)
	}
	if len(q) != 1 {
		t.Fatalf("Location %q has unexpected extra query params: %v", loc, q)
	}

	// The decoded return value must reconstruct the original request URI.
	returnVal := q.Get("return")
	if returnVal != "/proxy/app/jobs/?a=1&b=2" {
		t.Errorf("return param %q does not match original URI", returnVal)
	}
}

// --- Handler tests ----------------------------------------------------------

func TestHandler_SWGatePageServed(t *testing.T) {
	h := swgate.Handler(swgate.Config{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxy/api/sw-gate?return=%2Fproxy%2F", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "serviceWorker") {
		t.Error("gate page should contain serviceWorker registration code")
	}
	if !strings.Contains(body, "/proxy/api/sw.js") {
		t.Error("gate page should reference /proxy/api/sw.js")
	}
}

func TestHandler_SWGateRejectsAbsoluteReturn(t *testing.T) {
	h := swgate.Handler(swgate.Config{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxy/api/sw-gate?return=https://evil.com/", nil)
	h.ServeHTTP(rec, req)

	// Should redirect to /proxy/api/sw-gate?return=/proxy/ (sanitised)
	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if strings.Contains(loc, "evil.com") {
		t.Fatalf("open redirect not prevented: Location=%s", loc)
	}
}

func TestHandler_SWGateRejectsProtocolRelativeReturn(t *testing.T) {
	h := swgate.Handler(swgate.Config{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxy/api/sw-gate?return=//evil.com/", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if strings.Contains(loc, "evil.com") {
		t.Fatalf("open redirect not prevented: Location=%s", loc)
	}
}

func TestHandler_SWJSServedWithAllowedHeader(t *testing.T) {
	h := swgate.Handler(swgate.Config{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxy/api/sw.js", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Service-Worker-Allowed") != "/" {
		t.Errorf("missing or wrong Service-Worker-Allowed header: %q",
			rec.Header().Get("Service-Worker-Allowed"))
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Errorf("expected JS content type, got %q", ct)
	}
}

func TestHandler_SWJSContainsHardcodedInjectPath(t *testing.T) {
	// sw.js is a plain static file — the inject URL must always be the
	// hardcoded constant /proxy/api/spark-inject.js regardless of config.
	h := swgate.Handler(swgate.Config{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxy/api/sw.js", nil)
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "{{") {
		t.Error("sw.js still contains unexpanded template placeholder")
	}
	if !strings.Contains(body, "/proxy/api/spark-inject.js") {
		t.Errorf("sw.js does not contain hardcoded inject path, body:\n%s", body)
	}
}

func TestHandler_InjectJSServesContentWhenConfigured(t *testing.T) {
	const jsContent = "console.log('hello from inject');"
	h := swgate.Handler(swgate.Config{InjectScript: jsContent})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxy/api/spark-inject.js", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "javascript") {
		t.Errorf("expected JS content type, got %q", ct)
	}
	if body := rec.Body.String(); body != jsContent {
		t.Errorf("expected body %q, got %q", jsContent, body)
	}
}

func TestHandler_InjectJSNotFoundWhenNotConfigured(t *testing.T) {
	h := swgate.Handler(swgate.Config{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxy/api/spark-inject.js", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandler_UnknownPathReturns404(t *testing.T) {
	h := swgate.Handler(swgate.Config{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/proxy/api/sw-unknown", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
