# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Context-mode middleware (`mw_contextmode`) — local BM25-based compression for large tool outputs, runs before LLM summarisation to reduce cost and latency
- `GetSecretRequest.Reason` field — carries the LLM's justification for why a secret is needed
- Justification propagation via `toolcontext.WithJustification`/`GetJustification` context helpers
- `justification.go` — extracts `_justification` from tool call JSON args and threads it through context
- `toolcontext` package for cross-package context value access (justification, tool name)
- `MCPCaller` interface with counterfeiter fake for unit-testing `ClientTool.Call` without a real MCP server
- Comprehensive unit tests for MCP package: `ClientTool.Call`, `shouldIncludeTool`, `buildStdioEnv`, `expandEnvValue`
- Arrange-Act-Assert (AAA) mandatory testing rule in `Agents.md`

### Changed

- **Breaking:** `SecretProvider.GetSecret` signature changed from `(ctx, string)` to `(ctx, GetSecretRequest)` — all call sites migrated
- Context-mode middleware enabled by default (`ContextModeConfig.Disabled` field; set to `true` to opt out)
- Replaced hand-rolled test fakes with counterfeiter-generated `securityfakes.FakeSecretProvider` across `config`, `langfuse`, and `modelprovider` test suites
- Removed redundant tests in `model_test.go` and `security_test.go`

### Fixed

- `extractJustification` now handles `sjson.DeleteBytes` errors (falls back to original args) and returns a `found` bool so callers distinguish missing vs empty `_justification`
- Secret audit logging gated on non-empty resolved values — no longer fires audit events for missing/unconfigured secrets
- `chunkText` now splits on word boundaries when a sentence exceeds `targetSize`, matching its documented behaviour
- `scoreChunks` precomputes lowercase chunks to avoid redundant `strings.ToLower` calls in BM25 scoring loops
- Google web search `GetSecretRequest.Reason` populated for CredentialsFile lookup
- `MiddlewareConfig` comment updated to mention `Disabled` flag alongside `Enabled`
- `parseSecretName` slice-bounds panic in `configbased_provider.go` when query string is present

## [0.1.3] - 2026-02-27

### Added

- Signed binaries for release artifacts
- OpenSSF Best Practices compliance: `SECURITY.md`, `CODE_OF_CONDUCT.md`, `CHANGELOG.md`
- GitHub Issue Templates for bug reports and feature requests
- Dependabot configuration for automated dependency updates
- CodeQL static analysis workflow
- Testing policy documented in `CONTRIBUTING.md`

### Changed

- Updated `README.md` with links to community governance files

### Security

- Added CodeQL analysis to CI pipeline for every PR
- Added Dependabot for Go modules and GitHub Actions

## [0.1.1] - 2026-02-27

### Changed

- Enforce strict crypto only: removed `DisableWeakAlgorithms` option

### Security

- Added Scorecard workflow for supply-chain security
- Raspberry Pi builds deferred

## [0.1.0] - 2026-02-27

### Added

- Initial public release — "Hello World"

## [0.0.13] - 2026-02-27

### Added

- Interface-driven knowledge graph with in-memory store
- `genie doctor` command for config validation
- MCP secret lookup support
- Audit secret lookups for Manager and keyring
- Setup wizard: ask user to opt in to auto-approve read-only tools
- Scoped keyring by config path (multi-instance support)
- Unified data platform
- Feedback form
- TTL cache for HITL approvals
- Milvus vector store support

### Changed

- AG-UI chat: prevent duplicate bubble and hide front-desk messages
- Per-agent circuit breaker scoping for policy isolation

### Fixed

- Chat UI: scroll propagation, fullscreen background, and duplicate messages

[Unreleased]: https://github.com/stackgenhq/genie/compare/v0.1.3...HEAD
[0.1.3]: https://github.com/stackgenhq/genie/compare/v0.1.1...v0.1.3
[0.1.1]: https://github.com/stackgenhq/genie/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/stackgenhq/genie/releases/tag/v0.1.0
