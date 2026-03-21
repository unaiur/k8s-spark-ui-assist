We will build a Go service for Kubernetes that listens to events for Spark driver pods.

We will make a lightweight service that requires the minimum possible memory. We will
ensure that all source format is consistent, enforcing code formatting tools at build time.
We will also include unit tests to ensure that business logic is solid.

It will keep a list of all running Spark drivers in the same namespace that this service.
A pod is considered to be a Spark driver if it has these two labels (both):
 - app.kubernetes.io/instance: spark-job
 - spark-role: driver

We will keep in that list following tag fields about the Spark job:
 - Creation timestamp
 - spark-app-selector tag
 - spark-app-name tag

To keep the list of Spark drivers updated, it will:
* List all pods matching those labels at start up
* Listen to start and stop Kubernetes events, to add or remove pods from the list

Finally, it will expose a web service with a single endpoint at '/' that list all Spark jobs:
 - <a href="/live/{{spark-app-selector}}/">{{spark-app-name}}</a> (running for [N days and ]HH:MM:SS)

Durations will not include 0 days component. For example: (running for 23:59:59) -> (running for 1 day 00:00:00)

As an optional feature, it can create HttpRoutes for Spark Drivers. Configuration:
 - http-route.enabled: true
 - http-route.hostname (str) hostname to include in the HTTPRoute at spec.hostnames[0]
 - http-route.gateway-name (str) name to put in spec.parentRefs[0].name
 - http-route.gateway-namespace (str) namespace to put in spec.parentRefs[0].namespace

When starting, it will create HTTPRoutes for those Spark Drivers that does not have one. When
receiving the event that a Spark driver has been created, it also creates a route. And it
deletes the routes when it detects that a Spark driver has terminated.

It will generate an HTTP routes in current namespace like this:
```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: "{{ spark-app-selector }}-ui-route"
spec:
  parentRefs:
    - name: "{{ gateway-name }}" 
      namespace: {{ gateway-namespace }}
      port: 443
  hostnames:
    - "{{ hostname }}"
  rules:
    - matches:
        - path:
            type: PathPrefix
            value: /live/{{ spark-app-selector }}
      filters:
        - type: URLRewrite
          urlRewrite:
            path:
              type: ReplacePrefixMatch
              replacePrefixMatch: /
      backendRefs:
        - name: {{ spark-app-name }}-ui-svc
          port: 4040
          kind: Service
```
