// Package k8s provides a KubernetesSvc abstraction over the raw dynamic client.
//
// KubernetesSvc bundles the context, dynamic client, and namespace together so
// that callers (api, httproute) do not need to pass them separately on every
// call.
package k8s

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/unaiur/k8s-spark-ui-assist/internal/config"
	"github.com/unaiur/k8s-spark-ui-assist/internal/labels"
	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

var (
	podGVR = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}

	httpRouteGVR = schema.GroupVersionResource{
		Group:    "gateway.networking.k8s.io",
		Version:  "v1",
		Resource: "httproutes",
	}
)

// managedByLabel / managedByValue are the label applied to every HTTPRoute
// created by this service so that listing and reconciliation can be scoped.
const (
	managedByLabel   = "app.kubernetes.io/managed-by"
	managedByValue   = "spark-ui-assist"
	driverPathPrefix = "/proxy/"
)

// KubernetesSvc bundles a context, dynamic client, and namespace so that
// individual operations do not need all three passed separately.
type KubernetesSvc struct {
	ctx       context.Context
	client    dynamic.Interface
	namespace string
}

// New creates a KubernetesSvc.
func New(ctx context.Context, client dynamic.Interface, namespace string) *KubernetesSvc {
	return &KubernetesSvc{ctx: ctx, client: client, namespace: namespace}
}

// SparkDriverState returns the state string for the Spark driver pod identified
// by appID (the spark-app-selector label value). Returns ("", nil) when no
// matching pod exists.
//
// State derivation rules (in priority order):
//  1. If any container status is present, inspect the first container:
//     - waiting  → waiting.reason (e.g. "ContainerCreating", "CrashLoopBackOff")
//     - running  → "Running"
//     - terminated → terminated.reason if non-empty, else "Error" (non-zero
//     exitCode) or "Completed" (zero exitCode)
//  2. No container status yet: inspect pod conditions for a False PodScheduled
//     condition and return its reason; otherwise return "Pending".
func (s *KubernetesSvc) SparkDriverState(appID string) (string, error) {
	list, err := s.client.Resource(podGVR).Namespace(s.namespace).List(s.ctx, metav1.ListOptions{
		LabelSelector: labels.DriverSelectorForApp(appID),
	})
	if err != nil {
		return "", err
	}
	if len(list.Items) == 0 {
		return "", nil
	}

	pod := mostRecent(list.Items)
	return stateFromPod(pod), nil
}

// CreateDriverRoute creates an HTTPRoute for the given driver using cfg.
// It is a no-op when the route already exists (AlreadyExists is silently
// ignored so that concurrent calls are safe).
func (s *KubernetesSvc) CreateDriverRoute(d store.Driver, cfg config.HTTPRouteConfig) error {
	rc := s.client.Resource(httpRouteGVR).Namespace(s.namespace)
	route := buildRoute(d, cfg, s.namespace, driverPathPrefix)
	_, err := rc.Create(s.ctx, route, metav1.CreateOptions{})
	return err
}

// DeleteRoute deletes the HTTPRoute with the given name.
// Returns nil if the route does not exist.
func (s *KubernetesSvc) DeleteRoute(name string) error {
	return s.client.Resource(httpRouteGVR).Namespace(s.namespace).Delete(s.ctx, name, metav1.DeleteOptions{})
}

