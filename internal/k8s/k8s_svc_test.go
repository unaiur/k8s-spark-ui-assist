package k8s_test

import (
	"context"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/unaiur/k8s-spark-ui-assist/internal/config"
	k8ssvc "github.com/unaiur/k8s-spark-ui-assist/internal/k8s"
	"github.com/unaiur/k8s-spark-ui-assist/internal/labels"
	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

const namespace = "default"

var (
	podGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

	httpRouteGVR = schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "httproutes",
	}
)

// newScheme returns a scheme that knows about Pod, PodList, HTTPRoute and HTTPRouteList.
func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "PodList"}, &unstructured.UnstructuredList{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRoute"}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{Group: "gateway.networking.k8s.io", Version: "v1", Kind: "HTTPRouteList"}, &unstructured.UnstructuredList{})
	return s
}

// newSvc creates a KubernetesSvc wired to a fresh fake dynamic client.
func newSvc(client *dynamicfake.FakeDynamicClient) *k8ssvc.KubernetesSvc {
	return k8ssvc.New(context.Background(), client, namespace)
}

// testCfg is a minimal HTTPRouteConfig used in route tests.
var testCfg = config.HTTPRouteConfig{
	Hostname:         "spark.example.com",
	GatewayName:      "main-gateway",
	GatewayNamespace: "gateway-ns",
}

// newDriver is a test helper for building a store.Driver.
func newDriver(appSelector, appName string) store.Driver {
	return store.Driver{
		PodName:     appSelector + "-pod",
		CreatedAt:   time.Now(),
		AppSelector: appSelector,
		AppName:     appName,
	}
}

// addPod creates a pod in the fake client.
func addPod(t *testing.T, client *dynamicfake.FakeDynamicClient, pod *unstructured.Unstructured) {
	t.Helper()
	_, err := client.Resource(podGVR).Namespace(namespace).Create(
		context.Background(), pod, metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("addPod: %v", err)
	}
}

// makePod creates a fake pod with the given containerStatus state.
// exitCode uses float64 to match the type produced by real Kubernetes JSON decoding.
func makePod(appID, containerState, reason string, exitCode float64) *unstructured.Unstructured {
	pod := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      appID + "-driver",
				"namespace": namespace,
				"labels": map[string]interface{}{
					labels.LabelInstance: labels.InstanceValue,
					labels.LabelRole:     labels.RoleValue,
					labels.LabelSelector: appID,
					labels.LabelAppName:  "my-job",
				},
			},
			"status": map[string]interface{}{},
		},
	}
	pod.SetCreationTimestamp(metav1.Now())

	if containerState != "" {
		stateMap := map[string]interface{}{}
		switch containerState {
		case "waiting":
			waitingMap := map[string]interface{}{}
			if reason != "" {
				waitingMap["reason"] = reason
			}
			stateMap["waiting"] = waitingMap
		case "running":
			stateMap["running"] = map[string]interface{}{
				"startedAt": "2026-01-01T00:00:00Z",
			}
		case "terminated":
			terminatedMap := map[string]interface{}{"exitCode": exitCode}
			if reason != "" {
				terminatedMap["reason"] = reason
			}
			stateMap["terminated"] = terminatedMap
		}
		_ = unstructured.SetNestedSlice(pod.Object, []interface{}{
			map[string]interface{}{
				"name":  "spark-kubernetes-driver",
				"state": stateMap,
			},
		}, "status", "containerStatuses")
	}
	return pod
}

// makePendingPod creates a pod with no container statuses. If condReason is
// non-empty a PodScheduled=False condition with that reason is added.
func makePendingPod(appID, condReason string) *unstructured.Unstructured {
	pod := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      appID + "-driver",
				"namespace": namespace,
				"labels": map[string]interface{}{
					labels.LabelInstance: labels.InstanceValue,
					labels.LabelRole:     labels.RoleValue,
					labels.LabelSelector: appID,
					labels.LabelAppName:  "my-job",
				},
			},
			"status": map[string]interface{}{},
		},
	}
	pod.SetCreationTimestamp(metav1.Now())
	if condReason != "" {
		_ = unstructured.SetNestedSlice(pod.Object, []interface{}{
			map[string]interface{}{
				"type":   "PodScheduled",
				"status": "False",
				"reason": condReason,
			},
		}, "status", "conditions")
	}
	return pod
}

// ---- SparkDriverState -------------------------------------------------------

func TestSparkDriverStateRunning(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePod("spark-abc", "running", "", 0))

	state, err := newSvc(client).SparkDriverState("spark-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "Running" {
		t.Errorf("expected Running, got %q", state)
	}
}

