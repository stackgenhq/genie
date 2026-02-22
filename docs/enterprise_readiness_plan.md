# Enterprise Readiness Implementation Plan for Genie

This document outlines the step-by-step implementation plan for integrating enterprise-ready capabilities into the `genie` ReAcTree orchestrator. These capabilities focus on increasing predictability, bounding LLM non-determinism, and providing auditability. 

**Core Requirement:** All features described below *must* be strictly opt-in via configuration flags to avoid disrupting the current default developer experience.

## General Configuration Updates

To ensure these features are opt-in, we will introduce a new configuration block in `TreeConfig` or `AppConfig`:

```go
type EnterpriseFeatures struct {
    EnableCriticMiddleware bool `mapstructure:"enable_critic_middleware"`
    EnableActionReflection bool `mapstructure:"enable_action_reflection"`
    EnableDryRunSimulation bool `mapstructure:"enable_dry_run_simulation"`
    EnableMCPServerAccess  bool `mapstructure:"enable_mcp_server_access"`
    EnableAuditDashboard   bool `mapstructure:"enable_audit_dashboard"`
}
```

---

## Phase 1: Deterministic Branch Pruning (The "Critic" Middleware)

**Goal:** Prevent runaway API costs by validating actions before they run using deterministic rules or a smaller, cheaper "Judge LLM".

### Implementation Steps:
1. **Define Middleware Interface:** Create an `ActionValidator` interface in `pkg/reactree/middleware.go`.
    ```go
    type ActionValidator interface {
        Validate(ctx context.Context, action NodeAction) error
    }
    ```
2. **Implement concrete validators:**
    *   `DeterministicValidator`: Checks actions against an allowlist of permitted tools or IP ranges.
    *   `LLMCriticValidator`: Uses a cheaper model (e.g., GPT-4o-mini or Claude 3.5 Haiku) to quickly score the relevancy of a proposed action to the overall goal.
3. **Hook into Executor:** Modify `pkg/reactree/orchestrator.go` and `tree.go`. Right before a node executes its external tool, invoke the middleware chain.
4. **Opt-In Trigger:** Wrap the execution in an `if config.EnterpriseFeatures.EnableCriticMiddleware` check. If an action fails validation, the branch is immediately marked `PRUNED`, returning an error to the planner rather than a panic.

---

## Phase 2: State Isolation (Internal vs. External Actions & RAR Loops)

**Goal:** Implement Reasoning-Action-Reflection (RAR) loops and distinguish between harmless "internal" updates and side-effectful "external" calls.

### Implementation Steps:
1. **Action Categorization:** Update the `Tool` or `Action` definitions to include a `SideEffect` boolean or `ActionType` enum (`Internal` vs. `External`).
2. **Mandatory Reflection Step:** For `External` actions, inject a Reflection node execution prior to the action. 
3. **Execution Engine Mod:** In `pkg/reactree/orchestrator.go`, alter the execution loop. When evaluating an `External` action, if `EnableActionReflection = true`, prompt the engine to output an "Internal Monologue" (e.g., "Why am I doing this? Is it safe?").
4. **Checkpointing Integration:** Save this internal monologue to the SQLite checkpoint store (using the Durable Checkpointing system) so it is formally recorded.

---

## Phase 3: Standardized Connectivity (Model Context Protocol - MCP)

**Goal:** Move away from hardcoded tools to enterprise-managed tool registries via MCP, allowing IT to handle access limits.

### Implementation Steps:
1. **MCP Client Integration:** Introduce an MCP client wrapper in `pkg/mcp/` or use the existing implementation to fetch the tool spec dynamically.
2. **Tool Registry Adapter:** Update `pkg/tools/registry.go` so that if `EnableMCPServerAccess` is true, it queries a configured MCP server URL to retrieve tool definitions, overwriting or supplementing the local tools context.
3. **Agent Creation Modification:** Ensure `pkg/reactree/create_agent.go` passes the MCP-backed `tools.Registry` down to sub-agents.
4. **Opt-In Trigger:** Configured via `mcp_servers` array in the standard configuration.

---

## Phase 4: Genie Sentinel (Pre-Flight Dry Runs)

**Goal:** Simulate the execution tree without side effects, giving users a "Plan & Cost Estimate" before approval.

### Implementation Steps:
1. **Simulation Mode:** Introduce a `DryRun` boolean to the ReAcTree `Executor`.
2. **Mock Execution:** In `tree.go`, if `DryRun` is true, external tools return mock payloads (or simply standard schemas without firing actual HTTP requests).
3. **Plan Visualizer:** Traverse the fully generated, albeit mocked, tree and compile a summary: "Steps Planned: X", "Estimated Tokens: Y", "Third-Party Systems Touched: Z".
4. **Opt-In Trigger:** Add a CLI flag `--dry-run` or the config `EnableDryRunSimulation`. An API endpoint `/api/v1/simulate` will expose this directly to the user interface.

---

## Phase 5: The Predictability Audit Dashboard (Agent SOC2)

**Goal:** Provide an immutable timeline of agent reasoning, critic validations, and tool calls for compliance.

### Implementation Steps:
1. **Event Sourcing:** Enhance the SQLite Durable Checkpointer. Ensure it writes structured JSON logs of every node transition, tool call, and critic rejection.
2. **Audit Exporter Interface:** Create a service in `pkg/audit/` that consumes these checkpoints and reformats them for SIEM ingestion (e.g., Splunk format) or an internal visualization endpoint.
3. **Opt-In Trigger:** `EnableAuditDashboard`. When true, starts an isolated Go routine that serves a read-only visual interface parsing the `.genie.db` checkpoints into a Gantt-style tree layout of the agent's actions.

---

## Rollout Strategy
1. Implement **Phase 1 (Critic Middleware)** first, as it tackles the immediate token sprawl/predictability issue in `orchestrator.go` and `tree.go`.
2. Implement **Phase 4 (Dry Runs)**. It pairs naturally with Phase 1 to build trust.
3. Overhaul the connection layer with **Phase 3 (MCP)** while moving the ReAcTree modifications into **Phase 2** to separate logic from execution.