// ListRoutesWithLabelSelector returns all HTTPRoutes matching selector.
func (s *KubernetesSvc) ListRoutesWithLabelSelector(selector string) ([]unstructured.Unstructured, error) {
	list, err := s.client.Resource(httpRouteGVR).Namespace(s.namespace).List(s.ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// GetRoute fetches the HTTPRoute with the given name.
func (s *KubernetesSvc) GetRoute(name string) (*unstructured.Unstructured, error) {
	obj, err := s.client.Resource(httpRouteGVR).Namespace(s.namespace).Get(s.ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return obj, nil
}

// ---- pod state helpers -------------------------------------------------------

func mostRecent(pods []unstructured.Unstructured) unstructured.Unstructured {
	best := pods[0]
	for _, p := range pods[1:] {
		if p.GetCreationTimestamp().After(best.GetCreationTimestamp().Time) {
			best = p
		}
	}
	return best
}

func stateFromPod(pod unstructured.Unstructured) string {
	containerStatuses, _, _ := unstructured.NestedSlice(pod.Object, "status", "containerStatuses")
	if len(containerStatuses) > 0 {
		if cs, ok := containerStatuses[0].(map[string]interface{}); ok {
			if state := containerStateString(cs); state != "" {
				return state
			}
		}
	}
	return pendingReason(pod)
}

func pendingReason(pod unstructured.Unstructured) string {
	conditions, _, _ := unstructured.NestedSlice(pod.Object, "status", "conditions")
	for _, raw := range conditions {
		cond, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _ := cond["type"].(string)
		condStatus, _ := cond["status"].(string)
		if condType == "PodScheduled" && condStatus == "False" {
			if reason, _ := cond["reason"].(string); reason != "" {
				return reason
			}
		}
	}
	return "Pending"
}

func containerStateString(cs map[string]interface{}) string {
	stateMap, _, _ := unstructured.NestedMap(cs, "state")
	if stateMap == nil {
		return ""
	}

	if waiting, ok := stateMap["waiting"].(map[string]interface{}); ok {
		if reason, _ := waiting["reason"].(string); reason != "" {
			return reason
		}
		return "Waiting"
	}

	if _, ok := stateMap["running"]; ok {
		return "Running"
	}

	if terminated, ok := stateMap["terminated"].(map[string]interface{}); ok {
		if reason, _ := terminated["reason"].(string); reason != "" {
			return reason
		}
		if isZeroExitCode(terminated) {
			return "Completed"
		}
		return "Error"
	}

	return ""
}

// isZeroExitCode reports whether the exitCode field in a terminated container
// status map is zero. exitCode may be int64 (fake client) or float64 (real
// JSON decoding); both are handled.
func isZeroExitCode(terminated map[string]interface{}) bool {
	val, ok := terminated["exitCode"]
	if !ok {
		return false
	}
	switch v := val.(type) {
	case int64:
		return v == 0
	case float64:
		return v == 0
	case int:
		return v == 0
	default:
		return false
	}
}

// ---- HTTPRoute build helper --------------------------------------------------

func buildRoute(d store.Driver, cfg config.HTTPRouteConfig, namespace string, prefix string) *unstructured.Unstructured {
	pathPrefix := prefix + d.AppSelector
	jobsRedirectTarget := pathPrefix + "/jobs/"
	svcName := d.AppName + "-ui-svc"
	name := d.RouteName()

	redirectRule := func(exactPath string) interface{} {
		return map[string]interface{}{
			"matches": []interface{}{
				map[string]interface{}{
					"path": map[string]interface{}{
						"type":  "Exact",
						"value": exactPath,
					},
				},
			},
			"filters": []interface{}{
				map[string]interface{}{
					"type": "RequestRedirect",
					"requestRedirect": map[string]interface{}{
						"path": map[string]interface{}{
							"type":            "ReplaceFullPath",
							"replaceFullPath": jobsRedirectTarget,
						},
						"statusCode": int64(302),
					},
				},
			},
		}
	}

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "HTTPRoute",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"labels": map[string]interface{}{
					managedByLabel: managedByValue,
				},
			},
			"spec": map[string]interface{}{
				"parentRefs": []interface{}{
					map[string]interface{}{
						"name":      cfg.GatewayName,
						"namespace": cfg.GatewayNamespace,
					},
				},
				"hostnames": []interface{}{cfg.Hostname},
				"rules": []interface{}{
					redirectRule(pathPrefix),
					redirectRule(pathPrefix + "/"),
					map[string]interface{}{
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
					},
				},
			},
		},
	}
}

// ManagedBySelector returns the label selector string used to list all
// HTTPRoutes owned by this manager.
func ManagedBySelector() string {
	return managedByLabel + "=" + managedByValue
}
