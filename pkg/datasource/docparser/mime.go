// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package docparser

import (
	"mime"
	"path/filepath"
	"strings"
)

// DetectMIME returns the MIME type for a filename based on its extension.
// Falls back to "application/octet-stream" for unknown types.
func DetectMIME(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return "application/octet-stream"
	}

	// Common document types that mime.TypeByExtension may not know.
	switch ext {
	case ".pdf":
		return "application/pdf"
	case ".docx":
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	case ".pptx":
		return "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".xlsx":
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	case ".doc":
		return "application/msword"
	case ".ppt":
		return "application/vnd.ms-powerpoint"
	case ".xls":
		return "application/vnd.ms-excel"
	case ".md", ".markdown":
		return "text/markdown"
	}

	// Fall back to stdlib.
	mimeType := mime.TypeByExtension(ext)
	if mimeType != "" {
		return mimeType
	}
	return "application/octet-stream"
}
