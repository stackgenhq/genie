# StackGen MCP with Dynamic Client Registration

This example shows how to use Genie with **OAuth2 Dynamic Client Registration
(RFC 7591)** to connect to the StackGen MCP server over SSE.

## What This Demonstrates

- **Dynamic Client Registration** — Genie registers itself as an OAuth client
  automatically. No pre-configured `client_id` or `client_secret` needed.
- **MCP Authorization Spec** — Follows the [MCP Authorization spec (2025-03-26)](https://modelcontextprotocol.io/specification/2025-03-26/basic/authorization)
- **Per-user tokens** — Each user gets their own token, scoped to their identity
- **PKCE** — Proof Key for Code Exchange for public client security

## How It Works

```
Genie                          StackGen MCP                 Auth Server
  │                                │                            │
  │ GET /.well-known/              │                            │
  │   oauth-protected-resource     │                            │
  │───────────────────────────────▶│                            │
  │ { authorization_servers: [...] }                            │
  │◀───────────────────────────────│                            │
  │                                                             │
  │ GET /.well-known/oauth-authorization-server                 │
  │────────────────────────────────────────────────────────────▶│
  │ { registration_endpoint, authorization_endpoint, ... }      │
  │◀────────────────────────────────────────────────────────────│
  │                                                             │
  │ POST /register  (Dynamic Client Registration)               │
  │   { client_name: "Genie", redirect_uris: [...] }           │
  │────────────────────────────────────────────────────────────▶│
  │   { client_id: "dyn-xxx", client_secret: "..." }           │
  │◀────────────────────────────────────────────────────────────│
  │                                                             │
  │ ── User invokes a StackGen tool ──                          │
  │                                                             │
  │ 🔐 "Please sign in: [click here]"  → User clicks           │
  │                                                             │
  │ GET /authorize?client_id=dyn-xxx&code_challenge=...         │
  │────────────────────────────────────────────────────────────▶│
  │                                     ← browser redirect →    │
  │ GET /oauth/callback?code=xyz&state=abc                      │
  │                                                             │
  │ POST /token  (code exchange with PKCE verifier)             │
  │────────────────────────────────────────────────────────────▶│
  │   { access_token: "...", refresh_token: "..." }             │
  │◀────────────────────────────────────────────────────────────│
  │                                                             │
  │ ── Token stored per-user. All tool calls now authenticated ──
```

## Setup

### 1. Set Environment Variables

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
export GEMINI_API_KEY="..."
export GITHUB_TOKEN="ghp_..."
```

That's it — **no OAuth client credentials needed**. Genie handles DCR
automatically.

### 2. Run Genie

```bash
cd examples/stackgen-oauth
genie
```

### 3. Chat

```
You: Discover my AWS resources in us-east-1

Genie: 🔐 Authentication required for StackGen
       Please sign in: [Click here to connect](https://auth.stackgen.com/authorize?...)

# (click link → authenticate → return to chat)

Genie: ✅ Connected! Starting discovery of your AWS resources in us-east-1...
```

## Go Integration

```go
import "github.com/stackgenhq/genie/pkg/credstore"

mgr := credstore.NewManager(credstore.NewManagerRequest{
    Backend: credstore.NewMemoryBackend(),
})

// No client_id/secret needed — DCR handles it
mgr.RegisterMCPOAuth(credstore.NewMCPOAuthStoreRequest{
    ServiceName: "stackgen",
    Config: credstore.MCPOAuthConfig{
        ServerURL:   "http://poc.cloud.stackgen.com/api/mcp/sse",
        RedirectURI: "https://your-server.com/oauth/callback",
        ClientName:  "Genie Agent",
        Scopes:      []string{"openid", "email", "profile"},
    },
})

// Mount callback handler
http.Handle("/oauth/callback", mgr.CallbackHandler())
```

## Auth Modes Comparison

| Mode | Config | Use Case |
|------|--------|----------|
| `mcp_oauth` | No client creds needed (DCR) | MCP servers following the spec |
| `oauth` | Requires `client_id` + `client_secret` | GitHub, Google, Azure AD via goth |
| `static` | Token from env/secrets | GitHub PAT, API keys |
