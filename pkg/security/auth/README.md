# Authentication and OIDC Setup

The `pkg/security/auth` package provides generic OIDC (OpenID Connect) authentication for Genie. It handles browser login, session management via signed cookies, and integrates with standard OIDC providers.

## Configuration

To enable OIDC, you need to configure the application with the following parameters:

* `IssuerURL`: The discovery endpoint for your OIDC provider (e.g., `https://accounts.google.com`).
* `ClientID`: Your application's OAuth client ID.
* `ClientSecret`: Your application's OAuth client secret.
* `RedirectURL`: (Optional) The explicit redirect URL. If omitted, it automatically detects it based on the incoming request protocol and host.
* `CookieSecret`: A secret used to sign the session cookies. If not provided via configuration, it can be set via the `AGUI_COOKIE_SECRET` environment variable. If missing entirely, a random ephemeral secret is generated on startup (meaning sessions will invalidate on restarts).
* `AllowedDomains`: An optional list of allowed domains or email addresses to restrict who can log in.

### Example Configuration

```yaml
oidc:
  enabled: true
  issuer_url: "https://accounts.google.com"
  client_id: "your-client-id.apps.googleusercontent.com"
  client_secret: "your-client-secret"
  allowed_domains:
    - "stackgen.com"
```

## Provider Setup Instructions

### 1. Google (Google Workspace / GCP)

Google is a natively supported OIDC provider.

1. Go to the [Google Cloud Console](https://console.cloud.google.com/).
2. Navigate to **APIs & Services** -> **Credentials**.
3. Click **Create Credentials** -> **OAuth client ID**.
4. Set the Application type to **Web application**.
5. Give the application a name (e.g., "Genie").
6. Under **Authorized redirect URIs**, add your application's Callback URL (see [Redirect URLs](#redirect-urls)).
7. Copy the generated **Client ID** and **Client Secret** into your Genie configuration.
8. Set the `IssuerURL` in your configuration to `https://accounts.google.com`.

### 2. GitHub (Via Dex or similar Identity Broker)

*Note: The `coreos/go-oidc` library strictly requires OpenID Connect discovery (`/.well-known/openid-configuration`), which GitHub's native OAuth2 does not provide. To use GitHub authentication with this package, you must use an identity broker like [Dex](https://github.com/dexidp/dex).*

1. Deploy and configure Dex with the GitHub connector.
2. Register an OAuth application in GitHub:
    * Go to **Developer Settings** -> **OAuth Apps** -> **New OAuth App**.
    * Set the callback URL to your Dex instance's callback endpoint.
3. In Genie, configure OIDC to point at your Dex instance:
    * `IssuerURL`: URL of your Dex instance (e.g., `https://dex.yourdomain.com`).
    * `ClientID` / `ClientSecret`: The static credentials you configured for Genie inside Dex.

## Redirect URLs

When configuring your OAuth application in the provider console (Google, Dex, Okta, etc.), you must whitelist the **Authorized redirect URIs**.

The redirect URL for Genie follows this format:

```text
https://<your-host-or-domain>/auth/callback
```

**Examples:**
* **Local Development:** `http://localhost:8080/auth/callback`
* **Production Deployment:** `https://genie.stackgen.com/auth/callback`

If you haven't explicitly set the `RedirectURL` config variable, Genie automatically determines the host and protocol from the incoming HTTP request (using headers like `X-Forwarded-Proto` for HTTPS detection behind proxies).

## Authentication Flow Endpoints

The auth package implements the following endpoints:

* `GET /auth/login` — Redirects the user to the OIDC provider's consent screen.
* `GET /auth/callback` — Handles the callback, exchanges the code for tokens, verifies claims, and sets the `genie_session` cookie.
* `GET /auth/logout` — Clears the `genie_session` cookie.
* `GET /auth/info` — Returns the current user's authenticated session state in JSON format.
