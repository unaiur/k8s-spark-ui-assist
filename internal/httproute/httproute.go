// Package httproute manages Gateway API HTTPRoute objects for Spark driver UIs.
package httproute

import (
	"context"
	"log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayclient "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"

	"github.com/unaiur/k8s-spark-ui-assist/internal/config"
	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

// Manager creates and deletes HTTPRoutes for Spark driver pods.
type Manager struct {
	client    gatewayclient.Interface
	namespace string
	cfg       config.HTTPRouteConfig
}

// New creates a new Manager.
func New(client gatewayclient.Interface, namespace string, cfg config.HTTPRouteConfig) *Manager {
	return &Manager{client: client, namespace: namespace, cfg: cfg}
}

// Ensure creates an HTTPRoute for the given driver if one does not already exist.
func (m *Manager) Ensure(ctx context.Context, d store.Driver) {
	name := d.AppSelector + "-ui-route"
	_, err := m.client.GatewayV1().HTTPRoutes(m.namespace).Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		// Already exists.
		return
	}

	route := m.buildRoute(d)
	_, err = m.client.GatewayV1().HTTPRoutes(m.namespace).Create(ctx, route, metav1.CreateOptions{})
	if err != nil {
		log.Printf("httproute: failed to create HTTPRoute %s: %v", name, err)
		return
	}
	log.Printf("httproute: created HTTPRoute %s", name)
}

// Delete removes the HTTPRoute associated with the given spark-app-selector value.
func (m *Manager) Delete(ctx context.Context, appSelector string) {
	name := appSelector + "-ui-route"
	err := m.client.GatewayV1().HTTPRoutes(m.namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		log.Printf("httproute: failed to delete HTTPRoute %s: %v", name, err)
		return
	}
	log.Printf("httproute: deleted HTTPRoute %s", name)
}

func (m *Manager) buildRoute(d store.Driver) *gatewayv1.HTTPRoute {
	pathPrefix := "/live/" + d.AppSelector
	replacePrefixMatch := "/"
	port := gatewayv1.PortNumber(443)
	gwNamespace := gatewayv1.Namespace(m.cfg.GatewayNamespace)
	hostname := gatewayv1.Hostname(m.cfg.Hostname)
	svcName := gatewayv1.ObjectName(d.AppName + "-ui-svc")
	svcPort := gatewayv1.PortNumber(4040)

	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      d.AppSelector + "-ui-route",
			Namespace: m.namespace,
		},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{
					{
						Name:      gatewayv1.ObjectName(m.cfg.GatewayName),
						Namespace: &gwNamespace,
						Port:      &port,
					},
				},
			},
			Hostnames: []gatewayv1.Hostname{hostname},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					Matches: []gatewayv1.HTTPRouteMatch{
						{
							Path: &gatewayv1.HTTPPathMatch{
								Type:  pathMatchTypePtr(gatewayv1.PathMatchPathPrefix),
								Value: &pathPrefix,
							},
						},
					},
					Filters: []gatewayv1.HTTPRouteFilter{
						{
							Type: gatewayv1.HTTPRouteFilterURLRewrite,
							URLRewrite: &gatewayv1.HTTPURLRewriteFilter{
								Path: &gatewayv1.HTTPPathModifier{
									Type:               gatewayv1.PrefixMatchHTTPPathModifier,
									ReplacePrefixMatch: &replacePrefixMatch,
								},
							},
						},
					},
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Name: svcName,
									Port: &svcPort,
								},
							},
						},
					},
				},
			},
		},
	}
}

func pathMatchTypePtr(t gatewayv1.PathMatchType) *gatewayv1.PathMatchType {
	return &t
}
