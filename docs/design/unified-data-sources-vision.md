# Unified Data Sources: Vision and Design

This document lays out the vision for treating **Google Drive, Gmail, Linear, GitHub, Slack, and Calendar** as a coherent set of **data sources** in Genie: one config model, one sync/ingest path, and one search/recap surface. It summarizes how others approach the problem, what trpc-agent-go provides today, what “vectorizing” means in this context, and a proposed direction for Genie.

---

## 1. How Others Do It

### 1.1 Research / RAG Frameworks

- **LlamaIndex**  
  Uses **data connectors** (Readers) that turn diverse sources into a common **Document** (text + metadata). Connectors live in **LlamaHub** (Google Docs, Notion, Slack, Discord, DBs, web). The pipeline is: **load** (connector) → **transform** (chunk, embed) → **index/store** → query. One abstraction per source; ingestion and retrieval are source-agnostic once documents are produced.

- **UniversalRAG / HetaRAG-style**  
  Multi-source RAG is modeled with:
  - **Unified retrieval plane**: vector indices, knowledge graphs, full-text, and structured DBs behind one query interface.
  - **Modality- and granularity-aware routing**: choose which corpus and which granularity (e.g. doc vs chunk) per query.
  - **Semantic-aware indexing** and optional **distributed processing** for scale.

- **MMORE (Massive Multimodal Open RAG)**  
  Single pipeline for 15+ types (text, tables, images, email, audio, video) with a **unified intermediate format** and modular, parallel processing.

- **Common themes**  
  - One **connector/reader per source** that emits a **normalized item** (text + metadata + identity).  
  - **Chunking** and **embedding** as shared steps after loading.  
  - **Single retrieval/index layer** (or a small set of backends) that doesn’t care about the original system.  
  - **Scope/filters** per source (folder, label, channel, repo) configured in one place.

### 1.2 Implications for Genie

- Treat each of Drive, Gmail, Linear, GitHub, Slack, (and optionally Calendar) as a **data source** with:
  - A **connector** that can list/enumerate items and fetch content in scope.
  - A **normalized shape**: stable ID, source name, updated time, content/snippet, metadata (e.g. `product`, `category` for buckets).
- One **sync/ingest** pipeline that iterates over enabled sources, pulls new/changed items, and writes into the vector store (using existing **Upsert** by `source:external_id`).
- **Search and recap** operate on “all data sources” or a subset, not on each integration ad hoc.

---

## 2. What trpc-agent-go Provides Out of the Box

Genie depends on **trpc-agent-go** for the knowledge/vector side. From the codebase and public package docs:

### 2.1 Knowledge Package

- **`knowledge/document`**  
  - **Document** type: ID, Content (string), Metadata (map).  
  - Used when **adding** to the vector store: Genie builds `document.Document` from `vector.BatchItem` (ID, Text, Metadata) and passes it with the embedding.

- **`knowledge/document/reader`**  
  - **File-format readers** (CSV, JSON, Markdown, Text) registered by extension.  
  - Genie’s **runbook** loader uses these to read from the filesystem and produce content that is then turned into `BatchItem`s and stored.  
  - There is **no** built-in notion of “Slack reader” or “Gmail reader”; those would be Genie-level connectors that eventually produce the same document/BatchItem shape.

- **`knowledge/embedder`**  
  - **Embedder** interface: `GetEmbedding(ctx, text string) ([]float64, error)`.  
  - Implementations: OpenAI, Ollama (OpenAI-compatible), HuggingFace TEI, Gemini.  
  - Genie wires this via `vector.Config` (embedding_provider, api keys, etc.) and uses it inside the vector store on **Add** and **Search** (query embedding).

- **`knowledge/vectorstore`**  
  - **VectorStore** interface: `Add(ctx, doc *document.Document, embedding []float64)`, `Search(ctx, query)`, `Delete(ctx, id)`, `Get(ctx, id)`, `GetMetadata(ctx)`, `Close()`.  
  - **SearchQuery**: vector, limit, optional **SearchFilter** (metadata key-value).  
  - Backends: **inmemory** (optional JSON snapshot), **Qdrant**.  
  - No built-in “data source” or “connector” abstraction; trpc-agent-go gives **storage + embedder**, not ingestion from external APIs.

### 2.2 What Genie Adds Today

