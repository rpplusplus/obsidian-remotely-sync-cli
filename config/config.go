package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds the application configuration.
type Config struct {
	VaultPath  string     `yaml:"vault_path"`
	S3         S3Config   `yaml:"s3"`
	Encryption EncConfig  `yaml:"encryption"`
	Sync       SyncConfig `yaml:"sync"`
}

// S3Config holds S3/COS connection settings.
type S3Config struct {
	Endpoint  string `yaml:"endpoint"`
	Region    string `yaml:"region"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	Bucket    string `yaml:"bucket"`
	Prefix    string `yaml:"prefix"`
}

// EncConfig holds encryption settings.
type EncConfig struct {
	Method   string `yaml:"method"`
	Password string `yaml:"password"`
}

// SyncConfig holds sync behavior settings.
type SyncConfig struct {
	Direction string `yaml:"direction"` // pull, push, bidirectional
	Conflict  string `yaml:"conflict"`  // keep_local, keep_remote, keep_newer
}

// DefaultConfigPath returns ~/.obsidian-remotely-sync-cli/config.yaml
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".obsidian-remotely-sync-cli", "config.yaml")
}

// DefaultStatePath returns ~/.obsidian-remotely-sync-cli/state.db
func DefaultStatePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".obsidian-remotely-sync-cli", "state.db")
}

// Load reads the config from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Validate required fields
	if cfg.VaultPath == "" {
		return nil, fmt.Errorf("vault_path is required")
	}
	if cfg.S3.Bucket == "" {
		return nil, fmt.Errorf("s3.bucket is required")
	}
	if cfg.Encryption.Password == "" {
		return nil, fmt.Errorf("encryption.password is required")
	}

	// Defaults
	if cfg.S3.Endpoint == "" {
		cfg.S3.Endpoint = "https://cos.ap-nanjing.myqcloud.com"
	}
	if cfg.S3.Region == "" {
		cfg.S3.Region = "ap-nanjing"
	}
	if cfg.Encryption.Method == "" {
		cfg.Encryption.Method = "openssl-base64"
	}
	if cfg.Sync.Direction == "" {
		cfg.Sync.Direction = "bidirectional"
	}
	if cfg.Sync.Conflict == "" {
		cfg.Sync.Conflict = "keep_newer"
	}

	return &cfg, nil
}

// DefaultConfig returns a default config struct for `init`.
func DefaultConfig() *Config {
	return &Config{
		VaultPath: "/path/to/vault",
		S3: S3Config{
			Endpoint:  "https://cos.ap-nanjing.myqcloud.com",
			Region:    "ap-nanjing",
			AccessKey: "",
			SecretKey: "",
			Bucket:    "",
			Prefix:    "",
		},
		Encryption: EncConfig{
			Method:   "openssl-base64",
			Password: "",
		},
		Sync: SyncConfig{
			Direction: "bidirectional",
			Conflict:  "keep_newer",
		},
	}
}

// WriteDefault writes the default config to the given path.
func WriteDefault(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("config file already exists at %s", path)
	}

	data, err := yaml.Marshal(DefaultConfig())
	if err != nil {
		return fmt.Errorf("marshaling default config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	return nil
}
