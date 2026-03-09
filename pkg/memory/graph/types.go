// Package graph provides a knowledge graph store (entities and relations) for
// agent memory. The store is interface-driven so backends can be swapped
// (in-memory now; Trigo or SQLite later). When persistence is
// configured, the in-memory implementation snapshots to ~/.genie/<agent>/memory.bin.zst (gob+zstd).
package graph

// Entity represents a node in the knowledge graph with an id, type, and
// optional JSON attributes. Used for people, repos, issues, documents, etc.
type Entity struct {
	ID    string            `json:"id"`
	Type  string            `json:"type"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Relation represents a directed edge: subject --[predicate]--> object.
// Example: ("person-1", "WORKED_ON", "issue-2").
type Relation struct {
	SubjectID string `json:"subject_id"`
	Predicate string `json:"predicate"`
	ObjectID  string `json:"object_id"`
}

// Neighbor is an entity reachable from a given node (1-hop) with the
// predicate that connects them. Used for graph_query tool results.
type Neighbor struct {
	Entity    Entity `json:"entity"`
	Predicate string `json:"predicate"`
	Outgoing  bool   `json:"outgoing"` // true = from subject to this entity, false = from this entity to object
}
