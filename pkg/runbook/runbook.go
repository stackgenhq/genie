// Package runbook provides a loader for customer-provided instructional files ("runbooks")
// using the trpc-agent-go document reader framework. Runbooks are loaded from configured
// file paths or directories and their content is injected into the agent's system prompt,
// giving customers a way to define instructions that Genie must follow (e.g., deployment
// procedures, coding standards, troubleshooting playbooks).
//
// Without this package, there would be no mechanism for customers to supply structured,
// multi-format instructions beyond the single Agents.md file.
package runbook

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/osutils"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader"

	// Register document readers for supported file formats via init().
	_ "trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader/csv"
	_ "trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader/json"
	_ "trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader/markdown"
	_ "trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader/text"
)

type Runbook interface {
	Load(ctx context.Context) (int, error)
	KeepWatching(ctx context.Context)
}

const (
	// conventionDir is the auto-discovered runbook directory inside
	// the working directory. This follows convention-over-configuration:
	// customers can simply drop files here without touching config.
	conventionDir = ".genie/runbooks"
)

// Config holds the runbook loader configuration. Customers can specify
// explicit paths via config files or rely on the convention directory
// (.genie/runbooks/) in the working directory.
type Config struct {
	// Paths is a list of file paths or directories containing runbook files.
	// Both absolute and relative paths are supported. When a directory is
	// specified, all supported files within it are loaded (non-recursive).
	Paths []string `yaml:"runbook_paths,omitempty" toml:"runbook_paths,omitempty"`
}

// Loader reads runbook files from the filesystem using the trpc-agent-go
// document reader registry. It resolves the appropriate reader for each file
// based on its extension and concatenates the content with per-file headers.
type Loader struct {
	config           Config
	workingDirectory string
	vectorStore      vector.IStore
}

// NewLoader creates a new runbook Loader scoped to the given working directory
// and configuration. The working directory is used to resolve relative paths
// and to auto-discover the .genie/runbooks/ convention directory.
// Without this constructor, callers would need to manually assemble paths
// and reader selection logic.
func NewLoader(workingDirectory string, cfg Config, vectorStore vector.IStore) *Loader {
	return &Loader{
		config:           cfg,
		workingDirectory: workingDirectory,
		vectorStore:      vectorStore,
	}
}

func (l *Loader) KeepWatching(ctx context.Context) {
	// Start a file watcher to keep runbooks in sync with the vector store.
	if watcher, err := NewWatcher(l, l.vectorStore, l.WatchDirs()); err != nil {
		logger.GetLogger(ctx).Warn("failed to start runbook watcher", "error", err)
	} else {
		go watcher.Start(ctx)
	}
}

// Load discovers all runbook files and ingests them into the vector store
// for semantic search via the search_runbook tool. Returns the number of
// files successfully ingested.
func (l *Loader) Load(ctx context.Context) (int, error) {
	if l.vectorStore == nil {
		return 0, fmt.Errorf("vector store is not initialized")
	}

	runbookFiles := l.LoadFiles(ctx)
	if len(runbookFiles) == 0 {
		return 0, nil
	}

	logr := logger.GetLogger(ctx).With("fn", "runbook.Loader.Load")
	items := make([]vector.BatchItem, 0, len(runbookFiles))
	for _, rbFile := range runbookFiles {
		id := RunbookID(l.workingDirectory, rbFile.Path)
		items = append(items, vector.BatchItem{
			ID:   id,
			Text: rbFile.Content,
			Metadata: map[string]string{
				"type":   "runbook",
				"source": rbFile.Name,
			},
		})
	}

	if err := l.vectorStore.Add(ctx, items...); err != nil {
		logr.Warn("failed to ingest runbooks into vector store", "error", err)
		return 0, err
	}

	logr.Info("Runbooks ingested into vector store", "count", len(items))
	return len(items), nil
}

// RunbookID returns a stable, deterministic ID for a runbook file
// based on its path relative to the working directory. This prevents
// ID collisions when files with the same name exist in different
// directories (e.g. ops/deploy.md vs dev/deploy.md).
func RunbookID(workingDir, fullPath string) string {
	rel, err := filepath.Rel(workingDir, fullPath)
	if err != nil {
		// Fallback to base name if we can't compute relative path.
		rel = filepath.Base(fullPath)
	}
	return fmt.Sprintf("runbook:%s", rel)
}

