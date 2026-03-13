// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package gdrive provides a DataSource connector that enumerates Google Drive
// files in configured folders for vectorization. It uses the existing
// gdrive.Service to list and read file content; text files and Google Docs are
// included as NormalizedItems.
package gdrive

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/datasource"
)

const (
	datasourceNameGDrive   = "gdrive"
	listPageSize           = 100
	defaultMaxRecurseDepth = 10
)

// GDriveConnector implements datasource.DataSource for Google Drive.
// It recursively lists files in each folder in scope, reads text content
// where possible, and returns one NormalizedItem per file for the sync
// pipeline to vectorize.
type GDriveConnector struct {
	svc      Service
	maxDepth int
}

// NewGDriveConnector returns a DataSource that lists and reads Drive files.
// The caller must provide an initialised gdrive.Service (credentials and
// auth are handled by the service). maxDepth controls how deep subfolder
// traversal goes; pass 0 to use the default (10).
func NewGDriveConnector(svc Service, maxDepth int) *GDriveConnector {
	if maxDepth <= 0 {
		maxDepth = defaultMaxRecurseDepth
	}
	return &GDriveConnector{svc: svc, maxDepth: maxDepth}
}

// Name returns the source identifier for Google Drive.
func (c *GDriveConnector) Name() string {
	return datasourceNameGDrive
}

// ListItems recursively lists files in each folder in scope.GDriveFolderIDs,
// reads content for supported types (Docs, plain text), and returns one
// NormalizedItem per file with ID "gdrive:fileId". Binary or unsupported files
// are included with content set to the structured header only.
func (c *GDriveConnector) ListItems(ctx context.Context, scope datasource.Scope) ([]datasource.NormalizedItem, error) {
	return c.listItemsWithSince(ctx, scope, time.Time{})
}

// ListItemsSince returns only files modified after the given time (per folder).
// Uses the Drive API modifiedTime query so only changed files are fetched.
// Subfolders are always traversed regardless of their modification time.
func (c *GDriveConnector) ListItemsSince(ctx context.Context, scope datasource.Scope, since time.Time) ([]datasource.NormalizedItem, error) {
	return c.listItemsWithSince(ctx, scope, since)
}

func (c *GDriveConnector) listItemsWithSince(ctx context.Context, scope datasource.Scope, since time.Time) ([]datasource.NormalizedItem, error) {
	if len(scope.GDriveFolderIDs) == 0 {
		return nil, nil
	}
	var out []datasource.NormalizedItem
	for _, folderID := range scope.GDriveFolderIDs {
		items, err := c.listFolderItems(ctx, folderID, since, 0, "")
		if err != nil {
			return nil, fmt.Errorf("gdrive folder %s: %w", folderID, err)
		}
		out = append(out, items...)
	}
	return out, nil
}

// listFolderItems recursively lists files in a single folder. folderPath
// tracks the breadcrumb trail (e.g. "Marketing/Campaigns/2026") so each
// file's metadata includes its location in the Drive hierarchy.
func (c *GDriveConnector) listFolderItems(ctx context.Context, folderID string, since time.Time, depth int, folderPath string) ([]datasource.NormalizedItem, error) {
	if depth >= c.maxDepth {
		return nil, nil
	}
	files, err := c.svc.ListFolderModifiedSince(ctx, folderID, since, listPageSize)
	if err != nil {
		return nil, err
	}
	var out []datasource.NormalizedItem
	for _, f := range files {
		if f.IsFolder {
			childPath := appendFolderPath(folderPath, f.Name)
			sub, err := c.listFolderItems(ctx, f.ID, since, depth+1, childPath)
			if err != nil {
				return nil, fmt.Errorf("subfolder %s (%s): %w", f.Name, f.ID, err)
			}
			out = append(out, sub...)
			continue
		}
		if shouldSkipFile(f, since) {
			continue
		}
		item := c.buildNormalizedItem(ctx, f, folderPath)
		out = append(out, item)
	}
	return out, nil
}

// buildNormalizedItem creates a NormalizedItem from a Drive FileInfo. It reads
// file content for text-like types and constructs the structured content
// header with folder path context.
func (c *GDriveConnector) buildNormalizedItem(ctx context.Context, f FileInfo, folderPath string) datasource.NormalizedItem {
	updatedAt, _ := parseDriveTime(f.ModifiedTime)
	content := buildContentHeader(f, folderPath)

	if isTextLike(f.MimeType) {
		body, err := c.svc.ReadFile(ctx, f.ID)
		if err == nil && body != "" {
			content = content + "\n\n" + body
		}
	}

	return datasource.NormalizedItem{
		ID:        "gdrive:" + f.ID,
		Source:    datasourceNameGDrive,
		SourceRef: &datasource.SourceRef{Type: datasourceNameGDrive, RefID: f.ID},
		UpdatedAt: updatedAt,
		Content:   content,
		Metadata:  buildMetadata(f, folderPath),
	}
}

