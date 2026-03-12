# Azure / Microsoft Entra ID OAuth Setup

Azure uses Microsoft Entra ID (formerly Azure AD) for OAuth 2.0. Goth's
`azureadv2` provider handles this with built-in endpoint configuration.

## Create App Registration

1. Go to [Azure Portal → Microsoft Entra ID → App registrations](https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade)
2. Click **"+ New registration"**
3. Fill in:
   - **Name**: `Genie Agent`
   - **Supported account types**: Single tenant, Multitenant, or + personal accounts
   - **Redirect URI**: Platform: Web, URI: `https://your-genie-server.com/oauth/callback`
4. Note the **Application (client) ID** and **Directory (tenant) ID**

## Create Client Secret

1. Go to **Certificates & secrets** → **"+ New client secret"**
2. Note the **Value** (shown only once)

## API Permissions

Add delegated permissions based on what Genie needs:

| API | Permission | Description |
|-----|-----------|-------------|
| Microsoft Graph | `User.Read` | Sign in and read user profile |
| Microsoft Graph | `Mail.Read` | Read user mail |
| Microsoft Graph | `Calendars.ReadWrite` | Read/write calendars |
| Microsoft Graph | `Files.Read.All` | Read all files user can access |
| Azure DevOps | `vso.code` | Read source code |

## Registration

```go
import "github.com/markbates/goth/providers/azureadv2"

mgr.RegisterOAuth(credstore.NewOAuthStoreRequest{
    ServiceName: "azure",
    Provider: azureadv2.New(
        clientID, clientSecret, callbackURL,
        azureadv2.ProviderOptions{
            Tenant: azureadv2.TenantType(tenantID), // or azureadv2.CommonTenant
            Scopes: []azureadv2.ScopeType{
                "User.Read",
                "Mail.Read",
                "Calendars.ReadWrite",
            },
        },
    ),
})
```

### Tenant Options

| Value | Usage |
|-------|-------|
| `azureadv2.CommonTenant` | Any Azure AD org + personal Microsoft accounts |
| `azureadv2.OrganizationsTenant` | Any Azure AD org (no personal) |
| `azureadv2.ConsumersTenant` | Personal Microsoft accounts only |
| `azureadv2.TenantType("your-tenant-id")` | Single tenant |

## Azure DevOps (PAT Alternative)

For Azure DevOps specifically, a PAT can be used as a static token:

1. Go to [dev.azure.com → User Settings → Personal Access Tokens](https://dev.azure.com/_usersSettings/tokens)
2. Select scopes (e.g., Code: Read & Write)

```go
mgr.RegisterStatic(credstore.NewStaticStore(credstore.NewStaticStoreRequest{
    ServiceName: "azdo",
    Provider:    secretProvider,
    SecretName:  "AZDO_PAT",
}))
```
