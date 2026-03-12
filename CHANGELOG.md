# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **Unified identity package** (`pkg/identity`) — introduces a single `Sender` type carrying user ID, display name, role, and authentication method. Replaces the dual-identity system (`authcontext.Principal` + `messenger.Sender`) with one type shared by auth and messenger paths. `messenger.Sender` is now a type alias for `identity.Sender`.

- **Confidence-gated accomplishment storage** — `TreeResult` now carries a `Confidence` score (0.0–1.0) computed from execution signals (task completion, status, iteration efficiency, repetition, output presence). Only results above a configurable threshold (default 0.5) are stored, preventing failed/garbage outputs from polluting memory.
- **`AccomplishmentConfidenceThreshold`** config field in `[persona]` — controls the minimum confidence required to store an accomplishment (default 0.5 / 50%). Exposed in Config Builder UI under the Persona section with TOML/YAML serialization.
- Agent learning from failures — previously, failed task outputs were discarded; the agent now stores them as episodic memories with LLM-generated verbal reflections, enabling it to avoid repeating the same mistakes.
- Recency-weighted episodic memory retrieval using exponential decay (`e^(-0.01 × hours)`), so recent lessons surface first and old episodes naturally fade over ~2 weeks without manual pruning.
- Importance scoring for episodic memories — each stored episode receives a 1-10 importance score via a cheap LLM call (`TaskEfficiency`), boosting critical lessons in weighted retrieval.
- Daily wisdom consolidation (`EpisodeConsolidator`) — reads recent episodes, summarizes them into concise bullet-point lessons via LLM, and stores as `WisdomNote`s. Idempotent per period; ready to be wired into a cron job.
- Wisdom notes injected into agent prompts as a `## Consolidated Lessons` section, providing distilled experience alongside raw episodic memories.
- Loop-level failure capture — adaptive loop terminations (repetition, errors, failure status) now stored as failure episodes with reflections via `storeLoopFailureEpisode`.
- `FailureReflector` interface + `ExpertFailureReflector` implementation (uses cheapest `TaskEfficiency` model) for LLM-generated actionable failure summaries.
- `ImportanceScorer` interface + `ExpertImportanceScorer` implementation; robust parsing handles noisy LLM output with integer cleansing and clamping to 1–10.
- `WisdomStore` interface + `EpisodeSummarizer` interface with `ExpertEpisodeSummarizer` implementation backed by `memory.Service`.
- `RetrieveWeighted` method on episodic memory — scores episodes as a weighted sum (0.6 × recency + 0.4 × importance) for balanced retrieval.
- Failure episodes display with ⚠️ prefix and verbal reflection in agent prompts; capped at 500 runes to prevent prompt bloat.
- Early-return guard in `RetrieveWisdom` when `limit ≤ 0` to prevent unnecessary processing.
- QA test plan for agent learning features (`qa/20260310_failure_learning.md`) — 10 manual acceptance tests plus 96 automated Ginkgo/Gomega specs.
- 96 Ginkgo/Gomega test specs across `memory/failure_learning_test.go`, `memory/consolidation_test.go`, `memory/plan_advisor_test.go`, `failure_reflector_test.go`, `importance_and_consolidation_test.go`, and `agent_node_pvt_test.go`.
- **Semantic router middleware chain** (`pkg/semanticrouter/semanticmiddleware/`) — composable classification pipeline where each tier enriches a shared `ClassifyContext` and can either decide or pass downstream. Tiers: **L0 Regex** → **L1 Vector** → **Follow-Up Bypass** → **L2 LLM**.
- **L0 regex pre-filter** — catches conversational follow-ups ("try again", "same thing", "but I wanted") at ~0 cost (<1ms, no embedding or LLM). Configurable via `semantic_router.l0_regex` with support for custom patterns.
- **L1 vector near-miss enrichment** — when a route match is below threshold but above 0.5, the closest route and score are injected into the `ClassifyContext` so the L2 LLM can use the signal as a routing hint.
- **Follow-up bypass middleware** — ensures messages flagged as follow-ups by L0 skip the expensive L2 LLM call, even when L1 doesn't match (e.g. dummy embedder).
- **`follow_up` L1 route** — new built-in route with 10 utterances for common continuation patterns, expanded from the original 2 routes (jailbreak + salutation) to 3.
- **Semantic cache TTL** — `CacheTTL` config field (default 5m) enforces temporal decay on cached responses. Operational queries (health checks, pod listings) no longer return stale data.
- **Background cache pruning** — `PruneStaleCacheEntries` is now wired to a background ticker (default every 1h, configurable via `prune_interval`). Expired cache entries are cleaned up automatically, preventing unbounded vector store growth. The ticker starts with the router and stops gracefully on `Close()`.
- 34 Ginkgo/Gomega test specs for the `semanticmiddleware` package covering chain building, L0 regex matching, L1 vector routing, and follow-up bypass behavior.
- **Langfuse trace analyzer** (`pkg/langfuse/trace_analyzer.go`) — queries the Langfuse API to produce per-trace execution breakdowns: user request, tool calls (with parent), sub-agent detection (spans with child generations), LLM call counts, vector store operation counts, token usage, cost, and duration. Filterable by user, session, agent name, tags, and time window. Includes `FormatReport()` for human-readable markdown reports.
- **Internal task context marker** (`orchestratorcontext.WithInternalTask`) — background events (cron triggers, heartbeats, webhooks) bypass the semantic cache and classification pipeline entirely. Prevents cron tasks from receiving stale cached responses and keeps cron results from polluting the cache for future user queries.
- **MCP prompts as skills** — MCP server prompts are now discoverable and loadable as Genie skills via `PromptRepository` adapter (`pkg/mcp/prompt_repo.go`). MCP prompts appear in `discover_skills` and can be loaded via `load_skill`, prefixed with the server name (e.g. `stackgen_cloud_discovery_playbook`).
- **Composite skill repository** (`pkg/skills/composite_repo.go`) — merges multiple `skill.Repository` sources (local FS + MCP prompts) into a unified, deduplicated, sorted skill index.
- **Generic executable tool** (`pkg/tools/executable/`) — config-driven tool wrapper for arbitrary binaries with secret validation, minimal environment isolation (`PATH`, `HOME` + configured vars only), and shell metacharacter injection prevention. Replaces the hardcoded `ghcli` tool.
- **Cloud discovery example** (`examples/cloud-discovery/`) — full `.genie.toml` config, `AGENTS.md` persona, and `cloud-discovery` skill for AWS resource scanning and StackGen AppStack generation.
- Orchestrator now has access to `memory_search` and `memory_store` tools, allowing it to query vector memory at session start instead of relying on `read_notes` (which is empty at the start of a conversation).

