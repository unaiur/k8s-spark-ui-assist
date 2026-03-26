// Package swgate implements the Service Worker installation gate and related
// HTTP endpoints under /proxy/api/.
//
// Provided endpoints:
//
//	GET /proxy/api/sw-gate
//	  Serves an HTML page that registers the Service Worker and then redirects
//	  to the URL given by the "return" query parameter (must be a relative
//	  path). If "return" is absent or not a relative path, redirects to
//	  /proxy/.
//
//	GET /proxy/api/sw.js
//	  Serves the Service Worker script with the "Service-Worker-Allowed: /"
//	  header so that the SW can claim the full origin scope, not just /proxy/.
//	  The script always injects /proxy/api/spark-inject.js into targeted HTML
//	  pages; the server decides at runtime whether to serve content or 404.
//
//	GET /proxy/api/spark-inject.js
//	  When InjectScript is non-empty, serves it as JavaScript. When empty,
//	  returns 404. The SW injects a <script src="/proxy/api/spark-inject.js">
//	  tag into every intercepted HTML response on targeted paths; the browser
//	  then fetches this endpoint to load the script.
//
// GateMiddleware wraps an http.Handler and redirects HTML navigation GET
// requests to /proxy/api/sw-gate when the request does not carry the ?_sw=1
// bypass parameter. The middleware only triggers for paths that end with "/",
// ".html", or ".htm", and never for paths under /proxy/api/.
package swgate

import (
	_ "embed"
	"net/http"
	"net/url"
	"strings"
)

//go:embed gate.html
var gateHTML []byte

//go:embed sw.js
var swJS []byte

const (
	gatePath      = "/proxy/api/sw-gate"
	swJSPath      = "/proxy/api/sw.js"
	injectJSPath  = "/proxy/api/spark-inject.js"
	bypassParam   = "_sw"
	apiPrefix     = "/proxy/api/"
	dashboardPath = "/proxy/"
)

// Config holds the swgate configuration.
type Config struct {
	// InjectScript is the JavaScript content served at
	// /proxy/api/spark-inject.js. When empty, that endpoint returns 404 and
	// the browser silently fails to load the injected script tag. EXPERIMENTAL.
	InjectScript string
}

// Handler returns an http.Handler for the three SW endpoints:
//
//	GET /proxy/api/sw-gate
//	GET /proxy/api/sw.js
//	GET /proxy/api/spark-inject.js
//
// It is intended to be registered in the mux before the generic
// /proxy/api/ handler so that Go's longest-prefix routing picks these
// exact paths first.
func Handler(cfg Config) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case gatePath:
			serveGate(w, r)
		case swJSPath:
			serveSWJS(w, r)
		case injectJSPath:
			serveInjectJS(w, r, cfg.InjectScript)
		default:
			http.NotFound(w, r)
		}
	})
}

// GateMiddleware wraps next and redirects HTML navigation GET requests that
// lack the ?_sw=1 bypass parameter to /proxy/api/sw-gate.
//
// A request is considered an HTML navigation when ALL of the following hold:
//   - method is GET
//   - path ends with "/", ".html", or ".htm"
//   - path does not start with /proxy/api/
//   - the ?_sw query parameter is absent
//
// The dashboard path (/proxy/) is included so that the first visit installs
// the SW; after installation the SW adds ?_sw=1 automatically.
func GateMiddleware(cfg Config, next http.Handler) http.Handler {
	_ = cfg // reserved for future per-config gate behaviour
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isGateTarget(r) {
			// Build the gate URL with the original request URI as the return
			// target. Use url.QueryEscape so that any &, =, # in the original
			// query string are safely encoded as a single opaque value.
			returnURI := r.URL.RequestURI()
			// Strip _sw from the return URI so it stays clean.
			returnURI = stripBypassParam(returnURI)
			gateURL := gatePath + "?return=" + url.QueryEscape(returnURI)
			http.Redirect(w, r, gateURL, http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isGateTarget reports whether r should be redirected to the SW gate.
func isGateTarget(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	path := r.URL.Path
	// Never trigger for API paths (includes the gate/SW endpoints themselves).
	if strings.HasPrefix(path, apiPrefix) {
		return false
	}
	// Already has the bypass param — SW is active.
	if r.URL.Query().Has(bypassParam) {
		return false
	}
	// Only HTML navigation paths.
	return isHTMLPath(path)
}

// isHTMLPath reports whether path looks like an HTML document request.
func isHTMLPath(path string) bool {
	if strings.HasSuffix(path, "/") {
		return true
	}
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".html") || strings.HasSuffix(lower, ".htm")
}

// serveGate serves the SW installation page.
func serveGate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ret, err := url.QueryUnescape(r.URL.Query().Get("return"))
	if err != nil || !isRelativePath(ret) {
		ret = dashboardPath
	}
	// Remove ?_sw from the return URL to keep it clean.
	ret = stripBypassParam(ret)

	// If the sanitised return path differs from what the URL encodes, redirect
	// to the canonical gate URL so the browser and gate.html JS see a clean value.
	// We compare the canonical encoding of ret against the raw query string to
	// avoid false positives from different-but-equivalent encodings (e.g.
	// %2F vs %2f, or %2Fproxy%2F vs the decoded /proxy/).
	canonical := gatePath + "?return=" + url.QueryEscape(ret)
	if r.URL.RequestURI() != canonical {
		http.Redirect(w, r, canonical, http.StatusFound)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(gateHTML)
}

// serveSWJS serves the Service Worker script.
func serveSWJS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Allow the SW to claim the full origin scope (/), not just /proxy/.
	w.Header().Set("Service-Worker-Allowed", "/")
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(swJS)
}

// serveInjectJS serves the configured JS content, or 404 if not set.
func serveInjectJS(w http.ResponseWriter, r *http.Request, injectScript string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if injectScript == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(injectScript))
}

// isRelativePath reports whether s is a valid relative URL path
// (starts with "/" but not "//", which would be a protocol-relative URL).
func isRelativePath(s string) bool {
	if s == "" || s[0] != '/' {
		return false
	}
	// Reject protocol-relative URLs (//host/...) to prevent open redirect.
	if len(s) > 1 && s[1] == '/' {
		return false
	}
	return true
}

// stripBypassParam removes the _sw query parameter from a raw request URI
// (path + optional query string), using net/url for correct parsing.
func stripBypassParam(rawURI string) string {
	// Split on ? to separate path from query.
	idx := strings.IndexByte(rawURI, '?')
	if idx < 0 {
		return rawURI
	}
	path := rawURI[:idx]
	q, err := url.ParseQuery(rawURI[idx+1:])
	if err != nil {
		return path
	}
	delete(q, bypassParam)
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}
