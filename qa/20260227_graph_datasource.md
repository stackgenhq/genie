# Feature: Knowledge Graph with Data Source Data

## Why

Genie was given a knowledge graph (entities and relations) so the agent can store and query structured relationships (e.g. who worked on what, which repo an issue belongs to). When combined with **data sources** (Gmail, Drive, GitHub, etc.), the agent can use `memory_search` to pull synced content and then use graph tools to build and query a graph from that data — e.g. extract people and issues from emails and answer relationship questions.

## Problem

Without the graph, the agent could only do semantic search over text (vector memory). There was no way to store or traverse explicit relations (subject–predicate–object), so questions like “Who is connected to project X?” or “What issues did Alice work on?” required the LLM to infer from raw search results instead of querying a graph.

## Benefit

- **Structured relations**: The agent can store entities (people, repos, issues, documents) and relations (WORKED_ON, OWNS, MENTIONS) and query one-hop neighbors via `graph_query`.
- **Datasource + graph**: Data from Gmail, Drive, GitHub (via data sources sync and `memory_search`) can be used to populate the graph; the agent then uses both vector search and graph traversal to answer.
- **Interface-driven**: The store is behind an interface so the implementation can be swapped (in-memory now; Milvus or other backends later) without changing tools or prompts.

---

## Test 1: Agent uses graph tools while using datasource data (memory_search + graph)

### Arrange

- Genie is configured with:
  - **Vector memory** with persistence (e.g. `[vector_memory]` with `persistence_dir` and a valid `embedding_provider` such as `openai`).
  - **Data sources** enabled and at least one source (e.g. Gmail or Drive) with valid credentials and scope so sync runs and content is indexed.
  - **Graph** not disabled: `[graph]` with `disabled = false` (or section omitted; default is enabled). Optional: `data_dir` set (e.g. `~/.genie/<agent>`) so the graph persists to `memory.bin.zst` (gob+zstd).
- Server is running and connected via `docs/chat.html` (or another client).
- At least one data sources sync has completed so `memory_search` can return content from Gmail/Drive (e.g. emails or file snippets that mention people and projects).

### Act

1. In the chat UI, send a prompt that asks the agent to:
   - **Use data from your synced sources** (so it is motivated to call `memory_search`), and
   - **Build a small graph** of entities and relations from that data (e.g. people and what they worked on or mentioned), and
   - **Answer a relationship question** using the graph.

   Example prompts (adjust to your data):

   - *“Search my recent emails for anything about project Alpha. From what you find, build a graph of people and what they’re working on or mentioned. Then tell me who is connected to project Alpha and how.”*
   - *“Use my synced Drive and email to find mentions of team members and projects. Store them as entities and relations in your graph, then use the graph to list who is related to the ‘platform’ project.”*

2. Wait for the agent to complete (it may call `memory_search` one or more times, then `graph_store_entity` / `graph_store_relation`, then `graph_query`).

### Assert

1. **Tool usage (blackbox)**  
   In the chat UI or audit log, confirm that within the same conversation turn (or a short sequence of tool calls):
   - The agent used **`memory_search`** (or equivalent) to retrieve content (from datasource-synced vector memory).
   - The agent used **`graph_store_entity`** and/or **`graph_store_relation`** at least once.
   - The agent used **`graph_query`** at least once (to answer the relationship question using the graph).

2. **Answer consistency**  
   The final reply is consistent with:
   - Data that would have come from the synced source (e.g. names, projects mentioned in emails/Drive).
   - Relationships that could be derived from that data (e.g. “Alice worked on X”, “Bob is connected to project Alpha”).

3. **No hard failure**  
   The run completes without the agent reporting that graph tools are missing or that it cannot use the graph. If the agent reports “no data found” from `memory_search`, that is acceptable as long as it attempted both memory search and graph operations.

### Notes

- If datasource sync has not run or returned no items, `memory_search` may return nothing; the agent can still be asked to “store a few example entities and relations, then query the graph” to verify graph tools alone. For the full flow (datasource + graph), ensure sync has run and some content is indexed.
- Graph persistence: If `data_dir` is set, restart Genie and ask a question that uses `graph_query` for an entity stored in the previous run; the graph should still be available (stored in `~/.genie/<agent>/memory.bin.zst`).

---

## Test 2: Graph disabled — no graph tools used

### Arrange

- Genie config has `[graph]` with `disabled = true`.

### Act

1. Start Genie and connect via chat UI.
2. Ask the agent to “store an entity in your graph and then query its neighbors” (or similar).

### Assert

1. The agent does **not** call `graph_store_entity`, `graph_store_relation`, or `graph_query` (these tools are not registered when graph is disabled).
2. The agent may say it cannot use a graph or that the capability is not available; the server runs normally and other tools (e.g. `memory_search`) still work.

---

## Troubleshooting

| Issue | Check |
|--------|--------|
| Agent never calls graph tools | Ensure `[graph]` is present and `disabled = true` is not set (default is enabled). Restart Genie after config change. |
| Agent says no data from memory_search | Run data sources sync first; ensure `[data_sources]` is enabled and at least one source (Gmail/Drive/GitHub) has credentials and scope. See [data_sources_sync.md](data_sources_sync.md). |
| Graph not persisted across restarts | Set `[graph]` `data_dir` (e.g. `~/.genie/<agent>`). When empty, the graph is in-memory only. Confirm directory is writable and `memory.bin.zst` appears after storing entities. |
