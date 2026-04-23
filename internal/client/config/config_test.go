package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFromArgsYAMLAndCLIOverride(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "client.yaml")
	configYAML := "" +
		"data_dir: " + tempDir + "\n" +
		"udp_listen_addr: ':2237'\n" +
		"server_url: 'wss://example.test:8443'\n" +
		"shared_secret: 'yaml-secret'\n" +
		"tenant_id: 'tenant-from-yaml'\n" +
		"source_name: 'source-from-yaml'\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadFromArgs([]string{
		"--config=" + configPath,
		"--tenant-id=tenant-from-cli",
		"--source-display-name=Station From CLI",
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.TenantID != "tenant-from-cli" {
		t.Fatalf("tenant override failed: got %q", cfg.TenantID)
	}
	if cfg.SourceName != "source-from-yaml" {
		t.Fatalf("source name should come from YAML: got %q", cfg.SourceName)
	}
	if cfg.SourceDisplayName != "Station From CLI" {
		t.Fatalf("source display override failed: got %q", cfg.SourceDisplayName)
	}
}

func TestLoadFromArgsRejectsSingleDashLongFlags(t *testing.T) {
	_, err := loadFromArgs([]string{"-shared-secret=bad"})
	if err == nil {
		t.Fatal("expected single-dash long flag to be rejected")
	}
	if !strings.Contains(err.Error(), "--shared-secret") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadForCLIEnvOverridesYAML(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "client.yaml")
	configYAML := "" +
		"data_dir: " + tempDir + "\n" +
		"udp_listen_addr: ':2237'\n" +
		"server_url: 'wss://example.test:8443'\n" +
		"shared_secret: 'yaml-secret'\n" +
		"tenant_id: 'tenant-from-yaml'\n" +
		"source_name: 'source-from-yaml'\n" +
		"auto_trust_on_first_use: true\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv(envTenantID, "tenant-from-env")
	t.Setenv(envAutoTrustOnFirstUse, "false")

	cfg, err := LoadForCLI(configPath, DefaultConfig(), func(string) bool { return false })
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.TenantID != "tenant-from-env" {
		t.Fatalf("tenant env override failed: got %q", cfg.TenantID)
	}
	if cfg.AutoTrustOnFirstUse {
		t.Fatal("auto trust env override failed: expected false")
	}
	if cfg.SourceName != "source-from-yaml" {
		t.Fatalf("source name should remain from YAML: got %q", cfg.SourceName)
	}
}
