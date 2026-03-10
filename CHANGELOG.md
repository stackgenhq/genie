# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Agent learning from failures — previously, failed task outputs were discarded; the agent now stores them as episodic memories with LLM-generated verbal reflections, enabling it to avoid repeating the same mistakes.
- Recency-weighted episodic memory retrieval using exponential decay (`e^(-0.01 × hours)`), so recent lessons surface first and old episodes naturally fade over ~2 weeks without manual pruning.
- Importance scoring for episodic memories — each stored episode receives a 1-10 importance score via a cheap LLM call (`TaskEfficiency`), boosting critical lessons in weighted retrieval.
- Daily wisdom consolidation (`EpisodeConsolidator`) — reads recent episodes, summarizes them into concise bullet-point lessons via LLM, and stores as `WisdomNote`s. Idempotent per period; ready to be wired into a cron job.
- Wisdom notes injected into agent prompts as a `## Consolidated Lessons` section, providing distilled experience alongside raw episodic memories.
- Loop-level failure capture — adaptive loop terminations (repetition, errors, failure status) now stored as failure episodes with reflections via `storeLoopFailureEpisode`.
- QA test plan for agent learning features (`qa/20260310_failure_learning.md`) — 6 manual test scenarios plus inventory of 46 automated Ginkgo/Gomega tests.


## [0.1.7] - 2026-03-10

### Added