### Changed

- **Accomplishments routed through episodic memory pipeline** — `storeAccomplishment()` no longer writes raw Q&A directly to the vector store. Results are now scored by `ImportanceScorer` (1–10 significance), stored as episodes with recency decay (λ=0.01, ~3% weight after 14 days), and consolidated into wisdom notes by the daily `EpisodeConsolidator`. This prevents ephemeral data (e.g. AWS cost lookups) from permanently polluting vector memory.
- `Toggles` struct refactored: boolean feature flags (`EnableCriticMiddleware`, `EnableActionReflection`, `EnableDryRunSimulation`, `EnableMCPServerAccess`, `EnableAuditDashboard`) removed; only `DryRun.Enabled` (via `FeaturesConfig`) and runtime-injected dependencies remain.
- `FeaturesConfig` simplified from 5 boolean flags to a single `DryRun DryRunConfig` struct; config files use `[features.dry_run] enabled = true` instead of individual flags.
- `ActionReflector` is now activated purely by setting `Toggles.Reflector` (non-nil); the redundant `EnableActionReflection` boolean guard was removed.
- `ImportanceScorer` and `WisdomStore` are now propagated through `Toggles` → `tree` → `AgentNodeConfig`, consistent with `FailureReflector` and other injectables.
- `docs/config-builder.html` and `docs/js/config-builder.js` updated to reflect removal of deprecated feature-flag fields from the config schema.
- **Semantic router refactored from monolithic `Classify` method to a composable middleware chain** — L0/L1/L2 logic moved from hardcoded if/else branches into separate, testable `semanticmiddleware.Middleware` functions composed via `BuildChain`.
- **L2 classification optimized**: streaming disabled and `MaxTokens` capped at 30 for a response expected to be a single word, eliminating streaming overhead on ~2500-token prompts.
- **L2 prompt enriched with upstream signals**: `buildL2Message` now includes near-miss route hints from L1 and follow-up context from L0, enabling better-informed classification decisions.
- `semanticrouter.Config` extended with `CacheTTL`, `L0Regex`, and `FollowUpBypass` middleware config structs, all exposed through the agent config chain (`config.go` → `semanticrouter.Config` → `mw.*Config`).
- `ErrToolCallRejected` introduced so that intentional tool call rejections (e.g., from user rejections, HITL re-planning feedback, or validation/schema rechecks) do not trigger the circuit breaker and inappropriately penalize healthy tools.
- HalGuard `verifyLight` prompt now includes tool call context (`ToolSummary` field on `PostCheckRequest`) — the verifier is told which tools the sub-agent called, preventing false-positive hallucination flags on tool-sourced data (e.g. AWS VPC IDs from `run_shell`).
- Loop detection threshold (`maxConsecutiveRepeatCalls`) increased from 2 → 3 to tolerate one accidental retry (common with Gemini Flash) while still catching true infinite loops.
- `SkillToolProvider` and `LoadSkillsFromConfig` now accept additional `skill.Repository` sources (e.g. MCP `PromptRepository`) via variadic `additionalRepos` parameters.
- Orchestrator Phase 1 (ANALYZE) prompt updated to prefer `memory_search` (vector memory) over `read_notes` at session start.
- Sub-agent audit metadata now stores the full goal string instead of truncating to 200 chars.
- **Breaking**: `Authenticator.Authenticate` now returns `*identity.Sender` (was `*authcontext.Principal`). The `identity.Sender` type has `DisplayName` instead of `Name`; `Role` and `AuthenticatedVia` fields are unchanged. Auth middleware now calls `identity.WithSender`/`identity.GetSender` instead of `authcontext.WithPrincipal`/`authcontext.GetPrincipal`.
- HITL `CreatedBy` and `CanResolve` now source user identity from `identity.GetSender(ctx)` instead of `authcontext.GetPrincipal(ctx)`. Identical string values flow through; no behavioral change.
- Langfuse tracing (`withPrincipalBaggage`) reads user identity from `identity.GetSender(ctx)` and maps `DisplayName` (was `Name`) to `langfuse.trace.metadata.user_name`.
- **`IStore` interface refactored to 2-parameter pattern** — `Search` and `SearchWithFilter` unified into `Search(ctx, SearchRequest)` with optional `Filter`; `Add`, `Upsert`, `Delete` now accept `AddRequest`, `UpsertRequest`, `DeleteRequest` structs. All callers across `orchestrator`, `semanticrouter`, `graph`, `reactree`, `report`, and `app` packages updated.

