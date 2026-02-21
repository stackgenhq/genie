# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | ✅ |
| Previous minor | ⚠️ Critical fixes only |
| Older | ❌ |

## Reporting a Vulnerability

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please report vulnerabilities privately:

1. **GitHub Security Advisories (preferred):** Use the [Report a vulnerability](https://github.com/appcd-dev/stackgen-genie/security/advisories/new) button on the Security tab.
2. **Email:** Send details to **security@stackgen.com**.

### What to include

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

### What to expect

- **Acknowledgment** within 48 hours
- **Assessment** within 5 business days
- **Fix timeline** communicated once severity is determined
- **Credit** in the release notes (unless you prefer anonymity)

## Scope

The following are in scope for security reports:

- Authentication/authorization bypasses
- Secret leakage (API keys, tokens in logs, error messages, or config)
- Remote code execution
- Injection vulnerabilities (SQL, command, template)
- Denial of service via resource exhaustion
- CORS misconfigurations allowing credential theft

## Security Best Practices for Users

- **Never commit API keys** — use `${VAR}` placeholders with environment variables or `[security.secrets]` with runtimevar backends
- **Restrict CORS origins** in production — override the default `*` with specific origins
- **Enable HITL** for destructive tools in production environments
- **Review tool permissions** — use `denied_tools` and `always_allowed` in `[hitl]` config
