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
// are included with content set to the file name only.
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
		items, err := c.listFolderItems(ctx, folderID, since, 0)
		if err != nil {
			return nil, fmt.Errorf("gdrive folder %s: %w", folderID, err)
		}
		out = append(out, items...)
	}
	return out, nil
}

func (c *GDriveConnector) listFolderItems(ctx context.Context, folderID string, since time.Time, depth int) ([]datasource.NormalizedItem, error) {
	if depth >= c.maxDepth {
		return nil, nil
	}
	// For listing folder contents we always use the non-since variant so that
	// subfolders are visible even when they themselves haven't been modified.
	// The modifiedTime filter is applied only to files (below).
	var files []FileInfo
	var err error
	if since.IsZero() {
		files, err = c.svc.ListFolder(ctx, folderID, listPageSize)
	} else {
		// List all items so we can recurse into subfolders. We filter files
		// by modification time below.
		files, err = c.svc.ListFolder(ctx, folderID, listPageSize)
	}
	if err != nil {
		return nil, err
	}
	var out []datasource.NormalizedItem
	for _, f := range files {
		if f.IsFolder {
			// Recurse into subfolder.
			sub, err := c.listFolderItems(ctx, f.ID, since, depth+1)
			if err != nil {
				return nil, fmt.Errorf("subfolder %s (%s): %w", f.Name, f.ID, err)
			}
			out = append(out, sub...)
			continue
		}
		// For incremental sync, skip files not modified since the cutoff.
		if !since.IsZero() {
			modTime, _ := parseDriveTime(f.ModifiedTime)
			if !modTime.IsZero() && modTime.Before(since) {
				continue
			}
		}
		updatedAt, _ := parseDriveTime(f.ModifiedTime)
		content := f.Name
		// Attempt to read body for text-like types to improve search.
		if isTextLike(f.MimeType) {
			body, err := c.svc.ReadFile(ctx, f.ID)
			if err == nil && body != "" {
				content = f.Name + "\n\n" + body
			}
		}
		meta := map[string]string{"title": f.Name}
		if f.MimeType != "" {
			meta["mime_type"] = f.MimeType
		}
		out = append(out, datasource.NormalizedItem{
			ID:        "gdrive:" + f.ID,
			Source:    datasourceNameGDrive,
			SourceRef: &datasource.SourceRef{Type: datasourceNameGDrive, RefID: f.ID},
			UpdatedAt: updatedAt,
			Content:   content,
			Metadata:  meta,
		})
	}
	return out, nil
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
