// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package gdrive

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

//go:generate go tool counterfeiter -generate

// Service defines the capabilities of the Google Drive connector. It gives
// the AI agent read-only access to search, list, and read files from Google
// Drive. Write operations are intentionally omitted to reduce blast radius.
//
//counterfeiter:generate . Service
type Service interface {
	// Search finds files matching a query string (Google Drive search syntax).
	Search(ctx context.Context, query string, maxResults int) ([]FileInfo, error)

	// ListFolder lists files in a specific folder by folder ID.
	ListFolder(ctx context.Context, folderID string, maxResults int) ([]FileInfo, error)

	// ListFolderModifiedSince lists files in a folder that were modified after the given time.
	// Used for incremental sync; the Drive API supports modifiedTime in the query.
	ListFolderModifiedSince(ctx context.Context, folderID string, since time.Time, maxResults int) ([]FileInfo, error)

	// GetFile returns metadata about a file.
	GetFile(ctx context.Context, fileID string) (*FileDetail, error)

	// ReadFile reads the text content of a file (Google Docs, Sheets, text).
	// For Google Docs/Sheets, it exports as plain text.
	// For binary files, it returns an error with the MIME type.
	ReadFile(ctx context.Context, fileID string) (string, error)

	// Validate performs a lightweight health check.
	Validate(ctx context.Context) error
}

// Config holds configuration for the Google Drive connector.
type Config struct {
	CredentialsFile string `yaml:"credentials_file,omitempty" toml:"credentials_file,omitempty"` // Path to service account JSON
	// Alternatively, set GOOGLE_APPLICATION_CREDENTIALS env var.

	// MaxDepth limits how deep recursive folder traversal goes during data
	// source sync. 0 or unset defaults to defaultMaxRecurseDepth (10).
	MaxDepth int `yaml:"max_depth,omitempty" toml:"max_depth,omitempty"`
}

// ── Domain Types ────────────────────────────────────────────────────────

// FileInfo describes a file or folder in Google Drive.
type FileInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MimeType     string `json:"mime_type"`
	Size         int64  `json:"size,omitempty"`
	ModifiedTime string `json:"modified_time,omitempty"`
	WebViewLink  string `json:"web_view_link,omitempty"`
	IsFolder     bool   `json:"is_folder"`
}

// FileDetail is an extended file description.
type FileDetail struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	MimeType     string   `json:"mime_type"`
	Size         int64    `json:"size,omitempty"`
	ModifiedTime string   `json:"modified_time,omitempty"`
	CreatedTime  string   `json:"created_time,omitempty"`
	Owners       []string `json:"owners,omitempty"`
	WebViewLink  string   `json:"web_view_link,omitempty"`
	IsFolder     bool     `json:"is_folder"`
}

// ── Request Types ───────────────────────────────────────────────────────

type searchRequest struct {
	Query      string `json:"query" jsonschema:"description=Google Drive search query (supports operators like name contains 'report' and mimeType='application/pdf'),required"`
	MaxResults int    `json:"max_results" jsonschema:"description=Maximum number of results (default 20)"`
}

type listFolderRequest struct {
	FolderID   string `json:"folder_id" jsonschema:"description=Google Drive folder ID (use 'root' for the root folder),required"`
	MaxResults int    `json:"max_results" jsonschema:"description=Maximum number of results (default 50)"`
}

type fileIDRequest struct {
	FileID string `json:"file_id" jsonschema:"description=Google Drive file ID,required"`
}

// ── Tool Constructors ───────────────────────────────────────────────────

type toolSet struct {
	s Service
}

func NewSearchTool(name string, s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.search,
		function.WithName(name+"_search"),
		function.WithDescription(
			"Search for files in Google Drive using the live Drive API. "+
				"IMPORTANT: If Google Drive data sources are synced, use memory_search with filter {\"source\": \"gdrive\"} FIRST — "+
				"it searches pre-indexed content and is faster. Only use this tool when memory_search returns no results, "+
				"you need real-time file listings, or you need to query by Drive-specific operators. "+
				"Results include file name, type, modified time, and link — present these directly without calling get_file for each result."),
	)
}

func NewListFolderTool(name string, s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listFolder,
		function.WithName(name+"_list_folder"),
		function.WithDescription(
			"List files in a Google Drive folder. Use 'root' for the root folder. "+
				"Results include file name, type, modified time, and link for each file. "+
				"Use this to browse folder contents; present results directly to the user."),
	)
}

func NewGetFileTool(name string, s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.getFile,
		function.WithName(name+"_get_file"),
		function.WithDescription(
			"Get detailed metadata about a specific Google Drive file including owners, created/modified dates, and links. "+
				"Only use this when you need owner or creation date info that search/list_folder don't provide."),
	)
}

