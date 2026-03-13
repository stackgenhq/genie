// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package vector

import (
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/chunking"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
)

// MaxCharsPerEmbeddingChunk is a safe character limit so that chunked text
// stays under embedding model token limits (e.g. OpenAI text-embedding-3-small
// 8191 tokens). Email/HTML can be ~2 chars per token; 8000 tokens ≈ 16000 chars.
const MaxCharsPerEmbeddingChunk = 16_000

// embeddingChunkOverlap is 10% of the chunk size so that boundary text is
// preserved in both adjacent chunks, preventing mid-thought splits.
const embeddingChunkOverlap = 1_600

// defaultChunkStrategy uses trpc-agent-go's RecursiveChunking which splits on
// paragraph boundaries (\n\n) → line boundaries (\n) → words → characters,
// preserving natural text structure. This is the industry-standard approach
// (equivalent to LangChain's RecursiveCharacterTextSplitter).
var defaultChunkStrategy = chunking.NewRecursiveChunking(
	chunking.WithRecursiveChunkSize(MaxCharsPerEmbeddingChunk),
	chunking.WithRecursiveOverlap(embeddingChunkOverlap),
)

// ChunkTextForEmbedding splits text into chunks using trpc-agent-go's
// RecursiveChunking strategy (paragraph→line→word→character boundaries,
// with 10% overlap). Returns a single chunk if text is short enough;
// otherwise multiple chunks suitable for embedding one at a time.
func ChunkTextForEmbedding(text string) []string {
	return chunkWithStrategy(text, defaultChunkStrategy)
}

// chunkWithStrategy splits text using the given chunking strategy.
func chunkWithStrategy(text string, strategy chunking.Strategy) []string {
	if text == "" {
		return nil
	}
	doc := &document.Document{Content: text}
	chunks, err := strategy.Chunk(doc)
	if err != nil || len(chunks) == 0 {
		return []string{text}
	}
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = c.Content
	}
	return out
}

// ChunkContentToBatchItems splits content into chunks and returns BatchItems
// ready for Upsert with stable IDs and chunk metadata. Uses the default
// RecursiveChunking strategy.
func ChunkContentToBatchItems(itemID, content string, baseMeta map[string]string) ([]BatchItem, error) {
	return chunkToBatchItems(itemID, content, baseMeta, defaultChunkStrategy)
}

// chunkToBatchItems splits content using the given strategy and returns
// BatchItems with stable chunk IDs and metadata.
func chunkToBatchItems(itemID, content string, baseMeta map[string]string, strategy chunking.Strategy) ([]BatchItem, error) {
	if content == "" {
		return nil, nil
	}
	doc := &document.Document{ID: itemID, Content: content}
	chunks, err := strategy.Chunk(doc)
	if err != nil {
		return nil, fmt.Errorf("chunk content: %w", err)
	}
	if len(chunks) == 0 {
		return nil, nil
	}
	out := make([]BatchItem, 0, len(chunks))
	for i, c := range chunks {
		meta := copyMetaWithChunkInfo(baseMeta, i, len(chunks))
		chunkID := buildChunkID(itemID, i, len(chunks))
		out = append(out, BatchItem{ID: chunkID, Text: c.Content, Metadata: meta})
	}
	return out, nil
}

// copyMetaWithChunkInfo copies base metadata and adds chunk_index/chunk_total
// when there are multiple chunks.
func copyMetaWithChunkInfo(baseMeta map[string]string, index, total int) map[string]string {
	meta := make(map[string]string, len(baseMeta)+4)
	for k, v := range baseMeta {
		meta[k] = v
	}
	if total > 1 {
		meta["chunk_index"] = fmt.Sprintf("%d", index)
		meta["chunk_total"] = fmt.Sprintf("%d", total)
	}
	return meta
}

// buildChunkID returns itemID for single-chunk documents, or
// itemID:chunk:N for multi-chunk documents.
func buildChunkID(itemID string, index, total int) string {
	if total <= 1 {
		return itemID
	}
	return fmt.Sprintf("%s:chunk:%d", itemID, index)
}
