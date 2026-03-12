# GitLab Authentication Setup

GitLab supports both PAT (static) and OAuth2. Goth's `gitlab` provider handles
both GitLab.com and self-hosted instances.

## Option 1: Personal Access Token (Static)

### Steps

1. Go to [GitLab → User Settings → Access Tokens](https://gitlab.com/-/user_settings/personal_access_tokens)
2. Click **"Add new token"**
3. Select scopes: `api`, `read_api`, `read_repository`, `write_repository`
4. Note the token

### Registration

```go
mgr.RegisterStatic(credstore.NewStaticStore(credstore.NewStaticStoreRequest{
    ServiceName: "gitlab",
    Provider:    secretProvider,
    SecretName:  "GITLAB_TOKEN",
}))
```

---

## Option 2: OAuth2 Application (Multi-User)

### Create OAuth Application

1. Go to **User Settings → Applications** (or Admin Area → Applications for instance-wide)
2. Click **"New application"**
3. Fill in:
   - **Name**: `Genie Agent`
   - **Redirect URI**: `https://your-genie-server.com/oauth/callback`
   - **Confidential**: Yes
   - **Scopes**: `api`, `read_user`, `openid`, `email`, `profile`
4. Note the **Application ID** and **Secret**

### Registration (GitLab.com)

```go
import "github.com/markbates/goth/providers/gitlab"

mgr.RegisterOAuth(credstore.NewOAuthStoreRequest{
    ServiceName: "gitlab",
    Provider:    gitlab.New(clientID, clientSecret, callbackURL, "api", "read_user", "openid", "email"),
})
```

### Registration (Self-Hosted)

For self-hosted instances, use `NewCustomisedURL` to point to your instance:

```go
import "github.com/markbates/goth/providers/gitlab"

mgr.RegisterOAuth(credstore.NewOAuthStoreRequest{
    ServiceName: "gitlab",
    Provider: gitlab.NewCustomisedURL(
        clientID, clientSecret, callbackURL,
        "https://git.yourcompany.com/oauth/authorize",
        "https://git.yourcompany.com/oauth/token",
        "https://git.yourcompany.com/api/v4/user",
        "api", "read_user", "openid", "email",
    ),
})
```
