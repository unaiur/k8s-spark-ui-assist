package shs

import (
	"context"
	"encoding/json"
	"log"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

var deploymentsGVR = schema.GroupVersionResource{
	Group:    "apps",
	Version:  "v1",
	Resource: "deployments",
}

// gracePeriod is the window after the scheduled stop time within which the
// scheduler will still fire immediately on startup (e.g. after a pod restart).
const gracePeriod = 30 * time.Minute

// RunStopScheduler scales the SHS Deployment to zero replicas once per day at
// the given UTC hour and minute. It runs until ctx is cancelled.
//
// now is a clock function injected for testability; pass time.Now in production.
//
// On startup the scheduler checks whether the current time falls within the
// grace period after the scheduled time: if so, it fires immediately and then
// waits until the next day's occurrence. Otherwise it waits for the next
// occurrence (today or tomorrow, whichever comes first).
func RunStopScheduler(ctx context.Context, client dynamic.Interface, namespace, deployment string, hour, minute int, now func() time.Time) {
	for {
		delay := durationUntilNextStop(now(), hour, minute)
		log.Printf("shs scheduler: next stop of %q in %s (at %02d:%02d UTC)", deployment, delay.Round(time.Second), hour, minute)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}

		if err := scaleDeploymentToZero(ctx, client, namespace, deployment); err != nil {
			log.Printf("shs scheduler: failed to scale down %q: %v", deployment, err)
		} else {
			log.Printf("shs scheduler: scaled %q to 0 replicas", deployment)
		}
	}
}

// durationUntilNextStop computes how long to wait from t until the next
// occurrence of the scheduled stop time. If t falls within the grace period
// after today's stop time, the function returns zero so the caller fires
// immediately. Otherwise it returns the duration until the next occurrence
// (today if the time hasn't passed yet, or tomorrow if it has).
func durationUntilNextStop(t time.Time, hour, minute int) time.Duration {
	t = t.UTC()
	todayStop := time.Date(t.Year(), t.Month(), t.Day(), hour, minute, 0, 0, time.UTC)

	elapsed := t.Sub(todayStop)
	if elapsed >= 0 && elapsed < gracePeriod {
		// Within the grace window — fire now.
		return 0
	}
	if t.Before(todayStop) {
		return todayStop.Sub(t)
	}
	// Already past today's stop (and outside grace period) — wait until tomorrow.
	tomorrowStop := todayStop.Add(24 * time.Hour)
	return tomorrowStop.Sub(t)
}

// scaleDeploymentToZero patches the named Deployment to spec.replicas = 0.
func scaleDeploymentToZero(ctx context.Context, client dynamic.Interface, namespace, name string) error {
	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"replicas": int64(0),
		},
	}
	patchBytes, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = client.Resource(deploymentsGVR).Namespace(namespace).
		Patch(ctx, name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	return err
}
