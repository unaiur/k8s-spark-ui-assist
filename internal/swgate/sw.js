// Service Worker for k8s-spark-ui-assist.
//
// Responsibilities:
//   1. Add ?_sw=1 to all navigation requests so the gate endpoint lets them
//      through without triggering a redirect loop.
//   2. Intercept HTML responses on targeted paths and inject a <script> tag
//      before the first <body> element.
//
// The injected script is always served from INJECT_SCRIPT_URL. The server
// returns 200 with JS content when a script is configured, or 404 when not.
// The SW always injects the tag; if the server returns 404 the browser simply
// reports a failed script load (no functional impact on the page).
const INJECT_SCRIPT_URL = "/proxy/api/spark-inject.js";

// shouldInject returns true for paths where script injection is desired.
// Explicitly targets driver UI pages (/proxy/<appID>/*) and SHS pages
// (/ and /history/*). All other paths, including /proxy/api/*, are excluded.
function shouldInject(pathname) {
  // Always exclude API / SW resources.
  if (pathname.startsWith("/proxy/api/")) return false;

  // Driver UI pages under /proxy/ (non-API).
  if (pathname.startsWith("/proxy/")) return true;

  // SHS root and history pages.
  if (pathname === "/") return true;
  if (pathname.startsWith("/history/")) return true;

  // Do not inject into any other paths.
  return false;
}

self.addEventListener("install", () => {
  // Take control immediately without waiting for old SW to be discarded.
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  // Claim all open clients so pages already loaded are covered.
  event.waitUntil(self.clients.claim());
});

self.addEventListener("fetch", (event) => {
  const req = event.request;

  // Only intercept same-origin GET requests.
  if (req.method !== "GET") return;
  let url;
  try {
    url = new URL(req.url);
  } catch (_) {
    return;
  }
  if (url.origin !== self.location.origin) return;

  if (req.mode === "navigate") {
    // Add bypass param to navigation requests if not already present.
    if (!url.searchParams.has("_sw")) {
      url.searchParams.set("_sw", "1");
      event.respondWith(fetch(new Request(url.toString(), { headers: req.headers })));
      return;
    }
  }

  // For sub-resource requests: fetch normally then inject into HTML if needed.
  event.respondWith(
    fetch(req).then((response) => maybeInject(url, response))
  );
});

async function maybeInject(url, response) {
  if (!shouldInject(url.pathname)) return response;

  const ct = response.headers.get("Content-Type") || "";
  if (!ct.includes("text/html")) return response;

  const text = await response.text();
  // Inject the script tag immediately before the opening <body> tag.
  // There should be exactly one <body> tag in a well-formed HTML document.
  const injected = text.replace(
    /(<body[\s>])/i,
    `<script src="${INJECT_SCRIPT_URL}" defer><\/script>$1`
  );

  // Re-create the response with the modified body.
  // We must rebuild the headers because the original body stream is consumed.
  const headers = new Headers(response.headers);
  // Content-Length is now wrong; remove it so the browser re-measures.
  headers.delete("Content-Length");

  return new Response(injected, {
    status: response.status,
    statusText: response.statusText,
    headers,
  });
}
