package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

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

// staticSHSState is a test-only SHSState that returns a fixed value.
type staticSHSState struct{ up bool }

func (s *staticSHSState) IsUp() bool { return s.up }

// newFakeDynClient returns a fake dynamic client that has the apps/v1 Deployment scheme registered.
func newFakeDynClient(objs ...runtime.Object) dynamic.Interface {
	scheme := runtime.NewScheme()
	// Register the apps/v1 Deployment GVK so the fake client can handle it.
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}, &unstructured.Unstructured{})
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DeploymentList"}, &unstructured.UnstructuredList{})
	return fake.NewSimpleDynamicClient(scheme, objs...)
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
	Handler(s, fixedNow, nil, SHSConfig{}).ServeHTTP(rec, req)

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
	Handler(s, fixedNow, nil, SHSConfig{}).ServeHTTP(rec, req)

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
	Handler(s, fixedNow, nil, SHSConfig{}).ServeHTTP(rec, req)

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
	Handler(s, fixedNow, nil, SHSConfig{}).ServeHTTP(rec, req)

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
	Handler(s, fixedNow, nil, SHSConfig{}).ServeHTTP(rec, req)

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
		Handler(s, fixedNow, nil, SHSConfig{}).ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Errorf("path %q: expected 302, got %d", path, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/proxy/" {
			t.Errorf("path %q: expected Location: /proxy/, got %q", path, loc)
		}
	}
}

// ---- Dashboard SHS button tests ---------------------------------------------

// TestDashboardSHSButtonAbsentWhenNotConfigured verifies no SHS link is shown
// when SHSConfig has no Deployment set.
func TestDashboardSHSButtonAbsentWhenNotConfigured(t *testing.T) {
	s := newStore()
	req := httptest.NewRequest(http.MethodGet, "/proxy/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow, nil, SHSConfig{}).ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "Spark History Server") {
		t.Errorf("expected no SHS link when SHSConfig is empty, got:\n%s", body)
	}
}

// TestDashboardSHSButtonLinksToRootWhenUp verifies the SHS link goes to "/"
// when SHS is up.
func TestDashboardSHSButtonLinksToRootWhenUp(t *testing.T) {
	s := newStore()
	shsCfg := SHSConfig{
		Deployment: "spark-history",
		Namespace:  "default",
		State:      &staticSHSState{up: true},
	}
	req := httptest.NewRequest(http.MethodGet, "/proxy/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow, nil, shsCfg).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `href="/"`) {
		t.Errorf("expected SHS link to / when SHS is up, got:\n%s", body)
	}
	if !strings.Contains(body, "Spark History Server") {
		t.Errorf("expected 'Spark History Server' text in body, got:\n%s", body)
	}
}

