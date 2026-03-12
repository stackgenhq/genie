// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package graph

import (
	"context"
)

//go:generate go tool counterfeiter -generate

// IStore is the interface for the knowledge graph store. All callers use this
// interface so the implementation can be swapped (in-memory, Trigo,
// SQLite, etc.) without changing tool or app code.
//
//counterfeiter:generate . IStore
type IStore interface {
	// AddEntity stores an entity by id; overwrites if id exists.
	AddEntity(ctx context.Context, e Entity) error
	// AddRelation stores a directed relation; idempotent (same triple is a no-op).
	AddRelation(ctx context.Context, r Relation) error
	// DeleteEntity removes an entity and all its incident relations by ID.
	// Returns nil if the entity does not exist (idempotent).
	DeleteEntity(ctx context.Context, id string) error
	// DeleteRelation removes a specific directed relation triple.
	// Returns nil if the relation does not exist (idempotent).
	DeleteRelation(ctx context.Context, r Relation) error
	// GetEntity returns the entity by id, or nil if not found.
	GetEntity(ctx context.Context, id string) (*Entity, error)
	// RelationsOut returns relations where subject_id equals id (outgoing edges).
	RelationsOut(ctx context.Context, id string) ([]Relation, error)
	// RelationsIn returns relations where object_id equals id (incoming edges).
	RelationsIn(ctx context.Context, id string) ([]Relation, error)
	// Neighbors returns entities reachable in one hop from id (outgoing and
	// incoming), with the connecting predicate and direction. Limit caps the
	// total number of neighbors returned.
	Neighbors(ctx context.Context, id string, limit int) ([]Neighbor, error)
	// ShortestPath returns the shortest path of entity IDs from source to target.
	// Returns nil path and nil error if no path exists. Returns an error if
	// source or target vertex does not exist. Not all implementations may support
	// this; they may return an error.
	ShortestPath(ctx context.Context, sourceID, targetID string) ([]string, error)
	// DeleteAll removes all entities and relations from the graph.
	// This is a destructive operation — use with caution.
	DeleteAll(ctx context.Context) error
	// Close releases resources (e.g. flush snapshot, close connection).
	Close(ctx context.Context) error
}
