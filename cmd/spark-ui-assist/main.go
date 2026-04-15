package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/unaiur/k8s-spark-ui-assist/internal/api"
	"github.com/unaiur/k8s-spark-ui-assist/internal/config"
	"github.com/unaiur/k8s-spark-ui-assist/internal/httproute"
	"github.com/unaiur/k8s-spark-ui-assist/internal/server"
	"github.com/unaiur/k8s-spark-ui-assist/internal/shs"
	"github.com/unaiur/k8s-spark-ui-assist/internal/store"
	"github.com/unaiur/k8s-spark-ui-assist/internal/watcher"
)

func main() {
	cfg := config.Parse()

	restCfg, err := loadKubeConfig()
	if err != nil {
		log.Fatalf("failed to build Kubernetes config: %v", err)
	}

	dynClient, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		log.Fatalf("failed to create dynamic client: %v", err)
	}

	s := store.New()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	mgr := httproute.New(ctx, dynClient, cfg.Namespace, cfg.HTTPRoute)
	// Ensure routes for already-running drivers once the informer has synced;
	// handled via OnAdd callbacks triggered by the initial List.
	routeHandler := &httpRouteHandler{ctx: ctx, mgr: mgr}

	lw := watcher.NewListerWatcher(cfg.Namespace, dynClient)

	onSynced := func() {
		log.Printf("httproute: informer synced, reconciling routes")
		if err := mgr.Reconcile(ctx, s.ListRunning()); err != nil {
			log.Printf("httproute: initial reconcile failed: %v", err)
		}
		// Always ensure the fallback root route exists so "/" is reachable
		// immediately after startup, before any SHS state is known.
		mgr.EnsureFallbackRootRoute(ctx)
	}
	go watcher.Watch(ctx, lw, s, routeHandler, onSynced)

	// Start the SHS EndpointSlice watcher if configured, and build SHSConfig.
	var shsCfg server.SHSConfig
	if cfg.HTTPRoute.SHSService != "" {
		shsState := &shs.State{}
		shsRouteH := &shsRouteHandler{ctx: ctx, mgr: mgr}
		// Combine route handler and state updates in a single shs.Handler.
		combined := &combinedSHSHandler{route: shsRouteH, state: shsState}
		go shs.Watch(ctx, dynClient, cfg.Namespace, cfg.HTTPRoute.SHSService, combined, nil)
		log.Printf("shs: watching Endpoints for service %q", cfg.HTTPRoute.SHSService)

		shsCfg = server.SHSConfig{
			Deployment: cfg.HTTPRoute.SHSDeployment,
			Namespace:  cfg.Namespace,
			State:      shsState,
			Client:     dynClient,
		}
	}

	mux := http.NewServeMux()
	// Register the SHS wake endpoint before the /proxy/api/ catch-all so the
	// more-specific pattern takes precedence.
	mux.Handle("/proxy/api/shs/wake", server.WakeHandler(shsCfg))
	mux.Handle("/proxy/api/", api.Handler(s, mgr))
	mux.Handle("/", server.Handler(s, time.Now, mgr, shsCfg))

	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutCancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	log.Printf("Listening on :8080, watching namespace %q", cfg.Namespace)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("HTTP server error: %v", err)
	}
}

// httpRouteHandler bridges watcher events to the HTTPRoute manager.
type httpRouteHandler struct {
	ctx context.Context
	mgr *httproute.Manager
}

func (h *httpRouteHandler) OnAdd(d store.Driver) {
	h.mgr.Ensure(h.ctx, d)
}

func (h *httpRouteHandler) OnRemove(appSelector string) {
	h.mgr.Delete(h.ctx, appSelector)
}

// shsRouteHandler bridges SHS Endpoints events to the HTTPRoute manager.
type shsRouteHandler struct {
	ctx context.Context
	mgr *httproute.Manager
}

func (h *shsRouteHandler) OnUp() {
	h.mgr.EnsureSHSRoute(h.ctx)
}

func (h *shsRouteHandler) OnDown() {
	h.mgr.EnsureFallbackRootRoute(h.ctx)
}

// combinedSHSHandler fans out SHS events to both the route handler and the state tracker.
type combinedSHSHandler struct {
	route *shsRouteHandler
	state *shs.State
}

func (h *combinedSHSHandler) OnUp() {
	h.state.OnUp()
	h.route.OnUp()
}

func (h *combinedSHSHandler) OnDown() {
	h.state.OnDown()
	h.route.OnDown()
}

// loadKubeConfig tries in-cluster config first, then falls back to KUBECONFIG / default kubeconfig.
func loadKubeConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
}
