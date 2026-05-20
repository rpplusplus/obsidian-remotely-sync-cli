# Contributing

Thanks for your interest in contributing to obsidian-remotely-sync-cli!

## Development Setup

```bash
# Clone
git clone https://github.com/rpplusplus/obsidian-remotely-sync-cli.git
cd obsidian-remotely-sync-cli

# Build
make build

# Run tests
make test

# Lint
make lint
```

Requires Go 1.21+. For users in China, set the Go proxy:

```bash
go env -w GOPROXY=https://goproxy.cn,direct
```

## Project Structure

```
obsidian-remotely-sync-cli/
├── main.go              # CLI entry point
├── helpers.go           # Shared helpers (S3 connection, etc.)
├── config/config.go     # YAML config loading + validation
├── crypto/openssl.go    # OpenSSL-compatible AES-256-CBC encrypt/decrypt
├── s3/client.go         # S3/COS client wrapper
├── sync/engine.go       # Sync engine with SQLite state tracking
└── *_test.go            # Unit tests
```

## Architecture

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│  Local Vault │────▶│  Sync Engine │◀────│  S3 / COS   │
│  (filesystem)│     │  (diff + state)│     │  (encrypted)│
└─────────────┘     └──────────────┘     └─────────────┘
                          │
                    ┌─────┴─────┐
                    │  SQLite DB │  (sync state)
                    └───────────┘
```

## Adding a New Storage Backend

1. Create `s3/yourbackend.go` implementing the same interface as `s3/client.go`
2. Add config fields in `config/config.go`
3. Update `helpers.go` to instantiate the new client
4. Add tests

## Commit Convention

```
type: concise description

Optional body.
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`

## Pull Requests

1. Fork the repo
2. Create a feature branch
3. Add tests for new functionality
4. Ensure `make test` passes
5. Open a PR with a clear description

## Security

If you find a security vulnerability, please report it privately via email
rather than opening a public issue.
