package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fullConfig() HTTPRouteConfig {
	return HTTPRouteConfig{
		Hostname:         "spark.example.com",
		GatewayName:      "main-gw",
		GatewayNamespace: "gateway-ns",
		SelfService:      "spark-ui-assist",
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
			wantMsg: "http-route.hostname, http-route.gateway-name, http-route.gateway-namespace, self-service",
		},
		{
			name: "hostname missing",
			cfg: HTTPRouteConfig{
				GatewayName:      "main-gw",
				GatewayNamespace: "gateway-ns",
				SelfService:      "spark-ui-assist",
			},
			wantErr: true,
			wantMsg: "http-route.hostname",
		},
		{
			name: "gateway-name missing",
			cfg: HTTPRouteConfig{
				Hostname:         "spark.example.com",
				GatewayNamespace: "gateway-ns",
				SelfService:      "spark-ui-assist",
			},
			wantErr: true,
			wantMsg: "http-route.gateway-name",
		},
		{
			name: "gateway-namespace missing",
			cfg: HTTPRouteConfig{
				Hostname:    "spark.example.com",
				GatewayName: "main-gw",
				SelfService: "spark-ui-assist",
			},
			wantErr: true,
			wantMsg: "http-route.gateway-namespace",
		},
		{
			name: "self-service missing",
			cfg: HTTPRouteConfig{
				Hostname:         "spark.example.com",
				GatewayName:      "main-gw",
				GatewayNamespace: "gateway-ns",
			},
			wantErr: true,
			wantMsg: "self-service",
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

// ---- inClusterNamespace -----------------------------------------------------

// TestInClusterNamespaceReadsFile verifies that inClusterNamespace returns the
// content of the namespace file when it exists.
func TestInClusterNamespaceReadsFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "namespace")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString("my-namespace"); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	f.Close()

	orig := inClusterNamespacePath
	inClusterNamespacePath = f.Name()
	t.Cleanup(func() { inClusterNamespacePath = orig })

	if got := inClusterNamespace(); got != "my-namespace" {
		t.Errorf("inClusterNamespace() = %q, want my-namespace", got)
	}
}

// TestInClusterNamespaceMissingFileReturnsDefault verifies that inClusterNamespace
// returns "default" when the namespace file does not exist.
func TestInClusterNamespaceMissingFileReturnsDefault(t *testing.T) {
	orig := inClusterNamespacePath
	inClusterNamespacePath = filepath.Join(t.TempDir(), "does-not-exist")
	t.Cleanup(func() { inClusterNamespacePath = orig })

	if got := inClusterNamespace(); got != "default" {
		t.Errorf("inClusterNamespace() = %q, want default", got)
	}
}
