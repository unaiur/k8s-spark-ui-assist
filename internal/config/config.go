// Package config parses service configuration from command-line flags.
package config

import (
	"errors"
	"flag"
	"log"
	"os"
	"strings"
)

// Config holds all service configuration.
type Config struct {
	// Namespace to watch for Spark driver pods. Defaults to the in-cluster namespace.
	Namespace string

	// HTTPRoute feature configuration.
	HTTPRoute HTTPRouteConfig
}

// HTTPRouteConfig holds configuration for the HTTPRoute creation feature.
type HTTPRouteConfig struct {
	Hostname         string
	GatewayName      string
	GatewayNamespace string
	// SelfService is the name of the Kubernetes Service for this application.
	// It is used to build the root HTTPRoute when SHS is not available.
	SelfService string
	// SHSService is the name of the Kubernetes Service for the Spark History
	// Server. When non-empty, a root HTTPRoute is managed dynamically.
	SHSService string
}

// Parse reads configuration from command-line flags and returns a Config.
func Parse() *Config {
	cfg := &Config{}

	defaultNS := inClusterNamespace()

	flag.StringVar(&cfg.Namespace, "namespace", defaultNS, "Kubernetes namespace to watch")
	flag.StringVar(&cfg.HTTPRoute.Hostname, "http-route.hostname", "", "Hostname to set in HTTPRoute spec.hostnames[0]")
	flag.StringVar(&cfg.HTTPRoute.GatewayName, "http-route.gateway-name", "", "Gateway name for HTTPRoute spec.parentRefs[0].name")
	flag.StringVar(&cfg.HTTPRoute.GatewayNamespace, "http-route.gateway-namespace", "", "Gateway namespace for HTTPRoute spec.parentRefs[0].namespace")
	flag.StringVar(&cfg.HTTPRoute.SelfService, "self-service", "", "Kubernetes Service name for this application (used to build root HTTPRoute)")
	flag.StringVar(&cfg.HTTPRoute.SHSService, "shs-service", "", "Kubernetes Service name for the Spark History Server (optional)")

	flag.Parse()

	if err := cfg.HTTPRoute.Validate(); err != nil {
		flag.Usage()
		log.Fatalf("invalid configuration: %v", err)
	}

	return cfg
}

// Validate checks that all required HTTPRoute fields are present.
// Returns a non-nil error describing any missing fields.
func (c *HTTPRouteConfig) Validate() error {
	var missing []string
	if c.Hostname == "" {
		missing = append(missing, "http-route.hostname")
	}
	if c.GatewayName == "" {
		missing = append(missing, "http-route.gateway-name")
	}
	if c.GatewayNamespace == "" {
		missing = append(missing, "http-route.gateway-namespace")
	}
	// SelfService is only required when SHS integration is enabled.
	if c.SHSService != "" && c.SelfService == "" {
		missing = append(missing, "self-service")
	}
	if len(missing) > 0 {
		return errors.New("missing required flags: " + strings.Join(missing, ", "))
	}
	return nil
}

// inClusterNamespacePath is the path of the service-account namespace file.
// It is a variable so tests can override it without touching the filesystem at
// the real mount point.
var inClusterNamespacePath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

// inClusterNamespace reads the namespace from the service-account volume mount.
// Returns "default" if the file is not present (running outside a cluster).
func inClusterNamespace() string {
	data, err := os.ReadFile(inClusterNamespacePath)
	if err != nil {
		return "default"
	}
	return string(data)
}
