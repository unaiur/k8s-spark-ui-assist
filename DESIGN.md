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
which returns an error listing every missing field when `http-route.enabled=true`.
The service exits immediately with a usage message if validation fails.

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

Uses a `client-go` [informer](https://pkg.go.dev/k8s.io/client-go/tools/cache) scoped to
the target namespace with a server-side label selector:

```
app.kubernetes.io/instance=spark-job,spark-role=driver
```

The server-side filter means the service only receives events for Spark driver pods;
all other pod traffic is ignored at the API server level, keeping network and memory costs
low.

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

Optional feature, activated by `-http-route.enabled=true`.

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
  ├─ kubernetes.NewForConfig()        build k8s client
  ├─ store.New()                      empty driver map
  ├─ (optional) httproute.New()       build HTTPRoute manager
  ├─ watcher.NewListerWatcher()       scoped list/watch
  ├─ go watcher.Watch()               starts informer loop
  └─ http.ListenAndServe(:8080)       serves until SIGTERM/SIGINT
```

On shutdown the context is cancelled, which stops the informer. The HTTP server is given
a 5-second graceful shutdown window before the process exits.

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