// WatchDirs returns the resolved directories that should be watched
// for runbook file changes. It includes the convention directory and
// any configured directory paths.
func (l *Loader) WatchDirs() []string {
	seen := make(map[string]bool)
	var dirs []string

	// Convention directory.
	convDir := filepath.Join(l.workingDirectory, conventionDir)
	if info, err := os.Stat(convDir); err == nil && info.IsDir() {
		seen[convDir] = true
		dirs = append(dirs, convDir)
	}

	// Configured paths that are directories.
	for _, p := range l.config.Paths {
		absPath := p
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(l.workingDirectory, absPath)
		}
		info, err := os.Stat(absPath)
		if err != nil || !info.IsDir() {
			continue
		}
		if !seen[absPath] {
			seen[absPath] = true
			dirs = append(dirs, absPath)
		}
	}

	return dirs
}

// RunbookFile represents a single loaded runbook with its source path
// and text content. Used by LoadFiles to return individual entries for
// vector store ingestion.
type RunbookFile struct {
	Name    string // base filename, e.g. "deployment-guide.md"
	Path    string // absolute path to the file
	Content string // full text content of the runbook
}

// LoadFiles reads all configured runbook files and returns them as
// individual RunbookFile entries. Unlike Load(), which concatenates
// everything into a single string for persona injection, LoadFiles
// preserves per-file boundaries so callers can ingest each file
// separately (e.g. into a vector store).
func (l *Loader) LoadFiles(ctx context.Context) []RunbookFile {
	logr := logger.GetLogger(ctx).With("fn", "runbook.Loader.LoadFiles")

	files := l.discoverFiles(ctx)
	if len(files) == 0 {
		logr.Debug("no runbook files found")
		return nil
	}

	var result []RunbookFile

	for _, filePath := range files {
		content, err := l.readFile(filePath)
		if err != nil {
			logr.Warn("failed to read runbook file, skipping",
				"path", filePath, "error", err)
			continue
		}
		if content == "" {
			continue
		}

		result = append(result, RunbookFile{
			Name:    filepath.Base(filePath),
			Path:    filePath,
			Content: content,
		})
		logr.Info("runbook loaded", "path", filePath, "size", len(content))
	}

	return result
}

// discoverFiles collects all runbook file paths from both the convention
// directory (.genie/runbooks/) and explicitly configured paths. Duplicates
// are removed. Unsupported file extensions are excluded.
func (l *Loader) discoverFiles(ctx context.Context) []string {
	logr := logger.GetLogger(ctx).With("fn", "runbook.Loader.discoverFiles")
	seen := make(map[string]bool)
	var files []string

	// 1. Auto-discover convention directory.
	convDir := filepath.Join(l.workingDirectory, conventionDir)
	if info, err := os.Stat(convDir); err == nil && info.IsDir() {
		logr.Debug("auto-discovered runbook convention directory", "path", convDir)
		discovered := l.listSupportedFiles(convDir)
		for _, f := range discovered {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}

	// 2. Process explicitly configured paths.
	for _, p := range l.config.Paths {
		absPath := p
		if !filepath.IsAbs(absPath) {
			absPath = filepath.Join(l.workingDirectory, absPath)
		}

		info, err := os.Stat(absPath)
		if err != nil {
			logr.Warn("configured runbook path not found, skipping",
				"path", p, "resolved", absPath, "error", err)
			continue
		}

		if info.IsDir() {
			discovered := l.listSupportedFiles(absPath)
			for _, f := range discovered {
				if !seen[f] {
					seen[f] = true
					files = append(files, f)
				}
			}
			continue
		}

		// Single file.
		if !seen[absPath] && l.isSupportedExtension(absPath) {
			seen[absPath] = true
			files = append(files, absPath)
		}
	}

	return files
}

// listSupportedFiles returns all files in a directory (recursively) that have
// a supported extension (as registered in the trpc-agent-go reader registry).
func (l *Loader) listSupportedFiles(dir string) []string {
	entries, err := osutils.GetAllFiles(dir)
	if err != nil {
		return nil
	}

	var files []string
	for _, fullPath := range entries {
		if l.isSupportedExtension(fullPath) {
			files = append(files, fullPath)
		}
	}
	return files
}

// isSupportedExtension checks whether a file's extension is registered in
// the trpc-agent-go reader registry.
func (l *Loader) isSupportedExtension(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == "" {
		return false
	}
	_, ok := reader.GetReader(ext, reader.WithChunk(false))
	return ok
}

// readFile reads a single runbook file using the appropriate document reader
// and returns its text content. Content exceeding maxSize bytes is truncated.
func (l *Loader) readFile(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	r, ok := reader.GetReader(ext, reader.WithChunk(false))
	if !ok {
		return "", fmt.Errorf("no reader registered for extension %q", ext)
	}

	docs, err := r.ReadFromFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	// Concatenate all document content (some readers may split into chunks).
	var sb strings.Builder
	for _, doc := range docs {
		if doc == nil || doc.Content == "" {
			continue
		}
		sb.WriteString(doc.Content)
		sb.WriteString("\n")
	}

	content := strings.TrimSpace(sb.String())

	return content, nil
}
