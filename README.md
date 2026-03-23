# k8s-spark-ui-assist

A lightweight Kubernetes service that discovers running Apache Spark jobs in its namespace
and serves a web page with links to each job's Spark UI.

## What it does

When Spark submits a job on Kubernetes it creates a *driver pod* that hosts the Spark UI
on port 4040. Those pods are short-lived and their addresses change between runs, making
them hard to bookmark or share.

`k8s-spark-ui-assist` solves this by:

1. Watching the namespace for Spark driver pods (identified by their labels).
2. Maintaining a live list of running jobs.
3. Serving a single web page at `/` that links to every active Spark UI, showing how long
   each job has been running.
4. Creating a [Gateway API](https://gateway-api.sigs.k8s.io/) `HTTPRoute` for
   each driver as it starts, and deleting it when the job finishes, so the UIs are
   reachable through a shared gateway hostname without extra manual steps.

## Requirements

- Kubernetes cluster with RBAC permissions to `list` and `watch` pods in the service namespace.
- [Gateway API](https://gateway-api.sigs.k8s.io/) CRDs installed and a running gateway.

Spark driver pods must carry these two labels for the service to recognise them:

| Label | Value |
|---|---|
| `app.kubernetes.io/instance` | `spark-job` |
| `spark-role` | `driver` |

The following labels are read for display purposes:

| Label | Used for |
|---|---|
| `spark-app-name` | Human-readable job name shown on the page |
| `spark-app-selector` | URL path segment and HTTPRoute name |

## Running

### Helm (recommended)

A Helm chart is provided in the `chart/` directory. It creates the Deployment,
ClusterIP Service, ServiceAccount, RBAC Role + RoleBinding, and an HTTPRoute that
exposes `/` through your gateway.

```sh
helm install spark-assist ./chart \
  --namespace spark --create-namespace \
  --set httpHostname=spark.example.com \
  --set httpGatewayName=main-gateway \
  --set httpGatewayNamespace=gateway-system
```

All three Gateway parameters are required; the chart fails with a clear error if any
are omitted.

#### Helm values

| Value | Default | Description |
|---|---|---|
| `httpHostname` | _(required)_ | Hostname for the service HTTPRoute and all Spark driver HTTPRoutes (`spec.hostnames[0]`) |
| `httpGatewayName` | _(required)_ | Gateway name for all HTTPRoutes (`spec.parentRefs[0].name`) |
| `httpGatewayNamespace` | _(required)_ | Gateway namespace for all HTTPRoutes (`spec.parentRefs[0].namespace`) |
| `image.repository` | `ghcr.io/unaiur/k8s-spark-ui-assist` | Container image repository |
| `image.tag` | chart `appVersion` | Image tag |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy |
| `replicaCount` | `1` | Number of replicas |
| `resources` | `requests: 32Mi/10m`, `limits: 64Mi/100m` | Pod resource requests and limits |
| `service.type` | `ClusterIP` | Kubernetes Service type |
| `service.port` | `80` | Service port |
| `serviceAccount.name` | release fullname | Override the ServiceAccount name |
| `serviceAccount.annotations` | `{}` | Extra annotations (e.g. for IRSA / Workload Identity) |

### In-cluster without Helm

Deploy the service in the same namespace as your Spark jobs. It auto-detects the namespace
from the service account token.

```sh
docker build -t k8s-spark-ui-assist:latest .
# push to your registry, then apply your own manifests
```

The HTTP server listens on **port 8080**.

### Locally against a cluster

```sh
go run ./cmd/spark-ui-assist -namespace my-spark-namespace
```

The service falls back to your local `~/.kube/config` when it is not running inside a cluster.

## Configuration flags

| Flag | Default | Description |
|---|---|---|
| `-namespace` | auto-detected from service account | Kubernetes namespace to watch |
| `-http-route.hostname` | _(required)_ | Hostname placed in `spec.hostnames[0]` |
| `-http-route.gateway-name` | _(required)_ | Gateway name for `spec.parentRefs[0].name` |
| `-http-route.gateway-namespace` | _(required)_ | Gateway namespace for `spec.parentRefs[0].namespace` |

All three `-http-route.*` flags are required; the service exits immediately with an error
listing the missing flags if any are omitted.

## Web UI

Visiting `http://<service>:8080/` returns an HTML page like:

```
Running Spark Jobs

• my-etl-job   (running for 01:23:45)
• nightly-ml   (running for 2 days 08:00:00)
```

Each entry is a link to `/live/<spark-app-selector>/`, which your gateway or ingress should
proxy to the driver pod's port 4040.

Durations omit the days component when less than 24 hours have elapsed
(`23:59:59` → `1 day 00:00:00`).

## Development

```sh
make test    # fmt check + vet + unit tests (race detector on)
make build   # produces ./spark-ui-assist binary
```

The build enforces `gofmt` formatting — unformatted files cause `make build` to fail.

## License

GNU General Public License v3.0 — see [LICENSE](LICENSE).