- **`pkg/memory/vector`**  
  - Wraps trpc-agent-go’s VectorStore + Embedder.  
  - **IStore**: Add, Upsert (delete-then-add by ID), Delete, Search, SearchWithFilter.  
  - **BatchItem**: ID, Text, Metadata (e.g. `type`, `source`, and user-defined keys via **AllowedMetadataKeys**).  
  - **memory_store** / **memory_search** tools: agent can store/retrieve by text and optional metadata filter; optional stable **id** for upsert.

- **`pkg/runbook`**  
  - Example of a “data source” in the sense used here: filesystem paths → document readers → **BatchItem** (id = `runbook:<path>`, metadata = type/source) → **vectorStore.Add**.  
  - Runbook watcher can re-load and re-add; with stable IDs this could be upsert for “updated docs” behavior.

So: **trpc-agent-go provides the vector store, embedder, and document shape**. It does **not** provide connectors for Slack, Drive, Gmail, etc., or a unified “data source” abstraction. Genie would define the **data source** contract and implement one connector per system (Drive, Gmail, Linear, GitHub, Slack, Calendar), each emitting items that are then **vectorized** and stored via the existing store.

---

## 3. What It Means to “Vectorize” a Data Source

**Vectorizing** here means: turning **items** from a data source (files, messages, issues, events) into **vectors** stored in the same vector store the agent already uses for memory and runbooks, so they can be found by **semantic search** (and optional metadata filter).

### 3.1 Steps

1. **Item**  
   One unit from the source: e.g. one Drive file, one Gmail message, one Linear issue, one Slack message or thread, one calendar event. Each has:
   - A **stable ID** (e.g. `gdrive:<fileId>`, `gmail:<messageId>`, `linear:<issueId>`, `slack:<channelId>:<ts>`).
   - **Content** to search over (title + body, snippet, or full text).
   - **Metadata** (source, updated_at, author, labels, etc.; can include product/category when **AllowedMetadataKeys** is used).
   - Optionally **updated_at** (or equivalent) for incremental sync.

2. **Normalize**  
   Map the item into a **normalized record** (e.g. ID, source, updated_at, content string, metadata map). This is the output of the “connector” and the input to the next step.

3. **Chunk (optional)**  
   For long content, split into chunks (e.g. by token limit or semantic boundaries). Each chunk can be stored as a separate vector with metadata pointing back to the parent (e.g. `parent_id`, `chunk_index`). Genie’s runbooks today often store one vector per file; for very long docs or threads, chunking improves recall.

4. **Embed**  
   Call the **embedder** (`GetEmbedding(ctx, text)`) on the content (or each chunk). The embedder is the same one used for memory_search and runbook search (trpc-agent-go/knowledge/embedder).

5. **Store**  
   **Add** or **Upsert** into the vector store with:
   - **ID**: stable key (e.g. `source:external_id` or `source:external_id#chunk_2`).
   - **Document**: Content (and metadata).
   - **Embedding**: the vector from step 4.

After that, **memory_search** (and SearchWithFilter) can retrieve by semantic similarity and by metadata (e.g. `source=gdrive`, `product=ai-sre`), without the agent needing to call each integration’s API for every question.

### 3.2 Why Do It

- **Search across everything**: “What did we promise GEICO?” can hit Drive, Gmail, Linear, Slack in one semantic search instead of the agent calling five tools.
- **Recap and prep**: Daily recap or “Prep Deck” can pull from a pre-vectorized corpus (plus live API calls if needed for freshness) instead of only on-demand tool use.
- **Overwrite when appropriate**: Using **Upsert** with stable IDs, re-syncing a source replaces old content so the store stays current (e.g. “periodically check if docs updated and re-embed”).

---

## 4. Vision: Unified Data Sources in Genie

### 4.1 Principles

- **One abstraction**: “Data source” = something that can be **enabled and scoped** in config, **enumerated** (list items in scope), and **turned into normalized items** (ID, content, metadata, updated_at) for vectorization.
- **Reuse existing pieces**: Same vector store (and Upsert, AllowedMetadataKeys), same embedder, same **BatchItem**/document shape. New work is: **data source interface**, **connectors** (Drive, Gmail, Linear, GitHub, Slack, Calendar), **sync/ingest job**, and optional **unified search** tool or recap flow.
- **Config in one place**: e.g. a `data_sources` (or per-source) section where each source has:
  - Enabled (bool) and scope (folder ID, label, team, repo, channels, calendar ID).
  - Optional sync schedule (e.g. every 15 minutes, or on-demand only).
