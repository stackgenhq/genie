// Package vector provides the vector store used for Genie's semantic memory:
// embedding and searching over documents (runbooks, synced data from Drive,
// Gmail, Slack, etc.) so the agent can retrieve relevant context via memory_search.
//
// It solves the problem of giving the agent access to large, heterogeneous
// corpora: items are embedded, stored in an IStore (in-memory or Qdrant),
// and queried by semantic similarity. The sync pipeline upserts NormalizedItems
// from data sources; tools expose search and (when configured) delete. Without
// this package, the agent would have no persistent, searchable memory across runs.
package vector