- Added `notification` tool with support for sending alerts and messages via Slack, Webhooks, Discord, and Twilio, enabling both orchestrator and sub-agents to trigger external notifications.
- New `coding` task type (`TaskCoding`) for pure code generation, algorithmic problem solving, and script writing — benchmarked via HumanEval / MBPP / LiveCodeBench. Orchestrator `create_agent` tool description and JSON schema updated to surface the new option alongside `planning`, `tool_calling`, `terminal_calling`, and `efficiency`.
- Hallucination guard module (`halguard`) providing a two-phase check: a pre-execution multi-signal verification using weighted signals (e.g., Role-Play detection, Information Density) and a post-execution multi-model consistency checker based on Finch-Zk to catch potential LLM hallucinations.
- OpenTelemetry (OTel) spans and attributes recorded for hallucination guard `PreCheck` and `PostCheck` execution.
- Direct EventBus emission via `emitShortCircuit` for refused/out-of-scope orchestrator responses, bypassing the EventAdapter to ensure timely delivery to the AG-UI.
- Make agent capabilities resume creation optional through `disable_resume` config.
- Semantic router routes can now be dynamically configured via config file (`routes` field).
- Defined an `IRouter` interface for the semantic router to improve testability and abstraction.
- AG-UI authentication middleware (`pkg/security/auth`) supporting password-based auth (config → env → keyring → auto-generate) and JWT/OIDC token validation with JWKS auto-discovery from trusted issuers.
- Terraform-based K8s deployment (`installation/k8s/`) with IRSA (ReadOnlyAccess), External Secrets (AWS Secrets Manager), ConfigMap, Ingress, and `random_password` for `AGUI_PASSWORD`.
- K8s deployment example for DevOps-in-K8s use case (`examples/devops-in-k8s/`).
- Config Builder UI: authentication controls (password protected toggle, JWT trusted issuers, allowed audiences) with TOML/YAML serialization.
- Documentation: AG-UI auth configuration reference, password resolution order, and security best practices in `docs.yml`.
- Replaced Google-hardcoded OAuth protocol in AG-UI authentication with dynamic OpenId Connect (OIDC) support via `/.well-known/openid-configuration` auto-discovery, allowing `issuer_url` support for generic SSO gateways like Okta and Auth0.
- Implemented static API key authentication via `Authorization: Bearer <key>` and `X-API-Key: <key>` for M2M communication to bypass interactive SSO.
- Auth middleware context now sets `authcontext.Principal` metadata indicating the current request's user ID and role, wired across the task orchestration bus via `MessageOrigin`.
- Auth middleware now explicitly permits incoming CORS `OPTIONS` preflight requests, allowing browsers to perform valid API checks.
- GitHub CLI (`gh_cli`) agent tool (`pkg/tools/ghcli`) — wraps the `gh` binary for GitHub operations (PRs, issues, Actions, deployments, Dependabot alerts). Configurable via `[ghcli]` config section and Config Builder UI.
- Shell tool refactored into `pkg/tools/unix` with configuration-based security: `ShellToolConfig` provides `allowed_env` (environment variable allow-list) and `timeout` fields. Only explicitly listed env vars (plus `PATH`) are resolved via `SecretProvider` and injected into shell processes (principle of least privilege). Config Builder UI updated with shell tool security section.
- Credential isolation architecture for K8s deployments — init container + sidecar pattern with `credential-bootstrap.sh`, `credential-refresh.sh`, and `genie-entrypoint.sh` scripts in `examples/devops-in-k8s/scripts/`.
- Checkpoint retry wrapper (`pkg/db/checkpoint_retry.go`) — automatic retry with exponential backoff for transient database errors (connection refused, deadline exceeded) on all `CheckpointSaver` operations. Wraps the GORM saver transparently.
- Knowledge graph: unified `graph_store` tool (replaces `graph_store_entity` and `graph_store_relation`) with `action=entity|relation|batch`; batch action stores multiple entities/relations concurrently, with parallel embedding via `AddEntities` for vector-backed stores.
- Knowledge graph: unified `graph_query` tool (replaces separate `graph_query`, `graph_get_entity`, `graph_shortest_path`) with `action=neighbors|get_entity|shortest_path|explore|batch`; `explore` action returns a full ego-graph (root + connected entities + relations) in a single call; `batch` action runs multiple sub-queries in parallel.
- Knowledge graph: vector-backed store (`pkg/memory/graph/vectorstore.go`) — reuses the configured vector store (Qdrant/Milvus) for graph storage instead of in-memory gob+zstd snapshots. Selectable via `graph.backend = "vectorstore"` config. Config Builder UI updated with backend selector.
- Episodic memory in orchestrator — stores Q&A turns as episodes (pending → promoted/demoted by user 👍/👎 reactions) and recalls relevant past episodes during context assembly, enabling the agent to learn from its own interaction history.
- HITL audit trail — `WithHITLAuditor` option injects a durable `audit.Auditor` into the HITL middleware; every approval, rejection, and auto-approval decision (with reason classification: `always_allowed`, `approve_list`, `cache_hit`) is written to the audit log.
- HITL principal-scoped approvals — `CreatedBy` field on `ApprovalRequest`/`CreateRequest` tracks the requesting user; `CanResolve` authorization check ensures only the creator or admins can approve/reject; AG-UI `handleApprove` enforces principal-scoped resolution.
- Semantic cache responses now emitted via AG-UI event bus (`agui.EmitAgentMessage`) so streaming web UI clients see cache-hit responses that previously bypassed the tree executor.
- Sub-agent shared memory instructions — `buildSubAgentInstruction` now includes `INCREMENTAL REPORTING` (per-item results as they complete) and `SHARED MEMORY` (findings written to working memory for sibling agents).
- Auth middleware logs first unauthenticated request IP/path and injects `principal` + `request_id` into logger context and OTel trace attributes (`langfuse.user.id`) for authenticated requests.
- Hardened Qdrant Terraform deployment for multi-AZ HA with PodDisruptionBudgets, topology spread constraints online for - Multimodal media attachment support in AG-UI chat (extracting and processing images, video, and audio from browser data-URLs).
- Automated WAV conversion using `ffmpeg` for unsupported audio formats (like OGG from WhatsApp voice notes) before forwarding them to the LLM.
- Cron tool suite: added `list_recurring_tasks`, `delete_recurring_task`, `history_recurring_task`, `toggle_recurring_task` (pause/resume), and `trigger_recurring_task` (run-now on demand) agent tools for managing and inspecting scheduled tasks at runtime.
- Cron scheduler now injects a notify-on-failure instruction at dispatch-time for all cron tasks (config-defined and tool-created), directing sub-agents to use the `notify` tool when they cannot complete scheduled work.
### Changed

