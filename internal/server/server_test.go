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

// TestHandlerRootServesPage checks that GET "/" returns 200 and the driver list
// with links using the configured prefix.
func TestHandlerRootServesPage(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-1",
		AppSelector: "spark-abc",
		AppName:     "my-job",
		CreatedAt:   fixedNow().Add(-time.Hour),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow, "/proxy/").ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "/proxy/spark-abc/jobs/") {
		t.Errorf("expected driver link with /proxy/ prefix in body, got:\n%s", body)
	}
}

// TestHandlerCustomPrefix checks that a non-default prefix is used in the links.
func TestHandlerCustomPrefix(t *testing.T) {
	s := newStore(store.Driver{
		PodName:     "pod-1",
		AppSelector: "spark-abc",
		AppName:     "my-job",
		CreatedAt:   fixedNow().Add(-time.Hour),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	Handler(s, fixedNow, "/live/").ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "/live/spark-abc/jobs/") {
		t.Errorf("expected driver link with /live/ prefix in body, got:\n%s", body)
	}
	if strings.Contains(body, "/proxy/") {
		t.Errorf("unexpected /proxy/ prefix in body when /live/ was configured")
	}
}

// TestHandlerNonRootRedirects checks that any path other than "/" gets a
// 302 redirect to "/".
func TestHandlerNonRootRedirects(t *testing.T) {
	paths := []string{"/foo", "/proxy/spark-abc/", "/anything"}
	s := newStore()

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		Handler(s, fixedNow, "/proxy/").ServeHTTP(rec, req)

		if rec.Code != http.StatusFound {
			t.Errorf("path %q: expected 302, got %d", path, rec.Code)
		}
		if loc := rec.Header().Get("Location"); loc != "/" {
			t.Errorf("path %q: expected Location: /, got %q", path, loc)
		}
	}
}
