// White-box tests for unexported shs helpers.
// Being in package shs gives access to readyEndpointCount and sliceState,
// letting us test edge cases without a full fake client setup.
package shs

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/cache"
)

// buildSliceObj builds a minimal EndpointSlice unstructured object with the
// given endpoint entries for use in unit tests.
func buildSliceObj(name string, endpoints []interface{}) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"endpoints": endpoints,
		},
	}
	obj.SetName(name)
	return obj
}

// ---- readyEndpointCount -----------------------------------------------------

func TestReadyEndpointCountAllReady(t *testing.T) {
	ep := buildSliceObj("s", []interface{}{
		map[string]interface{}{"conditions": map[string]interface{}{"ready": true}},
		map[string]interface{}{"conditions": map[string]interface{}{"ready": true}},
	})
	if n := readyEndpointCount(ep); n != 2 {
		t.Errorf("got %d, want 2", n)
	}
}

func TestReadyEndpointCountNotReady(t *testing.T) {
	ep := buildSliceObj("s", []interface{}{
		map[string]interface{}{"conditions": map[string]interface{}{"ready": false}},
	})
	if n := readyEndpointCount(ep); n != 0 {
		t.Errorf("got %d, want 0", n)
	}
}

// TestReadyEndpointCountNoConditionsField verifies that an endpoint with no
// conditions.ready field is treated as ready, matching the Kubernetes API spec
// which defines the field as optional and defaults it to true for compatibility.
func TestReadyEndpointCountNoConditionsField(t *testing.T) {
	ep := buildSliceObj("s", []interface{}{
		// no "conditions" field at all
		map[string]interface{}{"addresses": []interface{}{"10.0.0.1"}},
		// conditions present but no "ready" key
		map[string]interface{}{"conditions": map[string]interface{}{}},
	})
	if n := readyEndpointCount(ep); n != 2 {
		t.Errorf("got %d, want 2 (absent conditions.ready should default to ready)", n)
	}
}

func TestReadyEndpointCountMixed(t *testing.T) {
	ep := buildSliceObj("s", []interface{}{
		map[string]interface{}{"conditions": map[string]interface{}{"ready": true}},
		map[string]interface{}{"conditions": map[string]interface{}{"ready": false}},
		map[string]interface{}{"addresses": []interface{}{"10.0.0.3"}}, // no conditions → ready
	})
	if n := readyEndpointCount(ep); n != 2 {
		t.Errorf("got %d, want 2", n)
	}
}

func TestReadyEndpointCountEmpty(t *testing.T) {
	ep := buildSliceObj("s", []interface{}{})
	if n := readyEndpointCount(ep); n != 0 {
		t.Errorf("got %d, want 0", n)
	}
}

// ---- sliceState.deleteSlice (tombstone path) ---------------------------------

// minimalHandler records OnUp/OnDown calls for use in synchronous white-box tests.
type minimalHandler struct {
	calls []string
}

func (h *minimalHandler) OnUp()   { h.calls = append(h.calls, "up") }
func (h *minimalHandler) OnDown() { h.calls = append(h.calls, "down") }

// TestDeleteSliceTombstone verifies that when the informer delivers a
// DeletedFinalStateUnknown tombstone (which happens when the watch misses a
// delete event), the slice is still removed and OnDown is fired correctly.
func TestDeleteSliceTombstone(t *testing.T) {
	h := &minimalHandler{}
	s := newSliceState("svc", h)

	ep := buildSliceObj("slice-1", []interface{}{
		map[string]interface{}{"conditions": map[string]interface{}{"ready": true}},
	})

	// Simulate the slice being added (triggers OnUp).
	s.handleSlice(ep)
	if len(h.calls) != 1 || h.calls[0] != "up" {
		t.Fatalf("expected [up] after handleSlice, got %v", h.calls)
	}

	// Deliver the delete as a tombstone (the normal informer path when a
	// watch event is missed and the object is re-listed as gone).
	tombstone := cache.DeletedFinalStateUnknown{Key: "default/slice-1", Obj: ep}
	s.deleteSlice(tombstone)

	if len(h.calls) != 2 || h.calls[1] != "down" {
		t.Errorf("expected [up down] after tombstone delete, got %v", h.calls)
	}
	if s.totalReady() != 0 {
		t.Errorf("expected totalReady=0 after delete, got %d", s.totalReady())
	}
}

// TestDeleteSliceNoOnDownWhenAlreadyDown verifies that deleting a slice when
// the service is already down does not emit a spurious OnDown.
func TestDeleteSliceNoOnDownWhenAlreadyDown(t *testing.T) {
	h := &minimalHandler{}
	s := newSliceState("svc", h)

	ep := buildSliceObj("slice-1", []interface{}{})
	// Add with zero ready endpoints — wasReady stays false, no OnUp.
	s.handleSlice(ep)
	if len(h.calls) != 0 {
		t.Fatalf("expected no events, got %v", h.calls)
	}

	// Delete the slice — still down, no OnDown should fire.
	s.deleteSlice(ep)
	if len(h.calls) != 0 {
		t.Errorf("expected no events after delete when already down, got %v", h.calls)
	}
}
