# Credstore Provider Setup Guide

This directory contains guides for setting up OAuth clients and credentials
for each supported service provider. These are used with the `credstore`
package to enable per-user authentication for MCP tools and third-party
integrations.

## Supported Auth Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| **Static Token** | User provides a PAT/API key via `SecretProvider` | GitHub PAT, Slack bot token, generic API keys |
| **OAuth2 (goth)** | Browser-based login via chat link using [markbates/goth](https://github.com/markbates/goth) | GitHub Apps, Google, Azure AD, AWS Cognito, GitLab, and 40+ more |

## Registration Examples

### Static Token

```go
mgr.RegisterStatic(credstore.NewStaticStore(credstore.NewStaticStoreRequest{
    ServiceName: "github",
    Provider:    secretProvider,
    SecretName:  "GITHUB_TOKEN",
}))
```

### OAuth2 (goth provider)

```go
import "github.com/markbates/goth/providers/github"

mgr.RegisterOAuth(credstore.NewOAuthStoreRequest{
    ServiceName: "github",
    Provider:    github.New(clientID, clientSecret, callbackURL, "repo", "read:org"),
})
```

## Provider Guides

- [GitHub](GITHUB.md) — PAT or GitHub App OAuth
- [Google Cloud / Workspace](GOOGLE.md) — OAuth2 for Calendar, Drive, Gmail, GCP APIs
- [AWS](AWS.md) — Static access keys or Amazon Cognito OAuth
- [Azure / Microsoft](AZURE.md) — Azure AD (Entra ID) OAuth2
- [Atlassian (Jira/Confluence)](ATLASSIAN.md) — OAuth 2.0 (3LO)
- [GitLab](GITLAB.md) — PAT or OAuth2

## All Available Goth Providers

Goth ships with 40+ pre-configured providers. Pass any `goth.Provider` to
`NewOAuthStoreRequest.Provider`. See the full list:
https://github.com/markbates/goth#supported-providers
