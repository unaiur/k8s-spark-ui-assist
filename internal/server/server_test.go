package server

import (
	"context"
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

// recordingEnsurer records calls to Ensure for assertion in tests.
type recordingEnsurer struct {
	ensured []store.Driver
}

func (e *recordingEnsurer) Ensure(_ context.Context, d store.Driver) {
	e.ensured = append(e.ensured, d)
}

// ---- Dashboard tests --------------------------------------------------------

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
	Handler(s, fixedNow, nil).ServeHTTP(rec, req)

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

// TestHandlerDashboardPendingDriver checks that a Pending driver links to its
// status page and shows a grey badge.
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
	Handler(s, fixedNow, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	body := rec.Body.String()
	// Pending driver should link to its status page, not the Spark UI.
	if !strings.Contains(body, "/proxy/spark-xyz/") {
		t.Errorf("pending driver should link to status page /proxy/spark-xyz/, got:\n%s", body)
	}
	if strings.Contains(body, "/proxy/spark-xyz/jobs/") {
		t.Errorf("pending driver should NOT have a /jobs/ link, got:\n%s", body)
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
	Handler(s, fixedNow, nil).ServeHTTP(rec, req)

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
	Handler(s, fixedNow, nil).ServeHTTP(rec, req)

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
	Handler(s, fixedNow, nil).ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "title=") {
		t.Errorf("expected no title attribute when Reason is empty, got:\n%s", body)
	}
}

// TestHandlerNonProxyRedirects checks that paths that are not the dashboard and
// not a /proxy/<appID>/… path get a 302 redirect to "/proxy/".
func TestHandlerNonProxyRedirects(t *testing.T) {
	paths := []string{"/", "/foo", "/anything"}
	s := newStore()

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		Handler(s, fixedNow, nil).ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Errorf("path %q: expected 302, got %d", path, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/proxy/" {
			t.Errorf("path %q: expected Location: /proxy/, got %q", path, loc)
		}
	}
}

// ---- Proxy status page tests ------------------------------------------------

// TestProxyStatusPendingShowsMessageAndRefresh verifies that hitting
// /proxy/<appID>/ when the driver is Pending shows a starting-up message and
// a 10-second auto-refresh meta tag.
func TestProxyStatusPendingShowsMessageAndRefresh(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-1",
		AppSelector: "spark-abc",
		AppName:     "my-job",
		State:       store.StatePending,
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/spark-abc/jobs/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "starting up") {
		t.Errorf("expected 'starting up' in body, got:\n%s", body)
	}
	if !strings.Contains(body, `content="10"`) {
		t.Errorf("expected 10-second refresh meta tag, got:\n%s", body)
	}
}

// TestProxyStatusPendingWithReasonIncludesReason verifies that the reason is
// included in the status message when set.
func TestProxyStatusPendingWithReasonIncludesReason(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-1",
		AppSelector: "spark-abc",
		AppName:     "my-job",
		State:       store.StatePending,
		Reason:      "Cannot pull the image",
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/spark-abc/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow, nil).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "Cannot pull the image") {
		t.Errorf("expected reason in body, got:\n%s", body)
	}
}

// TestProxyStatusRunningTriggersEnsureAndRefresh verifies that hitting the
// status page for a Running driver calls Ensure and shows a 3-second refresh.
func TestProxyStatusRunningTriggersEnsureAndRefresh(t *testing.T) {
	d := store.Driver{
		PodName:     "pod-1",
		AppSelector: "spark-abc",
		AppName:     "my-job",
		State:       store.StateRunning,
	}
	s := newStore(d)
	ensurer := &recordingEnsurer{}

	req := httptest.NewRequest(http.MethodGet, "/proxy/spark-abc/jobs/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow, ensurer).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "being configured") {
		t.Errorf("expected 'being configured' in body, got:\n%s", body)
	}
	if !strings.Contains(body, `content="5"`) {
		t.Errorf("expected 5-second refresh meta tag, got:\n%s", body)
	}
	if len(ensurer.ensured) != 1 || ensurer.ensured[0].AppSelector != "spark-abc" {
		t.Errorf("expected Ensure called once for spark-abc, got %v", ensurer.ensured)
	}
}

// TestProxyStatusFailedShowsHistoryLinkNoRefresh verifies that a Failed driver
// shows a history link and no auto-refresh.
func TestProxyStatusFailedShowsHistoryLinkNoRefresh(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-1",
		AppSelector: "spark-abc",
		AppName:     "my-job",
		State:       store.StateFailed,
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/spark-abc/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow, nil).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "/history/spark-abc/jobs/") {
		t.Errorf("expected history link in body, got:\n%s", body)
	}
	if strings.Contains(body, `http-equiv="refresh"`) {
		t.Errorf("failed driver should NOT have auto-refresh, got:\n%s", body)
	}
	if !strings.Contains(body, "failed") {
		t.Errorf("expected 'failed' in body, got:\n%s", body)
	}
}

// TestProxyStatusSucceededShowsHistoryLinkNoRefresh verifies that a Succeeded
// driver shows a history link and no auto-refresh.
func TestProxyStatusSucceededShowsHistoryLinkNoRefresh(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-1",
		AppSelector: "spark-abc",
		AppName:     "my-job",
		State:       store.StateSucceeded,
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/spark-abc/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow, nil).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "/history/spark-abc/jobs/") {
		t.Errorf("expected history link in body, got:\n%s", body)
	}
	if strings.Contains(body, `http-equiv="refresh"`) {
		t.Errorf("succeeded driver should NOT have auto-refresh, got:\n%s", body)
	}
	if !strings.Contains(body, "completed") {
		t.Errorf("expected 'completed' in body, got:\n%s", body)
	}
}

// TestProxyStatusMissingPodShowsHistoryLink verifies that when no driver is
// found in the store, the status page mentions the pod is missing and offers
// a history link.
func TestProxyStatusMissingPodShowsHistoryLink(t *testing.T) {
	s := newStore() // empty store

	req := httptest.NewRequest(http.MethodGet, "/proxy/spark-gone/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/history/spark-gone/jobs/") {
		t.Errorf("expected history link in body, got:\n%s", body)
	}
	if !strings.Contains(body, "purged") {
		t.Errorf("expected 'purged' mention in body, got:\n%s", body)
	}
}

// TestProxyStatusRunningNilEnsurerIsSafe verifies that passing nil for the
// ensurer on a Running driver does not panic.
func TestProxyStatusRunningNilEnsurerIsSafe(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-1",
		AppSelector: "spark-abc",
		AppName:     "my-job",
		State:       store.StateRunning,
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/spark-abc/", nil)
	rec := httptest.NewRecorder()
	// Should not panic.
	Handler(s, fixedNow, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
