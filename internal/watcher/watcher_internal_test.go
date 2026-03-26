// White-box tests for unexported watcher helpers.
// Being in package watcher (not watcher_test) gives access to the private
// functions isSparkDriver, stateFromPodPhase, and driverFromPod,
// and lets us exercise the cache.DeletedFinalStateUnknown tombstone path in
// DeleteFunc without needing a real informer.
package watcher

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

// ---- label selector helpers -------------------------------------------------

func TestDriverSelector(t *testing.T) {
	sel := driverSelector()

	if !strings.Contains(sel, labelInstance+"="+instanceValue) {
		t.Errorf("driverSelector() missing %s=%s: got %q",
			labelInstance, instanceValue, sel)
	}
	if !strings.Contains(sel, labelRole+"="+roleValue) {
		t.Errorf("driverSelector() missing %s=%s: got %q",
			labelRole, roleValue, sel)
	}
}

func TestDriverSelectorForApp(t *testing.T) {
	const appID = "spark-abc123"
	sel := driverSelectorForApp(appID)

	if !strings.Contains(sel, driverSelector()) {
		t.Errorf("driverSelectorForApp() does not contain base selector %q: got %q",
			driverSelector(), sel)
	}
	if !strings.Contains(sel, labelSelector+"="+appID) {
		t.Errorf("driverSelectorForApp() missing %s=%s: got %q",
			labelSelector, appID, sel)
	}
}

func TestDriverSelectorForAppDistinct(t *testing.T) {
	s1 := driverSelectorForApp("app-one")
	s2 := driverSelectorForApp("app-two")

	if s1 == s2 {
		t.Errorf("selectors for different appIDs should differ, both got %q", s1)
	}
	if strings.Contains(s1, "app-two") {
		t.Errorf("selector for app-one should not mention app-two: %q", s1)
	}
	if strings.Contains(s2, "app-one") {
		t.Errorf("selector for app-two should not mention app-one: %q", s2)
	}
}

// ---- isSparkDriver ----------------------------------------------------------

func TestIsSparkDriverTrue(t *testing.T) {
	pod := &unstructured.Unstructured{}
	pod.SetLabels(map[string]string{
		labelInstance: instanceValue,
		labelRole:     roleValue,
	})
	if !isSparkDriver(pod) {
		t.Error("expected isSparkDriver true for correctly labelled pod")
	}
}

func TestIsSparkDriverMissingRole(t *testing.T) {
	pod := &unstructured.Unstructured{}
	pod.SetLabels(map[string]string{
		labelInstance: instanceValue,
		// no spark-role=driver
	})
	if isSparkDriver(pod) {
		t.Error("expected isSparkDriver false when spark-role label is absent")
	}
}

