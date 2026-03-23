// Package httproute manages per-driver rules inside a shared Gateway API HTTPRoute.
//
// The HTTPRoute itself is created and owned by the Helm chart.  It contains a
// permanent catch-all rule (path "/") that routes traffic to the dashboard
// service.  The Manager adds one path-prefix rule per active Spark driver
// (path "/live/<appSelector>") and removes it when the driver stops.
//
// Because driver rules must be evaluated before the catch-all, they are always
// inserted before the last rule in the route's rule list.
//
// Reconcile() should be called once the pod informer has fully synced so that
// stale rules left over from a previous instance of the service are cleaned up.
package httproute

import (
	"context"
	"log"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/unaiur/k8s-spark-ui-assist/internal/config"
	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

var httpRouteGVR = schema.GroupVersionResource{
	Group:    "gateway.networking.k8s.io",
	Version:  "v1",
	Resource: "httproutes",
}

const maxRetries = 5

// Manager adds and removes per-driver rules inside the shared HTTPRoute.
type Manager struct {
	client    dynamic.Interface
	namespace string
	cfg       config.HTTPRouteConfig
}

// New creates a new Manager. The shared HTTPRoute must already exist (created
// by the Helm chart); its name is taken from cfg.RouteName.
func New(client dynamic.Interface, namespace string, cfg config.HTTPRouteConfig) *Manager {
	return &Manager{client: client, namespace: namespace, cfg: cfg}
}

// Ensure adds a routing rule for the given driver to the shared HTTPRoute if
// one is not already present. The rule is inserted before the catch-all rule.
// The operation retries on conflict.
func (m *Manager) Ensure(ctx context.Context, d store.Driver) {
	m.updateWithRetry(ctx, "add rule for "+d.AppSelector, func(rules []interface{}) []interface{} {
		for _, r := range rules {
			if ruleMatchesDriver(r, d.AppSelector) {
				return nil // already present, no update needed
			}
		}
		return insertBeforeLast(rules, buildDriverRule(d))
	})
}

// Delete removes the routing rule for the given appSelector from the shared
// HTTPRoute. It is a no-op if no such rule exists. The HTTPRoute itself is
// never deleted — it is owned by the Helm chart.
// The operation retries on conflict.
func (m *Manager) Delete(ctx context.Context, appSelector string) {
	m.updateWithRetry(ctx, "remove rule for "+appSelector, func(rules []interface{}) []interface{} {
		filtered := removeDriverRule(rules, appSelector)
		if len(filtered) == len(rules) {
			return nil // rule was not present, no update needed
		}
		return filtered
	})
}

// Reconcile synchronises the driver rules in the shared HTTPRoute against the
// provided list of currently active drivers. It removes rules for drivers that
// are no longer active and adds rules for drivers that are missing one.
// Call this once after the pod informer has fully synced.
func (m *Manager) Reconcile(ctx context.Context, active []store.Driver) {
	activeSet := make(map[string]store.Driver, len(active))
	for _, d := range active {
		activeSet[d.AppSelector] = d
	}

	m.updateWithRetry(ctx, "reconcile", func(rules []interface{}) []interface{} {
		kept := make([]interface{}, 0, len(rules))
		presentSelectors := make(map[string]bool)
		changed := false

		for _, r := range rules {
			sel := driverSelector(r)
			if sel == "" {
				kept = append(kept, r) // catch-all or other non-driver rule; always keep
				continue
			}
			if _, ok := activeSet[sel]; ok {
				kept = append(kept, r)
				presentSelectors[sel] = true
			} else {
				log.Printf("httproute: reconcile: removing stale rule for %s", sel)
				changed = true
			}
		}

		for _, d := range active {
			if !presentSelectors[d.AppSelector] {
				log.Printf("httproute: reconcile: adding missing rule for %s", d.AppSelector)
				kept = insertBeforeLast(kept, buildDriverRule(d))
				changed = true
			}
		}

		if !changed {
			return nil // already up to date
		}
		return kept
	})
}

// updateWithRetry fetches the shared HTTPRoute, calls mutate with the current
// rules, and writes the result back. If mutate returns nil the update is
// skipped. Conflicts are retried up to maxRetries times.
// op is a short description used in log messages.
func (m *Manager) updateWithRetry(ctx context.Context, op string, mutate func([]interface{}) []interface{}) {
	rc := m.client.Resource(httpRouteGVR).Namespace(m.namespace)

	for attempt := range maxRetries {
		route, err := rc.Get(ctx, m.cfg.RouteName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return // route doesn't exist; nothing to do
		}
		if err != nil {
			log.Printf("httproute: %s: failed to get HTTPRoute %s: %v", op, m.cfg.RouteName, err)
			return
		}

		newRules := mutate(getRules(route))
		if newRules == nil {
			return // mutator signalled no change needed
		}

		setRules(route, newRules)
		_, updateErr := rc.Update(ctx, route, metav1.UpdateOptions{})
		if updateErr == nil {
			log.Printf("httproute: %s: updated HTTPRoute %s", op, m.cfg.RouteName)
			return
		}
		if errors.IsConflict(updateErr) {
			log.Printf("httproute: %s: conflict, retrying (attempt %d)", op, attempt+1)
			continue
		}
		log.Printf("httproute: %s: failed to update HTTPRoute %s: %v", op, m.cfg.RouteName, updateErr)
		return
	}

	log.Printf("httproute: %s: gave up after %d retries", op, maxRetries)
}

// buildDriverRule returns a single HTTPRoute rule for the given driver.
// The path value "/live/<appSelector>" acts as the unique key for the rule.
func buildDriverRule(d store.Driver) interface{} {
	pathPrefix := "/live/" + d.AppSelector
	svcName := d.AppName + "-ui-svc"

	return map[string]interface{}{
		"matches": []interface{}{
			map[string]interface{}{
				"path": map[string]interface{}{
					"type":  "PathPrefix",
					"value": pathPrefix,
				},
			},
		},
		"filters": []interface{}{
			map[string]interface{}{
				"type": "URLRewrite",
				"urlRewrite": map[string]interface{}{
					"path": map[string]interface{}{
						"type":               "ReplacePrefixMatch",
						"replacePrefixMatch": "/",
					},
				},
			},
		},
		"backendRefs": []interface{}{
			map[string]interface{}{
				"name": svcName,
				"port": int64(4040),
			},
		},
	}
}

// driverSelector returns the appSelector encoded in the rule's path value
// ("/live/<appSelector>"), or "" if the rule is not a driver rule.
func driverSelector(rule interface{}) string {
	r, ok := rule.(map[string]interface{})
	if !ok {
		return ""
	}
	matches, _, _ := unstructured.NestedSlice(r, "matches")
	for _, m := range matches {
		val, _, _ := unstructured.NestedString(m.(map[string]interface{}), "path", "value")
		if len(val) > len("/live/") && val[:6] == "/live/" {
			return val[6:]
		}
	}
	return ""
}

// ruleMatchesDriver returns true when the rule belongs to appSelector.
func ruleMatchesDriver(rule interface{}, appSelector string) bool {
	return driverSelector(rule) == appSelector
}

// removeDriverRule returns a copy of rules with the rule for appSelector removed.
func removeDriverRule(rules []interface{}, appSelector string) []interface{} {
	out := make([]interface{}, 0, len(rules))
	for _, r := range rules {
		if !ruleMatchesDriver(r, appSelector) {
			out = append(out, r)
		}
	}
	return out
}

// insertBeforeLast inserts elem before the last element of rules.
// If rules is empty the element becomes the only element.
func insertBeforeLast(rules []interface{}, elem interface{}) []interface{} {
	if len(rules) == 0 {
		return []interface{}{elem}
	}
	out := make([]interface{}, 0, len(rules)+1)
	out = append(out, rules[:len(rules)-1]...)
	out = append(out, elem)
	out = append(out, rules[len(rules)-1])
	return out
}

// getRules extracts the spec.rules slice from an HTTPRoute object.
func getRules(route *unstructured.Unstructured) []interface{} {
	rules, _, _ := unstructured.NestedSlice(route.Object, "spec", "rules")
	return rules
}

// setRules replaces the spec.rules slice on an HTTPRoute object.
func setRules(route *unstructured.Unstructured, rules []interface{}) {
	_ = unstructured.SetNestedSlice(route.Object, rules, "spec", "rules")
}
