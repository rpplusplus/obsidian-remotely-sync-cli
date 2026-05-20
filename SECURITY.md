# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |

## Reporting a Vulnerability

If you discover a security vulnerability, please report it privately:

1. **Do NOT** open a public GitHub issue
2. Email the maintainers directly
3. Include a description of the vulnerability and steps to reproduce

We will respond within 7 days and work with you to address the issue.

## Security Considerations

- **Config file permissions**: `~/.obsidian-remotely-sync-cli/config.yaml` contains your S3 credentials and encryption password. The `init` command creates it with `0600` permissions (owner read/write only). Do not change this.
- **State database**: `~/.obsidian-remotely-sync-cli/state.db` contains file metadata but no credentials or content.
- **Encryption**: obsidian-remotely-sync-cli uses OpenSSL-compatible AES-256-CBC encryption. The encryption password is never stored in the state database.
- **S3 bucket**: Ensure your S3 bucket has appropriate access controls. The bucket should not be publicly accessible.
