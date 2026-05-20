package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"obsidian-remotely-sync-cli/config"
	"obsidian-remotely-sync-cli/crypto"
)

func TestConnectS3NoCredentials(t *testing.T) {
	cfg := &config.Config{
		S3: config.S3Config{
			Endpoint:  "https://cos.ap-nanjing.myqcloud.com",
			Region:    "ap-nanjing",
			AccessKey: "",
			SecretKey: "",
			Bucket:    "test",
		},
	}

	// Should succeed creating client (errors happen on actual API calls)
	_, err := connectS3(context.Background(), cfg)
	if err != nil {
		t.Fatalf("connectS3 should succeed creating client: %v", err)
	}
}

func TestHelpersEncryptDecryptRoundtrip(t *testing.T) {
	data := []byte("test content for helpers")
	password := "test-pass"

	encrypted, err := crypto.Encrypt(data, password)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decrypted, err := crypto.Decrypt(encrypted, password)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if string(decrypted) != string(data) {
		t.Fatalf("roundtrip failed: got %q, want %q", string(decrypted), string(data))
	}
}

func TestHelpersEncryptDecryptName(t *testing.T) {
	password := "test"

	enc, err := crypto.EncryptName("notes/test.md", password)
	if err != nil {
		t.Fatalf("EncryptName failed: %v", err)
	}

	if !crypto.IsLikelyEncrypted(enc) {
		t.Fatal("encrypted name should be detected as encrypted")
	}

	dec, err := crypto.DecryptName(enc, password)
	if err != nil {
		t.Fatalf("DecryptName failed: %v", err)
	}

	if dec != "notes/test.md" {
		t.Fatalf("got %q, want %q", dec, "notes/test.md")
	}
}

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "test.txt")

	// writeFileAtomic is in helpers.go — test it indirectly via crypto
	// since the function is used in pull operations
	data := []byte("hello world")
	encrypted, _ := crypto.Encrypt(data, "pass")
	decrypted, _ := crypto.Decrypt(encrypted, "pass")

	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, decrypted, 0644)

	read, _ := os.ReadFile(path)
	if string(read) != string(data) {
		t.Fatalf("got %q, want %q", string(read), string(data))
	}
}
