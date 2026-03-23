// Package httproute manages a single shared Gateway API HTTPRoute for all Spark driver UIs.
//
// Instead of one HTTPRoute per driver, a single HTTPRoute named "<release>-spark-ui-routes"
// (or whatever the Manager is configured with) holds one rule per active driver.
// Ensure adds a rule for a driver if it is not already present; Delete removes the rule
// for a driver and deletes the whole HTTPRoute when the last rule is gone.
//
// The update loop retries on conflict to handle concurrent writers using optimistic
// concurrency (resourceVersion).
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

// Manager maintains a single shared HTTPRoute for all active Spark driver UIs.
type Manager struct {
	client    dynamic.Interface
	namespace string
	routeName string
	cfg       config.HTTPRouteConfig
}

// New creates a new Manager.
// routeName is the name of the shared HTTPRoute resource that will be created/managed.
func New(client dynamic.Interface, namespace string, cfg config.HTTPRouteConfig) *Manager {
	return &Manager{
		client:    client,
		namespace: namespace,
		routeName: "spark-ui-routes",
		cfg:       cfg,
	}
}

// Ensure adds a routing rule for the given driver to the shared HTTPRoute.
// If the shared HTTPRoute does not exist yet it is created. If a rule for the
// driver already exists the call is a no-op. The operation retries on conflict.
func (m *Manager) Ensure(ctx context.Context, d store.Driver) {
	rc := m.client.Resource(httpRouteGVR).Namespace(m.namespace)

	for attempt := range maxRetries {
		route, err := rc.Get(ctx, m.routeName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			// Route does not exist yet – create it with the single rule for this driver.
			newRoute := m.buildRoute([]interface{}{m.buildRule(d)})
			_, createErr := rc.Create(ctx, newRoute, metav1.CreateOptions{})
			if createErr == nil {
				log.Printf("httproute: created shared HTTPRoute %s with rule for %s", m.routeName, d.AppSelector)
				return
			}
			if errors.IsAlreadyExists(createErr) {
				// Lost a race with another goroutine; retry the loop to add our rule.
				log.Printf("httproute: concurrent create detected for %s, retrying (attempt %d)", m.routeName, attempt+1)
				continue
			}
			log.Printf("httproute: failed to create HTTPRoute %s: %v", m.routeName, createErr)
			return
		}
		if err != nil {
			log.Printf("httproute: failed to get HTTPRoute %s: %v", m.routeName, err)
			return
		}

		// Route exists – check whether a rule for this driver is already present.
		rules := getRules(route)
		for _, r := range rules {
			if ruleMatchesDriver(r, d.AppSelector) {
				// Already present; nothing to do.
				return
			}
		}

		// Append a new rule and update.
		newRules := append(rules, m.buildRule(d))
		setRules(route, newRules)

		_, updateErr := rc.Update(ctx, route, metav1.UpdateOptions{})
		if updateErr == nil {
			log.Printf("httproute: added rule for %s to HTTPRoute %s", d.AppSelector, m.routeName)
			return
		}
		if errors.IsConflict(updateErr) {
			log.Printf("httproute: conflict updating %s, retrying (attempt %d)", m.routeName, attempt+1)
			continue
		}
		log.Printf("httproute: failed to update HTTPRoute %s: %v", m.routeName, updateErr)
		return
	}

	log.Printf("httproute: gave up adding rule for %s after %d retries", d.AppSelector, maxRetries)
}

// Delete removes the routing rule for the given appSelector from the shared HTTPRoute.
// If the HTTPRoute has no remaining rules after removal it is deleted entirely.
// The operation retries on conflict.
func (m *Manager) Delete(ctx context.Context, appSelector string) {
	rc := m.client.Resource(httpRouteGVR).Namespace(m.namespace)

	for attempt := range maxRetries {
		route, err := rc.Get(ctx, m.routeName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			// Nothing to do.
			return
		}
		if err != nil {
			log.Printf("httproute: failed to get HTTPRoute %s: %v", m.routeName, err)
			return
		}

		rules := getRules(route)
		filtered := make([]interface{}, 0, len(rules))
		for _, r := range rules {
			if !ruleMatchesDriver(r, appSelector) {
				filtered = append(filtered, r)
			}
		}

		if len(filtered) == len(rules) {
			// Rule was not present; nothing to do.
			return
		}

		if len(filtered) == 0 {
			// No rules remain – delete the whole HTTPRoute.
			deleteErr := rc.Delete(ctx, m.routeName, metav1.DeleteOptions{})
			if deleteErr == nil {
				log.Printf("httproute: deleted shared HTTPRoute %s (no rules remaining)", m.routeName)
				return
			}
			if errors.IsNotFound(deleteErr) {
				return
			}
			log.Printf("httproute: failed to delete HTTPRoute %s: %v", m.routeName, deleteErr)
			return
		}

		// Update the route with the filtered rule list.
		setRules(route, filtered)
		_, updateErr := rc.Update(ctx, route, metav1.UpdateOptions{})
		if updateErr == nil {
			log.Printf("httproute: removed rule for %s from HTTPRoute %s", appSelector, m.routeName)
			return
		}
		if errors.IsConflict(updateErr) {
			log.Printf("httproute: conflict updating %s, retrying (attempt %d)", m.routeName, attempt+1)
			continue
		}
		log.Printf("httproute: failed to update HTTPRoute %s: %v", m.routeName, updateErr)
		return
	}

	log.Printf("httproute: gave up removing rule for %s after %d retries", appSelector, maxRetries)
}

// buildRoute constructs a new shared HTTPRoute with the given rules slice.
func (m *Manager) buildRoute(rules []interface{}) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "HTTPRoute",
			"metadata": map[string]interface{}{
				"name":      m.routeName,
				"namespace": m.namespace,
			},
			"spec": map[string]interface{}{
				"parentRefs": []interface{}{
					map[string]interface{}{
						"name":      m.cfg.GatewayName,
						"namespace": m.cfg.GatewayNamespace,
						"port":      int64(443),
					},
				},
				"hostnames": []interface{}{m.cfg.Hostname},
				"rules":     rules,
			},
		},
	}
}

// buildRule returns a single HTTPRoute rule for the given driver.
// The rule uses the spark-app-selector as the path prefix component so we can
// identify and remove it later without needing additional annotations.
func (m *Manager) buildRule(d store.Driver) interface{} {
	pathPrefix := "/live/" + d.AppSelector
	svcName := d.AppName + "-ui-svc"

	return map[string]interface{}{
		// Store the appSelector in a way we can recover it for matching.
		// We encode it as a header match on an internal header that will
		// never actually be present – but the path match itself is unique
		// enough: we key on the path value, which equals "/live/<appSelector>".
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

// ruleMatchesDriver returns true when the rule's path prefix value encodes the
// given appSelector (i.e. the path is "/live/<appSelector>").
func ruleMatchesDriver(rule interface{}, appSelector string) bool {
	r, ok := rule.(map[string]interface{})
	if !ok {
		return false
	}
	matches, _, _ := unstructured.NestedSlice(r, "matches")
	for _, m := range matches {
		val, _, _ := unstructured.NestedString(m.(map[string]interface{}), "path", "value")
		if val == "/live/"+appSelector {
			return true
		}
	}
	return false
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
