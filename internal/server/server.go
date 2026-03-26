// Package server implements the HTTP server that lists active Spark driver UIs.
package server

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

// Ensurer is implemented by httproute.Manager. Defined as an interface here so
// the server package does not import httproute.
type Ensurer interface {
	Ensure(ctx context.Context, d store.Driver)
}

//go:embed templates/index.gohtml
var indexTmplSrc string

//go:embed templates/status.gohtml
var statusTmplSrc string

var indexTmpl = template.Must(template.New("index").Parse(indexTmplSrc))
var statusTmpl = template.Must(template.New("status").Parse(statusTmplSrc))

// driverPathPrefix is the fixed URL path prefix for per-driver links.
// Spark UI requires this exact value to resolve its internal asset paths correctly.
const driverPathPrefix = "/proxy/"

type driverView struct {
	URL        template.URL
	AppName    string
	State      store.DriverState
	StateClass string
	Reason     string
	Duration   string
}

type statusView struct {
	AppName        string
	Message        string
	HistoryURL     string
	RefreshSeconds int
}

// dashboardPath is the canonical URL path for the dashboard page.
const dashboardPath = driverPathPrefix

// Handler returns an http.Handler that serves the Spark driver list dashboard
// and per-app status pages.
//
// Request routing:
//   - GET /proxy/            → dashboard (list of all drivers)
//   - GET /proxy/<appID>/…   → status page for that app (HTTPRoute is absent)
//   - anything else          → 302 redirect to /proxy/
//
// The ensurer is used on the status page when a Running driver is found but its
// HTTPRoute is missing; passing nil disables that recovery path.
func Handler(s *store.Store, now func() time.Time, ensurer Ensurer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Exact dashboard path.
		if path == dashboardPath {
			serveDashboard(w, r, s, now)
			return
		}

		// /proxy/<appID> or /proxy/<appID>/... — status page for that app.
		if strings.HasPrefix(path, driverPathPrefix) {
			rest := strings.TrimPrefix(path, driverPathPrefix)
			// rest is "<appID>" or "<appID>/..."
			appID := rest
			if idx := strings.Index(rest, "/"); idx >= 0 {
				appID = rest[:idx]
			}
			if appID != "" {
				serveProxyStatus(w, r, s, ensurer, appID)
				return
			}
		}

		http.Redirect(w, r, dashboardPath, http.StatusFound)
	})
}

// serveDashboard renders the driver list.
func serveDashboard(w http.ResponseWriter, r *http.Request, s *store.Store, now func() time.Time) {
	drivers := s.List()
	sort.Slice(drivers, func(i, j int) bool {
		return drivers[i].CreatedAt.Before(drivers[j].CreatedAt)
	})

	current := now()
	views := make([]driverView, 0, len(drivers))
	for _, d := range drivers {
		var url template.URL
		switch d.State {
		case store.StateRunning:
			url = template.URL(driverPathPrefix + d.AppSelector + "/jobs/")
		case store.StatePending:
			// Link to status page so users can see why the job isn't running yet.
			url = template.URL(driverPathPrefix + d.AppSelector + "/")
		case store.StateSucceeded, store.StateFailed:
			url = template.URL("/history/" + d.AppSelector + "/jobs/")
		}
		views = append(views, driverView{
			URL:        url,
			AppName:    d.AppName,
			State:      d.State,
			StateClass: stateClass(d.State),
			Reason:     d.Reason,
			Duration:   FormatDuration(current.Sub(d.CreatedAt)),
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTmpl.Execute(w, views); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// serveProxyStatus handles a request that arrived at /proxy/<appID>/… — meaning
// the HTTPRoute for appID is absent (otherwise the gateway would have proxied
// the request to the Spark UI directly). We look up the driver in the store and
// show a contextual message.
func serveProxyStatus(w http.ResponseWriter, r *http.Request, s *store.Store, ensurer Ensurer, appID string) {
	// Validate appID as a Kubernetes label value before using it in URLs or
	// store lookups; this prevents XSS/injection via a crafted path segment.
	if errs := validation.IsValidLabelValue(appID); len(errs) > 0 {
		http.Error(w, "invalid app ID", http.StatusBadRequest)
		return
	}

	historyURL := "/history/" + url.PathEscape(appID) + "/jobs/"

	d, found := s.FindBySelector(appID)

	var view statusView
	switch {
	case !found:
		view = statusView{
			AppName: appID,
			Message: "No driver pod found for this Spark job in Kubernetes. " +
				"The job may have completed and been purged.",
			HistoryURL: historyURL,
		}

	case d.State == store.StatePending:
		msg := "The Spark job is starting up and is not yet running."
		if d.Reason != "" {
			msg = "The Spark job is starting up: " + d.Reason + "."
		}
		view = statusView{
			AppName:        d.AppName,
			Message:        msg,
			RefreshSeconds: 10,
		}

	case d.State == store.StateRunning:
		// HTTPRoute is missing despite the pod being Running — trigger Ensure and
		// ask the browser to retry shortly.
		if ensurer != nil {
			ensurer.Ensure(r.Context(), d)
		}
		view = statusView{
			AppName:        d.AppName,
			Message:        "The Spark job is running and the connection is being configured.",
			RefreshSeconds: 5,
		}

	case d.State == store.StateFailed:
		view = statusView{
			AppName:    d.AppName,
			Message:    "The Spark job has failed.",
			HistoryURL: historyURL,
		}

	case d.State == store.StateSucceeded:
		view = statusView{
			AppName:    d.AppName,
			Message:    "The Spark job has completed successfully.",
			HistoryURL: historyURL,
		}

	default:
		view = statusView{
			AppName: d.AppName,
			Message: "The Spark job state is unknown.",
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := statusTmpl.Execute(w, view); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// stateClass returns the CSS class suffix used to colour the state badge.
func stateClass(s store.DriverState) string {
	switch s {
	case store.StateRunning:
		return "running"
	case store.StatePending:
		return "pending"
	case store.StateSucceeded:
		return "succeeded"
	case store.StateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// FormatDuration formats a duration as [N day(s) ]HH:MM:SS.
// The days component is omitted when the duration is less than 24 hours.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int(d.Seconds())
	seconds := totalSeconds % 60
	minutes := (totalSeconds / 60) % 60
	hours := (totalSeconds / 3600) % 24
	days := totalSeconds / 86400

	if days == 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
	}
	if days == 1 {
		return fmt.Sprintf("1 day %02d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d days %02d:%02d:%02d", days, hours, minutes, seconds)
}
