package gdrive

import (
	"context"
	"fmt"

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
	CredentialsFile string `yaml:"credentials_file" toml:"credentials_file"` // Path to service account JSON
	// Alternatively, set GOOGLE_APPLICATION_CREDENTIALS env var.
}

// ── Domain Types ────────────────────────────────────────────────────────

// FileInfo describes a file or folder in Google Drive.
type FileInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MimeType     string `json:"mime_type"`
	Size         int64  `json:"size,omitempty"`
	ModifiedTime string `json:"modified_time,omitempty"`
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

func NewSearchTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.search,
		function.WithName("gdrive_search"),
		function.WithDescription("Search for files in Google Drive. Supports Google Drive query syntax."),
	)
}

func NewListFolderTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listFolder,
		function.WithName("gdrive_list_folder"),
		function.WithDescription("List files in a Google Drive folder. Use 'root' for the root folder."),
	)
}

func NewGetFileTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.getFile,
		function.WithName("gdrive_get_file"),
		function.WithDescription("Get metadata about a Google Drive file including owners and links."),
	)
}

func NewReadFileTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.readFile,
		function.WithName("gdrive_read_file"),
		function.WithDescription("Read the text content of a Google Drive file. Google Docs/Sheets are exported as plain text."),
	)
}

// AllTools returns all Google Drive tools wired to the service.
func AllTools(s Service) []tool.Tool {
	return []tool.Tool{
		NewSearchTool(s),
		NewListFolderTool(s),
		NewGetFileTool(s),
		NewReadFileTool(s),
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
	// Truncate very large files to avoid context overflow.
	const maxLen = 100000
	if len(content) > maxLen {
		content = content[:maxLen] + fmt.Sprintf("\n\n... [truncated, total %d chars]", len(content))
	}
	return &readFileResponse{Content: content}, nil
}