// TestDashboardSHSButtonLinksToWakeWhenDown verifies the SHS link goes to the
// wake endpoint when SHS is down.
func TestDashboardSHSButtonLinksToWakeWhenDown(t *testing.T) {
	s := newStore()
	shsCfg := SHSConfig{
		Deployment: "spark-history",
		Namespace:  "default",
		State:      &staticSHSState{up: false},
	}
	req := httptest.NewRequest(http.MethodGet, "/proxy/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow, nil, shsCfg).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, wakePath) {
		t.Errorf("expected wake path %q in SHS link when SHS is down, got:\n%s", wakePath, body)
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
	Handler(s, fixedNow, nil, SHSConfig{}).ServeHTTP(rec, req)

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
	if !strings.Contains(body, `id="countdown"`) {
		t.Errorf("expected countdown span in body, got:\n%s", body)
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
	Handler(s, fixedNow, nil, SHSConfig{}).ServeHTTP(rec, req)

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
	Handler(s, fixedNow, ensurer, SHSConfig{}).ServeHTTP(rec, req)

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
	if !strings.Contains(body, `id="countdown"`) {
		t.Errorf("expected countdown span in body, got:\n%s", body)
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
	Handler(s, fixedNow, nil, SHSConfig{}).ServeHTTP(rec, req)

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
	Handler(s, fixedNow, nil, SHSConfig{}).ServeHTTP(rec, req)

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
	Handler(s, fixedNow, nil, SHSConfig{}).ServeHTTP(rec, req)

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
	Handler(s, fixedNow, nil, SHSConfig{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

// ---- SHS-aware status page tests -------------------------------------------

// TestProxyStatusSHSDownShowsWakeLink verifies that when SHS is configured and
// down, the status page for a finished job shows a wake link instead of the
// history link.
func TestProxyStatusSHSDownShowsWakeLink(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-1",
		AppSelector: "spark-abc",
		AppName:     "my-job",
		State:       store.StateSucceeded,
	})
	shsCfg := SHSConfig{
		Deployment: "spark-history",
		Namespace:  "default",
		State:      &staticSHSState{up: false},
	}

	req := httptest.NewRequest(http.MethodGet, "/proxy/spark-abc/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow, nil, shsCfg).ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "/history/spark-abc/jobs/") {
		t.Errorf("SHS down: expected NO history link, got:\n%s", body)
	}
	if !strings.Contains(body, wakePath) {
		t.Errorf("SHS down: expected wake path %q in body, got:\n%s", wakePath, body)
	}
}

// TestProxyStatusSHSUpShowsHistoryLink verifies that when SHS is configured and
// up, the status page shows the normal history link.
func TestProxyStatusSHSUpShowsHistoryLink(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-1",
		AppSelector: "spark-abc",
		AppName:     "my-job",
		State:       store.StateSucceeded,
	})
	shsCfg := SHSConfig{
		Deployment: "spark-history",
		Namespace:  "default",
		State:      &staticSHSState{up: true},
	}

	req := httptest.NewRequest(http.MethodGet, "/proxy/spark-abc/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow, nil, shsCfg).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "/history/spark-abc/jobs/") {
		t.Errorf("SHS up: expected history link in body, got:\n%s", body)
	}
	if strings.Contains(body, wakePath) {
		t.Errorf("SHS up: expected NO wake link in body, got:\n%s", body)
	}
}

// TestProxyStatusSHSNotConfiguredShowsHistoryLink verifies that when SHS is not
// configured, the status page always shows the history link.
func TestProxyStatusSHSNotConfiguredShowsHistoryLink(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-1",
		AppSelector: "spark-abc",
		AppName:     "my-job",
		State:       store.StateFailed,
	})

	req := httptest.NewRequest(http.MethodGet, "/proxy/spark-abc/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow, nil, SHSConfig{}).ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "/history/spark-abc/jobs/") {
		t.Errorf("SHS not configured: expected history link in body, got:\n%s", body)
	}
}

// ---- Wake endpoint tests ----------------------------------------------------

// TestWakeHandlerMethodNotAllowed verifies that unsupported methods (e.g. PUT)
// to the wake endpoint return 405.
func TestWakeHandlerMethodNotAllowed(t *testing.T) {
	shsCfg := SHSConfig{
		Deployment: "spark-history",
		Namespace:  "default",
		State:      &staticSHSState{up: false},
		Client:     newFakeDynClient(),
	}
	req := httptest.NewRequest(http.MethodPut, wakePath, nil)
	rec := httptest.NewRecorder()
	WakeHandler(shsCfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}

// TestWakeHandlerNotConfigured verifies that the wake endpoint returns 501 when
// SHSConfig has no Deployment.
func TestWakeHandlerNotConfigured(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, wakePath, nil)
	rec := httptest.NewRecorder()
	WakeHandler(SHSConfig{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("expected 501, got %d", rec.Code)
	}
}

// TestWakeHandlerSHSAlreadyUpRedirects verifies that when SHS is already up,
// the wake endpoint immediately redirects to the ?then= URL.
func TestWakeHandlerSHSAlreadyUpRedirects(t *testing.T) {
	shsCfg := SHSConfig{
		Deployment: "spark-history",
		Namespace:  "default",
		State:      &staticSHSState{up: true},
		Client:     newFakeDynClient(),
	}
	req := httptest.NewRequest(http.MethodPost, wakePath+"?then=/history/spark-abc/jobs/", nil)
	rec := httptest.NewRecorder()
	WakeHandler(shsCfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/history/spark-abc/jobs/" {
		t.Errorf("expected redirect to /history/spark-abc/jobs/, got %q", loc)
	}
}

// TestWakeHandlerPatchesDeploymentAndRedirects verifies that when SHS is
// down, a POST patches the Deployment and 303-redirects to the GET polling URL.
func TestWakeHandlerPatchesDeploymentAndRedirects(t *testing.T) {
	// Create a fake Deployment object.
	dep := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "spark-history",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"replicas": int64(0),
			},
		},
	}
	fc := newFakeDynClient(dep)
	shsCfg := SHSConfig{
		Deployment: "spark-history",
		Namespace:  "default",
		State:      &staticSHSState{up: false},
		Client:     fc,
	}

	req := httptest.NewRequest(http.MethodPost, wakePath+"?then=/history/spark-abc/jobs/", nil)
	rec := httptest.NewRecorder()
	WakeHandler(shsCfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected 303 See Other, got %d\nbody: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, wakePath) {
		t.Errorf("expected redirect to polling URL containing %q, got %q", wakePath, loc)
	}

	// Verify a Patch action was recorded.
	fakeDyn := fc.(*fake.FakeDynamicClient)
	var patched bool
	for _, action := range fakeDyn.Actions() {
		pa, ok := action.(k8stesting.PatchAction)
		if !ok {
			continue
		}
		if pa.GetResource() == (schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}) &&
			pa.GetName() == "spark-history" &&
			pa.GetPatchType() == types.MergePatchType {
			patched = true
		}
	}
	if !patched {
		t.Errorf("expected a MergePatch action on deployments/spark-history, got actions: %v", fakeDyn.Actions())
	}
}

