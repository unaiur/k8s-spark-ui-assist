// Package server implements the HTTP server that lists active Spark driver UIs.
package server

import (
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"time"

	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

var indexTmpl = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>Spark UIs</title>
<style>
  body { font-family: sans-serif; margin: 2em; }
  table { border-collapse: collapse; width: 100%; }
  th, td { text-align: left; padding: 8px 12px; border-bottom: 1px solid #ddd; }
  th { background: #f4f4f4; }
  .badge {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 4px;
    font-size: 0.85em;
    font-weight: bold;
    color: #fff;
  }
  .badge-running  { background: #2e7d32; }
  .badge-pending  { background: #757575; }
  .badge-unknown  { background: #e65100; }
</style>
</head>
<body>
<h1>Spark Jobs</h1>
<table>
  <thead>
    <tr><th>Job</th><th>State</th><th>Age</th></tr>
  </thead>
  <tbody>
  {{- range .}}
  <tr>
    <td>{{if .URL}}<a href="{{.URL}}">{{.AppName}}</a>{{else}}{{.AppName}}{{end}}</td>
    <td><span class="badge badge-{{.StateClass}}">{{.State}}</span></td>
    <td>{{.Duration}}</td>
  </tr>
  {{- end}}
  </tbody>
</table>
</body>
</html>
`))

// driverPathPrefix is the fixed URL path prefix for per-driver links.
// Spark UI requires this exact value to resolve its internal asset paths correctly.
const driverPathPrefix = "/proxy/"

type driverView struct {
	URL        template.URL
	AppName    string
	State      store.DriverState
	StateClass string
	Duration   string
}

// dashboardPath is the canonical URL path for the dashboard page.
// All other paths redirect here.
const dashboardPath = driverPathPrefix

// Handler returns an http.Handler that serves the Spark driver list.
// The dashboard is served at /proxy/ (the fixed driverPathPrefix), which is
// treated as the canonical URL for the dashboard. Any request whose path is
// not exactly "/proxy/" is redirected there with a 302 to keep a single,
// stable entry point regardless of how the gateway routes traffic to this handler.
func Handler(s *store.Store, now func() time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != dashboardPath {
			http.Redirect(w, r, dashboardPath, http.StatusFound)
			return
		}

		drivers := s.List()
		// Sort by creation time for stable output.
		sort.Slice(drivers, func(i, j int) bool {
			return drivers[i].CreatedAt.Before(drivers[j].CreatedAt)
		})

		current := now()
		views := make([]driverView, 0, len(drivers))
		for _, d := range drivers {
			var url template.URL
			if d.State == store.StateRunning {
				url = template.URL(driverPathPrefix + d.AppSelector + "/jobs/")
			}
			views = append(views, driverView{
				URL:        url,
				AppName:    d.AppName,
				State:      d.State,
				StateClass: stateClass(d.State),
				Duration:   FormatDuration(current.Sub(d.CreatedAt)),
			})
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := indexTmpl.Execute(w, views); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	})
}

// stateClass returns the CSS class suffix used to colour the state badge.
func stateClass(s store.DriverState) string {
	switch s {
	case store.StateRunning:
		return "running"
	case store.StatePending:
		return "pending"
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
