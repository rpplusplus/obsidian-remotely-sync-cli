package sync

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"obsidian-remotely-sync-cli/config"
	"obsidian-remotely-sync-cli/crypto"
	"obsidian-remotely-sync-cli/s3"
)

// Action represents what should happen to a file.
type Action int

const (
	ActionNone   Action = iota
	ActionPush          // upload local -> remote
	ActionPull          // download remote -> local
	ActionDeleteLocal   // delete local file
	ActionDeleteRemote  // delete remote object
	ActionConflict      // both sides changed, needs resolution
)

func (a Action) String() string {
	switch a {
	case ActionNone:
		return "none"
	case ActionPush:
		return "push"
	case ActionPull:
		return "pull"
	case ActionDeleteLocal:
		return "delete_local"
	case ActionDeleteRemote:
		return "delete_remote"
	case ActionConflict:
		return "conflict"
	default:
		return "unknown"
	}
}

// LocalFile holds metadata about a local file in the vault.
type LocalFile struct {
	Path    string // relative to vault root
	Size    int64
	ModTime time.Time
	Hash    string // SHA-256 hex
}

// FileDiff describes the sync action needed for a single file.
type FileDiff struct {
	Path       string
	Action     Action
	LocalHash  string // empty if file doesn't exist locally
	RemoteHash string // empty if file doesn't exist remotely
}

// SyncResult summarizes what a sync pass did.
type SyncResult struct {
	Pushed        int
	Pulled        int
	DeletedLocal  int
	DeletedRemote int
	Conflicts     []FileDiff
	Errors        []error
}

// SyncEngine coordinates bidirectional sync between a local vault and S3.
type SyncEngine struct {
	cfg    *config.Config
	s3c    *s3.Client
	db     *sql.DB
	vault  string
	password string
}

// NewEngine opens the SQLite state DB and returns a ready engine.
func NewEngine(cfg *config.Config, s3c *s3.Client) (*SyncEngine, error) {
	statePath := config.DefaultStatePath()

	// Ensure parent dir exists
	if err := os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
		return nil, fmt.Errorf("creating state dir: %w", err)
	}

	db, err := sql.Open("sqlite3", statePath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("opening state db: %w", err)
	}

	e := &SyncEngine{
		cfg:      cfg,
		s3c:      s3c,
		db:       db,
		vault:    cfg.VaultPath,
		password: cfg.Encryption.Password,
	}

	if err := e.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return e, nil
}

// Close releases the database handle.
func (e *SyncEngine) Close() error {
	return e.db.Close()
}