func TestSparkDriverStateWaitingWithReason(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePod("spark-abc", "waiting", "ContainerCreating", 0))

	state, err := newSvc(client).SparkDriverState("spark-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "ContainerCreating" {
		t.Errorf("expected ContainerCreating, got %q", state)
	}
}

func TestSparkDriverStateWaitingNoReason(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePod("spark-abc", "waiting", "", 0))

	state, err := newSvc(client).SparkDriverState("spark-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "Waiting" {
		t.Errorf("expected Waiting, got %q", state)
	}
}

func TestSparkDriverStateTerminatedCompleted(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePod("spark-abc", "terminated", "", 0))

	state, err := newSvc(client).SparkDriverState("spark-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "Completed" {
		t.Errorf("expected Completed, got %q", state)
	}
}

func TestSparkDriverStateTerminatedError(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePod("spark-abc", "terminated", "", 1))

	state, err := newSvc(client).SparkDriverState("spark-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "Error" {
		t.Errorf("expected Error, got %q", state)
	}
}

func TestSparkDriverStateTerminatedWithReason(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePod("spark-abc", "terminated", "OOMKilled", 137))

	state, err := newSvc(client).SparkDriverState("spark-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "OOMKilled" {
		t.Errorf("expected OOMKilled, got %q", state)
	}
}

func TestSparkDriverStatePendingNoConditions(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePendingPod("spark-abc", ""))

	state, err := newSvc(client).SparkDriverState("spark-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "Pending" {
		t.Errorf("expected Pending, got %q", state)
	}
}

func TestSparkDriverStatePendingUnschedulable(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	addPod(t, client, makePendingPod("spark-abc", "Unschedulable"))

	state, err := newSvc(client).SparkDriverState("spark-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "Unschedulable" {
		t.Errorf("expected Unschedulable, got %q", state)
	}
}

func TestSparkDriverStateNotFound(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())

	state, err := newSvc(client).SparkDriverState("unknown-app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "" {
		t.Errorf("expected empty state for missing driver, got %q", state)
	}
}

// TestSparkDriverStateOnlyDriverPodsMatched verifies that pods without the
// spark-role=driver label are not matched even if spark-app-selector matches.
func TestSparkDriverStateOnlyDriverPodsMatched(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())

	nonDriver := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name":      "spark-abc-executor",
				"namespace": namespace,
				"labels": map[string]interface{}{
					labels.LabelInstance: labels.InstanceValue,
					labels.LabelSelector: "spark-abc",
					// deliberately missing spark-role=driver
				},
			},
			"status": map[string]interface{}{},
		},
	}
	addPod(t, client, nonDriver)

	state, err := newSvc(client).SparkDriverState("spark-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "" {
		t.Errorf("non-driver pod should not be matched; got state %q", state)
	}
}

// TestSparkDriverStateMostRecentPodWins verifies that when multiple pods match,
// the one with the latest creation timestamp is used.
func TestSparkDriverStateMostRecentPodWins(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())

	older := makePod("spark-abc", "terminated", "", 1) // Error
	older.SetName("spark-abc-driver-old")
	older.SetCreationTimestamp(metav1.NewTime(time.Now().Add(-time.Minute)))

	newer := makePod("spark-abc", "running", "", 0) // Running
	newer.SetName("spark-abc-driver-new")
	newer.SetCreationTimestamp(metav1.Now())

	addPod(t, client, older)
	addPod(t, client, newer)

	state, err := newSvc(client).SparkDriverState("spark-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != "Running" {
		t.Errorf("expected state of most recent pod (Running), got %q", state)
	}
}

// ---- CreateDriverRoute / GetRoute / DeleteRoute / ListRoutesWithLabelSelector

func TestCreateAndGetRoute(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	svc := newSvc(client)
	d := newDriver("spark-abc", "my-job")

	if err := svc.CreateDriverRoute(d, testCfg); err != nil {
		t.Fatalf("CreateDriverRoute: %v", err)
	}

	route, err := svc.GetRoute(d.RouteName())
	if err != nil {
		t.Fatalf("GetRoute: %v", err)
	}
	if route.GetName() != d.RouteName() {
		t.Errorf("expected name %q, got %q", d.RouteName(), route.GetName())
	}
}

func TestGetRouteNotFound(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	svc := newSvc(client)

	_, err := svc.GetRoute("does-not-exist-ui-route")
	if err == nil {
		t.Fatal("expected error for missing route, got nil")
	}
}

