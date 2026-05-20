# obsidian-remotely-sync-cli

[![CI](https://github.com/<user>/obsidian-remotely-sync-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/<user>/obsidian-remotely-sync-cli/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/<user>/obsidian-remotely-sync-cli)](https://goreportcard.com/report/github.com/<user>/obsidian-remotely-sync-cli)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A command-line tool for syncing Obsidian vaults via S3-compatible object storage, with client-side encryption compatible with [remotely-save](https://github.com/remotely-save/remotely-save) (legacy OpenSSL format).

Built as a lightweight CLI alternative to the remotely-save Obsidian plugin — ideal for headless servers, CI/CD, and automated backups.

## Features

- **Bidirectional sync** — push, pull, and sync between local vault and S3
- **Client-side encryption** — AES-256-CBC (OpenSSL format), data-at-rest encrypted in S3
- **remotely-save compatible** — reads/writes vaults encrypted by remotely-save (legacy `openssl-base64` format)
- **S3-compatible** — works with AWS S3, Tencent COS, MinIO, Cloudflare R2, and others
- **SQLite state tracking** — incremental sync, no full re-scan on every run
- **Dry-run mode** — preview changes before applying
- **Single binary** — no runtime dependencies, cross-platform

## Encryption Compatibility

This tool supports the **legacy OpenSSL-base64** encryption format used by remotely-save:

| Feature | Supported |
|---------|-----------|
| openssl-base64 (AES-256-CBC) | ✅ Full support |
| Legacy filename encoding (base64url) | ✅ Full support |
| Folders-as-empty-files | ✅ Full support |
| XChaCha20-Poly1305 (new format) | ❌ Not yet supported |

> **Note:** remotely-save versions before `0.5.0` default to the legacy `openssl-base64` format. If your vault was encrypted with remotely-save `v0.5.0` or later, it may use the newer XChaCha20 format which is not yet supported. Check your `data.json` for `"type": "openssl-base64"` to confirm.

## Install

### Binary release (recommended)

Download from [Releases](https://github.com/<user>/obsidian-remotely-sync-cli/releases/latest) page. Available for:

- Linux (amd64, arm64)
- macOS (amd64, arm64)
- Windows (amd64)

```bash
# Linux amd64
curl -LO https://github.com/<user>/obsidian-remotely-sync-cli/releases/latest/download/obsidian-remotely-sync-cli-linux-amd64.tar.gz
tar xzf obsidian-remotely-sync-cli-linux-amd64.tar.gz
sudo mv obsidian-remotely-sync-cli /usr/local/bin/
```

### Build from source

```bash
git clone https://github.com/<user>/obsidian-remotely-sync-cli.git
cd obsidian-remotely-sync-cli
make build
```

### go install

```bash
go install github.com/<user>/obsidian-remotely-sync-cli@latest
```

## Quick Start

### 1. Create config file

```bash
mkdir -p ~/.obsidian-remotely-sync-cli
cat > ~/.obsidian-remotely-sync-cli/config.yaml << EOF
vault_path: /path/to/your/obsidian/vault

s3:
  endpoint: https://cos.ap-guangzhou.myqcloud.com
  region: ap-guangzhou
  bucket: your-bucket-name
  access_key_id: YOUR_ACCESS_KEY
  secret_access_key: YOUR_SECRET_KEY
  use_path_style: false

encryption:
  enabled: true
  password: "your-strong-password"
EOF
chmod 600 ~/.obsidian-remotely-sync-cli/config.yaml
```

> For Tencent COS, the endpoint format is `https://cos.<region>.myqcloud.com`. Set `use_path_style: false`.

### 2. Run sync

```bash
# Preview changes (dry run)
obsidian-remotely-sync-cli --dry-run sync

# Full sync
obsidian-remotely-sync-cli sync

# Pull only (download from S3)
obsidian-remotely-sync-cli pull

# Push only (upload to S3)
obsidian-remotely-sync-cli push
```

### 3. Set up cron

```bash
# Sync every 30 minutes
*/30 * * * * /usr/local/bin/obsidian-remotely-sync-cli -log-level warn sync >> /var/log/obsidian-sync.log 2>&1
```

## Usage

```
obsidian-remotely-sync-cli — encrypted Obsidian vault sync via S3

Usage:
  obsidian-remotely-sync-cli [flags] <command>

Commands:
  init      Create a default config file at the config path
  sync      Bidirectional sync between vault and S3
  push      Upload local changes to S3
  pull      Download remote changes to local vault
  status    Show pending sync actions without making changes

Flags:
  -config string    Path to config file (default: ~/.obsidian-remotely-sync-cli/config.yaml)
  -dry-run          Preview changes without applying
  -log-level string Log level: debug, info, warn, error (default: info)
  -state string     Path to state database (default: ~/.obsidian-remotely-sync-cli/state.db)
```

## How It Works

### Sync Process

1. **List remote** — scan all objects in the S3 bucket prefix
2. **Decrypt** — decrypt filenames and compute content hashes
3. **Compare** — diff against local vault and SQLite state (last-known hashes)
4. **Apply** — push/pull based on modification times and conflicts

### Encryption Details

**Data encryption** (AES-256-CBC, OpenSSL-compatible):
```
Password + Salt → PBKDF2 (10000 iterations, SHA-256) → Key + IV
Plaintext → AES-256-CBC encrypt → "Salted__" + 8-byte salt + ciphertext
```

**Filename encryption**:
```
Original name → AES-256-ECB encrypt → Base64URL encode → "/" suffix for folders
```

### State Management

Sync state is stored in a local SQLite database (`state.db`):
- Remote object ETags and encrypted names
- Local file paths and modification times
- Maps between encrypted ↔ decrypted names

Deleting the state file will trigger a full re-scan on the next sync.

## Migration from remotely-save

1. Copy your remotely-save `data.json` to get the encryption password
2. Create `~/.obsidian-remotely-sync-cli/config.yaml` with:
   - Same S3 credentials and bucket/prefix
   - Password from `data.json` → `encryption.password`
3. Run `obsidian-remotely-sync-cli --dry-run pull` to verify
4. Run `obsidian-remotely-sync-cli pull` to download

## Build & Release

```bash
# Build for current platform
make build

# Build for all platforms
make build-all

# Run tests
make test

# Create a release (requires git tag)
make tag TAG=v1.0.0
# Then GitHub Actions will build and upload binaries automatically
```

## Security

- Encryption password is read from config file (never logged)
- Config file should be `chmod 600`
- State database contains only hashes and metadata, no plaintext content
- For security issues, please see [SECURITY.md](SECURITY.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

[MIT](LICENSE)
