package config

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     HTTPRouteConfig
		wantErr bool
		wantMsg string
	}{
		{
			name:    "disabled — always valid",
			cfg:     HTTPRouteConfig{Enabled: false},
			wantErr: false,
		},
		{
			name: "enabled with all fields — valid",
			cfg: HTTPRouteConfig{
				Enabled:          true,
				Hostname:         "spark.example.com",
				GatewayName:      "main-gw",
				GatewayNamespace: "gateway-ns",
			},
			wantErr: false,
		},
		{
			name:    "enabled but all fields missing",
			cfg:     HTTPRouteConfig{Enabled: true},
			wantErr: true,
			wantMsg: "http-route.hostname, http-route.gateway-name, http-route.gateway-namespace",
		},
		{
			name: "enabled but hostname missing",
			cfg: HTTPRouteConfig{
				Enabled:          true,
				GatewayName:      "main-gw",
				GatewayNamespace: "gateway-ns",
			},
			wantErr: true,
			wantMsg: "http-route.hostname",
		},
		{
			name: "enabled but gateway-name missing",
			cfg: HTTPRouteConfig{
				Enabled:          true,
				Hostname:         "spark.example.com",
				GatewayNamespace: "gateway-ns",
			},
			wantErr: true,
			wantMsg: "http-route.gateway-name",
		},
		{
			name: "enabled but gateway-namespace missing",
			cfg: HTTPRouteConfig{
				Enabled:     true,
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
