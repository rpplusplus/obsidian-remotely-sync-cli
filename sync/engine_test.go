package sync

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"obsidian-remotely-sync-cli/config"
	"obsidian-remotely-sync-cli/s3"
)

// testEngine creates a SyncEngine with a temp vault and in-memory SQLite for testing.
func testEngine(t *testing.T) (*SyncEngine, string) {
	t.Helper()

	vault := t.TempDir()
	stateDir := t.TempDir()
	dbPath := filepath.Join(stateDir, "state.db")

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	cfg := &config.Config{
		VaultPath: vault,
		Encryption: config.EncConfig{
			Method:   "openssl-base64",
			Password: "test-password",
		},
		Sync: config.SyncConfig{
			Direction: "bidirectional",
			Conflict:  "keep_newer",
		},
	}

	e := &SyncEngine{
		cfg:      cfg,
		s3c:      nil, // we test diff logic without real S3
		db:       db,
		vault:    vault,
		password: cfg.Encryption.Password,
	}

	if err := e.migrate(); err != nil {
		db.Close()
		t.Fatalf("migrate: %v", err)
	}

	t.Cleanup(func() { db.Close() })

	return e, vault
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestMigrateCreatesTable(t *testing.T) {
	e, _ := testEngine(t)

	var name string
	err := e.db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='file_state'`).Scan(&name)
	if err != nil {
		t.Fatalf("table not found: %v", err)
	}
	if name != "file_state" {
		t.Fatalf("unexpected table name: %s", name)
	}
}

func TestScanLocal(t *testing.T) {
	e, vault := testEngine(t)

	// Create some test files
	writeFile(t, filepath.Join(vault, "note1.md"), "# Note 1")
	writeFile(t, filepath.Join(vault, "folder", "note2.md"), "# Note 2")
	writeFile(t, filepath.Join(vault, ".obsidian", "config.json"), "{}") // should be skipped
	writeFile(t, filepath.Join(vault, ".hidden"), "secret")              // should be skipped

	files, err := e.scanLocal()
	if err != nil {
		t.Fatalf("scanLocal: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	if _, ok := files["note1.md"]; !ok {
		t.Error("note1.md not found")
	}
	if _, ok := files["folder/note2.md"]; !ok {
		t.Error("folder/note2.md not found")
	}
}

func TestDiffNewLocal(t *testing.T) {
	e, vault := testEngine(t)

	// Only local file, no remote, no state -> should push
	writeFile(t, filepath.Join(vault, "new.md"), "content")

	local, err := e.scanLocal()
	if err != nil {
		t.Fatalf("scanLocal: %v", err)
	}

	diff := e.classifyFile("new.md", true, false, false, local, nil, fileStateRow{})
	if diff.Action != ActionPush {
		t.Errorf("expected ActionPush, got %s", diff.Action)
	}
}

func TestDiffNewRemote(t *testing.T) {
	e, _ := testEngine(t)

	// Only remote, no local, no state -> should pull
	diff := e.classifyFile("remote.md", false, true, false, nil, map[string]RemoteEntry{
		"remote.md": {Key: "remote.md", LastModified: time.Now()},
	}, fileStateRow{})
	if diff.Action != ActionPull {
		t.Errorf("expected ActionPull, got %s", diff.Action)
	}
}

func TestDiffUnchanged(t *testing.T) {
	e, vault := testEngine(t)

	writeFile(t, filepath.Join(vault, "unchanged.md"), "same content")
	hash, _ := fileSHA256(filepath.Join(vault, "unchanged.md"))

	local := map[string]LocalFile{
		"unchanged.md": {Path: "unchanged.md", Hash: hash, ModTime: time.Now()},
	}
	remote := map[string]RemoteEntry{
		"unchanged.md": {Key: "unchanged.md", LastModified: time.Now()},
	}
	prev := fileStateRow{
		LocalHash:   hash,
		RemoteMtime: time.Now().Unix(),
	}

	diff := e.classifyFile("unchanged.md", true, true, true, local, remote, prev)
	if diff.Action != ActionNone {
		t.Errorf("expected ActionNone, got %s", diff.Action)
	}
}

func TestDiffLocalChanged(t *testing.T) {
	e, vault := testEngine(t)

	writeFile(t, filepath.Join(vault, "modified.md"), "new content")
	hash, _ := fileSHA256(filepath.Join(vault, "modified.md"))

	local := map[string]LocalFile{
		"modified.md": {Path: "modified.md", Hash: hash, ModTime: time.Now()},
	}
	remote := map[string]RemoteEntry{
		"modified.md": {Key: "modified.md", LastModified: time.Now()},
	}
	prev := fileStateRow{
		LocalHash:   "old-hash",
		RemoteMtime: time.Now().Unix(), // same as remote -> remote unchanged
	}

	diff := e.classifyFile("modified.md", true, true, true, local, remote, prev)
	if diff.Action != ActionPush {
		t.Errorf("expected ActionPush, got %s", diff.Action)
	}
}

func TestDiffRemoteChanged(t *testing.T) {
	e, vault := testEngine(t)

	writeFile(t, filepath.Join(vault, "remote-changed.md"), "same")
	hash, _ := fileSHA256(filepath.Join(vault, "remote-changed.md"))

	local := map[string]LocalFile{
		"remote-changed.md": {Path: "remote-changed.md", Hash: hash, ModTime: time.Now()},
	}
	remote := map[string]RemoteEntry{
		"remote-changed.md": {Key: "remote-changed.md", LastModified: time.Now()},
	}
	prev := fileStateRow{
		LocalHash:   hash,               // local unchanged
		RemoteMtime: time.Now().Add(-time.Hour).Unix(), // remote changed (different mtime)
	}

	diff := e.classifyFile("remote-changed.md", true, true, true, local, remote, prev)
	if diff.Action != ActionPull {
		t.Errorf("expected ActionPull, got %s", diff.Action)
	}
}

func TestDiffBothChangedConflict(t *testing.T) {
	e, vault := testEngine(t)

	writeFile(t, filepath.Join(vault, "conflict.md"), "new local")
	hash, _ := fileSHA256(filepath.Join(vault, "conflict.md"))

	local := map[string]LocalFile{
		"conflict.md": {Path: "conflict.md", Hash: hash, ModTime: time.Now()},
	}
	remote := map[string]RemoteEntry{
		"conflict.md": {Key: "conflict.md", LastModified: time.Now()},
	}
	prev := fileStateRow{
		LocalHash:   "old-local-hash",
		RemoteMtime: time.Now().Add(-time.Hour).Unix(), // remote also changed
	}

	diff := e.classifyFile("conflict.md", true, true, true, local, remote, prev)
	if diff.Action != ActionConflict {
		t.Errorf("expected ActionConflict, got %s", diff.Action)
	}
}

func TestDiffDeletedLocally(t *testing.T) {
	e, _ := testEngine(t)

	// In state, not local, still remote -> should delete remote
	remote := map[string]RemoteEntry{
		"deleted.md": {Key: "deleted.md", LastModified: time.Now()},
	}
	prev := fileStateRow{
		LocalHash:  "hash",
		RemoteHash: "enc-key",
	}

	diff := e.classifyFile("deleted.md", false, true, true, nil, remote, prev)
	if diff.Action != ActionDeleteRemote {
		t.Errorf("expected ActionDeleteRemote, got %s", diff.Action)
	}
}

func TestDiffDeletedRemotely(t *testing.T) {
	e, vault := testEngine(t)

	writeFile(t, filepath.Join(vault, "gone-remote.md"), "still here")
	hash, _ := fileSHA256(filepath.Join(vault, "gone-remote.md"))

	local := map[string]LocalFile{
		"gone-remote.md": {Path: "gone-remote.md", Hash: hash, ModTime: time.Now()},
	}
	prev := fileStateRow{
		LocalHash:  hash,
		RemoteHash: "enc-key",
	}

	diff := e.classifyFile("gone-remote.md", true, false, true, local, nil, prev)
	if diff.Action != ActionDeleteLocal {
		t.Errorf("expected ActionDeleteLocal, got %s", diff.Action)
	}
}

func TestConflictResolution(t *testing.T) {
	e, _ := testEngine(t)

	local := LocalFile{ModTime: time.Now()}
	older := RemoteEntry{LastModified: time.Now().Add(-time.Hour)}
	newer := RemoteEntry{LastModified: time.Now().Add(time.Hour)}

	diff := FileDiff{Action: ActionConflict}

	// keep_newer: local is newer -> push
	e.cfg.Sync.Conflict = "keep_newer"
	if got := e.resolveConflict(diff, local, older); got != ActionPush {
		t.Errorf("keep_newer local newer: expected Push, got %s", got)
	}

	// keep_newer: remote is newer -> pull
	if got := e.resolveConflict(diff, local, newer); got != ActionPull {
		t.Errorf("keep_newer remote newer: expected Pull, got %s", got)
	}

	// keep_local: always push
	e.cfg.Sync.Conflict = "keep_local"
	if got := e.resolveConflict(diff, local, newer); got != ActionPush {
		t.Errorf("keep_local: expected Push, got %s", got)
	}

	// keep_remote: always pull
	e.cfg.Sync.Conflict = "keep_remote"
	if got := e.resolveConflict(diff, local, older); got != ActionPull {
		t.Errorf("keep_remote: expected Pull, got %s", got)
	}
}

func TestStateUpsertAndLoad(t *testing.T) {
	e, _ := testEngine(t)

	tx, _ := e.db.Begin()

	mtime := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := e.upsertState(tx, "test.md", "localhash", "remotehash", 100, 200, mtime, mtime); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	states, err := e.loadState()
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}

	s, ok := states["test.md"]
	if !ok {
		t.Fatal("state not found for test.md")
	}
	if s.LocalHash != "localhash" {
		t.Errorf("local hash: got %s, want localhash", s.LocalHash)
	}
	if s.RemoteHash != "remotehash" {
		t.Errorf("remote hash: got %s, want remotehash", s.RemoteHash)
	}
	if s.LocalSize != 100 {
		t.Errorf("local size: got %d, want 100", s.LocalSize)
	}
	if s.RemoteSize != 200 {
		t.Errorf("remote size: got %d, want 200", s.RemoteSize)
	}
}

func TestStateRemove(t *testing.T) {
	e, _ := testEngine(t)

	tx, _ := e.db.Begin()
	mtime := time.Now()
	e.upsertState(tx, "remove-me.md", "h", "r", 10, 20, mtime, mtime)
	if err := e.removeState(tx, "remove-me.md"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	tx.Commit()

	states, _ := e.loadState()
	if _, ok := states["remove-me.md"]; ok {
		t.Error("expected state to be removed")
	}
}

func TestDiffSummary(t *testing.T) {
	diffs := []FileDiff{
		{Path: "a.md", Action: ActionPush},
		{Path: "b.md", Action: ActionPush},
		{Path: "c.md", Action: ActionPull},
		{Path: "d.md", Action: ActionConflict},
		{Path: "e.md", Action: ActionDeleteLocal},
		{Path: "f.md", Action: ActionDeleteRemote},
	}

	summary := DiffSummary(diffs)
	if summary == "" {
		t.Error("expected non-empty summary")
	}
	// Should mention counts
	if summary == "Everything up to date." {
		t.Error("expected detailed summary, got 'up to date'")
	}
}

func TestDiffSummaryEmpty(t *testing.T) {
	if got := DiffSummary(nil); got != "Everything up to date." {
		t.Errorf("expected 'Everything up to date.', got %q", got)
	}
}

func TestActionString(t *testing.T) {
	tests := []struct {
		a    Action
		want string
	}{
		{ActionNone, "none"},
		{ActionPush, "push"},
		{ActionPull, "pull"},
		{ActionDeleteLocal, "delete_local"},
		{ActionDeleteRemote, "delete_remote"},
		{ActionConflict, "conflict"},
		{Action(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.a.String(); got != tt.want {
			t.Errorf("Action(%d).String() = %q, want %q", tt.a, got, tt.want)
		}
	}
}

// Ensure SyncEngine implements expected interfaces (compile-time check)
var _ = (*SyncEngine)(nil)
var _ = (*s3.Client)(nil) // ensure s3 package is referenced
