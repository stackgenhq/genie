package graph

import (
	"path/filepath"

	"github.com/stackgenhq/genie/pkg/osutils"
)

// Config holds configuration for the knowledge graph store. When Disabled is
// false and DataDir is set, the in-memory store persists to DataDir/memory.bin.zst
// (gob+zstd). Empty DataDir means no persistence (in-memory only). The implementation
// is interface-driven so DataDir can be ignored by a future backend (e.g. Milvus).
type Config struct {
	// Disabled turns off the knowledge graph and graph_* tools. When true, no
	// graph store is created and no graph tools are registered.
	Disabled bool `yaml:"disabled,omitempty" toml:"disabled,omitempty"`
	// DataDir is the directory for persistence (e.g. ~/.genie/<agent>). The
	// in-memory implementation writes memory.bin.zst (gob+zstd) here. Empty means no persistence.
	DataDir string `yaml:"data_dir,omitempty" toml:"data_dir,omitempty"`
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

// GraphLearnPendingFilename is the name of the file written by setup when the
// user opts into "build knowledge graph from data"; when present, the app runs
// one graph-learn pass after the first successful data sources sync.
const GraphLearnPendingFilename = "graph_learn_pending"

// PendingGraphLearnPath returns the path to the graph-learn pending flag file
// for the given agent. Setup writes this file when the user opts in; the app
// removes it after running the one-time graph-learn pass.
func PendingGraphLearnPath(agentName string) string {
	return filepath.Join(DataDirForAgent(agentName), GraphLearnPendingFilename)
}