// buildMetadata constructs the metadata map for a Drive file, including
// folder path for hierarchy-based filtering and discovery.
func buildMetadata(f FileInfo, folderPath string) map[string]string {
	meta := map[string]string{"title": f.Name}
	if f.MimeType != "" {
		meta["mime_type"] = f.MimeType
		meta["file_type"] = friendlyFileType(f.MimeType)
	}
	if f.WebViewLink != "" {
		meta["url"] = f.WebViewLink
	}
	if f.ModifiedTime != "" {
		meta["modified_date"] = f.ModifiedTime
	}
	if f.Size > 0 {
		meta["size"] = fmt.Sprintf("%d", f.Size)
	}
	if folderPath != "" {
		meta["folder_path"] = folderPath
	}
	return meta
}

// buildContentHeader constructs a structured text header for a Drive file so
// that chunked documents retain file context and the LLM can answer common
// questions ("what are the latest files?") directly from memory_search results.
func buildContentHeader(f FileInfo, folderPath string) string {
	var b strings.Builder
	b.WriteString("[Google Drive File]\n")
	b.WriteString("Title: " + f.Name + "\n")
	ft := friendlyFileType(f.MimeType)
	if ft != "" {
		b.WriteString("Type: " + ft + "\n")
	}
	if folderPath != "" {
		b.WriteString("Folder: " + folderPath + "\n")
	}
	if f.ModifiedTime != "" {
		if t, err := parseDriveTime(f.ModifiedTime); err == nil && !t.IsZero() {
			b.WriteString("Modified: " + t.Format("2006-01-02") + "\n")
		}
	}
	if f.WebViewLink != "" {
		b.WriteString("URL: " + f.WebViewLink + "\n")
	}
	return b.String()
}

// shouldSkipFile returns true if the file should be skipped during incremental
// sync (not modified since the cutoff time).
func shouldSkipFile(f FileInfo, since time.Time) bool {
	if since.IsZero() {
		return false
	}
	modTime, _ := parseDriveTime(f.ModifiedTime)
	return !modTime.IsZero() && modTime.Before(since)
}

// appendFolderPath appends a folder name to an existing path, handling the
// root case where the parent path is empty.
func appendFolderPath(parentPath, folderName string) string {
	if parentPath == "" {
		return folderName
	}
	return parentPath + "/" + folderName
}

// friendlyFileType maps a MIME type to a short human-readable label for
// display in search results and metadata filtering.
func friendlyFileType(mime string) string {
	switch mime {
	case "application/vnd.google-apps.document":
		return "document"
	case "application/vnd.google-apps.spreadsheet":
		return "spreadsheet"
	case "application/vnd.google-apps.presentation":
		return "presentation"
	case "application/vnd.google-apps.form":
		return "form"
	case "application/vnd.google-apps.drawing":
		return "drawing"
	case "application/pdf":
		return "pdf"
	case "application/json":
		return "json"
	case "application/x-yaml":
		return "yaml"
	default:
		if strings.HasPrefix(mime, "text/") {
			return "text"
		}
		if strings.HasPrefix(mime, "image/") {
			return "image"
		}
		if strings.HasPrefix(mime, "video/") {
			return "video"
		}
		if strings.HasPrefix(mime, "application/vnd.google-apps.") {
			return strings.TrimPrefix(mime, "application/vnd.google-apps.")
		}
		return "file"
	}
}

func isTextLike(mime string) bool {
	switch {
	case strings.HasPrefix(mime, "application/vnd.google-apps."):
		return true // Docs, Sheets (exported as text)
	case strings.HasPrefix(mime, "text/"):
		return true
	case mime == "application/json" || mime == "application/x-yaml":
		return true
	default:
		return false
	}
}

func parseDriveTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// Ensure GDriveConnector implements datasource.DataSource and datasource.ListItemsSince.
var (
	_ datasource.DataSource     = (*GDriveConnector)(nil)
	_ datasource.ListItemsSince = (*GDriveConnector)(nil)
)