func TestIsSparkDriverMissingInstance(t *testing.T) {
	pod := &unstructured.Unstructured{}
	pod.SetLabels(map[string]string{
		labelRole: roleValue,
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

// ---- stateAndReasonFromPod --------------------------------------------------

// helper: build a minimal pod with a given phase and optional PodScheduled
// condition and container status.
func podWithPhase(phase string) *unstructured.Unstructured {
	pod := &unstructured.Unstructured{Object: map[string]interface{}{}}
	if phase != "" {
		_ = unstructured.SetNestedField(pod.Object, phase, "status", "phase")
	}
	return pod
}

func setUnschedulableCondition(pod *unstructured.Unstructured) {
	_ = unstructured.SetNestedSlice(pod.Object, []interface{}{
		map[string]interface{}{
			"type":   "PodScheduled",
			"status": "False",
			"reason": "Unschedulable",
		},
	}, "status", "conditions")
}

func setContainerWaiting(pod *unstructured.Unstructured, reason string) {
	_ = unstructured.SetNestedSlice(pod.Object, []interface{}{
		map[string]interface{}{
			"name": "spark-kubernetes-driver",
			"state": map[string]interface{}{
				"waiting": map[string]interface{}{
					"reason": reason,
				},
			},
		},
	}, "status", "containerStatuses")
}

func setContainerTerminated(pod *unstructured.Unstructured, reason string, exitCode int64) {
	t := map[string]interface{}{"exitCode": exitCode}
	if reason != "" {
		t["reason"] = reason
	}
	_ = unstructured.SetNestedSlice(pod.Object, []interface{}{
		map[string]interface{}{
			"name":  "spark-kubernetes-driver",
			"state": map[string]interface{}{"terminated": t},
		},
	}, "status", "containerStatuses")
}

func TestStateAndReasonRunning(t *testing.T) {
	pod := podWithPhase("Running")
	state, reason := stateAndReasonFromPod(pod)
	if state != store.StateRunning {
		t.Errorf("state: got %q, want Running", state)
	}
	if reason != "" {
		t.Errorf("reason: got %q, want empty", reason)
	}
}

func TestStateAndReasonPendingNoDetail(t *testing.T) {
	pod := podWithPhase("Pending")
	state, reason := stateAndReasonFromPod(pod)
	if state != store.StatePending {
		t.Errorf("state: got %q, want Pending", state)
	}
	if reason != "" {
		t.Errorf("reason: got %q, want empty", reason)
	}
}

func TestStateAndReasonEmptyPhase(t *testing.T) {
	pod := podWithPhase("")
	state, reason := stateAndReasonFromPod(pod)
	if state != store.StatePending {
		t.Errorf("state: got %q, want Pending", state)
	}
	if reason != "" {
		t.Errorf("reason: got %q, want empty", reason)
	}
}

func TestStateAndReasonUnschedulable(t *testing.T) {
	pod := podWithPhase("Pending")
	setUnschedulableCondition(pod)
	state, reason := stateAndReasonFromPod(pod)
	if state != store.StatePending {
		t.Errorf("state: got %q, want Pending", state)
	}
	if reason != "Cannot be scheduled" {
		t.Errorf("reason: got %q, want %q", reason, "Cannot be scheduled")
	}
}

func TestStateAndReasonPendingContainerCreating(t *testing.T) {
	pod := podWithPhase("Pending")
	setContainerWaiting(pod, "ContainerCreating")
	state, reason := stateAndReasonFromPod(pod)
	if state != store.StatePending {
		t.Errorf("state: got %q, want Pending", state)
	}
	if reason != "Creating the container" {
		t.Errorf("reason: got %q, want %q", reason, "Creating the container")
	}
}

func TestStateAndReasonPendingCrashLoop(t *testing.T) {
	pod := podWithPhase("Pending")
	setContainerWaiting(pod, "CrashLoopBackOff")
	state, reason := stateAndReasonFromPod(pod)
	if state != store.StatePending {
		t.Errorf("state: got %q, want Pending", state)
	}
	if reason != "Container keeps crashing" {
		t.Errorf("reason: got %q, want %q", reason, "Container keeps crashing")
	}
}

func TestStateAndReasonPendingUnknownWaitingReason(t *testing.T) {
	// Unrecognised waiting reason passes through as-is.
	pod := podWithPhase("Pending")
	setContainerWaiting(pod, "SomeFutureReason")
	_, reason := stateAndReasonFromPod(pod)
	if reason != "SomeFutureReason" {
		t.Errorf("reason: got %q, want %q", reason, "SomeFutureReason")
	}
}

func TestStateAndReasonUnschedulableTakesPriorityOverWaiting(t *testing.T) {
	// Condition check has priority over container waiting reason.
	pod := podWithPhase("Pending")
	setUnschedulableCondition(pod)
	setContainerWaiting(pod, "ContainerCreating")
	_, reason := stateAndReasonFromPod(pod)
	if reason != "Cannot be scheduled" {
		t.Errorf("reason: got %q, want %q", reason, "Cannot be scheduled")
	}
}

func TestStateAndReasonSucceededNoContainerStatus(t *testing.T) {
	pod := podWithPhase("Succeeded")
	state, reason := stateAndReasonFromPod(pod)
	if state != store.StateSucceeded {
		t.Errorf("state: got %q, want Succeeded", state)
	}
	if reason != "" {
		t.Errorf("reason: got %q, want empty", reason)
	}
}

func TestStateAndReasonFailedOOMKilled(t *testing.T) {
	pod := podWithPhase("Failed")
	setContainerTerminated(pod, "OOMKilled", 137)
	state, reason := stateAndReasonFromPod(pod)
	if state != store.StateFailed {
		t.Errorf("state: got %q, want Failed", state)
	}
	if reason != "Out of memory" {
		t.Errorf("reason: got %q, want %q", reason, "Out of memory")
	}
}

func TestStateAndReasonFailedNoReason(t *testing.T) {
	pod := podWithPhase("Failed")
	setContainerTerminated(pod, "", 1)
	state, reason := stateAndReasonFromPod(pod)
	if state != store.StateFailed {
		t.Errorf("state: got %q, want Failed", state)
	}
	if reason != "" {
		t.Errorf("reason: got %q, want empty", reason)
	}
}

func TestStateAndReasonUnknownPhase(t *testing.T) {
	pod := podWithPhase("SomeFuturePhase")
	state, reason := stateAndReasonFromPod(pod)
	if state != store.StateUnknown {
		t.Errorf("state: got %q, want Unknown", state)
	}
	if reason != "" {
		t.Errorf("reason: got %q, want empty", reason)
	}
}

// ---- driverFromPod ----------------------------------------------------------

func TestDriverFromPod(t *testing.T) {
	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	pod := &unstructured.Unstructured{Object: map[string]interface{}{}}
	pod.SetName("my-pod")
	pod.SetCreationTimestamp(metav1.NewTime(ts))
	pod.SetLabels(map[string]string{
		labelSelector: "spark-abc",
		labelAppName:  "my-app",
	})
	_ = unstructured.SetNestedField(pod.Object, "Running", "status", "phase")

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
	if d.State != store.StateRunning {
		t.Errorf("State: got %q, want %q", d.State, store.StateRunning)
	}
}

func TestDriverFromPodPendingState(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]interface{}{}}
	pod.SetName("my-pod")
	_ = unstructured.SetNestedField(pod.Object, "Pending", "status", "phase")

	d := driverFromPod(pod)
	if d.State != store.StatePending {
		t.Errorf("State: got %q, want %q", d.State, store.StatePending)
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
