# MCP (Model Context Protocol) validation

## Why

Genie integrates with external tool servers via the Model Context Protocol. Users need a reliable way to confirm that configured MCP servers (e.g. GitHub, Playwright) are reachable and expose tools before relying on them in chat.

## Problem

Misconfigured or unreachable MCP servers cause silent failures or confusing errors at runtime. There was no built-in way to verify MCP connectivity and tool listing.

## Benefit

- **genie mcp validate** checks each configured MCP server: connect, initialize, list tools.
- Config supports **env** for stdio servers (e.g. `GITHUB_PERSONAL_ACCESS_TOKEN` from `$(gh auth token)`).
- Popular servers (GitHub MCP, Playwright MCP) work with Genie when configured correctly.

## Arrange

- Genie built: `make only-build` (or `go build -o build/genie ./cmd/...`).
- Optional: Node/npx available for stdio servers that use `npx` (e.g. Playwright, GitHub MCP).
- Optional: For GitHub MCP, `gh auth token` works (or set `GITHUB_PERSONAL_ACCESS_TOKEN` / `GH_TOKEN`).

## Act

1. **No MCP servers**  
   Run: `./build/genie mcp validate`  
   (with no `[[mcp.servers]]` in config).

2. **Playwright MCP (no token)**  
   Add to `.genie.toml`:
   ```toml
   [[mcp.servers]]
   name = "playwright"
   transport = "stdio"
   command = "npx"
   args = ["-y", "@playwright/mcp@latest"]
   ```
   Run: `./build/genie mcp validate`

3. **GitHub MCP (with gh token)**  
   Add to `.genie.toml`:
   ```toml
   [[mcp.servers]]
   name = "github"
   transport = "stdio"
   command = "npx"
   args = ["-y", "@anthropic/github-mcp-server"]
   [mcp.servers.env]
   GITHUB_PERSONAL_ACCESS_TOKEN = "${GH_TOKEN}"
   ```
   Run: `GH_TOKEN=$(gh auth token) ./build/genie mcp validate`  
   Or: `GITHUB_PERSONAL_ACCESS_TOKEN=$(gh auth token) ./build/genie mcp validate` (env passed to subprocess when not in config).

## Assert

- With **no** MCP servers: output includes "No MCP servers configured" and exit 0.
- With **Playwright** configured and npx/node available: output shows "✓ playwright: N tool(s)" and "All MCP servers OK."; exit 0.
- With **GitHub** configured and token set: output shows "✓ github: N tool(s)" and "All MCP servers OK."; exit 0.
- If a server fails (e.g. wrong command or missing token): "✗ name: error" and exit non-zero with "validation failed for 1 server(s)".

## References

- `config.toml.example` — MCP section with Playwright, GitHub, and env example.
- `pkg/mcp/README.md` — MCP package docs and configuration options.
- Popular MCP servers: [@playwright/mcp](https://github.com/microsoft/playwright-mcp), [@anthropic/github-mcp-server](https://github.com/modelcontextprotocol/servers/tree/main/src/github).
