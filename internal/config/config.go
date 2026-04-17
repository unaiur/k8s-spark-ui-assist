// Package config parses service configuration from command-line flags.
package config

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
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
	// SHSDeployment is the name of the Kubernetes Deployment for the Spark
	// History Server. When non-empty, the wake endpoint patches it to scale up.
	// Defaults to SHSService when SHSService is set and SHSDeployment is empty.
	// Set to "disabled" to disable wake/stop functionality even when SHSService
	// is set.
	SHSDeployment string
	// SHSStopEnabled is true when the automatic stop scheduler is active.
	// SHSStopHour and SHSStopMinute are the UTC time to scale the Deployment
	// to zero each day. Only meaningful when SHSStopEnabled is true.
	SHSStopEnabled bool
	SHSStopHour    int
	SHSStopMinute  int
}

// Parse reads configuration from command-line flags and returns a Config.
func Parse() *Config {
	cfg := &Config{}

	defaultNS := inClusterNamespace()

	var shsStopTime string

	flag.StringVar(&cfg.Namespace, "namespace", defaultNS, "Kubernetes namespace to watch")
	flag.StringVar(&cfg.HTTPRoute.Hostname, "http-route.hostname", "", "Hostname to set in HTTPRoute spec.hostnames[0]")
	flag.StringVar(&cfg.HTTPRoute.GatewayName, "http-route.gateway-name", "", "Gateway name for HTTPRoute spec.parentRefs[0].name")
	flag.StringVar(&cfg.HTTPRoute.GatewayNamespace, "http-route.gateway-namespace", "", "Gateway namespace for HTTPRoute spec.parentRefs[0].namespace")
	flag.StringVar(&cfg.HTTPRoute.SelfService, "self-service", "", "Kubernetes Service name for this application (used to build root HTTPRoute)")
	flag.StringVar(&cfg.HTTPRoute.SHSService, "shs-service", "", "Kubernetes Service name for the Spark History Server (optional)")
	flag.StringVar(&cfg.HTTPRoute.SHSDeployment, "shs-deployment", "", `Kubernetes Deployment name for the Spark History Server; defaults to -shs-service. Set to "disabled" to disable wake/stop features.`)
	flag.StringVar(&shsStopTime, "shs-stop-time", "disabled", `UTC time (HH:MM) to auto-scale the SHS Deployment to zero each day. Empty or "disabled" disables the scheduler.`)

	flag.Parse()

	// Default SHSDeployment to SHSService so callers only need to set one flag.
	if cfg.HTTPRoute.SHSDeployment == "" {
		cfg.HTTPRoute.SHSDeployment = cfg.HTTPRoute.SHSService
	}
	// Explicit "disabled" clears the deployment name so the rest of the code
	// can check for the empty string as the canonical "feature disabled" signal.
	if strings.EqualFold(cfg.HTTPRoute.SHSDeployment, "disabled") {
		cfg.HTTPRoute.SHSDeployment = ""
	}

	// Parse the stop time only when the SHS Deployment is configured and the
	// flag value is not empty / "disabled".
	if cfg.HTTPRoute.SHSDeployment != "" &&
		shsStopTime != "" &&
		!strings.EqualFold(shsStopTime, "disabled") {
		h, m, err := parseHHMM(shsStopTime)
		if err != nil {
			flag.Usage()
			log.Fatalf("invalid -shs-stop-time %q: %v", shsStopTime, err)
		}
		cfg.HTTPRoute.SHSStopEnabled = true
		cfg.HTTPRoute.SHSStopHour = h
		cfg.HTTPRoute.SHSStopMinute = m
	}

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
	// SelfService is always required: it names the root HTTPRoute's fallback
	// backend and is used to derive the route name.
	if c.SelfService == "" {
		missing = append(missing, "self-service")
	}
	if len(missing) > 0 {
		return errors.New("missing required flags: " + strings.Join(missing, ", "))
	}
	return nil
}

// parseHHMM parses a "HH:MM" string and returns (hour, minute, error).
func parseHHMM(s string) (int, int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM format")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("hour must be 0–23")
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("minute must be 0–59")
	}
	return h, m, nil
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
