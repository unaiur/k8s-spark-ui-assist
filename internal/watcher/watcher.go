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

	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

var podGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

// ---- label constants --------------------------------------------------------

const (
	labelInstance = "app.kubernetes.io/instance"
	labelRole     = "spark-role"
	labelSelector = "spark-app-selector"
	labelAppName  = "spark-app-name"

	instanceValue = "spark-job"
	roleValue     = "driver"
)

// driverSelector returns the label selector string that matches all Spark
// driver pods managed by this operator.
func driverSelector() string {
	return labelInstance + "=" + instanceValue + "," + labelRole + "=" + roleValue
}

// driverSelectorForApp returns the label selector string that matches the
// driver pod for a specific Spark application identified by appSelector.
func driverSelectorForApp(appSelector string) string {
	return driverSelector() + "," + labelSelector + "=" + appSelector
}

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
				d := driverFromPod(pod)
				s.Add(d)
				// Terminal pods are stored for display purposes but do not get
				// an HTTPRoute, so OnAdd is not fired for them.
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
				d := driverFromPod(pod)
				s.Add(d)
				if h != nil {
					switch d.State {
					case store.StateRunning:
						h.OnAdd(d)
					case store.StateSucceeded, store.StateFailed:
						// Pod has just terminated: delete the HTTPRoute.
						h.OnRemove(d.AppSelector)
					}
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
					h.OnRemove(pod.GetLabels()[labelSelector])
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
	labelSel := driverSelector()
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
	return podLabels[labelInstance] == instanceValue && podLabels[labelRole] == roleValue
}

// humanReason maps Kubernetes container-state reason strings to human-readable
// descriptions shown in the dashboard tooltip and API response.
var humanReason = map[string]string{
	"ContainerCreating": "Creating the container",
	"ImagePullBackOff":  "Cannot pull the image",
	"ErrImagePull":      "Failed to pull the image",
	"CrashLoopBackOff":  "Container keeps crashing",
	"OOMKilled":         "Out of memory",
	"Error":             "Container exited with error",
	"Completed":         "Completed successfully",
	"Unschedulable":     "Cannot be scheduled",
}

// toHumanReason returns the human-readable form of a Kubernetes reason string,
// falling back to the original value when no mapping exists.
func toHumanReason(k8sReason string) string {
	if h, ok := humanReason[k8sReason]; ok {
		return h
	}
	return k8sReason
}

// stateAndReasonFromPod derives the DriverState and a human-readable reason
// string for the given pod.
//
// Priority for Pending pods:
//  1. PodScheduled condition with status=False → reason from condition
//  2. First container's waiting reason
//  3. Empty reason (plain Pending)
//
// For Running pods the reason is always empty.
// For Succeeded/Failed pods the reason comes from the first container's
// terminated state (reason field, or exit-code-based fallback).
func stateAndReasonFromPod(pod *unstructured.Unstructured) (store.DriverState, string) {
	phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase")
	switch phase {
	case "Running":
		return store.StateRunning, ""
	case "Succeeded", "Failed":
		state := store.StateSucceeded
		if phase == "Failed" {
			state = store.StateFailed
		}
		return state, terminatedReason(pod)
	case "Pending", "":
		return store.StatePending, pendingReason(pod)
	default:
		return store.StateUnknown, ""
	}
}

// pendingReason returns a human-readable reason for a Pending pod.
// It checks the PodScheduled condition first, then the first container's
// waiting reason.
func pendingReason(pod *unstructured.Unstructured) string {
	// 1. Scheduling condition.
	conditions, _, _ := unstructured.NestedSlice(pod.Object, "status", "conditions")
	for _, c := range conditions {
		cond, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if cond["type"] == "PodScheduled" && cond["status"] == "False" {
			if r, _ := cond["reason"].(string); r != "" {
				return toHumanReason(r)
			}
		}
	}
	// 2. Container waiting reason.
	containerStatuses, _, _ := unstructured.NestedSlice(pod.Object, "status", "containerStatuses")
	if len(containerStatuses) > 0 {
		if cs, ok := containerStatuses[0].(map[string]interface{}); ok {
			if r, _, _ := unstructured.NestedString(cs, "state", "waiting", "reason"); r != "" {
				return toHumanReason(r)
			}
		}
	}
	return ""
}

// terminatedReason returns a human-readable reason for a Succeeded/Failed pod
// from its first container's terminated state.
func terminatedReason(pod *unstructured.Unstructured) string {
	containerStatuses, _, _ := unstructured.NestedSlice(pod.Object, "status", "containerStatuses")
	if len(containerStatuses) == 0 {
		return ""
	}
	cs, ok := containerStatuses[0].(map[string]interface{})
	if !ok {
		return ""
	}
	if r, _, _ := unstructured.NestedString(cs, "state", "terminated", "reason"); r != "" {
		return toHumanReason(r)
	}
	return ""
}

func driverFromPod(pod *unstructured.Unstructured) store.Driver {
	podLabels := pod.GetLabels()
	createdAt := pod.GetCreationTimestamp().Time
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	state, reason := stateAndReasonFromPod(pod)
	return store.Driver{
		PodName:     pod.GetName(),
		CreatedAt:   createdAt,
		AppSelector: podLabels[labelSelector],
		AppName:     podLabels[labelAppName],
		State:       state,
		Reason:      reason,
	}
}
