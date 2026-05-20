# Changelog

## [Unreleased]

### Fixed

- **Push creates duplicate S3 keys**: When pushing an updated file, the CLI re-encrypted the filename with a random salt each time, generating a new S3 object key. The old key was never deleted, causing `remotely-save` to see multiple objects decrypting to the same filename — it would serve whichever it found first (usually the oldest). Fixed by reusing the cached encrypted filename from the state DB, matching remotely-save's own behavior.

## [0.1.0] - 2026-05-20

### Added

- Initial release: bidirectional sync (push/pull/sync/status) via S3-compatible storage
- Client-side AES-256-CBC encryption compatible with remotely-save (legacy openssl-base64)
- SQLite state tracking for incremental sync
- Filename encryption (AES-256-CBC + base64url)
- Folder marker support (empty files with "/" suffix)
- Dry-run mode, configurable exclude paths
- GitHub Actions CI with multi-platform release builds
