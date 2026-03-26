package shs_test

import (
	"context"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/unaiur/k8s-spark-ui-assist/internal/shs"
)

var endpointsGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "endpoints"}

const namespace = "default"

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Endpoints"}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "EndpointsList"}, &unstructured.UnstructuredList{})
	return s
}

// buildEndpoints constructs a fake Endpoints object with the given number of
// ready addresses in a single subset.
func buildEndpoints(name string, readyCount int) *unstructured.Unstructured {
	addresses := make([]interface{}, readyCount)
	for i := range addresses {
		addresses[i] = map[string]interface{}{"ip": "10.0.0.1"}
	}
	subsets := []interface{}{}
	if readyCount > 0 {
		subsets = []interface{}{
			map[string]interface{}{
				"addresses": addresses,
			},
		}
	}
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Endpoints",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"subsets": subsets,
		},
	}
}

// recordingHandler records OnUp / OnDown calls.
type recordingHandler struct {
	mu    sync.Mutex
	calls []string
}

func (h *recordingHandler) OnUp() {
	h.mu.Lock()
	h.calls = append(h.calls, "up")
	h.mu.Unlock()
}

func (h *recordingHandler) OnDown() {
	h.mu.Lock()
	h.calls = append(h.calls, "down")
	h.mu.Unlock()
}

func (h *recordingHandler) Events() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]string, len(h.calls))
	copy(out, h.calls)
	return out
}

// waitFor polls f until it returns true or the deadline passes.
func waitFor(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

// TestWatchFiresOnUpWhenReadyAddressesPresent starts the watcher with an
// Endpoints object that already has ready addresses and verifies OnUp is called.
func TestWatchFiresOnUpWhenReadyAddressesPresent(t *testing.T) {
	ep := buildEndpoints("spark-history-server", 2)
	client := dynamicfake.NewSimpleDynamicClient(newScheme(), ep)

	h := &recordingHandler{}
	synced := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		shs.Watch(ctx, client, namespace, "spark-history-server", h, func() { close(synced) })
	}()

	<-synced
	waitFor(t, func() bool {
		events := h.Events()
		return len(events) == 1 && events[0] == "up"
	})
}

// TestWatchFiresOnDownWhenNoReadyAddresses starts the watcher with an Endpoints
// object that has no ready addresses and verifies no OnUp is called.
func TestWatchFiresOnDownWhenNoReadyAddresses(t *testing.T) {
	ep := buildEndpoints("spark-history-server", 0)
	client := dynamicfake.NewSimpleDynamicClient(newScheme(), ep)

	h := &recordingHandler{}
	synced := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		shs.Watch(ctx, client, namespace, "spark-history-server", h, func() { close(synced) })
	}()

	<-synced
	// Give a short time for any spurious calls to arrive.
	time.Sleep(50 * time.Millisecond)
	if events := h.Events(); len(events) != 0 {
		t.Errorf("expected no events for zero-ready-address Endpoints, got %v", events)
	}
}

// TestWatchFiresOnDownAfterUpdate creates an Endpoints with ready addresses,
// then updates it to have none and verifies the OnDown transition fires.
func TestWatchFiresOnDownAfterUpdate(t *testing.T) {
	ep := buildEndpoints("spark-history-server", 1)
	client := dynamicfake.NewSimpleDynamicClient(newScheme(), ep)

	h := &recordingHandler{}
	synced := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		shs.Watch(ctx, client, namespace, "spark-history-server", h, func() { close(synced) })
	}()

	<-synced
	// Wait for initial OnUp.
	waitFor(t, func() bool {
		events := h.Events()
		return len(events) >= 1 && events[0] == "up"
	})

	// Update the Endpoints to have no ready addresses.
	epDown := buildEndpoints("spark-history-server", 0)
	epDown.SetResourceVersion(ep.GetResourceVersion())
	_, err := client.Resource(endpointsGVR).Namespace(namespace).Update(
		context.Background(), epDown, metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("Update endpoints: %v", err)
	}

	waitFor(t, func() bool {
		events := h.Events()
		return len(events) >= 2 && events[1] == "down"
	})
}
