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
	// RouteName is the name of the shared HTTPRoute managed by Helm.
	// The Go service adds and removes per-driver rules from this route.
	RouteName        string
	Hostname         string
	GatewayName      string
	GatewayNamespace string
}

// Parse reads configuration from command-line flags and returns a Config.
func Parse() *Config {
	cfg := &Config{}

	defaultNS := inClusterNamespace()

	flag.StringVar(&cfg.Namespace, "namespace", defaultNS, "Kubernetes namespace to watch")
	flag.StringVar(&cfg.HTTPRoute.RouteName, "http-route.name", "", "Name of the shared HTTPRoute managed by Helm")
	flag.StringVar(&cfg.HTTPRoute.Hostname, "http-route.hostname", "", "Hostname to set in HTTPRoute spec.hostnames[0]")
	flag.StringVar(&cfg.HTTPRoute.GatewayName, "http-route.gateway-name", "", "Gateway name for HTTPRoute spec.parentRefs[0].name")
	flag.StringVar(&cfg.HTTPRoute.GatewayNamespace, "http-route.gateway-namespace", "", "Gateway namespace for HTTPRoute spec.parentRefs[0].namespace")

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
	if c.RouteName == "" {
		missing = append(missing, "http-route.name")
	}
	if c.Hostname == "" {
		missing = append(missing, "http-route.hostname")
	}
	if c.GatewayName == "" {
		missing = append(missing, "http-route.gateway-name")
	}
	if c.GatewayNamespace == "" {
		missing = append(missing, "http-route.gateway-namespace")
	}
	if len(missing) > 0 {
		return errors.New("missing required flags: " + strings.Join(missing, ", "))
	}
	return nil
}

// inClusterNamespace reads the namespace from the service-account volume mount.
// Returns "default" if the file is not present (running outside a cluster).
func inClusterNamespace() string {
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return "default"
	}
	return string(data)
}
