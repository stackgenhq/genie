# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Mid-run feedback injection — users can send asynchronous messages to the agent while it is processing via `POST /api/v1/inject`; feedback is stored in working memory with an `INTERRUPT` prefix
- Feedback input UI in chat.js (textarea + send button) — shown/hidden during active agent processing
- Caching for summarization middleware to avoid redundant LLM calls on repeated tool outputs
- Telemetry for prompt and completion tokens in expert metrics

### Fixed

- Sub-agent output extraction to properly capture `ObjectTypeToolResponse` events (essential for AWS CLI outputs)
- Orchestrator re-plan loops by de-duplicating sub-agent calls using exact working memory matching
- PII redaction logic to strip prefixes correctly when filtering messages
- Sub-agent output truncation limit increased from 4,000 to 16,000 characters to better accommodate large JSON dumps

## [0.1.6-rc.1] - 2026-03-02

### Added

- Context-based agent identity — `agent_name` config field honored end-to-end (system prompt, chat UI header, `/health` endpoint, audit log path)
- `persona_file` config option — load project-level coding standards from a custom file instead of the default `Agents.md`
- `/health` endpoint now returns `agent_name` in JSON response
- `orchestratorcontext` package for injecting agent identity into request context
- Chat UI dynamically displays the configured agent name

### Changed

- Default AG-UI port changed from `8080` to `9876`
- Default CORS origins updated from `["*"]` to `["https://stackgenhq.github.io"]`
- Docs proxy fixed: prevent double-prefix (`/genie/genie/...`) by capturing request path before director modifies it

### Removed

- `pkg/runbook` package (8 files) — runbook content is now discoverable via the skills system (`skills_roots`)
- `[runbook]` config section removed; migration path: move runbook directories into `skills_roots`
- `search_runbook` removed from default HITL auto-approve list
- Runbook UI removed from Config Builder

### Fixed

- HITL approval card readability — widened column, increased font sizes, light yellow theme for better accessibility
- XSS fix in `addToolCard` — escape `toolCallId` and friendly name in innerHTML via `safeDomId()`
- Removed unused `isReasoning` parameter from `appendToAssistantBubble()`
- MCP: added JSON struct tags and call `SetDefaults` before `Validate` (#8)
- Chat UI URL now shown in startup banner

### Security

- [StepSecurity] applied security best practices (#7)

### Dependencies

- Bumped `trpc-agent-go/model/anthropic`, `model/ollama`, `tool/google`, `embedder/huggingface`, `embedder/gemini`, `vectorstore/milvus`
- Bumped `github.com/mark3labs/mcp-go` from 0.44.0 to 0.44.1
- Bumped `gocloud.dev` from 0.44.0 to 0.45.0
- Bumped `google.golang.org/grpc` from 1.77.0 to 1.79.1
- Bumped `actions/checkout` from 4.2.2 to 6.0.2
- Bumped `ossf/scorecard-action` from 2.4.1 to 2.4.3
- Bumped `actions/attest-build-provenance` from 3.0.0 to 4.1.0
- Bumped `github/codeql-action` from 4.32.4 to 4.32.5
- Bumped `actions/upload-artifact` from 4.6.1 to 7.0.0

## [0.1.5] - 2026-03-01

### Added

- Configurable token tailoring per model provider (`enable_token_tailoring` field) — conversation history trimmed to model context window for efficiency (based on [arXiv:2601.14192](https://arxiv.org/abs/2601.14192))
- Token tailoring support wired for all providers: OpenAI, Gemini, Anthropic, Ollama, HuggingFace

### Security

- [StepSecurity] applied security best practices (#7)

## [0.1.4] - 2026-03-01

### Added

- Context-mode middleware (`mw_contextmode`) — local BM25-based compression for large tool outputs, runs before LLM summarisation to reduce cost and latency
- `GetSecretRequest.Reason` field — carries the LLM's justification for why a secret is needed
- Justification propagation via `toolcontext.WithJustification`/`GetJustification` context helpers
- `justification.go` — extracts `_justification` from tool call JSON args and threads it through context
- `toolcontext` package for cross-package context value access (justification, tool name)
- `MCPCaller` interface with counterfeiter fake for unit-testing `ClientTool.Call` without a real MCP server
- Comprehensive unit tests for MCP package: `ClientTool.Call`, `shouldIncludeTool`, `buildStdioEnv`, `expandEnvValue`
- Arrange-Act-Assert (AAA) mandatory testing rule in `Agents.md`
- Upgraded memory management with better vector store integration (#5)

### Changed

- **Breaking:** `SecretProvider.GetSecret` signature changed from `(ctx, string)` to `(ctx, GetSecretRequest)` — all call sites migrated
- **Breaking:** Context-mode middleware changed from enabled-by-default to opt-in (`ContextModeConfig.Enabled` field; set to `true` to activate)
- Replaced hand-rolled test fakes with counterfeiter-generated `securityfakes.FakeSecretProvider` across `config`, `langfuse`, and `modelprovider` test suites
- Removed redundant tests in `model_test.go` and `security_test.go`
- Updated CODEOWNERS to change ownership to @stackgenhq/gophers

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

[Unreleased]: https://github.com/stackgenhq/genie/compare/v0.1.6-rc.1...HEAD
[0.1.6-rc.1]: https://github.com/stackgenhq/genie/compare/v0.1.5...v0.1.6-rc.1
[0.1.5]: https://github.com/stackgenhq/genie/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/stackgenhq/genie/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/stackgenhq/genie/compare/v0.1.1...v0.1.3
[0.1.1]: https://github.com/stackgenhq/genie/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/stackgenhq/genie/releases/tag/v0.1.0
