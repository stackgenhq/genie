package gdrive

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const folderMimeType = "application/vnd.google-apps.folder"

// googleDocs mime types that support text export.
var exportableMimeTypes = map[string]string{
	"application/vnd.google-apps.document":     "text/plain",
	"application/vnd.google-apps.spreadsheet":  "text/csv",
	"application/vnd.google-apps.presentation": "text/plain",
}

// driveWrapper implements the Service interface using the official
// Google Drive API v3 client library.
type driveWrapper struct {
	svc *drive.Service
}

// New creates a new Google Drive Service based on the configuration.
// It delegates to the internal wrapper which uses the official Drive API v3
// client library. Without this factory, callers would need to know about
// the wrapper's internal constructor, coupling them to implementation details.
func New(ctx context.Context, cfg Config) (Service, error) {
	return newWrapper(ctx, cfg)
}

// newWrapper creates a Google Drive service client. If CredentialsFile is
// set, it's used for authentication; otherwise Application Default
// Credentials are used.
func newWrapper(ctx context.Context, cfg Config) (*driveWrapper, error) {
	var opts []option.ClientOption

	if cfg.CredentialsFile != "" {
		// Verify file exists before passing to the client.
		if _, err := os.Stat(cfg.CredentialsFile); err != nil {
			return nil, fmt.Errorf("gdrive: credentials file not found: %w", err)
		}
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile)) //nolint:staticcheck // TODO: migrate to non-deprecated credentials API
	}

	svc, err := drive.NewService(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("gdrive: failed to create Drive service: %w", err)
	}

	return &driveWrapper{svc: svc}, nil
}

// Search finds files matching the given query.
func (w *driveWrapper) Search(ctx context.Context, query string, maxResults int) ([]FileInfo, error) {
	call := w.svc.Files.List().
		Context(ctx).
		Q(query).
		PageSize(int64(maxResults)).
		Fields("files(id, name, mimeType, size, modifiedTime)")

	result, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("gdrive: search failed: %w", err)
	}

	return filesToInfos(result.Files), nil
}

// ListFolder lists files in the given folder.
func (w *driveWrapper) ListFolder(ctx context.Context, folderID string, maxResults int) ([]FileInfo, error) {
	query := fmt.Sprintf("'%s' in parents and trashed = false", folderID)
	call := w.svc.Files.List().
		Context(ctx).
		Q(query).
		PageSize(int64(maxResults)).
		Fields("files(id, name, mimeType, size, modifiedTime)")

	result, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("gdrive: list folder failed: %w", err)
	}

	return filesToInfos(result.Files), nil
}

// GetFile returns detailed metadata about a file.
func (w *driveWrapper) GetFile(ctx context.Context, fileID string) (*FileDetail, error) {
	file, err := w.svc.Files.Get(fileID).
		Context(ctx).
		Fields("id, name, mimeType, size, modifiedTime, createdTime, owners, webViewLink").
		Do()
	if err != nil {
		return nil, fmt.Errorf("gdrive: get file failed: %w", err)
	}

	detail := &FileDetail{
		ID:           file.Id,
		Name:         file.Name,
		MimeType:     file.MimeType,
		Size:         file.Size,
		ModifiedTime: file.ModifiedTime,
		CreatedTime:  file.CreatedTime,
		WebViewLink:  file.WebViewLink,
		IsFolder:     file.MimeType == folderMimeType,
	}
	for _, o := range file.Owners {
		detail.Owners = append(detail.Owners, o.DisplayName)
	}
	return detail, nil
}

// ReadFile reads text content from a file. For Google Docs/Sheets/Slides,
// it exports as plain text or CSV. For regular files, it downloads the content.
func (w *driveWrapper) ReadFile(ctx context.Context, fileID string) (string, error) {
	// First, get file metadata to determine mime type.
	file, err := w.svc.Files.Get(fileID).Context(ctx).Fields("mimeType, name, size").Do()
	if err != nil {
		return "", fmt.Errorf("gdrive: failed to get file metadata: %w", err)
	}

	if file.MimeType == folderMimeType {
		return "", fmt.Errorf("gdrive: cannot read a folder — use gdrive_list_folder instead")
	}

	// If it's a Google Workspace document, export it.
	if exportMime, ok := exportableMimeTypes[file.MimeType]; ok {
		resp, err := w.svc.Files.Export(fileID, exportMime).Context(ctx).Download()
		if err != nil {
			return "", fmt.Errorf("gdrive: export failed for %s: %w", file.Name, err)
		}
		defer func() { _ = resp.Body.Close() }()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("gdrive: failed to read exported content: %w", err)
		}
		return string(data), nil
	}

	// For text-based files, download directly.
	if !isTextMimeType(file.MimeType) {
		return "", fmt.Errorf("gdrive: cannot read binary file %q (mime: %s) — only text and Google Workspace files are supported",
			file.Name, file.MimeType)
	}

	resp, err := w.svc.Files.Get(fileID).Context(ctx).Download()
	if err != nil {
		return "", fmt.Errorf("gdrive: download failed for %s: %w", file.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("gdrive: failed to read file content: %w", err)
	}
	return string(data), nil
}

// Validate checks that the service account has Drive API access.
func (w *driveWrapper) Validate(ctx context.Context) error {
	_, err := w.svc.About.Get().Context(ctx).Fields("user").Do()
	if err != nil {
		return fmt.Errorf("gdrive: validate failed: %w", err)
	}
	return nil
}

// ── Helpers ─────────────────────────────────────────────────────────────

func filesToInfos(files []*drive.File) []FileInfo {
	infos := make([]FileInfo, 0, len(files))
	for _, f := range files {
		infos = append(infos, FileInfo{
			ID:           f.Id,
			Name:         f.Name,
			MimeType:     f.MimeType,
			Size:         f.Size,
			ModifiedTime: f.ModifiedTime,
			IsFolder:     f.MimeType == folderMimeType,
		})
	}
	return infos
}

func isTextMimeType(mime string) bool {
	return strings.HasPrefix(mime, "text/") ||
		mime == "application/json" ||
		mime == "application/xml" ||
		mime == "application/javascript" ||
		mime == "application/x-yaml" ||
		mime == "application/toml" ||
		strings.HasSuffix(mime, "+json") ||
		strings.HasSuffix(mime, "+xml")
}
