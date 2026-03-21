// Package watcher watches Kubernetes pods for Spark driver events.
package watcher

import (
	"context"
	"log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/tools/cache"

	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

const (
	labelInstance = "app.kubernetes.io/instance"
	labelRole     = "spark-role"
	labelSelector = "spark-app-selector"
	labelAppName  = "spark-app-name"

	instanceValue = "spark-job"
	roleValue     = "driver"
)

// Handler is called when a Spark driver pod is added or removed.
type Handler interface {
	OnAdd(d store.Driver)
	OnRemove(appSelector string)
}

// Watch lists existing Spark driver pods and then watches for changes.
// It blocks until ctx is cancelled.
func Watch(ctx context.Context, lw cache.ListerWatcher, s *store.Store, h Handler) {
	_, informer := cache.NewInformerWithOptions(cache.InformerOptions{
		ListerWatcher: lw,
		ObjectType:    &corev1.Pod{},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				pod, ok := obj.(*corev1.Pod)
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
				if h != nil {
					h.OnAdd(d)
				}
			},
			UpdateFunc: func(_, newObj interface{}) {
				pod, ok := newObj.(*corev1.Pod)
				if !ok {
					return
				}
				if !isSparkDriver(pod) {
					return
				}
				if isTerminated(pod) {
					s.Remove(pod.Name)
					if h != nil {
						h.OnRemove(pod.Labels[labelSelector])
					}
					return
				}
				d := driverFromPod(pod)
				s.Add(d)
				if h != nil {
					h.OnAdd(d)
				}
			},
			DeleteFunc: func(obj interface{}) {
				pod, ok := obj.(*corev1.Pod)
				if !ok {
					// Handle DeletedFinalStateUnknown tombstone.
					tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
					if !ok {
						log.Printf("watcher: unexpected object type on delete: %T", obj)
						return
					}
					pod, ok = tombstone.Obj.(*corev1.Pod)
					if !ok {
						log.Printf("watcher: tombstone contained unexpected type: %T", tombstone.Obj)
						return
					}
				}
				if !isSparkDriver(pod) {
					return
				}
				s.Remove(pod.Name)
				if h != nil {
					h.OnRemove(pod.Labels[labelSelector])
				}
			},
		},
	})

	informer.Run(ctx.Done())
}

// NewListerWatcher returns a ListerWatcher scoped to Spark driver pods in the given namespace.
func NewListerWatcher(namespace string, client cache.Getter) cache.ListerWatcher {
	labelSel := labelInstance + "=" + instanceValue + "," + labelRole + "=" + roleValue
	optsMod := func(opts *metav1.ListOptions) {
		opts.LabelSelector = labelSel
		opts.FieldSelector = fields.Everything().String()
	}
	return cache.NewFilteredListWatchFromClient(client, "pods", namespace, optsMod)
}

func isSparkDriver(pod *corev1.Pod) bool {
	return pod.Labels[labelInstance] == instanceValue && pod.Labels[labelRole] == roleValue
}

func isTerminated(pod *corev1.Pod) bool {
	phase := pod.Status.Phase
	return phase == corev1.PodSucceeded || phase == corev1.PodFailed
}

func driverFromPod(pod *corev1.Pod) store.Driver {
	return store.Driver{
		PodName:     pod.Name,
		CreatedAt:   pod.CreationTimestamp.Time,
		AppSelector: pod.Labels[labelSelector],
		AppName:     pod.Labels[labelAppName],
	}
}
