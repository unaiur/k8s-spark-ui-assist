# Design

This document describes the internal design of `k8s-spark-ui-assist`.

## Goals

- Minimal memory footprint: no unnecessary caching, no heavy frameworks.
- Correctness under concurrent load: the pod watcher and the HTTP server run in separate
  goroutines and share state safely.
- Consistent code style: `gofmt` compliance is enforced at build time.
- Solid business logic: every non-trivial package has unit tests.

## Pod detection

A pod is considered a Spark driver when it carries **both** of these labels:

| Label | Required value |
|---|---|
| `app.kubernetes.io/instance` | `spark-job` |
| `spark-role` | `driver` |

The following labels are read from each matching pod:

| Label | Stored as |
|---|---|
| `spark-app-selector` | `Driver.AppSelector` — used in URLs and HTTPRoute rule paths |
| `spark-app-name` | `Driver.AppName` — shown in the web UI |

A pod is considered terminated (and removed from the list) when its phase is
`Succeeded` or `Failed`.

## Package structure

```
cmd/spark-ui-assist/   main — wires all components, handles shutdown
internal/
  config/              flag parsing and validation
  store/               thread-safe in-memory driver map
  watcher/             Kubernetes informer for Spark driver pods
  httproute/           Gateway API HTTPRoute lifecycle management (per-driver routes)
  server/              HTTP handler and duration formatting
```

### `internal/config`

Parses command-line flags. After `flag.Parse()` it calls `HTTPRouteConfig.Validate()`,
which returns an error listing every missing required field. The service exits immediately
with a usage message if validation fails.

The in-cluster namespace is read from
`/var/run/secrets/kubernetes.io/serviceaccount/namespace`; it falls back to `"default"`
when that file is absent (local development).

### `internal/store`

A `map[string]Driver` protected by a `sync.RWMutex`.

- `Add(Driver)` — insert or overwrite by pod name.
- `Remove(podName)` — delete; no-op if the key is absent.
- `List() []Driver` — returns a snapshot under a read lock.

The store has no dependency on Kubernetes types; it deals only in the plain `Driver` struct,
which makes it straightforward to unit-test.

### `internal/watcher`

