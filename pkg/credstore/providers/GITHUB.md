# GitHub OAuth Setup

GitHub supports two authentication modes with `credstore`:

## Option 1: Static Token (Personal Access Token)

The simplest approach — user creates a PAT and provides it in config.

### Steps

1. Go to [GitHub Settings → Developer settings → Personal access tokens](https://github.com/settings/tokens)
2. Click **"Generate new token (classic)"** or **"Fine-grained token"**
3. Select scopes: `repo`, `read:org`, `workflow` (as needed)
4. Copy the token

### Registration

```go
mgr.RegisterStatic(credstore.NewStaticStore(credstore.NewStaticStoreRequest{
    ServiceName: "github",
    Provider:    secretProvider,
    SecretName:  "GH_TOKEN", // resolved from env/secrets manager
}))
```

---

## Option 2: OAuth2 App (Multi-User / Remote Genie)

For remote Genie serving multiple users, each user authenticates via GitHub OAuth.

### Create GitHub OAuth App

1. Go to [GitHub Settings → Developer settings → OAuth Apps](https://github.com/settings/developers)
2. Click **"New OAuth App"**
3. Fill in:
   - **Application name**: `Genie Agent`
   - **Homepage URL**: `https://your-genie-server.com`
   - **Authorization callback URL**: `https://your-genie-server.com/oauth/callback`
4. Click **"Register application"**
5. Note the **Client ID** and generate a **Client Secret**

### Scopes Reference

| Scope | Description |
|-------|-------------|
| `repo` | Full repository access (read/write) |
| `read:org` | Read org and team membership |
| `read:user` | Read user profile data |
| `user:email` | Access email addresses |
| `workflow` | Update GitHub Actions workflows |

### Registration

```go
import "github.com/markbates/goth/providers/github"

mgr.RegisterOAuth(credstore.NewOAuthStoreRequest{
    ServiceName: "github",
    Provider:    github.New(clientID, clientSecret, callbackURL, "repo", "read:org", "read:user"),
})
```

### GitHub App (Alternative)

For fine-grained permissions, use a [GitHub App](https://docs.github.com/en/apps/creating-github-apps):

1. Go to [GitHub Settings → Developer settings → GitHub Apps](https://github.com/settings/apps)
2. Set **Callback URL** to `https://your-genie-server.com/oauth/callback`
3. Enable **"Request user authorization (OAuth) during installation"**
4. Note the **Client ID** and generate a **Client secret**

The goth registration is identical — same `github.New(...)` constructor.
