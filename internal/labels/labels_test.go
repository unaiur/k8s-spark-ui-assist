package labels_test

import (
	"strings"
	"testing"

	"github.com/unaiur/k8s-spark-ui-assist/internal/labels"
)

func TestDriverSelector(t *testing.T) {
	sel := labels.DriverSelector()

	if !strings.Contains(sel, labels.LabelInstance+"="+labels.InstanceValue) {
		t.Errorf("DriverSelector() missing %s=%s: got %q",
			labels.LabelInstance, labels.InstanceValue, sel)
	}
	if !strings.Contains(sel, labels.LabelRole+"="+labels.RoleValue) {
		t.Errorf("DriverSelector() missing %s=%s: got %q",
			labels.LabelRole, labels.RoleValue, sel)
	}
}

func TestDriverSelectorForApp(t *testing.T) {
	const appID = "spark-abc123"
	sel := labels.DriverSelectorForApp(appID)

	// Must include everything from DriverSelector.
	if !strings.Contains(sel, labels.DriverSelector()) {
		t.Errorf("DriverSelectorForApp() does not contain base selector %q: got %q",
			labels.DriverSelector(), sel)
	}
	// Must also include the per-app selector term.
	if !strings.Contains(sel, labels.LabelSelector+"="+appID) {
		t.Errorf("DriverSelectorForApp() missing %s=%s: got %q",
			labels.LabelSelector, appID, sel)
	}
}

// TestDriverSelectorForAppDistinct verifies that two different appIDs produce
// different selectors and that neither contains the other's appID term.
func TestDriverSelectorForAppDistinct(t *testing.T) {
	s1 := labels.DriverSelectorForApp("app-one")
	s2 := labels.DriverSelectorForApp("app-two")

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
