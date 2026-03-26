// Package shs watches the Kubernetes Endpoints object for the Spark History
// Server (SHS) and fires callbacks when the service transitions between having
// at least one ready pod and having none.
//
// The watcher uses a shared-informer against the core/v1 Endpoints resource
// filtered to the single named object via a field selector. On every add/update
// event it counts the number of ready addresses across all subsets; if that
// count transitions across zero it invokes OnUp or OnDown on the provided
// Handler.
package shs

import (
	"context"
	"log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
)

var endpointsGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "endpoints"}

// Handler is called when the SHS availability transitions.
type Handler interface {
	// OnUp is called when the ready-address count transitions from 0 to ≥1.
	OnUp()
	// OnDown is called when the ready-address count transitions from ≥1 to 0.
	OnDown()
}

// Watch lists and watches the Endpoints object for serviceName in namespace.
// It calls h.OnUp / h.OnDown on availability transitions and blocks until ctx
// is cancelled.
//
// onSynced, if non-nil, is called once after the initial Endpoints state has
// been processed (mirroring the watcher.Watch convention).
func Watch(ctx context.Context, client dynamic.Interface, namespace, serviceName string, h Handler, onSynced func()) {
	ready := false // tracks last known state to detect transitions

	handleEndpoints := func(obj interface{}) {
		ep, ok := obj.(*unstructured.Unstructured)
		if !ok {
			return
		}
		n := readyAddressCount(ep)
		wasReady := ready
		ready = n > 0
		if ready && !wasReady {
			log.Printf("shs: service %s has %d ready address(es) — up", serviceName, n)
			h.OnUp()
		} else if !ready && wasReady {
			log.Printf("shs: service %s has no ready addresses — down", serviceName)
			h.OnDown()
		}
	}

	lw := newListerWatcher(client, namespace, serviceName)

	_, informer := cache.NewInformerWithOptions(cache.InformerOptions{
		ListerWatcher: lw,
		ObjectType:    &unstructured.Unstructured{},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				handleEndpoints(obj)
			},
			UpdateFunc: func(_, newObj interface{}) {
				handleEndpoints(newObj)
			},
			DeleteFunc: func(obj interface{}) {
				// Endpoints object deleted means service gone → treat as down.
				if ready {
					ready = false
					log.Printf("shs: endpoints for %s deleted — down", serviceName)
					h.OnDown()
				}
			},
		},
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

// newListerWatcher returns a ListerWatcher scoped to the single Endpoints
// object for serviceName in namespace (via a field selector on metadata.name).
func newListerWatcher(client dynamic.Interface, namespace, serviceName string) cache.ListerWatcher {
	fieldSel := fields.OneTermEqualSelector("metadata.name", serviceName).String()
	rc := client.Resource(endpointsGVR).Namespace(namespace)
	return &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			opts.FieldSelector = fieldSel
			return rc.List(context.Background(), opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			opts.FieldSelector = fieldSel
			return rc.Watch(context.Background(), opts)
		},
	}
}

// readyAddressCount returns the total number of ready addresses across all
// subsets of the given Endpoints object.
func readyAddressCount(ep *unstructured.Unstructured) int {
	subsets, _, _ := unstructured.NestedSlice(ep.Object, "subsets")
	total := 0
	for _, s := range subsets {
		subset, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		addrs, _, _ := unstructured.NestedSlice(subset, "addresses")
		total += len(addrs)
	}
	return total
}
