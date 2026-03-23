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
<head><meta charset="utf-8"><title>Spark UIs</title></head>
<body>
<h1>Running Spark Jobs</h1>
<ul>
{{- range .}}
<li><a href="/live/{{.AppSelector}}/">{{.AppName}}</a> (running for {{.Duration}})</li>
{{- end}}
</ul>
</body>
</html>
`))

type driverView struct {
	AppSelector string
	AppName     string
	Duration    string
}

// Handler returns an http.Handler that serves the Spark driver list.
// Requests whose URL path is not exactly "/" are redirected to "/" so that
// relative links in the page resolve correctly regardless of how the gateway
// routes traffic to this handler.
func Handler(s *store.Store, now func() time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.Redirect(w, r, "/", http.StatusMovedPermanently)
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
			views = append(views, driverView{
				AppSelector: d.AppSelector,
				AppName:     d.AppName,
				Duration:    FormatDuration(current.Sub(d.CreatedAt)),
			})
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := indexTmpl.Execute(w, views); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	})
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
