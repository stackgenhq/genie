// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package vector_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/memory/vector"
)

var _ = Describe("Chunking", func() {
	Describe("ChunkTextForEmbedding", func() {
		It("returns nil for empty text", func() {
			Expect(vector.ChunkTextForEmbedding("")).To(BeNil())
		})

		It("returns single chunk when text is under limit", func() {
			text := "short text"
			out := vector.ChunkTextForEmbedding(text)
			Expect(out).To(HaveLen(1))
			Expect(out[0]).To(Equal(text))
		})

		It("splits long text into multiple chunks", func() {
			// Build text over one chunk size.
			n := vector.MaxCharsPerEmbeddingChunk + 500
			text := strings.Repeat("a", n)
			out := vector.ChunkTextForEmbedding(text)
			Expect(len(out)).To(BeNumerically(">=", 2))
			for _, chunk := range out {
				Expect(len(chunk)).To(BeNumerically("<=", vector.MaxCharsPerEmbeddingChunk+1700))
			}
		})

		It("splits on paragraph boundaries when possible (recursive chunking)", func() {
			// Build two paragraphs that together exceed one chunk.
			para := strings.Repeat("word ", vector.MaxCharsPerEmbeddingChunk/6)
			text := para + "\n\n" + para
			out := vector.ChunkTextForEmbedding(text)
			Expect(len(out)).To(BeNumerically(">=", 2))
			// The first chunk should not contain the double-newline separator
			// because recursive chunking splits on \n\n first.
			Expect(out[0]).NotTo(ContainSubstring("\n\n"))
		})
	})

	Describe("ChunkContentToBatchItems", func() {
		It("returns nil for empty content", func() {
			items, err := vector.ChunkContentToBatchItems("id1", "", nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeNil())
		})

		It("returns single item without chunk metadata for short content", func() {
			meta := map[string]string{"source": "test"}
			items, err := vector.ChunkContentToBatchItems("id1", "short", meta)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].ID).To(Equal("id1"))
			Expect(items[0].Text).To(Equal("short"))
			Expect(items[0].Metadata["source"]).To(Equal("test"))
			Expect(items[0].Metadata).NotTo(HaveKey("chunk_index"))
		})

		It("adds chunk metadata for multi-chunk content", func() {
			longContent := strings.Repeat("x", vector.MaxCharsPerEmbeddingChunk+500)
			items, err := vector.ChunkContentToBatchItems("id2", longContent, map[string]string{"k": "v"})
			Expect(err).NotTo(HaveOccurred())
			Expect(len(items)).To(BeNumerically(">=", 2))
			// First chunk ID stays as-is or gets :chunk:0.
			Expect(items[0].ID).To(Equal("id2:chunk:0"))
			Expect(items[0].Metadata["chunk_index"]).To(Equal("0"))
			Expect(items[0].Metadata["k"]).To(Equal("v"))
			// Second chunk has index 1.
			Expect(items[1].ID).To(Equal("id2:chunk:1"))
			Expect(items[1].Metadata["chunk_index"]).To(Equal("1"))
		})

		It("preserves base metadata across all chunks", func() {
			longContent := strings.Repeat("y", vector.MaxCharsPerEmbeddingChunk+500)
			baseMeta := map[string]string{"source": "gdrive", "title": "Doc"}
			items, err := vector.ChunkContentToBatchItems("doc1", longContent, baseMeta)
			Expect(err).NotTo(HaveOccurred())
			for _, item := range items {
				Expect(item.Metadata["source"]).To(Equal("gdrive"))
				Expect(item.Metadata["title"]).To(Equal("Doc"))
			}
		})
	})
})
