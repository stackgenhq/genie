/*
Copyright © 2026 StackGen, Inc.
*/

package vector_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/memory/vector"
)

var _ = Describe("ChunkTextForEmbedding", func() {
	It("returns nil for empty text", func() {
		Expect(vector.ChunkTextForEmbedding("")).To(BeNil())
	})

	It("returns single chunk when text is under limit", func() {
		text := "short text"
		out := vector.ChunkTextForEmbedding(text)
		Expect(out).To(HaveLen(1))
		Expect(out[0]).To(Equal(text))
	})

	It("splits long text into multiple chunks under MaxCharsPerEmbeddingChunk", func() {
		// Build text just over one chunk so we get two chunks.
		n := vector.MaxCharsPerEmbeddingChunk + 100
		b := make([]byte, n)
		for i := range b {
			b[i] = 'a'
		}
		text := string(b)
		out := vector.ChunkTextForEmbedding(text)
		Expect(out).To(HaveLen(2))
		Expect(len(out[0])).To(BeNumerically("<=", vector.MaxCharsPerEmbeddingChunk))
		Expect(len(out[1])).To(BeNumerically("<=", vector.MaxCharsPerEmbeddingChunk))
		Expect(out[0] + out[1]).To(Equal(text))
	})

	It("splits long text using fixed-size chunking (trpc-agent-go)", func() {
		// trpc-agent-go FixedSizeChunking splits by character size; no newline preference.
		n := vector.MaxCharsPerEmbeddingChunk + 100
		b := make([]byte, n)
		for i := range b {
			b[i] = 'a'
		}
		text := string(b)
		out := vector.ChunkTextForEmbedding(text)
		Expect(out).To(HaveLen(2))
		Expect(len(out[0])).To(BeNumerically("<=", vector.MaxCharsPerEmbeddingChunk))
		Expect(len(out[1])).To(BeNumerically("<=", vector.MaxCharsPerEmbeddingChunk))
		Expect(out[0] + out[1]).To(Equal(text))
	})
})
