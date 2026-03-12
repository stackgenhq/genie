# Atlassian (Jira / Confluence) OAuth Setup

Atlassian uses OAuth 2.0 (3LO — three-legged OAuth) for Jira and Confluence
Cloud. Goth does not ship a built-in Atlassian provider, so we use the generic
`openidConnect` provider with Atlassian's endpoints.

## Create OAuth 2.0 App

1. Go to [Atlassian Developer Console](https://developer.atlassian.com/console/myapps/)
2. Click **"Create" → "OAuth 2.0 integration"**
3. Fill in **Name**: `Genie Agent`

## Configure Authorization

1. Go to **Authorization** → **"Add"** next to **OAuth 2.0 (3LO)**
2. Set **Callback URL**: `https://your-genie-server.com/oauth/callback`

## Add API Scopes

In **Permissions**, add:

| API | Scope | Description |
|-----|-------|-------------|
| Jira API | `read:jira-work` | Read issues, projects, boards |
| Jira API | `write:jira-work` | Create/update issues |
| Confluence API | `read:confluence-content.all` | Read pages and spaces |
| Confluence API | `write:confluence-content` | Create/update pages |

## Get Credentials

Go to **Settings** and note the **Client ID** and **Secret**.

## Registration

Since goth doesn't have a built-in Atlassian provider, use `openidConnect`
with Atlassian's OAuth endpoints:

```go
import "github.com/markbates/goth/providers/openidConnect"

atlassianProvider, _ := openidConnect.New(
    clientID, clientSecret, callbackURL,
    "https://auth.atlassian.com/.well-known/openid-configuration",
    "read:jira-work", "write:jira-work", "offline_access",
)
atlassianProvider.SetName("atlassian")

mgr.RegisterOAuth(credstore.NewOAuthStoreRequest{
    ServiceName: "atlassian",
    Provider:    atlassianProvider,
})
```

## Important Notes

- Atlassian OAuth tokens expire after **1 hour**
- Include `offline_access` in scopes to receive a refresh token
- Refresh tokens are **rotated** — each refresh returns a new refresh token
- After authentication, call the [Accessible Resources endpoint](https://api.atlassian.com/oauth/token/accessible-resources) to get the `cloudId`