// TestWakeHandlerGETSHSDown verifies that a GET to the wake endpoint when SHS
// is still down renders the waiting page.
func TestWakeHandlerGETSHSDown(t *testing.T) {
	shsCfg := SHSConfig{
		Deployment: "spark-history",
		Namespace:  "default",
		State:      &staticSHSState{up: false},
		Client:     newFakeDynClient(),
	}
	req := httptest.NewRequest(http.MethodGet, wakePath+"?then=/history/spark-abc/jobs/", nil)
	rec := httptest.NewRecorder()
	WakeHandler(shsCfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Starting Spark History Server") {
		t.Errorf("expected waiting page title in body, got:\n%s", body)
	}
	if !strings.Contains(body, "countdown") {
		t.Errorf("expected countdown element in body, got:\n%s", body)
	}
	// Meta refresh must target the polling URL, not be bare.
	if !strings.Contains(body, ";url=") {
		t.Errorf("expected meta refresh url= in body, got:\n%s", body)
	}
}

// TestWakeHandlerGETSHSUp verifies that a GET to the wake endpoint when SHS
// is up redirects to the ?then= URL.
func TestWakeHandlerGETSHSUp(t *testing.T) {
	shsCfg := SHSConfig{
		Deployment: "spark-history",
		Namespace:  "default",
		State:      &staticSHSState{up: true},
		Client:     newFakeDynClient(),
	}
	req := httptest.NewRequest(http.MethodGet, wakePath+"?then=/history/spark-abc/jobs/", nil)
	rec := httptest.NewRecorder()
	WakeHandler(shsCfg).ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/history/spark-abc/jobs/" {
		t.Errorf("expected redirect to /history/spark-abc/jobs/, got %q", loc)
	}
}

// TestWakeHandlerOpenRedirectRejected verifies that an absolute ?then= URL is
// rejected and falls back to the dashboard path.
func TestWakeHandlerOpenRedirectRejected(t *testing.T) {
	shsCfg := SHSConfig{
		Deployment: "spark-history",
		Namespace:  "default",
		State:      &staticSHSState{up: true},
		Client:     newFakeDynClient(),
	}
	req := httptest.NewRequest(http.MethodPost, wakePath+"?then=https://evil.example.com/", nil)
	rec := httptest.NewRecorder()
	WakeHandler(shsCfg).ServeHTTP(rec, req)

	// SHS is up, so we get a redirect — but to the safe fallback, not the attacker URL.
	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if loc == "https://evil.example.com/" {
		t.Errorf("open redirect: Location should not be the attacker URL, got %q", loc)
	}
	if loc != dashboardPath {
		t.Errorf("expected fallback to dashboardPath %q, got %q", dashboardPath, loc)
	}
}