- **Incremental sync**: Where APIs support it (e.g. Drive modifiedTime, Gmail history, Linear updated_at), only fetch new/changed items and **Upsert** by stable ID so vector store reflects updates.

### 4.2 Proposed Data Source Contract (Conceptual)

A **DataSource** could look like:

- **Name()** → string (e.g. `gdrive`, `gmail`, `linear`, `github`, `slack`, `calendar`).
- **ListItems(ctx, scope)** → slice of **NormalizedItem** (ID, Source, UpdatedAt, Content, Metadata).  
  Scope is source-specific (e.g. folder ID, label, channel IDs).  
  Optional: **ListItemsSince(ctx, scope, since time)** for incremental sync.
- **Auth/config** is per source (existing OAuth, API tokens, etc.); the connector hides that behind the interface.

**NormalizedItem** (minimal):

- **ID**: string, stable and unique across all sources (e.g. `gdrive:fileId`, `slack:channelId:ts`).
- **Source**: string (same as Name()).
- **UpdatedAt**: time.Time (for incremental sync and ordering).
- **Content**: string (text to embed; can be snippet or full body).
- **Metadata**: map[string]string (e.g. title, author, type; can include product/category when allowed).

The **sync pipeline** would:

1. For each enabled data source and its scope, call **ListItems** (or **ListItemsSince** if supported).
2. For each item, build **BatchItem** (ID = item.ID, Text = item.Content, Metadata = map including source, updated_at, and any allowed keys).
3. Call **vectorStore.Upsert(ctx, batch)** so updated items overwrite old ones.

The **agent** can then use **memory_search** (with optional filter by source/product/category) to query across all vectorized data, and existing tools (e.g. Drive read, Gmail get) when it needs live or detailed access.

### 4.3 Scope of Initial Work

- **Doc (this file)**: Vision and “what vectorize means” — done.
- **Design**: Formalize **DataSource** (or equivalent) interface and **NormalizedItem** in a design doc or ADR; decide where it lives (e.g. `pkg/datasource` or under a new top-level package).
- **Config**: Add a way to enable/scope each source (could be under existing `[google_drive]` etc. plus a `[data_sources]` that references them, or a single unified section).
- **Connectors**: Implement one connector per system (Slack first for “Slack as data source,” then optionally Drive/Gmail/Linear/GitHub/Calendar) that implements the contract and returns NormalizedItems.
- **Sync**: A scheduled job (cron or daemon) that runs the pipeline above; optional “on-demand sync” via a tool or admin API.
- **Search/recap**: Keep using **memory_search** (+ filters); optional “search all data sources” tool that is a thin wrapper with a default filter set. Recap (Prep Deck / Delta / Brainstorm) can consume vectorized data plus live API calls.

### 4.4 What Stays Unchanged

- **trpc-agent-go**: Continue using its VectorStore, Embedder, and Document as-is. No change to the library.
- **Vector store and tools**: Existing **Add**, **Upsert**, **Search**, **SearchWithFilter**, **AllowedMetadataKeys**, and **memory_store** / **memory_search** stay; they are the sink for vectorized data.
- **Existing tools**: Drive read, Gmail list/get, Calendar list, Linear search, etc. remain for real-time and detailed use; the data source layer is about **bulk ingestion for search**, not replacing those tools.

---

## 5. References

- LlamaIndex: [Loading Data (Ingestion)](https://docs.llamaindex.ai/en/stable/understanding/loading/loading/), [Data Connectors (LlamaHub)](https://docs.llamaindex.ai/en/stable/module_guides/loading/connector/root.html).
- RAG chunking/embedding: e.g. [Chunking and Embedding (Mastra)](https://mastra.ai/en/docs/rag/chunking-and-embedding), [RAG Pipelines (Vectorize)](https://docs.vectorize.io/core-concepts/rag-pipelines).
- Multi-source RAG: UniversalRAG, HetaRAG, MMORE (see web search summaries in §1).
- Genie: `pkg/memory/vector` (store, tools, config), `pkg/runbook` (loader as a “file” data source), trpc-agent-go `knowledge/document`, `knowledge/embedder`, `knowledge/vectorstore`.
