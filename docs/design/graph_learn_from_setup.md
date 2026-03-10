# Knowledge Graph: Learning from Data (Setup and Blind Spots)

## Blind Spots

### 1. **Setup mentions graph capability but does not configure it explicitly**
- The setup wizard asks "Learn from my data?" and enables `[data_sources]` (Gmail, Drive, etc.). It now also tells the user that Genie can build a knowledge graph from their synced data after the first sync, but it still does **not** ask any graph-specific questions or write a `[graph]` section in the generated config.
- **Impact**: Graph is on by default (`disabled = false`), so tools are available and users have heard about the knowledge graph; they may not understand when it runs, how to trigger graph building, or how to change graph settings in config.
- **Mitigation**: Refine setup and post-setup copy/docs to (a) make clear that graph is enabled by default and can be customized via `[graph]` in config, and (b) explain that Genie will build a knowledge graph from synced data after the first sync (and how users can query or expand it).

### 2. **No automatic "build graph from memory" after sync**
- Data sources sync runs in the background and fills vector memory; the graph stays empty until the agent is explicitly asked to use `memory_search` and then `graph_store_entity` / `graph_store_relation`.
- **Impact**: New users who opted into "learn from my data" get semantic search but no graph unless they (or a prompt) trigger the agent to populate the graph.
- **Mitigation**: See "Kicking in the agent" below.

### 3. **Setup does not run the full app or sync**
- `genie setup` only writes config and runs `genie grant`; it does **not** start the server or run a sync. So there is no "right after setup" moment when vector memory already has data.
- **Impact**: Any "learn and build graph" step that assumes data is already in memory cannot run during setup itself; it must run after the user has started Genie and at least one sync has completed.

### 4. **Graph persistence path vs. setup**
- Graph persists to `~/.genie/<agent>/memory.bin.zst` (gob+zstd) when `data_dir` is set. When empty, the graph is in-memory only. Setup collects agent name; if the user sets `data_dir` (e.g. to `~/.genie/<agent>/`), the path is well-defined once the user runs Genie.
- **Blind spot**: If someone runs setup with one agent name and later changes it in config, the graph may be in the old path; no migration or mention in docs.

### 5. **No prompt or system hint to "use graph with datasource data"**
- The agent is not instructed by default to prefer building a graph from `memory_search` results when the user asks relationship-style questions.
- **Mitigation**: Add a persona or system hint (e.g. in orchestrator/skills) that when the user asks about "who worked on what" or "connections", the agent should use `memory_search` and then populate/query the graph.

### 6. **Rate limits and cost**
- A "build graph from all memory" pass could trigger many `memory_search` calls and many `graph_store_*` calls; with large mailboxes or Drive, that could hit rate limits or cost.
- **Mitigation**: If we add an automatic graph-learn pass, cap it (e.g. first N search results, or only after first sync with a limited window).

---

## Kicking in an agent to learn from data and create a knowledge graph

When setup has happened (user said "Yes, learn from my data"), we want the agent to eventually use that data and build a knowledge graph. Options:

### Option A: After first sync (on server start)
- **Where**: In `pkg/app/app.go`, after starting `runDataSourcesSync`, register a one-time or periodic "graph learn" trigger.
- **How**:  
  1. Run the first sync (or wait for the first tick of `runDataSourcesSync` to complete).  
  2. If graph is enabled and data sources are enabled, call the orchestrator once with a fixed internal message, e.g.  
     *"Using memory_search, retrieve a sample of recent content from memory. From the results, extract people, projects, and issues and store them as entities and relations using graph_store_entity and graph_store_relation. Use broad queries (e.g. 'recent emails', 'projects') and limit to the first 5–10 results to avoid overload."*  
  3. Run this in a background goroutine so startup is not blocked; optionally delay by 1–2 minutes so the first sync has time to complete.
- **Pros**: No new CLI; works for anyone who has data sources + graph enabled.  
- **Cons**: Requires refactoring so the app can invoke the orchestrator with an "internal" message without a real user channel (or use a synthetic channel). Need to avoid sending this to the user's chat.

### Option B: New command `genie learn` or `genie graph bootstrap`
- **Where**: New cobra command in `cmd/` that loads config, boots the app (or only vector + graph + orchestrator), runs one sync if needed, then runs one agent turn with the "build graph from memory" prompt.
- **How**: Similar to Option A but triggered explicitly by the user (or by setup at the end: "Run 'genie learn' to build your knowledge graph from synced data").
- **Pros**: Explicit, testable, no hidden background behavior.  
- **Cons**: User must run an extra command (unless setup invokes it); setup would need to start the app or run a subprocess.

### Option C: Cron action `genie:graph_learn`
- **Where**: Add a cron action (like `genie:report`) that runs the agent once with the "build graph from memory" goal.
- **How**: User (or setup) adds a cron task, e.g. `action = "genie:graph_learn"`, schedule `0 */6 * * *` (every 6 hours). The cron runner starts a minimal app context, runs one orchestrator turn with the fixed prompt, then exits.
- **Pros**: Recurring graph refresh; no change to setup flow.  
- **Cons**: First run may still be empty if no sync has run yet; need to document "ensure data sources have synced at least once."

### Option D: Setup writes a "pending graph learn" flag; app runs it after first sync
- **Where**: Setup writes a file or DB row, e.g. `~/.genie/<agent>/graph_learn_pending`. When the app's data-sources sync completes (first successful run), check for this flag; if set, run one agent turn with the graph-learn prompt and then clear the flag.
- **How**: In `runOneDataSourcesSync` (or after it in the sync loop), if sync succeeded and `graph_learn_pending` exists and graph is enabled, spawn a one-off agent run with the internal message and delete the flag.
- **Pros**: "When setup happens" directly triggers learn-after-first-sync without a new command.  
- **Cons**: Requires the app to support an internal/synthetic agent invocation and to pass the right context (no user channel).

---

## Recommendation

- **Short term**: Document in QA and user docs that after setup (with "learn from my data"), users can ask in chat: *"Search your memory for my recent emails and Drive content, then build a knowledge graph of people and projects and tell me who is connected to X."* That validates the flow without new code.
- **Next step**: Implement **Option D** (pending flag + run after first sync) so that when setup sets "learn from my data", setup also writes a pending flag (e.g. when we add a "Build knowledge graph from your data?" step and the user says Yes). When the app's first sync completes, it runs one agent turn to populate the graph and clears the flag. That requires:
  1. Setup wizard: optional step "Build a knowledge graph from your data? (Yes = Genie will run a one-time learning pass after the first sync.)" and write `graph_learn_pending` or a config field when Yes.
  2. In the app: after the first successful `runOneDataSourcesSync`, check for the pending flag; if set and graph is enabled, invoke the orchestrator once with the fixed graph-learn prompt (and a way to run without sending output to a real user channel).
