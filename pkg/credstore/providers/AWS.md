# AWS Authentication Setup

AWS supports multiple authentication modes for MCP tools.

## Option 1: Static Access Keys (Simple)

For single-user or dev environments with IAM user credentials.

### Steps

1. Go to [IAM Console → Users](https://console.aws.amazon.com/iam/home#/users)
2. Select your user → **Security credentials** tab
3. Click **"Create access key"** → Select **"Third-party service"**
4. Note the **Access Key ID** and **Secret Access Key**

### Registration

Static AWS keys are typically injected as environment variables for the MCP
server process, not through the credstore OAuth flow:

```go
// In MCP server config (genie.toml)
[[mcp.servers]]
name = "aws"
transport = "stdio"
command = "npx"
args = ["-y", "@modelcontextprotocol/server-aws"]
[mcp.servers.env]
AWS_ACCESS_KEY_ID = "${AWS_ACCESS_KEY_ID}"
AWS_SECRET_ACCESS_KEY = "${AWS_SECRET_ACCESS_KEY}"
AWS_REGION = "us-east-1"
```

> **⚠️ Security**: Static keys have no expiry. Prefer Amazon Cognito
> (Option 2) for production and multi-user deployments.

---

## Option 2: Amazon Cognito (OAuth2 — Multi-User)

For remote Genie. Users sign in via Cognito-hosted UI, which can federate
to corporate identity providers (Okta, Azure AD, etc.). Goth has a
built-in `amazon` provider.

### Create Cognito User Pool

1. Go to [Cognito Console](https://console.aws.amazon.com/cognito/v2/idp/user-pools)
2. **Create a User Pool** with your preferred settings
3. **Create an App Client**:
   - Select **"Public client"** or **"Confidential client"**
   - Set **Callback URL**: `https://your-genie-server.com/oauth/callback`
   - Enable **Authorization code grant**
   - Select scopes: `openid`, `email`, `profile`
4. Note the **Client ID**, **Client Secret**, and **Cognito Domain**

### Registration

```go
import "github.com/markbates/goth/providers/amazon"

mgr.RegisterOAuth(credstore.NewOAuthStoreRequest{
    ServiceName: "aws",
    Provider:    amazon.New(clientID, clientSecret, callbackURL, "profile"),
})
```

> **Note**: The `amazon` provider uses Login with Amazon endpoints. For
> Cognito-specific pools, you may also use goth's `openidConnect` provider
> with your Cognito domain's OIDC discovery URL:
> `https://cognito-idp.{region}.amazonaws.com/{poolId}/.well-known/openid-configuration`
