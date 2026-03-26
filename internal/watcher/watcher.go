// Package watcher watches Kubernetes pods for Spark driver events.
package watcher

import (
	"context"
	"log"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	"github.com/unaiur/k8s-spark-ui-assist/internal/labels"
	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

var podGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

// Handler is called when a Spark driver pod is added or removed.
type Handler interface {
	OnAdd(d store.Driver)
	OnRemove(appSelector string)
}

// Watch lists existing Spark driver pods and then watches for changes.
// It blocks until ctx is cancelled.
// onSynced, if non-nil, is called once after the initial pod list has been
// processed (i.e. after all existing pods have fired AddFunc).
func Watch(ctx context.Context, lw cache.ListerWatcher, s *store.Store, h Handler, onSynced func()) {
	store, informer := cache.NewInformerWithOptions(cache.InformerOptions{
		ListerWatcher: lw,
		ObjectType:    &unstructured.Unstructured{},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				pod, ok := obj.(*unstructured.Unstructured)
				if !ok {
					return
				}
				if !isSparkDriver(pod) {
					return
				}
				if isTerminated(pod) {
					return
				}
				d := driverFromPod(pod)
				s.Add(d)
				if h != nil && d.State == store.StateRunning {
					h.OnAdd(d)
				}
			},
			UpdateFunc: func(_, newObj interface{}) {
				pod, ok := newObj.(*unstructured.Unstructured)
				if !ok {
					return
				}
				if !isSparkDriver(pod) {
					return
				}
				if isTerminated(pod) {
					s.Remove(pod.GetName())
					if h != nil {
						h.OnRemove(pod.GetLabels()[labels.LabelSelector])
					}
					return
				}
				d := driverFromPod(pod)
				s.Add(d)
				if h != nil && d.State == store.StateRunning {
					h.OnAdd(d)
				}
			},
			DeleteFunc: func(obj interface{}) {
				pod, ok := obj.(*unstructured.Unstructured)
				if !ok {
					// Handle DeletedFinalStateUnknown tombstone.
					tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
					if !ok {
						log.Printf("watcher: unexpected object type on delete: %T", obj)
						return
					}
					pod, ok = tombstone.Obj.(*unstructured.Unstructured)
					if !ok {
						log.Printf("watcher: tombstone contained unexpected type: %T", tombstone.Obj)
						return
					}
				}
				if !isSparkDriver(pod) {
					return
				}
				s.Remove(pod.GetName())
				if h != nil {
					h.OnRemove(pod.GetLabels()[labels.LabelSelector])
				}
			},
		},
	})
	_ = store // used only for its HasSynced via the informer controller below

	if onSynced != nil {
		go func() {
			if cache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
				onSynced()
			}
		}()
	}

	informer.Run(ctx.Done())
}

// NewListerWatcher returns a ListerWatcher scoped to Spark driver pods in the given namespace.
func NewListerWatcher(namespace string, client dynamic.Interface) cache.ListerWatcher {
	labelSel := labels.DriverSelector()
	rc := client.Resource(podGVR).Namespace(namespace)
	return &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			opts.LabelSelector = labelSel
			return rc.List(context.Background(), opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			opts.LabelSelector = labelSel
			return rc.Watch(context.Background(), opts)
		},
	}
}

func isSparkDriver(pod *unstructured.Unstructured) bool {
	podLabels := pod.GetLabels()
	return podLabels[labels.LabelInstance] == labels.InstanceValue && podLabels[labels.LabelRole] == labels.RoleValue
}

func isTerminated(pod *unstructured.Unstructured) bool {
	phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase")
	return phase == "Succeeded" || phase == "Failed"
}

// stateFromPodPhase maps a Kubernetes pod phase string to a store.DriverState.
// Only non-terminated phases are expected here (terminated pods are filtered
// out before driverFromPod is called).
func stateFromPodPhase(phase string) store.DriverState {
	switch phase {
	case "Running":
		return store.StateRunning
	case "Pending", "":
		return store.StatePending
	default:
		return store.StateUnknown
	}
}

func driverFromPod(pod *unstructured.Unstructured) store.Driver {
	podLabels := pod.GetLabels()
	createdAt := pod.GetCreationTimestamp().Time
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase")
	return store.Driver{
		PodName:     pod.GetName(),
		CreatedAt:   createdAt,
		AppSelector: podLabels[labels.LabelSelector],
		AppName:     podLabels[labels.LabelAppName],
		State:       stateFromPodPhase(phase),
	}
}
