package config

import (
	"strings"
	"testing"
)

func fullConfig() HTTPRouteConfig {
	return HTTPRouteConfig{
		RouteName:        "my-release-spark-ui-assist",
		Hostname:         "spark.example.com",
		GatewayName:      "main-gw",
		GatewayNamespace: "gateway-ns",
		DriverPathPrefix: "/proxy/",
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     HTTPRouteConfig
		wantErr bool
		wantMsg string
	}{
		{
			name:    "all fields set — valid",
			cfg:     fullConfig(),
			wantErr: false,
		},
		{
			name:    "all fields missing",
			cfg:     HTTPRouteConfig{},
			wantErr: true,
			wantMsg: "http-route.name, http-route.hostname, http-route.gateway-name, http-route.gateway-namespace",
		},
		{
			name: "route-name missing",
			cfg: HTTPRouteConfig{
				Hostname:         "spark.example.com",
				GatewayName:      "main-gw",
				GatewayNamespace: "gateway-ns",
				DriverPathPrefix: "/proxy/",
			},
			wantErr: true,
			wantMsg: "http-route.name",
		},
		{
			name: "hostname missing",
			cfg: HTTPRouteConfig{
				RouteName:        "my-release-spark-ui-assist",
				GatewayName:      "main-gw",
				GatewayNamespace: "gateway-ns",
				DriverPathPrefix: "/proxy/",
			},
			wantErr: true,
			wantMsg: "http-route.hostname",
		},
		{
			name: "gateway-name missing",
			cfg: HTTPRouteConfig{
				RouteName:        "my-release-spark-ui-assist",
				Hostname:         "spark.example.com",
				GatewayNamespace: "gateway-ns",
				DriverPathPrefix: "/proxy/",
			},
			wantErr: true,
			wantMsg: "http-route.gateway-name",
		},
		{
			name: "gateway-namespace missing",
			cfg: HTTPRouteConfig{
				RouteName:        "my-release-spark-ui-assist",
				Hostname:         "spark.example.com",
				GatewayName:      "main-gw",
				DriverPathPrefix: "/proxy/",
			},
			wantErr: true,
			wantMsg: "http-route.gateway-namespace",
		},
		{
			name: "prefix without leading slash is invalid",
			cfg: func() HTTPRouteConfig {
				c := fullConfig()
				c.DriverPathPrefix = "proxy/"
				return c
			}(),
			wantErr: true,
			wantMsg: "http-route.driver-path-prefix",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantMsg != "" && !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("error %q does not mention %q", err.Error(), tc.wantMsg)
			}
		})
	}
}

// TestValidateNormalisesTrailingSlash verifies that a prefix without a trailing
// slash is accepted and normalised by appending one.
func TestValidateNormalisesTrailingSlash(t *testing.T) {
	cfg := fullConfig()
	cfg.DriverPathPrefix = "/proxy"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DriverPathPrefix != "/proxy/" {
		t.Errorf("expected DriverPathPrefix to be normalised to %q, got %q", "/proxy/", cfg.DriverPathPrefix)
	}
}