- Refactored `examples/devops-in-k8s` Terraform configurations to use structured input objects (`aws`, `genie`, `kubernetes`, `auth`) and properly inject local authentication configuration.
- Updated `examples/devops-in-k8s` k8s deployments to launch as root (`runAsUser: 0`) and included additional required runtime utilities (`aws-cli jq curl bash su-exec`).
- `Authenticator.Authenticate` now returns `*authcontext.Principal` instead of `bool`, enabling the auth middleware to inject identity metadata into the request context. When no auth is configured, a demo principal (`demo-user`) is injected as a pass-through.
- Parallelized execution of cross-model text generation during the `halguard` Post-Check using `errgroup`, significantly reducing latency.
- Refactored `semanticrouter` gatekeeper integration in the orchestrator to consume the `IRouter` interface, lowering code coupling.
- Simplified `semanticrouter.New` constructor by handling built-in and configured route merging internally.
- Cleaned up the `semanticrouter` public API by removing the unused `CheckJailbreak` method and making `Route` private.
- Credential bootstrap script now installs `gettext` (provides `envsubst`) in the init container and fixes file permissions with `chmod 0640` / `chown 65532:65532` for non-root genie process.
- K8s deployment adds `GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn` env var to suppress benign protobuf namespace conflict between Milvus and Qdrant gRPC clients (`common.proto`).
- K8s deployment resource adds `timeouts { create = "1m"; update = "1m" }` to prevent `tofu apply` from hanging when the rollout is stuck.
- Shell tool provider (`tools.NewShellToolProvider`) now accepts `SecretProvider` and `ShellToolConfig` for environment variable filtering.
- Retrieval tool classification in loop-detection middleware updated to reference consolidated `graph_query` and `memory_search` tool name constants.
- Default HITL read-only tool list updated: replaced `graph_store_entity`, `graph_store_relation`, `graph_get_entity`, `graph_shortest_path` with unified `graph_store` and `graph_query`; `memory_search` uses `vector.MemorySearchToolName` constant.
- HITL approval resolution now attributed to the authenticated principal (`resolvedBy: principal.ID`) instead of the hardcoded `"chat-ui"` string.
- Sub-agent instruction text streamlined — reduced redundancy, consolidated behavioral guardrails, removed the explicit `JUSTIFICATION` footer (justification is still extracted by middleware).
- `examples/devops-in-k8s/README.md` completely rewritten to match actual infrastructure: added architecture diagram, fixed `dev.auto.tfvars` filename, updated commands to use `tofu`, added 18 Terraform resources table and troubleshooting section.

- **Breaking**: the top-level `persona_file` config option has been moved to `[persona]` block. To migrate existing `.genie.toml` files, replace:

  ```toml
  persona_file = "path/to/your/persona.md"
  ```

  with:

  ```toml
  [persona]
  file = "path/to/your/persona.md"
  disable_resume = true
  ```
- Combined legacy `persona_file` and new `disable_resume` options into a nested `[persona]` table for cleaner organization.

### Fixed

- PostgreSQL compatibility: removed hardcoded `gorm:"type:blob"` tags from `SessionState`, `AppState`, and `UserState` models in `pkg/db/session_store.go` — `blob` is a SQLite/MySQL type that causes `ERROR: type "blob" does not exist (SQLSTATE 42704)` on PostgreSQL. GORM now auto-maps `[]byte` to `bytea` (PostgreSQL) or `blob` (SQLite).
- Checkpoint saver wrapped with retry logic to handle transient PostgreSQL connection failures (e.g. after pod evictions) instead of crashing.
- Semantic cache hits now properly emit responses to AG-UI streaming clients, which were previously invisible because the cache-hit path never entered the tree executor.
- Mitigated path traversal vulnerabilities by enforcing strict temporary directory boundaries during multimedia extraction.
- Prevented memory leaks in `docs/js/chat.js` by automatically revoking object URLs used for attachment previews.
- Prevented out-of-bounds panics on malformed data-URLs by implementing length assertions and checking empty data array.

### Removed

- Removed `installation/k8s/` directory (README, deployment.yaml, genie.toml, main.tf, terraform.tfvars.example) — consolidated into `examples/devops-in-k8s/`.
- Removed `pkg/tools/shell_tool.go` and `pkg/tools/shelltool/` package — replaced by `pkg/tools/unix/` with config-based security.
- Removed separate graph tools (`graph_store_entity`, `graph_store_relation`, `graph_get_entity`, `graph_shortest_path`) — consolidated into unified `graph_store` and `graph_query` tools with action-based dispatch.

## [0.1.6] - 2026-03-03

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

[Unreleased]: https://github.com/stackgenhq/genie/compare/v0.1.6...HEAD
[0.1.6]: https://github.com/stackgenhq/genie/compare/v0.1.6-rc.2...v0.1.6
[0.1.6-rc.2]: https://github.com/stackgenhq/genie/compare/v0.1.6-rc.1...v0.1.6-rc.2
[0.1.6-rc.1]: https://github.com/stackgenhq/genie/compare/v0.1.5...v0.1.6-rc.1
[0.1.5]: https://github.com/stackgenhq/genie/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/stackgenhq/genie/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/stackgenhq/genie/compare/v0.1.1...v0.1.3
[0.1.1]: https://github.com/stackgenhq/genie/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/stackgenhq/genie/releases/tag/v0.1.0
