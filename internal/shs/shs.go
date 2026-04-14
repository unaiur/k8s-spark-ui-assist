// Package shs watches the Kubernetes EndpointSlices for the Spark History
// Server (SHS) and fires callbacks when the service transitions between having
// at least one ready endpoint and having none.
//
// The watcher uses a shared-informer against the discovery.k8s.io/v1
// EndpointSlice resource filtered by the kubernetes.io/service-name label.
// On every add/update event it counts the number of ready endpoints across all
// slices; if that count transitions across zero it invokes OnUp or OnDown on
// the provided Handler.
//
// EndpointSlice is used instead of the deprecated core/v1 Endpoints, which
// produces deprecation warnings from the Kubernetes API server in v1.33+.
package shs

import (
	"context"
	"log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
)

var endpointSliceGVR = schema.GroupVersionResource{
	Group:    "discovery.k8s.io",
	Version:  "v1",
	Resource: "endpointslices",
}

// serviceNameLabel is the well-known label that the Kubernetes control plane
// sets on every EndpointSlice to associate it with its parent Service.
const serviceNameLabel = "kubernetes.io/service-name"

// Handler is called when the SHS availability transitions.
type Handler interface {
	// OnUp is called when the ready-endpoint count transitions from 0 to ≥1.
	OnUp()
	// OnDown is called when the ready-endpoint count transitions from ≥1 to 0.
	OnDown()
}

// Watch lists and watches the EndpointSlices for serviceName in namespace.
// It calls h.OnUp / h.OnDown on availability transitions and blocks until ctx
// is cancelled.
//
// onSynced, if non-nil, is called once after the initial EndpointSlice state
// has been processed (mirroring the watcher.Watch convention).
func Watch(ctx context.Context, client dynamic.Interface, namespace, serviceName string, h Handler, onSynced func()) {
	lw := newListerWatcher(ctx, client, namespace, serviceName)
	handler := newEventHandler(serviceName, h)

	_, informer := cache.NewInformerWithOptions(cache.InformerOptions{
		ListerWatcher: lw,
		ObjectType:    &unstructured.Unstructured{},
		Handler:       handler,
	})

	if onSynced != nil {
		go func() {
			if cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
				onSynced()
			}
		}()
	}

	informer.Run(ctx.Done())
}

// sliceState tracks per-slice ready-endpoint counts and fires OnUp/OnDown
// transitions on the provided Handler. It is extracted from Watch so it can
// be unit-tested independently.
type sliceState struct {
	serviceName string
	handler     Handler
	counts      map[string]int // EndpointSlice name → ready count
	wasReady    bool
}

func newSliceState(serviceName string, h Handler) *sliceState {
	return &sliceState{
		serviceName: serviceName,
		handler:     h,
		counts:      map[string]int{},
	}
}

func (s *sliceState) totalReady() int {
	n := 0
	for _, c := range s.counts {
		n += c
	}
	return n
}

func (s *sliceState) handleSlice(obj interface{}) {
	ep, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return
	}
	s.counts[ep.GetName()] = readyEndpointCount(ep)
	n := s.totalReady()
	isReady := n > 0
	if isReady && !s.wasReady {
		s.wasReady = true
		log.Printf("shs: service %s has %d ready endpoint(s) — up", s.serviceName, n)
		s.handler.OnUp()
	} else if !isReady && s.wasReady {
		s.wasReady = false
		log.Printf("shs: service %s has no ready endpoints — down", s.serviceName)
		s.handler.OnDown()
	}
}

func (s *sliceState) deleteSlice(obj interface{}) {
	// client-go may deliver a DeletedFinalStateUnknown tombstone
	// when the watch misses a delete event; unwrap it first.
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = tombstone.Obj
	}
	ep, ok := obj.(*unstructured.Unstructured)
	if !ok {
		return
	}
	delete(s.counts, ep.GetName())
	if s.wasReady && s.totalReady() == 0 {
		s.wasReady = false
		log.Printf("shs: endpointslice for %s deleted — down", s.serviceName)
		s.handler.OnDown()
	}
}

// newEventHandler builds the cache.ResourceEventHandlerFuncs for the informer.
func newEventHandler(serviceName string, h Handler) cache.ResourceEventHandlerFuncs {
	s := newSliceState(serviceName, h)
	return cache.ResourceEventHandlerFuncs{
		AddFunc:    func(obj interface{}) { s.handleSlice(obj) },
		UpdateFunc: func(_, newObj interface{}) { s.handleSlice(newObj) },
		DeleteFunc: s.deleteSlice,
	}
}

// newListerWatcher returns a ListerWatcher scoped to the EndpointSlices that
// belong to serviceName (via the kubernetes.io/service-name label selector).
func newListerWatcher(ctx context.Context, client dynamic.Interface, namespace, serviceName string) cache.ListerWatcher {
	labelSel := labels.Set{serviceNameLabel: serviceName}.String()
	rc := client.Resource(endpointSliceGVR).Namespace(namespace)
	return &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			opts.LabelSelector = labelSel
			return rc.List(ctx, opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			opts.LabelSelector = labelSel
			return rc.Watch(ctx, opts)
		},
	}
}

// readyEndpointCount returns the number of ready endpoints in an EndpointSlice.
// An endpoint is ready when its conditions.ready field is true (or absent,
// which the API spec treats as ready for compatibility).
func readyEndpointCount(ep *unstructured.Unstructured) int {
	endpoints, _, _ := unstructured.NestedSlice(ep.Object, "endpoints")
	total := 0
	for _, e := range endpoints {
		endpoint, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		// conditions.ready defaults to true when absent (API spec §EndpointConditions).
		ready, found, _ := unstructured.NestedBool(endpoint, "conditions", "ready")
		if !found || ready {
			total++
		}
	}
	return total
}
