package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "00:00:00"},
		{-5 * time.Second, "00:00:00"},
		{59 * time.Second, "00:00:59"},
		{time.Minute, "00:01:00"},
		{time.Hour, "01:00:00"},
		{23*time.Hour + 59*time.Minute + 59*time.Second, "23:59:59"},
		{24 * time.Hour, "1 day 00:00:00"},
		{24*time.Hour + 1*time.Second, "1 day 00:00:01"},
		{48 * time.Hour, "2 days 00:00:00"},
		{2*24*time.Hour + 3*time.Hour + 4*time.Minute + 5*time.Second, "2 days 03:04:05"},
	}

	for _, tc := range cases {
		got := FormatDuration(tc.d)
		if got != tc.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func newStore(drivers ...store.Driver) *store.Store {
	s := store.New()
	for _, d := range drivers {
		s.Add(d)
	}
	return s
}

func fixedNow() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

// TestHandlerDashboardRunningDriver checks that a Running driver gets a link
// and a green badge.
func TestHandlerDashboardRunningDriver(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-1",
		AppSelector: "spark-abc",
		AppName:     "my-job",
		CreatedAt:   fixedNow().Add(-time.Hour),
		State:       store.StateRunning,
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/proxy/spark-abc/jobs/") {
		t.Errorf("expected driver link with /proxy/ prefix in body, got:\n%s", body)
	}
	if !strings.Contains(body, "badge-running") {
		t.Errorf("expected badge-running class in body, got:\n%s", body)
	}
	if !strings.Contains(body, "Running") {
		t.Errorf("expected Running state text in body, got:\n%s", body)
	}
}

// TestHandlerDashboardPendingDriver checks that a Pending driver shows no link
// and a grey badge.
func TestHandlerDashboardPendingDriver(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-2",
		AppSelector: "spark-xyz",
		AppName:     "my-pending-job",
		CreatedAt:   fixedNow().Add(-time.Minute),
		State:       store.StatePending,
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "/proxy/spark-xyz/jobs/") {
		t.Errorf("pending driver should NOT have a link, got:\n%s", body)
	}
	if !strings.Contains(body, "badge-pending") {
		t.Errorf("expected badge-pending class in body, got:\n%s", body)
	}
	if !strings.Contains(body, "Pending") {
		t.Errorf("expected Pending state text in body, got:\n%s", body)
	}
}

// TestHandlerDashboardServesPage is the legacy smoke-test: GET "/proxy/" with a
// Running driver returns 200 and the driver link.
func TestHandlerDashboardServesPage(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-1",
		AppSelector: "spark-abc",
		AppName:     "my-job",
		CreatedAt:   fixedNow().Add(-time.Hour),
		State:       store.StateRunning,
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/proxy/spark-abc/jobs/") {
		t.Errorf("expected driver link with /proxy/ prefix in body, got:\n%s", body)
	}
}

// TestHandlerDashboardReasonTooltipPresent verifies that when a driver has a
// non-empty Reason, the badge span carries a title="…" attribute.
func TestHandlerDashboardReasonTooltipPresent(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-r",
		AppSelector: "spark-pending",
		AppName:     "my-pending-job",
		CreatedAt:   fixedNow().Add(-time.Minute),
		State:       store.StatePending,
		Reason:      "Cannot be scheduled",
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `title="Cannot be scheduled"`) {
		t.Errorf("expected title attribute with reason in body, got:\n%s", body)
	}
}

// TestHandlerDashboardReasonTooltipAbsent verifies that when Reason is empty,
// no title="…" attribute is rendered on the badge span.
func TestHandlerDashboardReasonTooltipAbsent(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-r",
		AppSelector: "spark-running",
		AppName:     "my-running-job",
		CreatedAt:   fixedNow().Add(-time.Minute),
		State:       store.StateRunning,
		Reason:      "",
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow).ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "title=") {
		t.Errorf("expected no title attribute when Reason is empty, got:\n%s", body)
	}
}

// TestHandlerNonProxyRedirects checks that any path other than "/proxy/" gets a
// 302 redirect to "/proxy/".
func TestHandlerNonProxyRedirects(t *testing.T) {
	paths := []string{"/", "/foo", "/proxy/spark-abc/", "/anything"}
	s := newStore()

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		Handler(s, fixedNow).ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Errorf("path %q: expected 302, got %d", path, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/proxy/" {
			t.Errorf("path %q: expected Location: /proxy/, got %q", path, loc)
		}
	}
}