// migrate creates the file_state table if it doesn't exist.
func (e *SyncEngine) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS file_state (
		path         TEXT PRIMARY KEY,
		local_hash   TEXT NOT NULL DEFAULT '',
		remote_hash  TEXT NOT NULL DEFAULT '',
		local_size   INTEGER NOT NULL DEFAULT 0,
		remote_size  INTEGER NOT NULL DEFAULT 0,
		local_mtime  INTEGER NOT NULL DEFAULT 0,
		remote_mtime INTEGER NOT NULL DEFAULT 0,
		synced_at    INTEGER NOT NULL DEFAULT 0
	);`
	_, err := e.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("creating file_state table: %w", err)
	}
	return nil
}

// ---------- Local scanning ----------

// scanLocal walks the vault directory and returns all non-hidden files with their SHA-256 hashes.
func (e *SyncEngine) scanLocal() (map[string]LocalFile, error) {
	files := make(map[string]LocalFile)

	err := filepath.WalkDir(e.vault, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories and hidden files/folders
		name := d.Name()
		if d.IsDir() {
			if name == ".obsidian" || name == ".trash" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(name, ".") {
			return nil
		}

		rel, err := filepath.Rel(e.vault, p)
		if err != nil {
			return fmt.Errorf("computing relative path: %w", err)
		}
		// Normalize to forward slashes for S3 key compatibility
		rel = filepath.ToSlash(rel)

		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", rel, err)
		}

		hash, err := fileSHA256(p)
		if err != nil {
			return fmt.Errorf("hashing %s: %w", rel, err)
		}

		files[rel] = LocalFile{
			Path:    rel,
			Size:    info.Size(),
			ModTime: info.ModTime().UTC(),
			Hash:    hash,
		}
		return nil
	})

	return files, err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ---------- Remote scanning ----------

// RemoteEntry pairs an S3 object with its decrypted name.
type RemoteEntry struct {
	Key          string       // decrypted relative path
	RawKey       string       // original S3 key (encrypted)
	Size         int64
	LastModified time.Time
}

// scanRemote lists remote objects and decrypts their names.
func (e *SyncEngine) scanRemote(ctx context.Context) (map[string]RemoteEntry, error) {
	objects, err := e.s3c.ListObjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing remote objects: %w", err)
	}

	entries := make(map[string]RemoteEntry, len(objects))
	for _, obj := range objects {
		decrypted, err := crypto.DecryptName(obj.Key, e.password)
		if err != nil {
			// Skip objects that don't look like ours
			continue
		}

		// Folder markers from remotely-save: encrypted name ends with "/".
		// These are empty/tiny files used to represent directory structure.
		// We handle them by ensuring the local directory exists, then skip.
		if strings.HasSuffix(decrypted, "/") {
			dirPath := filepath.Join(e.vault, decrypted)
			_ = os.MkdirAll(dirPath, 0755)
			continue
		}

		entries[decrypted] = RemoteEntry{
			Key:          decrypted,
			RawKey:       obj.Key,
			Size:         obj.Size,
			LastModified: obj.LastModified.UTC(),
		}
	}

	return entries, nil
}

// ---------- State management ----------

// fileStateRow is a row from the file_state table.
type fileStateRow struct {
	Path        string
	LocalHash   string
	RemoteHash  string
	LocalSize   int64
	RemoteSize  int64
	LocalMtime  int64
	RemoteMtime int64
	SyncedAt    int64
}

// loadState reads all rows from file_state.
func (e *SyncEngine) loadState() (map[string]fileStateRow, error) {
	rows, err := e.db.Query(`SELECT path, local_hash, remote_hash, local_size, remote_size, local_mtime, remote_mtime, synced_at FROM file_state`)
	if err != nil {
		return nil, fmt.Errorf("querying file_state: %w", err)
	}
	defer rows.Close()

	states := make(map[string]fileStateRow)
	for rows.Next() {
		var s fileStateRow
		if err := rows.Scan(&s.Path, &s.LocalHash, &s.RemoteHash, &s.LocalSize, &s.RemoteSize, &s.LocalMtime, &s.RemoteMtime, &s.SyncedAt); err != nil {
			return nil, fmt.Errorf("scanning file_state row: %w", err)
		}
		states[s.Path] = s
	}
	return states, rows.Err()
}

// upsertState inserts or updates a file's state after successful sync.
func (e *SyncEngine) upsertState(tx *sql.Tx, path, localHash, remoteHash string, localSize, remoteSize int64, localMtime, remoteMtime time.Time) error {
	now := time.Now().Unix()
	_, err := tx.Exec(`
		INSERT INTO file_state (path, local_hash, remote_hash, local_size, remote_size, local_mtime, remote_mtime, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO UPDATE SET
			local_hash   = excluded.local_hash,
			remote_hash  = excluded.remote_hash,
			local_size   = excluded.local_size,
			remote_size  = excluded.remote_size,
			local_mtime  = excluded.local_mtime,
			remote_mtime = excluded.remote_mtime,
			synced_at    = excluded.synced_at`,
		path, localHash, remoteHash, localSize, remoteSize,
		localMtime.Unix(), remoteMtime.Unix(), now)
	return err
}

// removeState deletes a file's state row.
func (e *SyncEngine) removeState(tx *sql.Tx, path string) error {
	_, err := tx.Exec(`DELETE FROM file_state WHERE path = ?`, path)
	return err
}

// ---------- Diff algorithm ----------

// Diff computes the set of FileDiffs by comparing local, remote, and last-known state.
func (e *SyncEngine) Diff(ctx context.Context) ([]FileDiff, error) {
	local, err := e.scanLocal()
	if err != nil {
		return nil, fmt.Errorf("scanning local: %w", err)
	}

	remote, err := e.scanRemote(ctx)
	if err != nil {
		return nil, fmt.Errorf("scanning remote: %w", err)
	}

	states, err := e.loadState()
	if err != nil {
		return nil, fmt.Errorf("loading state: %w", err)
	}

	// Collect all unique paths across local, remote, and state
	allPaths := make(map[string]struct{})
	for p := range local {
		allPaths[p] = struct{}{}
	}
	for p := range remote {
		allPaths[p] = struct{}{}
	}
	for p := range states {
		allPaths[p] = struct{}{}
	}

	var diffs []FileDiff

	for path := range allPaths {
		_, hasLocal := local[path]
		_, hasRemote := remote[path]
	 prevState, hasState := states[path]

		diff := e.classifyFile(path, hasLocal, hasRemote, hasState, local, remote, prevState)
		if diff.Action != ActionNone {
			diffs = append(diffs, diff)
		}
	}

	// Sort for deterministic output
	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].Path < diffs[j].Path
	})

	return diffs, nil
}

// classifyFile determines the action for a single file based on three-way state.
func (e *SyncEngine) classifyFile(
	path string,
	hasLocal, hasRemote, hasState bool,
	local map[string]LocalFile,
	remote map[string]RemoteEntry,
	prev fileStateRow,
) FileDiff {
	lf := local[path]
	re := remote[path]

	localHash := lf.Hash

	// Scenario 1: File exists nowhere in state, only locally -> new local file
	if !hasState && hasLocal && !hasRemote {
		return FileDiff{Path: path, Action: ActionPush, LocalHash: localHash}
	}

	// Scenario 2: File exists nowhere in state, only remotely -> new remote file
	if !hasState && !hasLocal && hasRemote {
		return FileDiff{Path: path, Action: ActionPull, RemoteHash: prev.RemoteHash}
	}

	// Scenario 3: File exists nowhere in state, in both -> new file on both sides, conflict
	if !hasState && hasLocal && hasRemote {
		// This is unusual on first sync; use conflict resolution
		return FileDiff{Path: path, Action: ActionConflict, LocalHash: localHash, RemoteHash: prev.RemoteHash}
	}

	// Scenario 4: In state but not local, not remote -> synced deletion, clean up state
	if hasState && !hasLocal && !hasRemote {
		return FileDiff{Path: path, Action: ActionNone}
	}

	// Scenario 5: In state, not local, but still remote -> deleted locally
	if hasState && !hasLocal && hasRemote {
		return FileDiff{Path: path, Action: ActionDeleteRemote, RemoteHash: prev.RemoteHash}
	}

	// Scenario 6: In state, still local, not remote -> deleted remotely
	if hasState && hasLocal && !hasRemote {
		return FileDiff{Path: path, Action: ActionDeleteLocal, LocalHash: localHash}
	}

	// Scenario 7: In state, in both -> compare hashes to find changes
	if hasState && hasLocal && hasRemote {
		localChanged := localHash != prev.LocalHash
		// We can't hash remote content directly; instead we compare the remote
		// object's LastModified against the last-synced remote mtime.
		remoteChanged := re.LastModified.Unix() != prev.RemoteMtime

		switch {
		case !localChanged && !remoteChanged:
			return FileDiff{Path: path, Action: ActionNone}
		case localChanged && !remoteChanged:
			return FileDiff{Path: path, Action: ActionPush, LocalHash: localHash}
		case !localChanged && remoteChanged:
			return FileDiff{Path: path, Action: ActionPull, RemoteHash: prev.RemoteHash}
		default:
			// Both changed -> conflict
			return FileDiff{Path: path, Action: ActionConflict, LocalHash: localHash, RemoteHash: prev.RemoteHash}
		}
	}

	return FileDiff{Path: path, Action: ActionNone}
}

// ---------- Conflict resolution ----------

// resolveConflict picks which side wins based on the configured strategy.
// Returns the resolved action: ActionPush (keep local) or ActionPull (keep remote).
func (e *SyncEngine) resolveConflict(diff FileDiff, local LocalFile, remote RemoteEntry) Action {
	switch e.cfg.Sync.Conflict {
	case "keep_local":
		return ActionPush
	case "keep_remote":
		return ActionPull
	case "keep_newer", "":
		if local.ModTime.After(remote.LastModified) {
			return ActionPush
		}
		return ActionPull
	default:
		// Unknown strategy, default to keep_newer
		if local.ModTime.After(remote.LastModified) {
			return ActionPush
		}
		return ActionPull
	}
}

// ---------- Execution ----------

// Run computes the diff and executes all sync actions.
func (e *SyncEngine) Run(ctx context.Context) (*SyncResult, error) {
	diffs, err := e.Diff(ctx)
	if err != nil {
		return nil, fmt.Errorf("computing diff: %w", err)
	}

	local, err := e.scanLocal()
	if err != nil {
		return nil, fmt.Errorf("re-scanning local: %w", err)
	}

	remote, err := e.scanRemote(ctx)
	if err != nil {
		return nil, fmt.Errorf("re-scanning remote: %w", err)
	}

	result := &SyncResult{}
	tx, err := e.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	for _, diff := range diffs {
		switch diff.Action {
		case ActionPush:
			if err := e.execPush(ctx, diff.Path, local[diff.Path]); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("push %s: %w", diff.Path, err))
				continue
			}
			result.Pushed++

		case ActionPull:
			if err := e.execPull(ctx, diff.Path, remote[diff.Path]); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("pull %s: %w", diff.Path, err))
				continue
			}
			result.Pulled++

		case ActionDeleteLocal:
			if err := e.execDeleteLocal(diff.Path); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("delete local %s: %w", diff.Path, err))
				continue
			}
			if err := e.removeState(tx, diff.Path); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("remove state %s: %w", diff.Path, err))
			}
			result.DeletedLocal++

		case ActionDeleteRemote:
			if err := e.execDeleteRemote(ctx, diff.Path, remote[diff.Path]); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("delete remote %s: %w", diff.Path, err))
				continue
			}
			if err := e.removeState(tx, diff.Path); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("remove state %s: %w", diff.Path, err))
			}
			result.DeletedRemote++

		case ActionConflict:
			resolved := e.resolveConflict(diff, local[diff.Path], remote[diff.Path])
			switch resolved {
			case ActionPush:
				if err := e.execPush(ctx, diff.Path, local[diff.Path]); err != nil {
					result.Errors = append(result.Errors, fmt.Errorf("conflict push %s: %w", diff.Path, err))
					continue
				}
				result.Pushed++
			case ActionPull:
				if err := e.execPull(ctx, diff.Path, remote[diff.Path]); err != nil {
					result.Errors = append(result.Errors, fmt.Errorf("conflict pull %s: %w", diff.Path, err))
					continue
				}
				result.Pulled++
			}
			// Record the conflict for logging
			result.Conflicts = append(result.Conflicts, diff)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("committing state changes: %w", err)
	}

	return result, nil
}

// execPush encrypts and uploads a local file, then updates state.
func (e *SyncEngine) execPush(ctx context.Context, relPath string, lf LocalFile) error {
	// Read local file
	data, err := os.ReadFile(filepath.Join(e.vault, relPath))
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	// Encrypt content
	encrypted, err := crypto.Encrypt(data, e.password)
	if err != nil {
		return fmt.Errorf("encrypting: %w", err)
	}

	// Encrypt filename
	encName, err := crypto.EncryptName(relPath, e.password)
	if err != nil {
		return fmt.Errorf("encrypting name: %w", err)
	}

	// Upload
	if err := e.s3c.PutObject(ctx, encName, encrypted); err != nil {
		return fmt.Errorf("uploading: %w", err)
	}

	// Update state
	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := e.upsertState(tx, relPath, lf.Hash, encName, lf.Size, int64(len(encrypted)), lf.ModTime, time.Now().UTC()); err != nil {
		return fmt.Errorf("updating state: %w", err)
	}

	return tx.Commit()
}

// execPull downloads and decrypts a remote file, then updates state.
func (e *SyncEngine) execPull(ctx context.Context, relPath string, re RemoteEntry) error {
	// Download encrypted content
	encrypted, err := e.s3c.GetObject(ctx, re.RawKey)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}

	// Decrypt content
	decrypted, err := crypto.Decrypt(encrypted, e.password)
	if err != nil {
		return fmt.Errorf("decrypting: %w", err)
	}

	// Ensure parent dir exists
	localPath := filepath.Join(e.vault, relPath)
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return fmt.Errorf("creating dir: %w", err)
	}

	// Write local file
	if err := os.WriteFile(localPath, decrypted, 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	// Hash the written file for state tracking
	hash, err := fileSHA256(localPath)
	if err != nil {
		return fmt.Errorf("hashing written file: %w", err)
	}

	// Update state
	info, _ := os.Stat(localPath)
	mtime := time.Now().UTC()
	if info != nil {
		mtime = info.ModTime().UTC()
	}

	tx, err := e.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := e.upsertState(tx, relPath, hash, re.RawKey, int64(len(decrypted)), re.Size, mtime, re.LastModified); err != nil {
		return fmt.Errorf("updating state: %w", err)
	}

	return tx.Commit()
}

// execDeleteLocal removes a local file.
func (e *SyncEngine) execDeleteLocal(relPath string) error {
	localPath := filepath.Join(e.vault, relPath)
	if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing file: %w", err)
	}
	return nil
}

// execDeleteRemote removes a remote object.
func (e *SyncEngine) execDeleteRemote(ctx context.Context, relPath string, re RemoteEntry) error {
	if err := e.s3c.DeleteObject(ctx, re.RawKey); err != nil {
		return fmt.Errorf("deleting object: %w", err)
	}
	return nil
}

// SyncDir returns the configured sync direction.
func (e *SyncEngine) SyncDir() string {
	return e.cfg.Sync.Direction
}

// DiffSummary returns a human-readable summary of pending diffs.
func DiffSummary(diffs []FileDiff) string {
	if len(diffs) == 0 {
		return "Everything up to date."
	}

	var pushes, pulls, delLocal, delRemote, conflicts int
	for _, d := range diffs {
		switch d.Action {
		case ActionPush:
			pushes++
		case ActionPull:
			pulls++
		case ActionDeleteLocal:
			delLocal++
		case ActionDeleteRemote:
			delRemote++
		case ActionConflict:
			conflicts++
		}
	}

	var parts []string
	if pushes > 0 {
		parts = append(parts, fmt.Sprintf("%d to push", pushes))
	}
	if pulls > 0 {
		parts = append(parts, fmt.Sprintf("%d to pull", pulls))
	}
	if delLocal > 0 {
		parts = append(parts, fmt.Sprintf("%d local deletions", delLocal))
	}
	if delRemote > 0 {
		parts = append(parts, fmt.Sprintf("%d remote deletions", delRemote))
	}
	if conflicts > 0 {
		parts = append(parts, fmt.Sprintf("%d conflicts", conflicts))
	}

	return strings.Join(parts, ", ")
}
