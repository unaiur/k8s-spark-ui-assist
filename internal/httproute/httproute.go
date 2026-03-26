// Package httproute manages per-driver Gateway API HTTPRoutes.
//
// For each active Spark driver the Manager creates a dedicated HTTPRoute named
// "<appSelector>-ui-route". When the driver stops the route is deleted. The
// Manager uses the label "app.kubernetes.io/managed-by: spark-ui-assist" so
// that Reconcile can list all routes it owns and clean up any that belong to
// drivers that are no longer active.
//
// Reconcile() should be called once the pod informer has fully synced so that
// stale routes left over from a previous instance of the service are cleaned up.
package httproute

import (
	"context"
	"log"

	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/unaiur/k8s-spark-ui-assist/internal/config"
	k8ssvc "github.com/unaiur/k8s-spark-ui-assist/internal/k8s"
	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
)

// Manager creates and deletes per-driver HTTPRoutes.
type Manager struct {
	svc *k8ssvc.KubernetesSvc
	cfg config.HTTPRouteConfig
}

// New creates a new Manager.
func New(svc *k8ssvc.KubernetesSvc, cfg config.HTTPRouteConfig) *Manager {
	return &Manager{svc: svc, cfg: cfg}
}

// Ensure creates the per-driver HTTPRoute if it does not already exist.
// It is a no-op when the route is already present.
func (m *Manager) Ensure(ctx context.Context, d store.Driver) {
	name := d.RouteName()

	_, err := m.svc.GetRoute(name)
	if err == nil {
		return // already exists
	}
	if !errors.IsNotFound(err) {
		log.Printf("httproute: ensure %s: failed to check existence: %v", name, err)
		return
	}

	if err := m.svc.CreateDriverRoute(d, m.cfg); err != nil {
		if errors.IsAlreadyExists(err) {
			return // created by a concurrent Ensure call; desired state is already met
		}
		log.Printf("httproute: ensure %s: failed to create: %v", name, err)
		return
	}
	log.Printf("httproute: created HTTPRoute %s", name)
}

// Delete removes the per-driver HTTPRoute for appSelector.
// It is a no-op when the route does not exist.
func (m *Manager) Delete(ctx context.Context, appSelector string) {
	d := store.Driver{AppSelector: appSelector}
	name := d.RouteName()

	err := m.svc.DeleteRoute(name)
	if errors.IsNotFound(err) {
		return // already gone
	}
	if err != nil {
		log.Printf("httproute: delete %s: failed: %v", name, err)
		return
	}
	log.Printf("httproute: deleted HTTPRoute %s", name)
}

// Reconcile synchronises the set of managed HTTPRoutes against the provided
// list of currently active drivers. It deletes routes for drivers that are no
// longer active and creates routes for drivers that are missing one.
// Call this once after the pod informer has fully synced.
func (m *Manager) Reconcile(ctx context.Context, active []store.Driver) {
	// Build a set of expected route names.
	wantedByName := make(map[string]store.Driver, len(active))
	for _, d := range active {
		wantedByName[d.RouteName()] = d
	}

	// List all HTTPRoutes owned by this manager.
	items, err := m.svc.ListRoutesWithLabelSelector(k8ssvc.ManagedBySelector())
	if err != nil {
		log.Printf("httproute: reconcile: failed to list managed HTTPRoutes: %v", err)
		return
	}

	presentNames := make(map[string]bool, len(items))
	for _, item := range items {
		name := item.GetName()
		presentNames[name] = true
		if _, ok := wantedByName[name]; !ok {
			log.Printf("httproute: reconcile: deleting stale HTTPRoute %s", name)
			if delErr := m.svc.DeleteRoute(name); delErr != nil && !errors.IsNotFound(delErr) {
				log.Printf("httproute: reconcile: failed to delete %s: %v", name, delErr)
			}
		}
	}

	for name, d := range wantedByName {
		if presentNames[name] {
			continue
		}
		log.Printf("httproute: reconcile: creating missing HTTPRoute %s", name)
		if createErr := m.svc.CreateDriverRoute(d, m.cfg); createErr != nil && !errors.IsAlreadyExists(createErr) {
			log.Printf("httproute: reconcile: failed to create %s: %v", name, createErr)
		}
	}
}
