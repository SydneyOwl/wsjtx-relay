package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/sydneyowl/wsjtx-relay/internal/shared/cliargs"
	"gopkg.in/yaml.v3"
)

type Config struct {
	ListenAddr        string        `yaml:"listen_addr"`
	DataDir           string        `yaml:"data_dir"`
	CertFile          string        `yaml:"cert_file"`
	KeyFile           string        `yaml:"key_file"`
	SharedSecret      string        `yaml:"shared_secret"`
	Public            bool          `yaml:"public"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	HeartbeatTimeout  time.Duration `yaml:"heartbeat_timeout"`
	MaxTimestampSkew  time.Duration `yaml:"max_timestamp_skew"`
}

func Load() (Config, error) {
	return loadFromArgs(os.Args[1:])
}

func loadFromArgs(args []string) (Config, error) {
	if err := cliargs.RejectSingleDashLongFlags(args); err != nil {
		return Config{}, err
	}

	flagValues := defaultConfig()
	configPath := ""

	fs := pflag.NewFlagSet("wsjtx-relay-server", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	BindFlags(fs, &flagValues, &configPath)
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	return LoadForCLI(configPath, flagValues, fs.Changed)
}

func DefaultConfig() Config {
	return defaultConfig()
}

func BindFlags(fs *pflag.FlagSet, cfg *Config, configPath *string) {
	fs.StringVar(configPath, "config", strings.TrimSpace(*configPath), "YAML config file")
	fs.StringVar(&cfg.ListenAddr, "listen-addr", cfg.ListenAddr, "HTTPS listen address")
	fs.StringVar(&cfg.DataDir, "data-dir", cfg.DataDir, "runtime data directory")
	fs.StringVar(&cfg.CertFile, "cert-file", cfg.CertFile, "TLS certificate file path")
	fs.StringVar(&cfg.KeyFile, "key-file", cfg.KeyFile, "TLS private key file path")
	fs.StringVar(&cfg.SharedSecret, "shared-secret", cfg.SharedSecret, "shared secret for relay authentication")
	fs.DurationVar(&cfg.HeartbeatInterval, "heartbeat-interval", cfg.HeartbeatInterval, "application heartbeat interval")
	fs.DurationVar(&cfg.HeartbeatTimeout, "heartbeat-timeout", cfg.HeartbeatTimeout, "connection timeout without valid frames")
	fs.DurationVar(&cfg.MaxTimestampSkew, "max-timestamp-skew", cfg.MaxTimestampSkew, "maximum tolerated auth timestamp skew")
	fs.BoolVar(&cfg.Public, "public", cfg.Public, "disable shared secret authentication (open relay mode)")
}

func LoadForCLI(configPath string, flagValues Config, flagChanged func(string) bool) (Config, error) {
	cfg := defaultConfig()
	if trimmedPath := strings.TrimSpace(configPath); trimmedPath != "" {
		if err := loadYAML(trimmedPath, &cfg); err != nil {
			return Config{}, err
		}
	}

	applyStringOverride(flagChanged, "listen-addr", flagValues.ListenAddr, &cfg.ListenAddr)
	applyStringOverride(flagChanged, "data-dir", flagValues.DataDir, &cfg.DataDir)
	applyStringOverride(flagChanged, "cert-file", flagValues.CertFile, &cfg.CertFile)
	applyStringOverride(flagChanged, "key-file", flagValues.KeyFile, &cfg.KeyFile)
	applyStringOverride(flagChanged, "shared-secret", flagValues.SharedSecret, &cfg.SharedSecret)
	applyDurationOverride(flagChanged, "heartbeat-interval", flagValues.HeartbeatInterval, &cfg.HeartbeatInterval)
	applyDurationOverride(flagChanged, "heartbeat-timeout", flagValues.HeartbeatTimeout, &cfg.HeartbeatTimeout)
	applyDurationOverride(flagChanged, "max-timestamp-skew", flagValues.MaxTimestampSkew, &cfg.MaxTimestampSkew)
	applyBoolOverride(flagChanged, "public", flagValues.Public, &cfg.Public)

	if err := normalizeAndValidate(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func defaultConfig() Config {
	return Config{
		ListenAddr:        ":8443",
		DataDir:           filepath.Join(".", "data"),
		HeartbeatInterval: 10 * time.Second,
		HeartbeatTimeout:  30 * time.Second,
		MaxTimestampSkew:  90 * time.Second,
	}
}

func normalizeAndValidate(cfg *Config) error {
	cfg.ListenAddr = strings.TrimSpace(cfg.ListenAddr)
	cfg.DataDir = strings.TrimSpace(cfg.DataDir)
	cfg.CertFile = strings.TrimSpace(cfg.CertFile)
	cfg.KeyFile = strings.TrimSpace(cfg.KeyFile)
	cfg.SharedSecret = strings.TrimSpace(cfg.SharedSecret)

	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8443"
	}
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join(".", "data")
	}
	if cfg.HeartbeatInterval <= 0 {
		return errors.New("heartbeat interval must be positive")
	}
	if cfg.HeartbeatTimeout < cfg.HeartbeatInterval {
		return errors.New("heartbeat timeout must be greater than or equal to heartbeat interval")
	}
	if cfg.MaxTimestampSkew <= 0 {
		return errors.New("max timestamp skew must be positive")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	if cfg.CertFile == "" {
		cfg.CertFile = filepath.Join(cfg.DataDir, "tls.crt")
	}
	if cfg.KeyFile == "" {
		cfg.KeyFile = filepath.Join(cfg.DataDir, "tls.key")
	}
	if cfg.Public {
		if cfg.SharedSecret != "" {
			log.Println("[WARN] shared_secret is set but will be ignored in public mode")
		}
		cfg.SharedSecret = ""
	} else {
		if cfg.SharedSecret == "" {
			return errors.New("shared_secret is required: set it in the config file or via --shared-secret")
		}
	}
	return nil
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

func applyStringOverride(flagChanged func(string) bool, name, value string, target *string) {
	if flagChanged != nil && flagChanged(name) {
		*target = value
	}
}

func applyDurationOverride(flagChanged func(string) bool, name string, value time.Duration, target *time.Duration) {
	if flagChanged != nil && flagChanged(name) {
		*target = value
	}
}

func applyBoolOverride(flagChanged func(string) bool, name string, value bool, target *bool) {
	if flagChanged != nil && flagChanged(name) {
		*target = value
	}
}
