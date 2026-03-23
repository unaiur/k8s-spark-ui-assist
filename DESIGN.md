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
| `spark-app-selector` | `Driver.AppSelector` — used in URLs and HTTPRoute names |
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
  httproute/           Gateway API HTTPRoute create/delete
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

`Watch` blocks until the context is cancelled, which makes it easy to run in a goroutine
and shut down cleanly on `SIGTERM`/`SIGINT`.

### `internal/httproute`

Uses the **dynamic client** (`dynamic.Interface`), building HTTPRoute objects as
`*unstructured.Unstructured` maps against the GVR
`gateway.networking.k8s.io/v1/httproutes`. This avoids importing
`sigs.k8s.io/gateway-api` and its type registrations entirely.

`Manager.Ensure(ctx, driver)` performs a `Get` before `Create` to stay idempotent — if the
route already exists it does nothing. `Manager.Delete(ctx, appSelector)` deletes by the
computed name `<spark-app-selector>-ui-route`.

The generated `HTTPRoute` routes requests whose path starts with `/live/<spark-app-selector>`
to the Spark UI service (`<spark-app-name>-ui-svc:4040`), rewriting the prefix to `/` so
the Spark UI receives requests at its expected root path.

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
  ├─ go watcher.Watch()               starts informer loop
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