func NewReadFileTool(name string, s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.readFile,
		function.WithName(name+"_read_file"),
		function.WithDescription(
			"Read the full text content of a single Google Drive file. Google Docs/Sheets are exported as plain text. "+
				"IMPORTANT: If Google Drive data sources are synced, document content is ALREADY indexed in memory_search. "+
				"Use memory_search with filter {\"source\": \"gdrive\"} FIRST. Only use this tool when: "+
				"(1) memory_search returns no results for the document, "+
				"(2) you need the very latest content that may not be synced yet, or "+
				"(3) you need to read a specific file by ID not in the sync scope. "+
				"To read MULTIPLE files, use "+name+"_read_files instead of spawning sub-agents."),
	)
}

// NewReadFilesTool creates a tool that reads multiple files in a single call.
// This eliminates the need to spawn sub-agents for parallel document reads.
func NewReadFilesTool(name string, s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.readFiles,
		function.WithName(name+"_read_files"),
		function.WithDescription(
			"Read the text content of MULTIPLE Google Drive files in one call. Returns content for each file with partial results on failures. "+
				"Use this instead of spawning sub-agents or calling read_file multiple times when you need to read several documents. "+
				"IMPORTANT: If Google Drive data sources are synced, document content is ALREADY indexed in memory_search — use that first."),
	)
}

// AllTools returns all Google Drive tools wired to the service, with tool names prefixed by name.
func AllTools(name string, s Service) []tool.Tool {
	return []tool.Tool{
		NewSearchTool(name, s),
		NewListFolderTool(name, s),
		NewGetFileTool(name, s),
		NewReadFileTool(name, s),
		NewReadFilesTool(name, s),
	}
}

// ── Tool Implementations ────────────────────────────────────────────────

func (ts *toolSet) search(ctx context.Context, req searchRequest) ([]FileInfo, error) {
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 20
	}
	return ts.s.Search(ctx, req.Query, maxResults)
}

func (ts *toolSet) listFolder(ctx context.Context, req listFolderRequest) ([]FileInfo, error) {
	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 50
	}
	return ts.s.ListFolder(ctx, req.FolderID, maxResults)
}

func (ts *toolSet) getFile(ctx context.Context, req fileIDRequest) (*FileDetail, error) {
	return ts.s.GetFile(ctx, req.FileID)
}

type readFileResponse struct {
	Content string `json:"content"`
}

func (ts *toolSet) readFile(ctx context.Context, req fileIDRequest) (*readFileResponse, error) {
	content, err := ts.s.ReadFile(ctx, req.FileID)
	if err != nil {
		return nil, err
	}
	content = truncateContent(content)
	return &readFileResponse{Content: content}, nil
}

// ── Batch Read ──────────────────────────────────────────────────────────

type readFilesRequest struct {
	FileIDs []string `json:"file_ids" jsonschema:"description=List of Google Drive file IDs to read (max 10),required"`
}

type readFileResult struct {
	FileID  string `json:"file_id"`
	Content string `json:"content,omitempty"`
	Error   string `json:"error,omitempty"`
}

type readFilesResponse struct {
	Files      []readFileResult `json:"files"`
	Successful int              `json:"successful"`
	Failed     int              `json:"failed"`
}

// maxBatchFiles is the maximum number of files allowed in a single batch read.
const maxBatchFiles = 10

// maxBatchConcurrency limits concurrent GDrive API calls to avoid rate limiting.
const maxBatchConcurrency = 5

func (ts *toolSet) readFiles(ctx context.Context, req readFilesRequest) (*readFilesResponse, error) {
	if len(req.FileIDs) == 0 {
		return nil, fmt.Errorf("file_ids must contain at least one file ID")
	}
	if len(req.FileIDs) > maxBatchFiles {
		return nil, fmt.Errorf("too many files: max %d, got %d", maxBatchFiles, len(req.FileIDs))
	}

	results := make([]readFileResult, len(req.FileIDs))
	var mu sync.Mutex
	var succeeded, failed int

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxBatchConcurrency)

	for i, fileID := range req.FileIDs {
		i := i
		fileID := fileID
		g.Go(func() error {
			content, err := ts.s.ReadFile(gctx, fileID)
			result := readFileResult{FileID: fileID}
			if err != nil {
				result.Error = err.Error()
				mu.Lock()
				failed++
				mu.Unlock()
			} else {
				result.Content = truncateContent(content)
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
			results[i] = result
			return nil // always continue, partial results are fine
		})
	}

	_ = g.Wait() // errors are captured per-file, not propagated

	return &readFilesResponse{
		Files:      results,
		Successful: succeeded,
		Failed:     failed,
	}, nil
}

// truncateContent trims very large file content to avoid context overflow.
func truncateContent(content string) string {
	const maxLen = 100000
	if len(content) > maxLen {
		return content[:maxLen] + fmt.Sprintf("\n\n... [truncated, total %d chars]", len(content))
	}
	return content
}
