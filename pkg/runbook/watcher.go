package runbook

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/memory/vector"
	"github.com/fsnotify/fsnotify"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader"
)

const (
	// debounceInterval is how long the watcher waits after the last event
	// before flushing batched changes. This prevents N disk writes when
	// many files change at once (e.g. git pull, cp -r).
	debounceInterval = 500 * time.Millisecond
)

// pendingEvent tracks a buffered file change before it is flushed.
type pendingEvent struct {
	path    string // absolute path to the file
	deleted bool   // true if the file was removed/renamed
}

// Watcher monitors runbook directories for file changes and keeps
// the vector store in sync. Events are debounced (500ms) and flushed
// in batch to avoid I/O thrashing on bulk updates.
type Watcher struct {
	loader     *Loader
	store      vector.IStore
	watcher    *fsnotify.Watcher
	dirs       []string
	workingDir string
}

// NewWatcher creates a Watcher that monitors the given directories for
// runbook file changes. The loader is used to read file content and the
// store is used to add/remove documents.
func NewWatcher(loader *Loader, store vector.IStore, dirs []string) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("error creating watchers: %w", err)
	}
	logr := logger.GetLogger(context.Background()).With("fn", "runbook.NewWatcher")

	w := &Watcher{
		loader:     loader,
		store:      store,
		watcher:    fw,
		dirs:       dirs,
		workingDir: loader.workingDirectory,
	}

	// Watch each directory (and subdirectories) that exists.
	for _, dir := range dirs {
		if err := w.watchRecursive(dir); err != nil {
			if err := fw.Close(); err != nil {
				logr.Warn("failed to close watcher", "error", err)
			}
		}
	}

	return w, nil
}

// watchRecursive adds a directory and all its subdirectories to the watcher.
func (w *Watcher) watchRecursive(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible paths
		}
		if d.IsDir() {
			return w.watcher.Add(path)
		}
		return nil
	})
}

// Start begins processing file system events with debouncing. It blocks
// until the context is cancelled. Call this in a goroutine.
func (w *Watcher) Start(ctx context.Context) {
	logr := logger.GetLogger(ctx).With("fn", "runbook.Watcher.Start")
	logr.Info("runbook watcher started", "dirs", w.dirs)

	// pending collects events keyed by absolute path. The last event
	// for each path wins, so rapid Create→Write→Write collapses to one add.
	pending := make(map[string]*pendingEvent)
	var timer *time.Timer

	resetTimer := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.NewTimer(debounceInterval)
	}

	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			// Flush any remaining events before exiting.
			if len(pending) > 0 {
				w.flush(ctx, pending)
			}
			logr.Info("runbook watcher stopped")
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// For new directories, start watching them too.
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if err := w.watcher.Add(event.Name); err != nil {
						logr.Warn("failed to watch new directory", "error", err)
					}
					continue
				}
			}

			// Only buffer files with supported extensions.
			if !w.isSupportedExtension(event.Name) {
				continue
			}

			switch {
			case event.Has(fsnotify.Create) || event.Has(fsnotify.Write):
				pending[event.Name] = &pendingEvent{path: event.Name, deleted: false}
			case event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename):
				pending[event.Name] = &pendingEvent{path: event.Name, deleted: true}
			}
			resetTimer()

		case <-func() <-chan time.Time {
			if timer != nil {
				return timer.C
			}
			return make(chan time.Time) // never fires
		}():
			if len(pending) > 0 {
				w.flush(ctx, pending)
				pending = make(map[string]*pendingEvent)
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			logr.Warn("runbook watcher error", "error", err)
		}
	}
}

// flush processes all buffered events in batch, calling AddBatch and
// DeleteBatch on the store to minimize disk I/O.
func (w *Watcher) flush(ctx context.Context, pending map[string]*pendingEvent) {
	logr := logger.GetLogger(ctx).With("fn", "runbook.Watcher.flush",
		"count", len(pending))

	var toAdd []vector.BatchItem
	var toDelete []string

	for _, ev := range pending {
		id := RunbookID(w.workingDir, ev.path)
		name := filepath.Base(ev.path)

		if ev.deleted {
			toDelete = append(toDelete, id)
			logr.Info("runbook queued for removal", "file", name)
			continue
		}

		content, err := w.loader.readFile(ev.path)
		if err != nil {
			logr.Warn("failed to read changed runbook", "path", ev.path, "error", err)
			continue
		}
		if content == "" {
			continue
		}

		// Delete first in case it already exists (update = delete + re-add).
		toDelete = append(toDelete, id)
		toAdd = append(toAdd, vector.BatchItem{
			ID:   id,
			Text: content,
			Metadata: map[string]string{
				"type":   "runbook",
				"source": name,
			},
		})
		logr.Info("runbook queued for ingestion", "file", name)
	}

	// Execute deletes first, then adds.
	if len(toDelete) > 0 {
		if err := w.store.Delete(ctx, toDelete...); err != nil {
			logr.Warn("failed to delete runbooks", "error", err)
		}
	}
	if len(toAdd) > 0 {
		if err := w.store.Add(ctx, toAdd...); err != nil {
			logr.Warn("failed to add runbooks", "error", err)
		}
	}

	logr.Info("runbook watcher flush complete", "added", len(toAdd), "deleted", len(toDelete))
}

// Close stops the file system watcher.
func (w *Watcher) Close() error {
	return w.watcher.Close()
}

// isSupportedExtension checks whether a file's extension is registered
// in the trpc-agent-go reader registry.
func (w *Watcher) isSupportedExtension(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == "" {
		return false
	}
	_, ok := reader.GetReader(ext, reader.WithChunk(false))
	return ok
}
