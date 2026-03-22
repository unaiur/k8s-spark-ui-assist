// Package httproute manages Gateway API HTTPRoute objects for Spark driver UIs.
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

// Manager creates and deletes HTTPRoutes for Spark driver pods.
type Manager struct {
	client    dynamic.Interface
	namespace string
	cfg       config.HTTPRouteConfig
}

// New creates a new Manager.
func New(client dynamic.Interface, namespace string, cfg config.HTTPRouteConfig) *Manager {
	return &Manager{client: client, namespace: namespace, cfg: cfg}
}

// Ensure creates an HTTPRoute for the given driver if one does not already exist.
func (m *Manager) Ensure(ctx context.Context, d store.Driver) {
	name := d.AppSelector + "-ui-route"
	rc := m.client.Resource(httpRouteGVR).Namespace(m.namespace)
	_, err := rc.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		// Already exists.
		return
	}
	if !errors.IsNotFound(err) {
		log.Printf("httproute: failed to get HTTPRoute %s: %v", name, err)
		return
	}

	route := m.buildRoute(d)
	_, err = rc.Create(ctx, route, metav1.CreateOptions{})
	if err != nil {
		log.Printf("httproute: failed to create HTTPRoute %s: %v", name, err)
		return
	}
	log.Printf("httproute: created HTTPRoute %s", name)
}

// Delete removes the HTTPRoute associated with the given spark-app-selector value.
func (m *Manager) Delete(ctx context.Context, appSelector string) {
	name := appSelector + "-ui-route"
	err := m.client.Resource(httpRouteGVR).Namespace(m.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		log.Printf("httproute: failed to delete HTTPRoute %s: %v", name, err)
		return
	}
	log.Printf("httproute: deleted HTTPRoute %s", name)
}

func (m *Manager) buildRoute(d store.Driver) *unstructured.Unstructured {
	pathPrefix := "/live/" + d.AppSelector
	svcName := d.AppName + "-ui-svc"

	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "HTTPRoute",
			"metadata": map[string]interface{}{
				"name":      d.AppSelector + "-ui-route",
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
				"rules": []interface{}{
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
