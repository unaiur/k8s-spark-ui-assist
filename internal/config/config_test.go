package config

import (
	"strings"
	"testing"
)

func fullConfig() HTTPRouteConfig {
	return HTTPRouteConfig{
		Hostname:         "spark.example.com",
		GatewayName:      "main-gw",
		GatewayNamespace: "gateway-ns",
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
			wantMsg: "http-route.hostname, http-route.gateway-name, http-route.gateway-namespace",
		},
		{
			name: "hostname missing",
			cfg: HTTPRouteConfig{
				GatewayName:      "main-gw",
				GatewayNamespace: "gateway-ns",
			},
			wantErr: true,
			wantMsg: "http-route.hostname",
		},
		{
			name: "gateway-name missing",
			cfg: HTTPRouteConfig{
				Hostname:         "spark.example.com",
				GatewayNamespace: "gateway-ns",
			},
			wantErr: true,
			wantMsg: "http-route.gateway-name",
		},
		{
			name: "gateway-namespace missing",
			cfg: HTTPRouteConfig{
				Hostname:    "spark.example.com",
				GatewayName: "main-gw",
			},
			wantErr: true,
			wantMsg: "http-route.gateway-namespace",
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
