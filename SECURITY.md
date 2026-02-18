# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest  | Yes       |

## Reporting a Vulnerability

If you discover a security vulnerability in Glint, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, use [GitHub's private vulnerability reporting](https://github.com/darshan-rambhia/glint/security/advisories/new) to submit a report. This keeps the conversation private between you and the maintainers until a fix is ready.

You should receive an acknowledgement within 48 hours. Once the issue is confirmed, a fix will be developed privately and released as a patch version.

## Scope

Glint connects to Proxmox VE and PBS APIs using API tokens. Security concerns include but are not limited to:

- Credential exposure (API tokens, webhook URLs)
- Unauthorized access to the Glint dashboard
- Injection vulnerabilities in the web UI
- Path traversal or file access issues
- SQLite database tampering
