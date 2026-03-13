// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package vector_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/memory/vector"
)

var _ = Describe("ContentChunker", func() {
	var chunker *vector.ContentChunker

	BeforeEach(func() {
		chunker = vector.NewContentChunker(200, 20)
	})

	Describe("ContentTypeFromMIME", func() {
		It("returns ContentTypeMarkdown for Google Docs", func() {
			Expect(vector.ContentTypeFromMIME("application/vnd.google-apps.document")).To(Equal(vector.ContentTypeMarkdown))
		})

		It("returns ContentTypeMarkdown for markdown files", func() {
			Expect(vector.ContentTypeFromMIME("text/markdown")).To(Equal(vector.ContentTypeMarkdown))
		})

		It("returns ContentTypePlain for spreadsheets", func() {
			Expect(vector.ContentTypeFromMIME("application/vnd.google-apps.spreadsheet")).To(Equal(vector.ContentTypePlain))
		})

		It("returns ContentTypePlain for plain text", func() {
			Expect(vector.ContentTypeFromMIME("text/plain")).To(Equal(vector.ContentTypePlain))
		})

		It("returns ContentTypePlain for unknown MIME types", func() {
			Expect(vector.ContentTypeFromMIME("application/octet-stream")).To(Equal(vector.ContentTypePlain))
		})
	})

	Describe("ChunkForType", func() {
		It("returns single chunk for short text", func() {
			out := chunker.ChunkForType("hello world", vector.ContentTypePlain)
			Expect(out).To(HaveLen(1))
			Expect(out[0]).To(Equal("hello world"))
		})

		It("splits long plain text using recursive strategy", func() {
			para1 := strings.Repeat("word ", 30)
			para2 := strings.Repeat("more ", 30)
			text := para1 + "\n\n" + para2
			out := chunker.ChunkForType(text, vector.ContentTypePlain)
			Expect(len(out)).To(BeNumerically(">=", 2))
		})

		It("splits markdown content using markdown strategy", func() {
			md := "# Heading One\n\nFirst section content here.\n\n## Heading Two\n\nSecond section with more details."
			out := chunker.ChunkForType(md, vector.ContentTypeMarkdown)
			Expect(len(out)).To(BeNumerically(">=", 1))
			// At minimum the content should be preserved.
			combined := strings.Join(out, "")
			Expect(combined).To(ContainSubstring("Heading One"))
			Expect(combined).To(ContainSubstring("Heading Two"))
		})

		It("returns nil for empty text", func() {
			out := chunker.ChunkForType("", vector.ContentTypePlain)
			Expect(out).To(BeNil())
		})
	})

	Describe("ChunkToBatchItemsForType", func() {
		It("returns batch items with chunk metadata for long content", func() {
			longText := strings.Repeat("x", 300)
			items, err := chunker.ChunkToBatchItemsForType("doc1", longText, map[string]string{"source": "gdrive"}, vector.ContentTypePlain)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(items)).To(BeNumerically(">=", 2))
			Expect(items[0].Metadata["source"]).To(Equal("gdrive"))
			Expect(items[0].Metadata["chunk_index"]).To(Equal("0"))
		})

		It("returns nil for empty content", func() {
			items, err := chunker.ChunkToBatchItemsForType("doc1", "", nil, vector.ContentTypePlain)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeNil())
		})
	})

	Describe("StrategyFor", func() {
		It("returns different strategies for different content types", func() {
			plainStrategy := chunker.StrategyFor(vector.ContentTypePlain)
			markdownStrategy := chunker.StrategyFor(vector.ContentTypeMarkdown)
			Expect(plainStrategy).NotTo(BeIdenticalTo(markdownStrategy))
		})
	})

	Describe("DefaultContentChunker", func() {
		It("is initialised and usable", func() {
			Expect(vector.DefaultContentChunker).NotTo(BeNil())
			out := vector.DefaultContentChunker.ChunkForType("hello", vector.ContentTypePlain)
			Expect(out).To(HaveLen(1))
			Expect(out[0]).To(Equal("hello"))
		})
	})
})
