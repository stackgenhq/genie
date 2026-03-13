// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package docparser

import (
	"context"
	"fmt"
	"strings"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/security"
)

//go:generate go tool counterfeiter -generate

// Provider is the interface that all document parsing backends implement.
// Given a ParseRequest (io.Reader + filename), it returns structured items
// ready for vectorization. Multi-page documents produce one NormalizedItem
// per page or logical section.
//
//counterfeiter:generate . Provider
type Provider interface {
	// Parse reads a document and returns one NormalizedItem per page or section.
	// Each item's Content contains the extracted text; Metadata includes
	// element_type, page_number, mime_type, and parser backend name.
	Parse(ctx context.Context, req ParseRequest) ([]datasource.NormalizedItem, error)
}

// New creates a Provider from the given Config. Only the sub-config matching
// cfg.Provider is used. Returns an error for unknown or empty provider names.
func (cfg Config) New(ctx context.Context, sp security.SecretProvider) (Provider, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		return nil, fmt.Errorf("docparser: provider is required (valid: docling, gemini)")
	}

	switch provider {
	case "docling":
		return newDoclingProvider(cfg.Docling)
	case "gemini":
		return newGeminiProvider(ctx, cfg.Gemini, sp)
	default:
		return nil, fmt.Errorf("docparser: unknown provider %q (valid: docling, gemini)", provider)
	}
}
