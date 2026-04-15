// Package server implements the HTTP server that lists active Spark driver UIs.
package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"

	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

// Ensurer is implemented by httproute.Manager. Defined as an interface here so
// the server package does not import httproute.
type Ensurer interface {
	Ensure(ctx context.Context, d store.Driver)
}

// SHSState reports current SHS availability.
type SHSState interface {
	IsUp() bool
}

// SHSConfig holds the configuration needed to wake the Spark History Server.
// It is optional; pass the zero value (nil fields) to disable SHS features.
type SHSConfig struct {
	// Deployment is the name of the Kubernetes Deployment to patch when waking.
	Deployment string
	// Namespace is the Kubernetes namespace of the Deployment.
	Namespace string
	// State reports current SHS endpoint availability.
	State SHSState
	// Client is the dynamic Kubernetes client used to patch the Deployment.
	Client dynamic.Interface
}

//go:embed templates/index.gohtml
var indexTmplSrc string

//go:embed templates/status.gohtml
var statusTmplSrc string

//go:embed templates/wake.gohtml
var wakeTmplSrc string

var indexTmpl = template.Must(template.New("index").Parse(indexTmplSrc))
var statusTmpl = template.Must(template.New("status").Parse(statusTmplSrc))
var wakeTmpl = template.Must(template.New("wake").Parse(wakeTmplSrc))

// driverPathPrefix is the fixed URL path prefix for per-driver links.
// Spark UI requires this exact value to resolve its internal asset paths correctly.
const driverPathPrefix = "/proxy/"

// wakePath is the URL path for the SHS wake endpoint.
const wakePath = "/proxy/api/shs/wake"

type driverView struct {
	URL        template.URL
	AppName    string
	State      store.DriverState
	StateClass string
	Reason     string
	Duration   string
}

type indexView struct {
	Drivers []driverView
	// SHSURL is the URL for the Spark History Server link shown on the dashboard.
	// Empty when SHSConfig is not configured.
	SHSURL template.URL
}

type statusView struct {
	AppName        string
	Message        string
	HistoryURL     string
	WakeURL        template.URL
	RefreshSeconds int
}

// dashboardPath is the canonical URL path for the dashboard page.
const dashboardPath = driverPathPrefix

// WakeHandler returns an http.Handler that handles POST /proxy/api/shs/wake.
// It is intended to be registered at the exact wake path in the HTTP mux.
func WakeHandler(shsCfg SHSConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveWake(w, r, shsCfg)
	})
}

// Handler returns an http.Handler that serves the Spark driver list dashboard
// and per-app status pages.
//
// Request routing:
//   - GET /proxy/            → dashboard (list of all drivers)
//   - POST /proxy/api/shs/wake → patch SHS Deployment to 1 replica, return waiting page
//   - GET /proxy/<appID>/…   → status page for that app (HTTPRoute is absent)
//   - anything else          → 302 redirect to /proxy/
//
// The ensurer is used on the status page when a Running driver is found but its
// HTTPRoute is missing; passing nil disables that recovery path.
// shsCfg controls SHS wake integration; pass zero SHSConfig to disable.
func Handler(s *store.Store, now func() time.Time, ensurer Ensurer, shsCfg SHSConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// SHS wake endpoint.
		if path == wakePath {
			serveWake(w, r, shsCfg)
			return
		}

		// Exact dashboard path.
		if path == dashboardPath {
			serveDashboard(w, r, s, now, shsCfg)
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
				serveProxyStatus(w, r, s, ensurer, shsCfg, appID)
				return
			}
		}

		http.Redirect(w, r, dashboardPath, http.StatusFound)
	})
}

