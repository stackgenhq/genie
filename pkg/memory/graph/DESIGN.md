# Knowledge graph storage design

## In-memory structure: dominikbraun/graph

The store uses **[dominikbraun/graph](https://pkg.go.dev/github.com/dominikbraun/graph)** for the in-memory graph:

- **Vertices** = `Entity` (hash = `Entity.ID`).
- **Edges** = directed (subject → object); multiple predicates per (subject, object) are stored in `EdgeProperties.Data` as `edgePredicates` ([]string).

This gives:

- Path finding via `ShortestPath` (and the library’s DFS, BFS, etc. if needed).
- Same snapshot format (entities + relations, gob+zstd to `memory.bin.zst`).
- Relations that reference missing entities auto-create placeholder vertices (type `"unknown"`) so edges can be added.

Alternatives considered:

| Option | Pros | Cons |
|--------|------|------|
| **EliasDB** (pure Go) | Full graph DB, indexes, EQL | Different data model and API; heavier; would require an adapter over our `IStore`. |
| **Kuzu** (C++ + Go bindings) | CSR/columnar layout, compression, Cypher | CGO, large dependency; overkill for typical agent graph size. |
| **Memgraph / RageDB** | Mature graph engines | External server; we need an embedded, process-local store. |

## Persistence: always gob + Zstandard

Persistence is **always** gob-encoded then Zstandard-compressed to `memory.bin.zst`. There is no JSON or other format. Save writes `memory.bin.zst`; load reads it (missing file is treated as empty store). The project uses `github.com/klauspost/compress/zstd`.
