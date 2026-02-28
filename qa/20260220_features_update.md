# QA Scenarios: Durable Checkpointing & Browser Integration

## Feature Overview
These scenarios test two key capabilities introduced for enterprise readiness:
1. **Durable Checkpointing**: Leveraging `sqlite` to persist `graph.State` during the execution of ReAcTree nodes. Providing resiliency during application restarts or interruptions.
2. **Browser Integration & Context Handling**: Incorporating `browser.Browser` initialization natively into the `agui` and `messenger` lifecycle, along with injecting context into `CodeOwner` for tool access using headless browser interfaces. 
3. **Tools Registry Refactoring**: Adopting `tools.Registry` wrapper for passing tools context down the hierarchy rather than raw tool slices.

## Scenario 1: Adaptive Loop State Continuity
**Objective:** Verify that `trpc-agent-go`'s SQLite `Checkpointer` saves and resumes graph nodes accurately.
1. Identify a multi-step task and prompt the engine.
2. Force-quit `genie` in the middle of executing a long-horizon plan (or an intermediate agent tool execution).
3. Restart `genie` passing the same session ID.
4. **Expected Result:** Upon restart, ExecutionEngine identifies the missing execution state from the SQLite checkpointer dataset (`checkpoints` table) and restores the Adaptive loop to its prior state.

## Scenario 2: Headless Browser Spawning (TUI/AGUI)
**Objective:** Verify that browser context integrates with downstream headless tools smoothly.
1. Spin up `genie`. Check logs to confirm `Browser initialized` is printed alongside `headless: true`.
2. As a user, ask Genie on the UI or messenger a question that requires navigating into an external blocked OR permitted domain.
3. Observe if `CodeOwner` forwards the `tabCtx` property accurately into tool executions.
4. Verify sub-agents do not stall.

## Scenario 3: Agent Creation Tool Registry Handling
**Objective:** Validate that orchestrators correctly supply the available tools to newly instantiated sub-agents.
1. Tell Genie: "Find all text files inside the project dir and then summarize them." (this usually prompts a subagent).
2. Monitor orchestrator's tool registry mapping.
3. Validate that `create_agent` tool properly unwraps the newly refactored `tools.Registry` object instead of raw tool slices.
4. Confirm no "tool not found" panics emerge and sub-agent succeeds.
