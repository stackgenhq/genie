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
// Used with trpc-agent-go's knowledge/chunking FixedSizeChunking strategy.
const MaxCharsPerEmbeddingChunk = 16_000

// embeddingChunkStrategy is the shared fixed-size chunking strategy for data-source
// content, so all callers use the same size/overlap as trpc-agent-go examples.
var embeddingChunkStrategy = chunking.NewFixedSizeChunking(
	chunking.WithChunkSize(MaxCharsPerEmbeddingChunk),
	chunking.WithOverlap(0),
)

// ChunkTextForEmbedding splits text into chunks of at most MaxCharsPerEmbeddingChunk
// characters using trpc-agent-go's knowledge/chunking FixedSizeChunking (UTF-8 safe,
// rune-based). Returns a single chunk if text is short enough; otherwise multiple
// chunks suitable for embedding one at a time. See:
// https://github.com/trpc-group/trpc-agent-go/tree/main/examples/knowledge/sources/fixed-chunking
func ChunkTextForEmbedding(text string) []string {
	if text == "" {
		return nil
	}
	doc := &document.Document{Content: text}
	chunks, err := embeddingChunkStrategy.Chunk(doc)
	if err != nil || len(chunks) == 0 {
		return []string{text}
	}
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = c.Content
	}
	return out
}

// ChunkContentToBatchItems uses trpc-agent-go's chunking to split content and
// returns BatchItems ready for Upsert (IDs, chunk metadata). Use this when
// you need both chunk text and stable chunk IDs/metadata from the library.
func ChunkContentToBatchItems(itemID, content string, baseMeta map[string]string) ([]BatchItem, error) {
	if content == "" {
		return nil, nil
	}
	doc := &document.Document{ID: itemID, Content: content}
	chunks, err := embeddingChunkStrategy.Chunk(doc)
	if err != nil {
		return nil, fmt.Errorf("chunk content: %w", err)
	}
	if len(chunks) == 0 {
		return nil, nil
	}
	out := make([]BatchItem, 0, len(chunks))
	for i, c := range chunks {
		meta := make(map[string]string, len(baseMeta)+4)
		for k, v := range baseMeta {
			meta[k] = v
		}
		if len(chunks) > 1 {
			meta["chunk_index"] = fmt.Sprintf("%d", i)
			meta["chunk_total"] = fmt.Sprintf("%d", len(chunks))
		}
		chunkID := itemID
		if len(chunks) > 1 {
			chunkID = fmt.Sprintf("%s:chunk:%d", itemID, i)
		}
		out = append(out, BatchItem{ID: chunkID, Text: c.Content, Metadata: meta})
	}
	return out, nil
}