Uses a `client-go` [informer](https://pkg.go.dev/k8s.io/client-go/tools/cache) backed by
the **dynamic client** (`k8s.io/client-go/dynamic`), scoped to the target namespace with a
server-side label selector:

```
app.kubernetes.io/instance=spark-job,spark-role=driver
```

The server-side filter means the service only receives events for Spark driver pods;
all other pod traffic is ignored at the API server level, keeping network and memory costs
low.

Using the dynamic client avoids importing `k8s.io/client-go/kubernetes` (the typed
clientset), which registers type metadata for every Kubernetes API group (~20+) at init
time. The informer works with `*unstructured.Unstructured` objects; the small set of fields
we need (`metadata.name`, `metadata.creationTimestamp`, `metadata.labels`,
`status.phase`) are extracted with `unstructured.NestedString` and the `GetXxx` accessor
methods.

Event handling:

| Event | Condition | Action |
|---|---|---|
| `Add` | pod not terminated | `store.Add` + `Handler.OnAdd` |
| `Add` | pod terminated | ignored |
| `Update` | pod not terminated | `store.Add` + `Handler.OnAdd` (upsert) |
| `Update` | pod terminated | `store.Remove` + `Handler.OnRemove` |
| `Delete` | any | `store.Remove` + `Handler.OnRemove` |

Tombstone objects (`DeletedFinalStateUnknown`) are unwrapped before processing to handle
the case where the informer missed a delete event while reconnecting.

The `Handler` interface is intentionally narrow so that the watcher does not need to know
about HTTPRoutes or any other side-effect:

```go
type Handler interface {
    OnAdd(d store.Driver)
    OnRemove(appSelector string)
}
```

`Watch` accepts an optional `onSynced func()` callback. After the informer's initial list
is complete (i.e. `WaitForCacheSync` returns), the callback is fired in a goroutine. This
is used by `main` to trigger `Reconcile` exactly once on startup.

`Watch` blocks until the context is cancelled, which makes it easy to run in a goroutine
and shut down cleanly on `SIGTERM`/`SIGINT`.

### `internal/httproute`

Uses the **dynamic client** (`dynamic.Interface`), building HTTPRoute objects as
plain `map[string]interface{}` values against the GVR
`gateway.networking.k8s.io/v1/httproutes`. This avoids importing
`sigs.k8s.io/gateway-api` and its type registrations entirely.

#### Ownership model

The **Helm chart** creates and owns one HTTPRoute for the dashboard (named after the Helm
release). The Go service creates a **separate HTTPRoute per active Spark driver**, named
`<appSelector>-ui-route`. Each driver route contains three rules: two 302 redirect rules
for bare `/proxy/<appSelector>` requests (with and without trailing slash) that send the
user to `/proxy/<appSelector>/jobs/`, plus a `PathPrefix /proxy/<appSelector>` forward
rule to port 4040 with a `ReplacePrefixMatch: /` URL rewrite. See the **Rule structure**
section below for full details.
All driver-managed routes carry the label `app.kubernetes.io/managed-by: spark-ui-assist`
so that Reconcile can list them with a single label-selector query.

#### `Manager` methods

| Method | Effect |
|---|---|
| `Ensure(ctx, driver)` | Creates the HTTPRoute `<appSelector>-ui-route` if it does not already exist. |
| `Delete(ctx, appSelector)` | Deletes the HTTPRoute for `appSelector`. No-op if it does not exist. |
| `Reconcile(ctx, active)` | Called once on startup after informer sync. Lists all managed HTTPRoutes, deletes stale ones, creates missing ones. |

Both `Ensure` and `Delete` are idempotent.

#### Rule structure

Every driver HTTPRoute contains exactly **3 rules**, in this order:

| # | Match | Action |
|---|---|---|
| 1 | `Exact /proxy/<appSelector>` | 302 redirect → `/proxy/<appSelector>/jobs/` |
| 2 | `Exact /proxy/<appSelector>/` | 302 redirect → `/proxy/<appSelector>/jobs/` |
| 3 | `PathPrefix /proxy/<appSelector>` | URLRewrite (`ReplacePrefixMatch: /`) + forward to `<appName>-ui-svc:4040` |

Rules 1 and 2 handle the case where a user navigates to the bare app URL without a
trailing slash or path, redirecting them directly to the Spark jobs page. Rule 3 forwards
all other requests to the Spark UI after stripping the `/proxy/<appSelector>` prefix.

The redirect filter uses `type: RequestRedirect` with `requestRedirect.path.type:
ReplaceFullPath` and `statusCode: 302` (Gateway API Extended support feature
`HTTPRoutePathRedirect`). Response-header rewriting of upstream `Location:` headers is
not implementable in standard Gateway API and is intentionally omitted.

### `internal/server`

A plain `net/http` handler with no external dependencies.

`Handler(store, nowFn)` takes a snapshot of the store on every request, sorts drivers by
creation time for a stable page order, and executes an HTML template.

`FormatDuration(d time.Duration) string` implements the duration display rule:

- Less than 24 hours: `HH:MM:SS`
- 1 day: `1 day HH:MM:SS`
- N days: `N days HH:MM:SS`

The `nowFn` parameter (type `func() time.Time`) is injected so tests can control the clock
without needing to manipulate real time.

## Startup sequence

```
main()
  ├─ config.Parse()                   parse & validate flags
  ├─ dynamic.NewForConfig()           single dynamic client for all API calls
  ├─ store.New()                      empty driver map
  ├─ httproute.New()                  build HTTPRoute manager
  ├─ watcher.NewListerWatcher()       scoped list/watch (dynamic client)
  ├─ go watcher.Watch(..., onSynced)  starts informer loop; fires onSynced after cache sync
  │     └─ onSynced()                 mgr.Reconcile — removes stale rules, adds missing ones
  └─ http.ListenAndServe(:8080)       serves until SIGTERM/SIGINT
```

On shutdown the context is cancelled, which stops the informer. The HTTP server is given
a 5-second graceful shutdown window before the process exits.

## Memory footprint

### Why the dynamic client matters

Importing `k8s.io/client-go/kubernetes` (the typed clientset) triggers `init()`-time
registration of every Kubernetes API group in a global scheme (~20+ groups: apps, batch,
autoscaling, admissionregistration, flowcontrol, …). The same applies to
`sigs.k8s.io/gateway-api/pkg/client/clientset/versioned`. These registrations are
permanent heap allocations that inflate RSS even though the service only ever touches pods
and HTTPRoutes.

Switching to `k8s.io/client-go/dynamic` drops both typed clientsets. Result:

| Build | Stripped binary (linux/amd64) |
|---|---|
| Typed clientsets | ~37 MB |
| Dynamic client | ~15 MB |

The binary size difference flows directly into a smaller RSS because Go maps read-only
text/rodata pages from the binary — fewer pages means fewer TLB entries and less kernel
page-table overhead.

### Working-set breakdown at steady state (100 pods)

| Component | Size |
|---|---|
| Go runtime (heap arena, GC metadata, stacks) | ~14 MB |
| client-go informer goroutines + HTTP server | ~3 MB |
| Informer cache — 100 `*unstructured.Unstructured` pod objects @ ~7 KB each | ~700 KB |
| `store.Store` map — 100 `Driver` structs @ ~300 bytes each | ~30 KB |
| Go allocator headroom (~25% fragmentation) | ~200 KB |
| **Total** | **~18 MB** |

The informer cache holds the **full** unstructured pod object for every live pod. A
realistic Spark driver pod (with containers, volumes, env vars, probes and annotations)
serialises to roughly 4–10 KB of JSON. Pods with many environment variables or large
annotations sit at the high end of that range.

### Recommended Kubernetes resource spec

```yaml
resources:
  requests:
    memory: 32Mi   # headroom above ~18 MB working set
    cpu: 10m
  limits:
    memory: 64Mi   # absorbs GC pause doubling and transient allocation spikes
    cpu: 100m
```

The `limits.memory` of 64 Mi is set at ~3× the steady-state working set. The Go GC can
temporarily double the live heap during a collection cycle; 64 Mi ensures the process is
not OOMKilled during a GC pause at peak load.

Scaling guidance:

| Concurrent Spark driver pods | Estimated RSS | Suggested limit |
|---|---|---|
| 10 | ~17 MB | 32 Mi |
| 100 | ~18 MB | 64 Mi |
| 500 | ~21 MB | 64 Mi |
| 1 000 | ~25 MB | 64 Mi |

Pod metadata dominates only at very high counts. Even at 1 000 pods the pod-cache
contribution (~7 MB) is small compared with the fixed Go runtime overhead (~17 MB), so
a single limit tier covers the practical operating range.

## Kubernetes client configuration

`loadKubeConfig()` tries `rest.InClusterConfig()` first (service account token + CA).
If that fails (running outside a cluster) it falls back to the standard `client-go` rules:
`KUBECONFIG` env var, then `~/.kube/config`.

## Build and container image

The Dockerfile uses a two-stage build:

1. **Builder** — `golang:1.23-alpine`: runs `gofmt` check, `go vet`, then compiles with
   `-trimpath -ldflags="-s -w"` to strip debug info and symbol tables.
2. **Runtime** — `gcr.io/distroless/static:nonroot`: no shell, no libc, no package
   manager. The final image contains only the statically linked binary, running as a
   non-root user.
