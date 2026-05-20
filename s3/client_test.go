package s3

import (
	"context"
	"testing"
)

func TestNewClientNoCredentials(t *testing.T) {
	// Should succeed creating client struct (errors happen on API calls)
	client, err := NewClient(context.Background(),
		"https://cos.ap-nanjing.myqcloud.com",
		"ap-nanjing",
		"", "", "test-bucket", "test-prefix",
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if client == nil {
		t.Fatal("client should not be nil")
	}
	if client.bucket != "test-bucket" {
		t.Errorf("bucket = %q, want %q", client.bucket, "test-bucket")
	}
	if client.prefix != "test-prefix" {
		t.Errorf("prefix = %q, want %q", client.prefix, "test-prefix")
	}
}

func TestFullKey(t *testing.T) {
	c := &Client{prefix: "my-prefix"}
	if got := c.fullKey("file.md"); got != "my-prefix/file.md" {
		t.Errorf("fullKey = %q, want %q", got, "my-prefix/file.md")
	}

	c2 := &Client{prefix: ""}
	if got := c2.fullKey("file.md"); got != "file.md" {
		t.Errorf("fullKey (no prefix) = %q, want %q", got, "file.md")
	}
}

func TestStripPrefix(t *testing.T) {
	c := &Client{prefix: "my-prefix"}
	if got := c.stripPrefix("my-prefix/file.md"); got != "file.md" {
		t.Errorf("stripPrefix = %q, want %q", got, "file.md")
	}

	// No prefix case
	c2 := &Client{prefix: ""}
	if got := c2.stripPrefix("file.md"); got != "file.md" {
		t.Errorf("stripPrefix (no prefix) = %q, want %q", got, "file.md")
	}

	// Key doesn't start with prefix
	if got := c.stripPrefix("other/file.md"); got != "other/file.md" {
		t.Errorf("stripPrefix (no match) = %q, want %q", got, "other/file.md")
	}
}

func TestRemoteFileFields(t *testing.T) {
	// Just verify the struct is usable
	rf := RemoteFile{
		Key:  "test-key",
		Size: 1024,
	}
	if rf.Key != "test-key" {
		t.Errorf("Key = %q", rf.Key)
	}
	if rf.Size != 1024 {
		t.Errorf("Size = %d", rf.Size)
	}
}
