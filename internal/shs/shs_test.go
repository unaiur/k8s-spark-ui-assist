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

var endpointSliceGVR = schema.GroupVersionResource{
	Group:    "discovery.k8s.io",
	Version:  "v1",
	Resource: "endpointslices",
}

const (
	namespace        = "default"
	serviceNameLabel = "kubernetes.io/service-name"
)

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "discovery.k8s.io", Version: "v1", Kind: "EndpointSlice"},
		&unstructured.Unstructured{},
	)
	s.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "discovery.k8s.io", Version: "v1", Kind: "EndpointSliceList"},
		&unstructured.UnstructuredList{},
	)
	return s
}

// buildEndpointSlice constructs a fake EndpointSlice for serviceName with the
// given number of ready endpoints.
func buildEndpointSlice(name, serviceName string, readyCount int) *unstructured.Unstructured {
	endpoints := make([]interface{}, readyCount)
	for i := range endpoints {
		endpoints[i] = map[string]interface{}{
			"addresses": []interface{}{"10.0.0.1"},
			"conditions": map[string]interface{}{
				"ready": true,
			},
		}
	}
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "discovery.k8s.io/v1",
			"kind":       "EndpointSlice",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"labels": map[string]interface{}{
					serviceNameLabel: serviceName,
				},
			},
			"endpoints": endpoints,
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

// TestWatchFiresOnUpWhenReadyEndpointsPresent starts the watcher with an
// EndpointSlice that already has ready endpoints and verifies OnUp is called.
func TestWatchFiresOnUpWhenReadyEndpointsPresent(t *testing.T) {
	ep := buildEndpointSlice("shs-slice-abc", "spark-history-server", 2)
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

// TestWatchFiresNoEventWhenNoReadyEndpoints starts the watcher with an
// EndpointSlice that has no ready endpoints and verifies no events are fired.
func TestWatchFiresNoEventWhenNoReadyEndpoints(t *testing.T) {
	ep := buildEndpointSlice("shs-slice-abc", "spark-history-server", 0)
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
		t.Errorf("expected no events for zero-ready-endpoint EndpointSlice, got %v", events)
	}
}

// TestWatchFiresOnDownAfterUpdate creates an EndpointSlice with ready endpoints,
// then updates it to have none and verifies the OnDown transition fires.
func TestWatchFiresOnDownAfterUpdate(t *testing.T) {
	ep := buildEndpointSlice("shs-slice-abc", "spark-history-server", 1)
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

	// Update the EndpointSlice to have no ready endpoints.
	epDown := buildEndpointSlice("shs-slice-abc", "spark-history-server", 0)
	epDown.SetResourceVersion(ep.GetResourceVersion())
	_, err := client.Resource(endpointSliceGVR).Namespace(namespace).Update(
		context.Background(), epDown, metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("Update EndpointSlice: %v", err)
	}

	waitFor(t, func() bool {
		events := h.Events()
		return len(events) >= 2 && events[1] == "down"
	})
}

// TestWatchFiresOnDownAfterDelete creates an EndpointSlice with ready endpoints,
// then deletes it and verifies the OnDown transition fires.
func TestWatchFiresOnDownAfterDelete(t *testing.T) {
	ep := buildEndpointSlice("shs-slice-abc", "spark-history-server", 1)
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
		return len(events) >= 1 && events[0] == "up"
	})

	err := client.Resource(endpointSliceGVR).Namespace(namespace).Delete(
		context.Background(), ep.GetName(), metav1.DeleteOptions{},
	)
	if err != nil {
		t.Fatalf("Delete EndpointSlice: %v", err)
	}

	waitFor(t, func() bool {
		events := h.Events()
		return len(events) >= 2 && events[1] == "down"
	})
}

// TestWatchAggregatesMultipleSlices verifies that multiple EndpointSlices for
// the same service are aggregated: OnDown fires only when ALL slices have zero
// ready endpoints.
func TestWatchAggregatesMultipleSlices(t *testing.T) {
	ep1 := buildEndpointSlice("shs-slice-1", "spark-history-server", 1)
	ep2 := buildEndpointSlice("shs-slice-2", "spark-history-server", 1)
	client := dynamicfake.NewSimpleDynamicClient(newScheme(), ep1, ep2)

	h := &recordingHandler{}
	synced := make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		shs.Watch(ctx, client, namespace, "spark-history-server", h, func() { close(synced) })
	}()

	<-synced
	waitFor(t, func() bool {
		return len(h.Events()) >= 1 && h.Events()[0] == "up"
	})

	// Drain ep1 — service still up via ep2.
	ep1Down := buildEndpointSlice("shs-slice-1", "spark-history-server", 0)
	ep1Down.SetResourceVersion(ep1.GetResourceVersion())
	_, err := client.Resource(endpointSliceGVR).Namespace(namespace).Update(
		context.Background(), ep1Down, metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("Update ep1: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if events := h.Events(); len(events) != 1 {
		t.Errorf("expected still only 1 event (up) after draining ep1, got %v", events)
	}

	// Drain ep2 — now truly down.
	ep2Down := buildEndpointSlice("shs-slice-2", "spark-history-server", 0)
	ep2Down.SetResourceVersion(ep2.GetResourceVersion())
	_, err = client.Resource(endpointSliceGVR).Namespace(namespace).Update(
		context.Background(), ep2Down, metav1.UpdateOptions{},
	)
	if err != nil {
		t.Fatalf("Update ep2: %v", err)
	}
	waitFor(t, func() bool {
		events := h.Events()
		return len(events) >= 2 && events[1] == "down"
	})
}
