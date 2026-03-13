// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package docparser provides a multi-backend document parser that converts
// files (PDF, DOCX, images, etc.) into []datasource.NormalizedItem for
// vectorization. Backends include Docling Serve and Gemini.
// The active provider is selected via Config.Provider.
package docparser

import "io"

// Config selects which parsing backend to use and holds per-provider settings.
// Only the sub-config matching Provider is used; the rest are ignored.
type Config struct {
	// Provider selects the active backend: "docling", "gemini".
	Provider string `toml:"provider,omitempty" yaml:"provider,omitempty"`

	Docling DoclingConfig `toml:"docling,omitempty" yaml:"docling,omitempty"`
	Gemini  GeminiConfig  `toml:"gemini,omitempty" yaml:"gemini,omitempty"`
}

// DoclingConfig holds settings for the Docling Serve sidecar backend.
type DoclingConfig struct {
	// BaseURL is the Docling Serve REST API base (e.g. "http://localhost:5001").
	BaseURL string `toml:"base_url,omitempty" yaml:"base_url,omitempty"`
}

// GeminiConfig holds settings for the Gemini file-upload backend.
type GeminiConfig struct {
	// Model is the Gemini model to use for document parsing (e.g. "gemini-2.0-flash").
	Model string `toml:"model,omitempty" yaml:"model,omitempty"`
}

// ParseRequest carries the file to parse. Reader provides the file content;
// Filename is used for MIME detection and metadata. SourceID is a stable
// prefix for generated item IDs (e.g. "gdrive:abc123").
type ParseRequest struct {
	// Reader provides the raw file content.
	Reader io.Reader
	// Filename is the original filename (used for MIME-type detection and metadata).
	Filename string
	// SourceID is a stable ID prefix for generated items (e.g. "gdrive:fileId").
	// Parsed pages/sections are suffixed with ":page:N".
	SourceID string
}
