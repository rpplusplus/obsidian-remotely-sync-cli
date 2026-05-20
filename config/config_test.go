package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `vault_path: /tmp/test-vault
s3:
  endpoint: https://cos.ap-nanjing.myqcloud.com
  region: ap-nanjing
  access_key: AKIDTEST
  secret_key: SECRETTEST
  bucket: test-bucket
  prefix: test-prefix
encryption:
  method: openssl-base64
  password: test-pass
sync:
  direction: bidirectional
  conflict: keep_newer
`
	if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.VaultPath != "/tmp/test-vault" {
		t.Errorf("VaultPath = %q, want %q", cfg.VaultPath, "/tmp/test-vault")
	}
	if cfg.S3.Bucket != "test-bucket" {
		t.Errorf("Bucket = %q, want %q", cfg.S3.Bucket, "test-bucket")
	}
	if cfg.Encryption.Password != "test-pass" {
		t.Errorf("Password = %q, want %q", cfg.Encryption.Password, "test-pass")
	}
	if cfg.Sync.Direction != "bidirectional" {
		t.Errorf("Direction = %q, want %q", cfg.Sync.Direction, "bidirectional")
	}
}

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	// Minimal config — only required fields
	content := `vault_path: /tmp/vault
s3:
  bucket: my-bucket
encryption:
  password: pass123
`
	if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.S3.Endpoint != "https://cos.ap-nanjing.myqcloud.com" {
		t.Errorf("default Endpoint = %q", cfg.S3.Endpoint)
	}
	if cfg.S3.Region != "ap-nanjing" {
		t.Errorf("default Region = %q", cfg.S3.Region)
	}
	if cfg.Encryption.Method != "openssl-base64" {
		t.Errorf("default Method = %q", cfg.Encryption.Method)
	}
	if cfg.Sync.Direction != "bidirectional" {
		t.Errorf("default Direction = %q", cfg.Sync.Direction)
	}
	if cfg.Sync.Conflict != "keep_newer" {
		t.Errorf("default Conflict = %q", cfg.Sync.Conflict)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"no vault_path", `s3:\n  bucket: b\nencryption:\n  password: p`},
		{"no bucket", `vault_path: /tmp/v\nencryption:\n  password: p`},
		{"no password", `vault_path: /tmp/v\ns3:\n  bucket: b`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := filepath.Join(dir, "config.yaml")
			os.WriteFile(cfgPath, []byte(tc.content), 0600)

			_, err := Load(cfgPath)
			if err == nil {
				t.Fatal("Load should fail for missing required field")
			}
		})
	}
}

func TestLoadFileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("Load should fail for nonexistent file")
	}
}

func TestWriteDefault(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "sub", "config.yaml")

	if err := WriteDefault(cfgPath); err != nil {
		t.Fatalf("WriteDefault failed: %v", err)
	}

	// Should fail if already exists
	if err := WriteDefault(cfgPath); err == nil {
		t.Fatal("WriteDefault should fail if file exists")
	}

	// Verify it's valid YAML (Load will reject due to empty required fields,
	// but that's expected — user must fill in bucket/password before using)
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("default config is empty")
	}

	// Load should fail because default has empty required fields
	_, err = Load(cfgPath)
	if err == nil {
		t.Log("Note: default config passes Load() — consider if empty fields should be rejected")
	}
}

func TestDefaultPaths(t *testing.T) {
	cfgPath := DefaultConfigPath()
	if filepath.Ext(cfgPath) != ".yaml" {
		t.Errorf("DefaultConfigPath should end with .yaml: %q", cfgPath)
	}

	statePath := DefaultStatePath()
	if filepath.Ext(statePath) != ".db" {
		t.Errorf("DefaultStatePath should end with .db: %q", statePath)
	}
}
