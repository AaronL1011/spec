# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |
| < latest | :x:               |

We only provide security updates for the latest release. Users are encouraged to upgrade to the latest version.

## Reporting a Vulnerability

If you discover a security vulnerability in spec-cli, please report it responsibly:

1. **Do not** open a public GitHub issue for security vulnerabilities
2. Email the maintainers directly or use [GitHub's private vulnerability reporting](https://github.com/aaronl1011/spec-cli/security/advisories/new)
3. Include:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
   - Any suggested fixes (optional)

We aim to respond within 48 hours and will work with you to understand and address the issue.

## Security Measures

### Build Security
- All releases are built with `CGO_ENABLED=0` for static binaries
- Binaries are built with `-trimpath` to remove local paths
- SHA256 checksums are provided for all release artifacts
- GitHub Actions workflows use pinned action versions (SHA hashes)

### Dependency Management
- Dependencies are automatically reviewed via Dependabot
- PRs undergo dependency review to catch known vulnerabilities
- We avoid dependencies with known security issues

### Verification

Verify downloaded binaries against the checksums:

```bash
# Download checksums file
curl -LO https://github.com/aaronl1011/spec-cli/releases/download/vX.Y.Z/checksums.txt

# Verify your download
sha256sum -c checksums.txt --ignore-missing
```

## Security Best Practices for Users

1. **Protect your tokens**: Store `GITHUB_TOKEN` and `AI_API_KEY` as environment variables or in a secrets manager, never in config files committed to git
2. **Review before running**: The `spec build` command spawns coding agents — review the context being provided
3. **Keep updated**: Run `brew upgrade spec` or re-download the latest release regularly
