package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFromArgsYAMLAndCLIOverride(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "server.yaml")
	configYAML := "" +
		"data_dir: " + tempDir + "\n" +
		"listen_addr: ':8443'\n" +
		"shared_secret: 'yaml-secret'\n" +
		"heartbeat_interval: 10s\n" +
		"heartbeat_timeout: 30s\n" +
		"max_timestamp_skew: 90s\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := loadFromArgs([]string{
		"--config=" + configPath,
		"--listen-addr=:9443",
	})
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.ListenAddr != ":9443" {
		t.Fatalf("listen addr override failed: got %q", cfg.ListenAddr)
	}
	if cfg.SharedSecret != "yaml-secret" {
		t.Fatalf("shared secret should come from YAML: got %q", cfg.SharedSecret)
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
