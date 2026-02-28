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

1. **GitHub Security Advisories (preferred):** Use the [Report a vulnerability](https://github.com/stackgenhq/genie/security/advisories/new) button on the Security tab.
2. **Email:** Send details to **<opensource@stackgen.com>**.

### What to include

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

### What to expect

- **Initial response** within 14 days (we aim to acknowledge within 48 hours)
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

## Delivery (MITM resistance)

The project is delivered via mechanisms that counter man-in-the-middle (MITM) attacks:

- **Source and binaries:** Git clone, Go install, and binary downloads use **HTTPS** (e.g. `https://github.com/stackgenhq/genie`, GitHub Releases, Go module proxy).
- **Containers:** Docker images are served from **HTTPS** (e.g. `ghcr.io/stackgenhq/genie`).
- **Package managers:** Homebrew and Scoop use **HTTPS** for taps and packages.

We do not distribute the project or release artifacts over plain HTTP.

- **OAuth2 callback:** The Google OAuth2 sign-in flow uses `http://localhost:8765` as the redirect URI. This is standard for desktop/installed applications: the redirect is to the user's own machine, the callback server binds only to localhost, and the authorization code is not sent over the network to any third party. See `pkg/tools/google/oauth/browser_flow.go`.

## Cryptographic Practices

- **Perfect forward secrecy (PFS):** TLS used by the project (HTTP clients, IMAP, and other TLS connections) is configured with cipher suites that use ephemeral key agreement (ECDHE). Session keys derived from these exchanges are not compromised if a long-term private key is later compromised. See `pkg/security/crypto.go` (`TLSConfig` and `defaultSecureCipherSuites`).

## Security Best Practices for Users

- **Never commit API keys** — use `${VAR}` placeholders with environment variables or `[security.secrets]` with runtimevar backends
- **Restrict CORS origins** in production — override the default `*` with specific origins
- **Enable HITL** for destructive tools in production environments
- **Review tool permissions** — use `denied_tools` and `always_allowed` in `[hitl]` config
