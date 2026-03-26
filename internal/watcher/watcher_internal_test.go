// White-box tests for unexported watcher helpers.
// Being in package watcher (not watcher_test) gives access to the private
// functions isSparkDriver, isTerminated, and driverFromPod, and lets us
// exercise the cache.DeletedFinalStateUnknown tombstone path in DeleteFunc
// without needing a real informer.
package watcher

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/unaiur/k8s-spark-ui-assist/internal/labels"
)

// ---- isSparkDriver ----------------------------------------------------------

func TestIsSparkDriverTrue(t *testing.T) {
	pod := &unstructured.Unstructured{}
	pod.SetLabels(map[string]string{
		labels.LabelInstance: labels.InstanceValue,
		labels.LabelRole:     labels.RoleValue,
	})
	if !isSparkDriver(pod) {
		t.Error("expected isSparkDriver true for correctly labelled pod")
	}
}

func TestIsSparkDriverMissingRole(t *testing.T) {
	pod := &unstructured.Unstructured{}
	pod.SetLabels(map[string]string{
		labels.LabelInstance: labels.InstanceValue,
		// no spark-role=driver
	})
	if isSparkDriver(pod) {
		t.Error("expected isSparkDriver false when spark-role label is absent")
	}
}

func TestIsSparkDriverMissingInstance(t *testing.T) {
	pod := &unstructured.Unstructured{}
	pod.SetLabels(map[string]string{
		labels.LabelRole: labels.RoleValue,
		// no app.kubernetes.io/instance=spark-job
	})
	if isSparkDriver(pod) {
		t.Error("expected isSparkDriver false when instance label is absent")
	}
}

func TestIsSparkDriverNoLabels(t *testing.T) {
	pod := &unstructured.Unstructured{}
	if isSparkDriver(pod) {
		t.Error("expected isSparkDriver false for unlabelled pod")
	}
}

// ---- isTerminated -----------------------------------------------------------

func TestIsTerminatedSucceeded(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]interface{}{}}
	_ = unstructured.SetNestedField(pod.Object, "Succeeded", "status", "phase")
	if !isTerminated(pod) {
		t.Error("expected isTerminated true for Succeeded pod")
	}
}

func TestIsTerminatedFailed(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]interface{}{}}
	_ = unstructured.SetNestedField(pod.Object, "Failed", "status", "phase")
	if !isTerminated(pod) {
		t.Error("expected isTerminated true for Failed pod")
	}
}

func TestIsTerminatedRunning(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]interface{}{}}
	_ = unstructured.SetNestedField(pod.Object, "Running", "status", "phase")
	if isTerminated(pod) {
		t.Error("expected isTerminated false for Running pod")
	}
}

func TestIsTerminatedNoPhase(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]interface{}{}}
	if isTerminated(pod) {
		t.Error("expected isTerminated false for pod with no phase")
	}
}

// ---- driverFromPod ----------------------------------------------------------

func TestDriverFromPod(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pod := &unstructured.Unstructured{Object: map[string]interface{}{}}
	pod.SetName("my-pod")
	pod.SetCreationTimestamp(metav1.NewTime(ts))
	pod.SetLabels(map[string]string{
		labels.LabelSelector: "spark-abc",
		labels.LabelAppName:  "my-app",
	})

	d := driverFromPod(pod)

	if d.PodName != "my-pod" {
		t.Errorf("PodName: got %q, want my-pod", d.PodName)
	}
	if d.AppSelector != "spark-abc" {
		t.Errorf("AppSelector: got %q, want spark-abc", d.AppSelector)
	}
	if d.AppName != "my-app" {
		t.Errorf("AppName: got %q, want my-app", d.AppName)
	}
	if !d.CreatedAt.Equal(ts) {
		t.Errorf("CreatedAt: got %v, want %v", d.CreatedAt, ts)
	}
}

// TestDriverFromPodZeroTimestampFallsBackToNow verifies that a pod with a zero
// creation timestamp gets a non-zero CreatedAt (filled with time.Now).
func TestDriverFromPodZeroTimestampFallsBackToNow(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]interface{}{}}
	pod.SetName("my-pod")
	// Do not set a creation timestamp — it will be the zero value.

	before := time.Now()
	d := driverFromPod(pod)
	after := time.Now()

	if d.CreatedAt.Before(before) || d.CreatedAt.After(after) {
		t.Errorf("CreatedAt should be approximately now; got %v", d.CreatedAt)
	}
}