// serveDashboard renders the driver list.
func serveDashboard(w http.ResponseWriter, r *http.Request, s *store.Store, now func() time.Time, shsCfg SHSConfig) {
	drivers := s.List()
	sort.Slice(drivers, func(i, j int) bool {
		return drivers[i].CreatedAt.Before(drivers[j].CreatedAt)
	})

	current := now()
	views := make([]driverView, 0, len(drivers))
	for _, d := range drivers {
		var u template.URL
		switch d.State {
		case store.StateRunning:
			u = template.URL(driverPathPrefix + d.AppSelector + "/jobs/")
		case store.StatePending:
			// Link to status page so users can see why the job isn't running yet.
			u = template.URL(driverPathPrefix + d.AppSelector + "/")
		case store.StateSucceeded, store.StateFailed:
			u = template.URL("/history/" + d.AppSelector + "/jobs/")
		}
		views = append(views, driverView{
			URL:        u,
			AppName:    d.AppName,
			State:      d.State,
			StateClass: stateClass(d.State),
			Reason:     d.Reason,
			Duration:   FormatDuration(current.Sub(d.CreatedAt)),
		})
	}

	iv := indexView{Drivers: views}
	if shsCfg.Deployment != "" {
		if shsCfg.State != nil && shsCfg.State.IsUp() {
			iv.SHSURL = "/"
		} else {
			iv.SHSURL = template.URL(wakePath + "?then=/")
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTmpl.Execute(w, iv); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// serveProxyStatus handles a request that arrived at /proxy/<appID>/… — meaning
// the HTTPRoute for appID is absent (otherwise the gateway would have proxied
// the request to the Spark UI directly). We look up the driver in the store and
// show a contextual message.
func serveProxyStatus(w http.ResponseWriter, r *http.Request, s *store.Store, ensurer Ensurer, shsCfg SHSConfig, appID string) {
	// Validate appID as a Kubernetes label value before using it in URLs or
	// store lookups; this prevents XSS/injection via a crafted path segment.
	if errs := validation.IsValidLabelValue(appID); len(errs) > 0 {
		http.Error(w, "invalid app ID", http.StatusBadRequest)
		return
	}

	historyURL := "/history/" + url.PathEscape(appID) + "/jobs/"

	// wakeURLFor returns the wake URL that redirects to the history page for appID
	// after SHS becomes ready.  Used when SHS is configured but down.
	wakeURLFor := func() template.URL {
		then := "/history/" + url.PathEscape(appID) + "/jobs/"
		return template.URL(wakePath + "?then=" + url.QueryEscape(then))
	}

	// historyOrWake picks HistoryURL vs WakeURL based on SHS state.
	type historyOrWake struct {
		HistoryURL string
		WakeURL    template.URL
	}
	shsLink := func() historyOrWake {
		if shsCfg.Deployment != "" && shsCfg.State != nil && !shsCfg.State.IsUp() {
			return historyOrWake{WakeURL: wakeURLFor()}
		}
		return historyOrWake{HistoryURL: historyURL}
	}

	d, found := s.FindBySelector(appID)

	var view statusView
	switch {
	case !found:
		hw := shsLink()
		view = statusView{
			AppName: appID,
			Message: "No driver pod found for this Spark job in Kubernetes. " +
				"The job may have completed and been purged.",
			HistoryURL: hw.HistoryURL,
			WakeURL:    hw.WakeURL,
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
		hw := shsLink()
		view = statusView{
			AppName:    d.AppName,
			Message:    "The Spark job has failed.",
			HistoryURL: hw.HistoryURL,
			WakeURL:    hw.WakeURL,
		}

	case d.State == store.StateSucceeded:
		hw := shsLink()
		view = statusView{
			AppName:    d.AppName,
			Message:    "The Spark job has completed successfully.",
			HistoryURL: hw.HistoryURL,
			WakeURL:    hw.WakeURL,
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

// deploymentGVR is the GroupVersionResource for Kubernetes Deployments.
var deploymentGVR = schema.GroupVersionResource{
	Group:    "apps",
	Version:  "v1",
	Resource: "deployments",
}

// serveWake handles POST /proxy/api/shs/wake?then=<target>.
// It patches the SHS Deployment to 1 replica and returns a waiting page.
// When the SHS is already up it redirects directly to ?then=.
func serveWake(w http.ResponseWriter, r *http.Request, shsCfg SHSConfig) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if shsCfg.Deployment == "" || shsCfg.Client == nil {
		http.Error(w, "SHS wake not configured", http.StatusNotImplemented)
		return
	}

	then := r.URL.Query().Get("then")
	if then == "" {
		then = "/"
	}

	// If SHS is already up, redirect immediately.
	if shsCfg.State != nil && shsCfg.State.IsUp() {
		http.Redirect(w, r, then, http.StatusFound)
		return
	}

	// Patch the Deployment: set replicas to 1.
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"replicas": int64(1),
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	_, err = shsCfg.Client.Resource(deploymentGVR).Namespace(shsCfg.Namespace).
		Patch(r.Context(), shsCfg.Deployment, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	if err != nil {
		log.Printf("shs wake: failed to patch deployment %q: %v", shsCfg.Deployment, err)
		http.Error(w, "failed to wake SHS: "+err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("shs wake: patched deployment %q to 1 replica", shsCfg.Deployment)

	// Return the waiting page.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := wakeTmpl.Execute(w, map[string]interface{}{
		"Then":           then,
		"WakePath":       wakePath,
		"RefreshSeconds": 5,
	}); err != nil {
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
