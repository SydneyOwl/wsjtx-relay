package config

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	DataDir             string `yaml:"data_dir"`
	UDPListenAddr       string `yaml:"udp_listen_addr"`
	ServerURL           string `yaml:"server_url"`
	SharedSecret        string `yaml:"shared_secret"`
	TenantID            string `yaml:"tenant_id"`
	SourceName          string `yaml:"source_name"`
	SourceDisplayName   string `yaml:"source_display_name"`
	TrustStorePath      string `yaml:"trust_store_path"`
	AutoTrustOnFirstUse bool   `yaml:"auto_trust_on_first_use"`
	ClientName          string `yaml:"client_name"`
	ClientVersion       string `yaml:"client_version"`
	InstanceID          string `yaml:"instance_id"`
}

func Load() (Config, error) {
	return loadFromArgs(os.Args[1:])
}

func loadFromArgs(args []string) (Config, error) {
	if err := rejectSingleDashLongFlags(args); err != nil {
		return Config{}, err
	}

	cfg := defaultConfig()
	configPath, remainingArgs, err := extractConfigPath(args)
	if err != nil {
		return Config{}, err
	}
	if configPath != "" {
		if err := loadYAML(configPath, &cfg); err != nil {
			return Config{}, err
		}
	}

	fs := flag.NewFlagSet("wsjtx-relay-client", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&configPath, "config", configPath, "YAML config file")
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "runtime data directory")
	fs.StringVar(&cfg.UDPListenAddr, "udp-listen-addr", cfg.UDPListenAddr, "UDP listen address for WSJT-X/JTDX")
	fs.StringVar(&cfg.ServerURL, "server-url", cfg.ServerURL, "relay server websocket base URL, e.g. wss://example.com:8443")
	fs.StringVar(&cfg.SharedSecret, "shared-secret", cfg.SharedSecret, "relay shared secret")
	fs.StringVar(&cfg.TenantID, "tenant-id", cfg.TenantID, "high-entropy tenant identifier")
	fs.StringVar(&cfg.SourceName, "source-name", cfg.SourceName, "logical source name unique inside the tenant")
	fs.StringVar(&cfg.SourceDisplayName, "source-display-name", cfg.SourceDisplayName, "display name shown to watchers")
	fs.StringVar(&cfg.TrustStorePath, "trust-store-path", cfg.TrustStorePath, "file path storing the trusted server SPKI fingerprint")
	fs.BoolVar(&cfg.AutoTrustOnFirstUse, "auto-trust-on-first-use", cfg.AutoTrustOnFirstUse, "trust the first seen server fingerprint automatically")
	fs.StringVar(&cfg.ClientName, "client-name", cfg.ClientName, "client name sent in the hello message")
	fs.StringVar(&cfg.ClientVersion, "client-version", cfg.ClientVersion, "client version sent in the hello message")
	fs.StringVar(&cfg.InstanceID, "instance-id", cfg.InstanceID, "optional stable instance identifier for process restarts")
	if err := fs.Parse(remainingArgs); err != nil {
		return Config{}, err
	}

	if err := normalizeAndValidate(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaultConfig() Config {
	return Config{
		DataDir:             filepath.Join(".", "data"),
		UDPListenAddr:       ":2237",
		AutoTrustOnFirstUse: true,
		ClientName:          "wsjtx-relay-client",
		ClientVersion:       "0.1.0",
	}
}

func normalizeAndValidate(cfg *Config) error {
	cfg.DataDir = strings.TrimSpace(cfg.DataDir)
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(".", "data")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.TrustStorePath) == "" {
		cfg.TrustStorePath = filepath.Join(cfg.DataDir, "trusted_server_fingerprint.txt")
	}

	cfg.ServerURL = strings.TrimRight(strings.TrimSpace(cfg.ServerURL), "/")
	cfg.SharedSecret = strings.TrimSpace(cfg.SharedSecret)
	cfg.TenantID = strings.TrimSpace(cfg.TenantID)
	cfg.SourceName = strings.TrimSpace(cfg.SourceName)
	cfg.SourceDisplayName = strings.TrimSpace(cfg.SourceDisplayName)
	cfg.UDPListenAddr = strings.TrimSpace(cfg.UDPListenAddr)
	cfg.TrustStorePath = strings.TrimSpace(cfg.TrustStorePath)
	cfg.ClientName = strings.TrimSpace(cfg.ClientName)
	cfg.ClientVersion = strings.TrimSpace(cfg.ClientVersion)
	cfg.InstanceID = strings.TrimSpace(cfg.InstanceID)

	if cfg.SourceDisplayName == "" {
		cfg.SourceDisplayName = cfg.SourceName
	}
	if cfg.ClientName == "" {
		cfg.ClientName = "wsjtx-relay-client"
	}
	if cfg.ClientVersion == "" {
		cfg.ClientVersion = "0.1.0"
	}
	if cfg.UDPListenAddr == "" {
		cfg.UDPListenAddr = ":2237"
	}

	switch {
	case cfg.ServerURL == "":
		return errors.New("server-url is required")
	case cfg.SharedSecret == "":
		return errors.New("shared-secret is required")
	case cfg.TenantID == "":
		return errors.New("tenant-id is required")
	case cfg.SourceName == "":
		return errors.New("source-name is required")
	}
	return nil
}

func rejectSingleDashLongFlags(args []string) error {
	for _, arg := range args {
		if arg == "--" {
			return nil
		}
		if strings.HasPrefix(arg, "--") || !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}
		return fmt.Errorf("use GNU-style long flags with two dashes, for example --%s instead of %s", strings.TrimPrefix(arg, "-"), arg)
	}
	return nil
}

func extractConfigPath(args []string) (string, []string, error) {
	remaining := make([]string, 0, len(args))
	configPath := ""

	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--config":
			if index+1 >= len(args) {
				return "", nil, errors.New("--config requires a file path")
			}
			configPath = args[index+1]
			index++
		case strings.HasPrefix(arg, "--config="):
			configPath = strings.TrimPrefix(arg, "--config=")
		default:
			remaining = append(remaining, arg)
		}
	}

	return strings.TrimSpace(configPath), remaining, nil
}

func loadYAML(path string, target any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file %q: %w", path, err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode config file %q: %w", path, err)
	}
	return nil
}
