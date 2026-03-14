// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package datasource defines the unified data source abstraction for Genie:
// connectors that enumerate items from external systems (Drive, Gmail, Slack,
// Linear, GitHub, Calendar) and produce normalized items for vectorization.
// The sync pipeline consumes these items and upserts them into the vector
// store so memory_search can query across all sources.
package datasource

import (
	"context"
	"strings"
	"time"
)

//go:generate go tool counterfeiter -generate

// DataSource is the contract for a connector that can list items from an
// external system in a normalized shape. Each connector (Slack, Drive, Gmail,
// etc.) implements this interface; the sync job calls ListItems (or
// ListItemsSince when supported) and turns results into vector.BatchItem for
// Upsert. Auth and credentials are per-source and handled by the connector;
// this interface is scope and item enumeration only.
//
//counterfeiter:generate . DataSource
type DataSource interface {
	// Name returns the source identifier (e.g. "gdrive", "gmail", "slack", "linear", "github", "calendar").
	// It is used as the "source" field in NormalizedItem and in metadata for filtering.
	Name() string

	// ListItems returns all items in scope for this source. Scope is
	// source-specific (e.g. folder IDs, channel IDs, label IDs) and is
	// supplied when the connector is constructed from config. The caller
	// (sync pipeline) then maps each NormalizedItem to a vector.BatchItem
	// and upserts into the vector store.
	ListItems(ctx context.Context, scope Scope) ([]NormalizedItem, error)
}

// ListItemsSince optionally supports incremental sync. Connectors that
// implement it can return only items updated after the given time.
// The sync pipeline can use this to avoid re-processing unchanged items.
type ListItemsSince interface {
	DataSource
	// ListItemsSince returns items in scope that were updated after the given time.
	ListItemsSince(ctx context.Context, scope Scope, since time.Time) ([]NormalizedItem, error)
}

// SourceRef identifies the origin of an item for lookup and verification.
// When returning search results, consumers can use Type + RefID to open or
// fetch the original (e.g. Gmail message, Drive file) and avoid hallucination.
type SourceRef struct {
	// Type is the source type (e.g. "gmail", "gdrive", "slack", "linear", "github", "calendar").
	Type string
	// RefID is the ID in that system to look up the original (e.g. message ID, file ID, channel:ts).
	RefID string
}

// NormalizedItem is the common shape produced by every data source connector.
// It has a stable ID (used for Upsert), source name, content to embed, and
// metadata for filtering. The sync pipeline maps this to vector.BatchItem
// and stores source_type + source_ref_id in metadata so retrieval can show origin.
type NormalizedItem struct {
	// ID is stable and unique across all sources (e.g. "gdrive:fileId", "slack:channelId:ts").
	ID string
	// Source is the same as DataSource.Name() (e.g. "gdrive", "slack").
	Source string
	// SourceRef identifies the origin for source material lookup (Type + RefID).
	// When set, sync stores source_type and source_ref_id in vector metadata so
	// search results can cite and verify the original (e.g. open Gmail message, Drive file).
	SourceRef *SourceRef
	// UpdatedAt is used for incremental sync and ordering.
	UpdatedAt time.Time
	// Content is the text to embed (title + body, snippet, or full text).
	Content string
	// Metadata holds optional keys (title, author, type, product, category, etc.)
	// that can be used for SearchWithFilter when AllowedMetadataKeys permits.
	Metadata map[string]string
}

// SourceRefID returns the ref ID for source material lookup. Uses item.SourceRef.RefID
// when set; otherwise derives from item.ID (e.g. "gmail:msg123" -> "msg123").
func (item *NormalizedItem) SourceRefID() string {
	if item == nil {
		return ""
	}
	if item.SourceRef != nil && item.SourceRef.RefID != "" {
		return item.SourceRef.RefID
	}
	if idx := strings.Index(item.ID, ":"); idx >= 0 && idx < len(item.ID)-1 {
		return item.ID[idx+1:]
	}
	return item.ID
}

// SourceConfig is the interface that all data source configuration types must
// implement. It enables generic scope building and source enumeration without
// per-source switch statements. Adding a new data source only requires a new
// config type that implements this interface.
type SourceConfig interface {
	// IsEnabled returns true when this source is turned on and has meaningful scope.
	IsEnabled() bool
	// ScopeValues returns the scope items for this source (e.g. folder IDs,
	// channel IDs, project keys). An empty slice means no scope is configured.
	ScopeValues() []string
}

// Scope is the source-specific scope passed to ListItems. It uses a generic
// map keyed by source name so adding new sources requires no struct changes.
// Use NewScope to create and Get to read.
type Scope struct {
	// Items maps source name → scope values (e.g. "gdrive" → folder IDs,
	// "slack" → channel IDs, "jira" → project keys).
	Items map[string][]string
}

// NewScope creates a Scope for a single source.
func NewScope(source string, values []string) Scope {
	return Scope{Items: map[string][]string{source: values}}
}

// Get returns the scope values for the given source name. Returns nil when the
// source has no scope configured.
func (s Scope) Get(source string) []string {
	if s.Items == nil {
		return nil
	}
	return s.Items[source]
}