func TestDeleteRoute(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	svc := newSvc(client)
	d := newDriver("spark-abc", "my-job")

	if err := svc.CreateDriverRoute(d, testCfg); err != nil {
		t.Fatalf("CreateDriverRoute: %v", err)
	}
	if err := svc.DeleteRoute(d.RouteName()); err != nil {
		t.Fatalf("DeleteRoute: %v", err)
	}

	_, err := svc.GetRoute(d.RouteName())
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestDeleteRouteNotFound(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	svc := newSvc(client)

	// DeleteRoute on a non-existent name should return a NotFound error
	// (callers are responsible for checking IsNotFound).
	err := svc.DeleteRoute("ghost-ui-route")
	if err == nil {
		t.Fatal("expected NotFound error, got nil")
	}
}

func TestListRoutesWithLabelSelector(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	svc := newSvc(client)

	// Create two routes via CreateDriverRoute (they get the managed-by label).
	d1 := newDriver("spark-aaa", "job-a")
	d2 := newDriver("spark-bbb", "job-b")
	if err := svc.CreateDriverRoute(d1, testCfg); err != nil {
		t.Fatalf("CreateDriverRoute d1: %v", err)
	}
	if err := svc.CreateDriverRoute(d2, testCfg); err != nil {
		t.Fatalf("CreateDriverRoute d2: %v", err)
	}

	items, err := svc.ListRoutesWithLabelSelector(k8ssvc.ManagedBySelector())
	if err != nil {
		t.Fatalf("ListRoutesWithLabelSelector: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("expected 2 routes, got %d", len(items))
	}
}

func TestListRoutesEmptyResult(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	svc := newSvc(client)

	items, err := svc.ListRoutesWithLabelSelector(k8ssvc.ManagedBySelector())
	if err != nil {
		t.Fatalf("ListRoutesWithLabelSelector: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 routes, got %d", len(items))
	}
}

// TestCreateRouteHasCorrectStructure verifies that the created HTTPRoute
// contains the expected path prefix, hostname, gateway ref, and backend.
func TestCreateRouteHasCorrectStructure(t *testing.T) {
	client := dynamicfake.NewSimpleDynamicClient(newScheme())
	svc := newSvc(client)
	d := newDriver("spark-abc123", "my-spark-job")

	if err := svc.CreateDriverRoute(d, testCfg); err != nil {
		t.Fatalf("CreateDriverRoute: %v", err)
	}

	route, err := svc.GetRoute(d.RouteName())
	if err != nil {
		t.Fatalf("GetRoute: %v", err)
	}

	// Hostname
	hostnames, _, _ := unstructured.NestedStringSlice(route.Object, "spec", "hostnames")
	if len(hostnames) == 0 || hostnames[0] != testCfg.Hostname {
		t.Errorf("expected hostname %q, got %v", testCfg.Hostname, hostnames)
	}

	// Gateway parentRef name
	parentRefs, _, _ := unstructured.NestedSlice(route.Object, "spec", "parentRefs")
	if len(parentRefs) == 0 {
		t.Fatal("expected parentRefs, got none")
	}
	gwName, _, _ := unstructured.NestedString(parentRefs[0].(map[string]interface{}), "name")
	if gwName != testCfg.GatewayName {
		t.Errorf("expected gateway %q, got %q", testCfg.GatewayName, gwName)
	}

	// Three rules
	rules, _, _ := unstructured.NestedSlice(route.Object, "spec", "rules")
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}

	// Rule 0: Exact /proxy/spark-abc123
	rule0 := rules[0].(map[string]interface{})
	matches0, _, _ := unstructured.NestedSlice(rule0, "matches")
	pathType0, _, _ := unstructured.NestedString(matches0[0].(map[string]interface{}), "path", "type")
	pathVal0, _, _ := unstructured.NestedString(matches0[0].(map[string]interface{}), "path", "value")
	if pathType0 != "Exact" || pathVal0 != "/proxy/spark-abc123" {
		t.Errorf("rule 0: expected Exact /proxy/spark-abc123, got %s %s", pathType0, pathVal0)
	}

	// Rule 2: PathPrefix /proxy/spark-abc123 with backendRef to <appName>-ui-svc
	rule2 := rules[2].(map[string]interface{})
	backends, _, _ := unstructured.NestedSlice(rule2, "backendRefs")
	if len(backends) == 0 {
		t.Fatal("rule 2: expected backendRefs")
	}
	backendName, _, _ := unstructured.NestedString(backends[0].(map[string]interface{}), "name")
	if backendName != "my-spark-job-ui-svc" {
		t.Errorf("rule 2: expected backend my-spark-job-ui-svc, got %q", backendName)
	}
}
