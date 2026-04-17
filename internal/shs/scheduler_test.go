package shs

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"
)

// newFakeClient returns a fake dynamic client with the apps/v1 Deployment
// scheme registered and the provided object pre-populated.
func newFakeClient(objs ...runtime.Object) *fake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
		&unstructured.Unstructured{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "DeploymentList"},
		&unstructured.UnstructuredList{},
	)
	return fake.NewSimpleDynamicClient(scheme, objs...)
}

// ---- durationUntilNextStop tests --------------------------------------------

func TestDurationUntilNextStop_BeforeToday(t *testing.T) {
	// Scheduled for 18:00; current time is 10:00 same day.
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	d := durationUntilNextStop(now, 18, 0)
	want := 8 * time.Hour
	if d != want {
		t.Errorf("got %v, want %v", d, want)
	}
}

func TestDurationUntilNextStop_ExactlyAtStopTime(t *testing.T) {
	// Current time is exactly the stop time — within grace period, should fire immediately.
	now := time.Date(2026, 1, 1, 18, 0, 0, 0, time.UTC)
	d := durationUntilNextStop(now, 18, 0)
	if d != 0 {
		t.Errorf("got %v, want 0 (fire immediately)", d)
	}
}

func TestDurationUntilNextStop_WithinGracePeriod(t *testing.T) {
	// Current time is 18:20, within 30-minute grace window — should fire immediately.
	now := time.Date(2026, 1, 1, 18, 20, 0, 0, time.UTC)
	d := durationUntilNextStop(now, 18, 0)
	if d != 0 {
		t.Errorf("got %v, want 0 (within grace period)", d)
	}
}

func TestDurationUntilNextStop_JustOutsideGracePeriod(t *testing.T) {
	// Current time is 18:31, just outside the 30-minute grace window.
	// Should wait until tomorrow 18:00.
	now := time.Date(2026, 1, 1, 18, 31, 0, 0, time.UTC)
	d := durationUntilNextStop(now, 18, 0)
	want := 23*time.Hour + 29*time.Minute
	if d != want {
		t.Errorf("got %v, want %v", d, want)
	}
}

func TestDurationUntilNextStop_Midnight(t *testing.T) {
	// Scheduled for 00:00; current time is 23:00 the day before.
	// Should wait 1 hour.
	now := time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC)
	d := durationUntilNextStop(now, 0, 0)
	want := time.Hour
	if d != want {
		t.Errorf("got %v, want %v", d, want)
	}
}

func TestDurationUntilNextStop_NonUTCInputNormalized(t *testing.T) {
	// Input time is in a non-UTC location; durationUntilNextStop should
	// interpret schedule in UTC regardless.
	loc := time.FixedZone("UTC+2", 2*3600)
	// 20:00 UTC+2 == 18:00 UTC — should fire immediately.
	now := time.Date(2026, 1, 1, 20, 0, 0, 0, loc)
	d := durationUntilNextStop(now, 18, 0)
	if d != 0 {
		t.Errorf("got %v, want 0 (18:00 UTC = 20:00 UTC+2)", d)
	}
}

// ---- scaleDeploymentToZero tests --------------------------------------------

func TestScaleDeploymentToZero_IssuesMergePatch(t *testing.T) {
	dep := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "spark-history",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"replicas": int64(1),
			},
		},
	}
	fc := newFakeClient(dep)

	err := scaleDeploymentToZero(context.Background(), fc, "default", "spark-history")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var patched bool
	for _, action := range fc.Actions() {
		pa, ok := action.(k8stesting.PatchAction)
		if !ok {
			continue
		}
		if pa.GetResource() == (schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}) &&
			pa.GetName() == "spark-history" &&
			pa.GetPatchType() == types.MergePatchType {
			patched = true
		}
	}
	if !patched {
		t.Errorf("expected MergePatch on deployments/spark-history; actions: %v", fc.Actions())
	}
}

// ---- RunStopScheduler integration test -------------------------------------

// TestRunStopSchedulerFiresImmediatelyInGracePeriod verifies that when the
// current time is within the grace window the scheduler patches the Deployment
// without waiting.
func TestRunStopSchedulerFiresImmediatelyInGracePeriod(t *testing.T) {
	dep := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      "spark-history",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"replicas": int64(1),
			},
		},
	}
	fc := newFakeClient(dep)

	// Simulate current time is exactly at the stop time (grace period → fire now).
	stopNow := time.Date(2026, 1, 1, 18, 0, 0, 0, time.UTC)
	// After the first call (which triggers immediate fire), return a time far
	// from the stop time so the scheduler sleeps ~24h without looping.
	calls := 0
	nowFn := func() time.Time {
		calls++
		if calls == 1 {
			return stopNow
		}
		// Well before tomorrow's stop — scheduler will sleep ~23h.
		return stopNow.Add(time.Minute)
	}

	// Use a PrependReactor to signal when the first patch action arrives.
	patchCh := make(chan struct{}, 1)
	fc.PrependReactor("patch", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		select {
		case patchCh <- struct{}{}:
		default:
		}
		return false, nil, nil // let the default reactor handle it
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		RunStopScheduler(ctx, fc, "default", "spark-history", 18, 0, nowFn)
	}()

	// Wait for the patch to arrive (or timeout).
	select {
	case <-patchCh:
		// Patch received — cancel the context so the scheduler exits.
		cancel()
	case <-ctx.Done():
		t.Error("timed out waiting for Deployment patch")
	}
	<-done

	var patched bool
	for _, action := range fc.Actions() {
		pa, ok := action.(k8stesting.PatchAction)
		if !ok {
			continue
		}
		if pa.GetResource().Resource == "deployments" && pa.GetName() == "spark-history" {
			patched = true
		}
	}
	if !patched {
		t.Errorf("expected Deployment to be patched; actions: %v", fc.Actions())
	}
}
