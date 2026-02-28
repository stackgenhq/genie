// Package datasource defines the unified data source abstraction for Genie.
// This file provides persistence of last-sync time per source for incremental sync.

package datasource

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const syncStateFilename = "datasource_sync_state.json"

// SyncState holds the last successful sync time per source name.
// It is persisted as JSON so incremental sync can resume after restarts.
// Only one Genie instance should use a given working directory; no file
// locking is performed, so multiple instances sharing the same dir can
// overwrite or corrupt the state file.
type SyncState map[string]string

// LoadSyncState reads the sync state from the given directory.
// The file is syncStateFilename inside dir. Returns nil map and nil error if
// the file does not exist (first run). Callers can treat missing file as
// "no previous sync" and perform a full ListItems.
func LoadSyncState(dir string) (SyncState, error) {
	path := filepath.Join(dir, syncStateFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sync state: %w", err)
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse sync state: %w", err)
	}
	return SyncState(raw), nil
}

// SaveSyncState writes the sync state to the given directory.
// Creates the directory if needed. The file is syncStateFilename inside dir.
func SaveSyncState(dir string, state SyncState) error {
	if state == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create sync state dir: %w", err)
	}
	path := filepath.Join(dir, syncStateFilename)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode sync state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write sync state: %w", err)
	}
	return nil
}

// LastSync returns the last sync time for the given source, or zero time if
// not present or unparseable. State values are RFC3339 strings.
func (s SyncState) LastSync(source string) time.Time {
	if s == nil {
		return time.Time{}
	}
	str, ok := s[source]
	if !ok || str == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, str)
	if err != nil {
		return time.Time{}
	}
	return t
}

// SetLastSync records the last sync time for the given source (RFC3339).
// Modifies the map in place; call SaveSyncState to persist.
func (s SyncState) SetLastSync(source string, t time.Time) {
	if s == nil {
		return
	}
	s[source] = t.UTC().Format(time.RFC3339)
}
