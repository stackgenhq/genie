// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package graph

import (
	"path/filepath"
	"strings"

	"github.com/stackgenhq/genie/pkg/osutils"
)

// Config holds configuration for the knowledge graph store. When Disabled is
// false and DataDir is set, the in-memory store persists to DataDir/memory.bin.zst
// (gob+zstd). Empty DataDir means no persistence (in-memory only). The implementation
// is interface-driven so DataDir can be ignored by a future backend.
type Config struct {
	// Disabled turns off the knowledge graph and graph_* tools. When true, no
	// graph store is created and no graph tools are registered.
	Disabled bool `yaml:"disabled,omitempty" toml:"disabled,omitempty"`
	// Backend selects the knowledge graph storage backend. Options:
	//   "inmemory" (default) — in-process dominikbraun/graph with gob+zstd snapshot persistence.
	//   "vectorstore"        — reuses the configured vector store (Qdrant); entities and
	//                          relations are stored as vector documents with metadata discriminators.
	// When Backend is "vectorstore", DataDir is ignored (persistence is handled by the vector store).
	Backend string `yaml:"backend,omitempty" toml:"backend,omitempty"`
	// DataDir is the directory for persistence (e.g. ~/.genie/<agent>). The
	// in-memory implementation writes memory.bin.zst (gob+zstd) here. Empty means no persistence.
	// Ignored when Backend is "vectorstore".
	DataDir string `yaml:"data_dir,omitempty" toml:"data_dir,omitempty"`
}

// IsVectorStoreBackend returns true when the Backend field requests the
// vector-backed store (case-insensitive, trimmed). Used by app.go to decide
// whether to delegate graph storage to the shared vector store instance.
func (c Config) IsVectorStoreBackend() bool {
	return strings.EqualFold(strings.TrimSpace(c.Backend), "vectorstore")
}

// DefaultConfig returns a config with the graph enabled by default (Disabled: false)
// and no DataDir (no persistence). Callers typically set DataDir from GenieConfig
// (e.g. DataDir = filepath.Join(GenieDir(), SanitizeForFilename(agentName))) when persistence is desired.
func DefaultConfig() Config {
	return Config{
		Disabled: false,
		DataDir:  "",
	}
}

// DataDirForAgent returns a directory path suitable for graph persistence for
// the given agent name: ~/.genie/<sanitized_agent>/ (or ~/.genie/genie/ if
// agentName is empty). Callers can pass this as Config.DataDir.
func DataDirForAgent(agentName string) string {
	safe := osutils.SanitizeForFilename(agentName)
	if safe == "" {
		safe = "genie"
	}
	return filepath.Join(osutils.GenieDir(), safe)
}

// GraphLearnPendingFilename is the name of the legacy pending file written by
// the setup wizard. Kept for backwards compatibility; new code should use the
// stop-file approach instead.
//
// Deprecated: use GraphLearnStopFilename.
const GraphLearnPendingFilename = "graph_learn_pending"

// GraphLearnStopFilename is the name of a sentinel file that, when present in
// the agent's data directory, prevents the automatic graph-learn pass from
// running after data source syncs. By default graph learn runs on every
// app restart after the first successful sync; create this file to opt out.
const GraphLearnStopFilename = "graph_learn_stop"

// StopGraphLearnPath returns the path to the graph-learn stop file for the
// given agent. When this file exists, the automatic graph-learn pass is
// skipped. Remove the file to re-enable learning.
func StopGraphLearnPath(agentName string) string {
	return filepath.Join(DataDirForAgent(agentName), GraphLearnStopFilename)
}
