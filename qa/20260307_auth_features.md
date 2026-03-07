# QA: Testing Authentication Features

The following scenarios describe how to manually test the newly released API Keys and OIDC integrations for AG-UI and Genie.

## 1. API Keys Authentication

API Keys allow headless machine-to-machine integrations (like build scripts, scheduled cron jobs, etc) without completing an interactive OAuth flow.

### Setup
1. Define static API keys in your configuration document (e.g. `genie.toml`). Keep it disabled for password auth to solely test this.
```toml
[messenger.agui.auth.password]
enabled = false

[messenger.agui.auth.api_keys]
keys = ["secret-token-1234", "bot-token-abcd"]
```

2. Run the genie interactive server.
```bash
./bin/genie serve
```

### Execution
1. Send a cURL request *without* the key and verify you receive a 401 Unauthorized result.
```bash
curl -i -H "Content-Type: application/json" -X POST http://localhost:9876/ -d '{"messages":[{"role":"user","content":"Hello"}]}'
# Expect HTTP/1.1 401 Unauthorized
```

2. Send a request using the conventional `Authorization: Bearer <key>` header:
```bash
curl -i -H "Content-Type: application/json" -H "Authorization: Bearer secret-token-1234" -X POST http://localhost:9876/ -d '{"messages":[{"role":"user","content":"Hello"}]}'
# Expect HTTP/1.1 200 OK with server event stream output.
```

3. Send a request using the custom `X-API-Key` header:
```bash
curl -i -H "Content-Type: application/json" -H "X-API-Key: bot-token-abcd" -X POST http://localhost:9876/ -d '{"messages":[{"role":"user","content":"Hello"}]}'
# Expect HTTP/1.1 200 OK
```

## 2. OIDC (OpenID Connect) Authentication

This updates the former "Login with Google" OAuth hardcoding to a generic OIDC client that leverages the standard `.well-known/openid-configuration` discovery endpoint, allowing providers like Okta, Auth0, Dex, Azure AD, etc.

Since a full provider isn't available out of the box in the test environment, you can test it directly with a generic setup.

### Setup
1. Retrieve standard OAuth Client IDs and Secrets (e.g., from an Auth0 Tenant, Okta dev account, or standard Google Workspace). Have a domain ready to restrict against.

```toml
[messenger.agui.auth.password]
enabled = false

[messenger.agui.auth.oidc]
issuer_url = "https://accounts.google.com" # Or your Okta domain: https://dev-12345.okta.com/oauth2/default
client_id = "YOUR_APP_CLIENT_ID"
client_secret = "YOUR_APP_SECRET"
allowed_domains = ["yourcompany.com"]
redirect_url = "http://localhost:9876/auth/callback"
```

### Execution
1. Send a request without authentication to the base `http://localhost:9876/` using curl over HTTP.
```bash
curl -i -X POST http://localhost:9876/
```
You should observe a 401 Unauthorized response that includes:
```json
{
  "error": "auth_required",
  "message": "Authentication required",
  "login_url": "/auth/login",
  "oauth_enabled": true
}
```

2. Visit `http://localhost:9876/auth/login` in a Browser.
3. You should be redirected completely standard to the OIDC identity provider you initialized.
4. Completing the login challenge redirects you to the `/auth/callback` which validates the returned challenge and stores `genie_session` within cookies.

## 3. Options Header Preflight CORS Avoidance

When performing requests via Front End Apps across differing origin boundaries, CORS sends a pre-flight `OPTIONS` network request. If this is gated by Authorization, normal requests can fail to complete the browser's workflow.

### Setup
Run the server configured with Password auth (default security enforcement).

### Execution
Send an OPTIONS request to the server endpoints.

```bash
curl -i -X OPTIONS http://localhost:9876/ \
     -H "Origin: http://another-domain.com" \
     -H "Access-Control-Request-Method: POST" \
     -H "Access-Control-Request-Headers: Authorization"
```

The response should be 200 OK (passed auth middleware successfully and fulfilled natively by `corsMiddleware`) rather than the expected `401 Unauthorized` that unauthenticated POST requests get.
