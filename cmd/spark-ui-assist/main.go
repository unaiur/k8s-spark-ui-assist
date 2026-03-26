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
	"github.com/unaiur/k8s-spark-ui-assist/internal/swgate"
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

	// Start the SHS Endpoints watcher if configured.
	if cfg.HTTPRoute.SHSService != "" {
		shsHandler := &shsRouteHandler{ctx: ctx, mgr: mgr}
		go shs.Watch(ctx, dynClient, cfg.Namespace, cfg.HTTPRoute.SHSService, shsHandler, nil)
		log.Printf("shs: watching Endpoints for service %q", cfg.HTTPRoute.SHSService)
	}

	mux := http.NewServeMux()
	// SW gate endpoints — registered before /proxy/api/ so Go's longest-prefix
	// routing picks these exact paths ahead of the generic API handler.
	swCfg := swgate.Config{InjectScript: cfg.InjectScript}
	swHandler := swgate.Handler(swCfg)
	mux.Handle("/proxy/api/sw-gate", swHandler)
	mux.Handle("/proxy/api/sw.js", swHandler)
	mux.Handle("/proxy/api/spark-inject.js", swHandler)
	mux.Handle("/proxy/api/", api.Handler(s, mgr))
	mux.Handle("/", swgate.GateMiddleware(swCfg, server.Handler(s, time.Now, mgr)))

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
