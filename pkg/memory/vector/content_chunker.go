// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package vector

import (
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/chunking"
)

// ContentType classifies document content for strategy selection.
type ContentType int

const (
	// ContentTypePlain is generic text content.
	ContentTypePlain ContentType = iota
	// ContentTypeMarkdown is markdown-structured content (Google Docs, .md files).
	ContentTypeMarkdown
)

// ContentChunker selects a chunking strategy based on content type. Reusable
// across data sources (GDrive, Slack, Gmail, etc.). Each datasource calls
// ChunkForType with the appropriate content type to get intelligent splitting.
type ContentChunker struct {
	chunkSize int
	overlap   int
	plain     chunking.Strategy
	markdown  chunking.Strategy
}

// NewContentChunker creates a ContentChunker with the given size and overlap.
// It initialises both RecursiveChunking (for generic text) and
// MarkdownChunking (for structured documents) from trpc-agent-go.
func NewContentChunker(chunkSize, overlap int) *ContentChunker {
	return &ContentChunker{
		chunkSize: chunkSize,
		overlap:   overlap,
		plain: chunking.NewRecursiveChunking(
			chunking.WithRecursiveChunkSize(chunkSize),
			chunking.WithRecursiveOverlap(overlap),
		),
		markdown: chunking.NewMarkdownChunking(
			chunking.WithMarkdownChunkSize(chunkSize),
			chunking.WithMarkdownOverlap(overlap),
		),
	}
}

// StrategyFor returns the appropriate chunking strategy for the content type.
func (cc *ContentChunker) StrategyFor(ct ContentType) chunking.Strategy {
	if ct == ContentTypeMarkdown {
		return cc.markdown
	}
	return cc.plain
}

// ChunkForType splits text using the strategy for the given content type and
// returns the text chunks.
func (cc *ContentChunker) ChunkForType(text string, ct ContentType) []string {
	return chunkWithStrategy(text, cc.StrategyFor(ct))
}

// ChunkToBatchItemsForType splits content and returns BatchItems using the
// strategy appropriate for the content type.
func (cc *ContentChunker) ChunkToBatchItemsForType(itemID, content string, baseMeta map[string]string, ct ContentType) ([]BatchItem, error) {
	return chunkToBatchItems(itemID, content, baseMeta, cc.StrategyFor(ct))
}

// ContentTypeFromMIME maps a MIME type to a ContentType. Google Workspace
// document types are treated as markdown because exported text often contains
// heading-like structure. This function is reusable by any datasource.
func ContentTypeFromMIME(mime string) ContentType {
	switch {
	case mime == "application/vnd.google-apps.document":
		return ContentTypeMarkdown
	case mime == "text/markdown":
		return ContentTypeMarkdown
	case strings.HasSuffix(mime, "+markdown"):
		return ContentTypeMarkdown
	default:
		return ContentTypePlain
	}
}

// DefaultContentChunker is the shared content-aware chunker using the standard
// embedding chunk size and overlap. Datasource connectors should use this
// instead of creating their own.
var DefaultContentChunker = NewContentChunker(MaxCharsPerEmbeddingChunk, embeddingChunkOverlap)
