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

	"github.com/unaiur/k8s-spark-ui-assist/internal/config"
	"github.com/unaiur/k8s-spark-ui-assist/internal/httproute"
	"github.com/unaiur/k8s-spark-ui-assist/internal/server"
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

	mgr := httproute.New(dynClient, cfg.Namespace, cfg.HTTPRoute)
	// Ensure routes for already-running drivers once the informer has synced;
	// handled via OnAdd callbacks triggered by the initial List.
	routeHandler := &httpRouteHandler{ctx: ctx, mgr: mgr}

	lw := watcher.NewListerWatcher(cfg.Namespace, dynClient)

	go watcher.Watch(ctx, lw, s, routeHandler)

	mux := http.NewServeMux()
	mux.Handle("/", server.Handler(s, time.Now))

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