### Fixed

- MCP tool adapter now strips `_justification` field from tool call arguments before forwarding to MCP servers — LLMs inject this field based on sub-agent instructions, but MCP servers reject it as an unknown field (`"error converting arguments: input is invalid"`).

### Removed

- Critic middleware (`middleware.go`) — `NewDeterministicValidator`, `WrapWithValidator` and associated test files removed; tool blocking is handled exclusively by HITL.
- `AuditEventCriticRejection` audit event constant and `AuditHook.OnToolValidation` implementation removed (hook interface still defined in `hooks.go` for future use; `NoOpHook` satisfies it).
- `EnableCriticMiddleware`, `EnableActionReflection`, `EnableDryRunSimulation`, `EnableMCPServerAccess`, and `EnableAuditDashboard` boolean fields removed from `FeaturesConfig`.
- `pkg/tools/ghcli/` package — replaced by generic `pkg/tools/executable/` tool with config-driven binary wrapping.
- `GHCli` field removed from `GenieConfig`; replaced by `ExecutableTools executable.Configs` with `[executable_tools]` config section.
- `pkg/security/authcontext/` package — replaced by `pkg/identity/` with a unified `Sender` type. `authcontext.Principal`, `WithPrincipal`, and `GetPrincipal` are no longer available; use `identity.Sender`, `identity.WithSender`, and `identity.GetSender` instead.


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

[Unreleased]: https://github.com/stackgenhq/genie/compare/v0.1.7...HEAD
[0.1.7]: https://github.com/stackgenhq/genie/compare/v0.1.6...v0.1.7
[0.1.6]: https://github.com/stackgenhq/genie/compare/v0.1.6-rc.2...v0.1.6
[0.1.6-rc.2]: https://github.com/stackgenhq/genie/compare/v0.1.6-rc.1...v0.1.6-rc.2
[0.1.6-rc.1]: https://github.com/stackgenhq/genie/compare/v0.1.5...v0.1.6-rc.1
[0.1.5]: https://github.com/stackgenhq/genie/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/stackgenhq/genie/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/stackgenhq/genie/compare/v0.1.1...v0.1.3
[0.1.1]: https://github.com/stackgenhq/genie/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/stackgenhq/genie/releases/tag/v0.1.0
