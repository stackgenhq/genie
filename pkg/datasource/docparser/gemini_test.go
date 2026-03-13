// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package docparser

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/genai"
)

var _ = Describe("extractGeminiText", func() {
	It("returns empty string for nil response", func() {
		Expect(extractGeminiText(nil)).To(BeEmpty())
	})

	It("returns empty string for no candidates", func() {
		resp := &genai.GenerateContentResponse{Candidates: nil}
		Expect(extractGeminiText(resp)).To(BeEmpty())
	})

	It("panics on empty candidate with nil Content", func() {
		resp := &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{{}},
		}
		Expect(func() { extractGeminiText(resp) }).To(Panic())
	})

	It("concatenates text parts", func() {
		resp := &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{
					Content: &genai.Content{
						Parts: []*genai.Part{
							{Text: "Hello "},
							{Text: "World"},
						},
					},
				},
			},
		}
		Expect(extractGeminiText(resp)).To(Equal("Hello World"))
	})

	It("returns empty string for empty text parts", func() {
		resp := &genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{
					Content: &genai.Content{
						Parts: []*genai.Part{
							{Text: ""},
							{Text: ""},
						},
					},
				},
			},
		}
		Expect(extractGeminiText(resp)).To(BeEmpty())
	})
})

var _ = Describe("splitPages", func() {
	It("returns a single item for text without page markers", func() {
		req := ParseRequest{
			Filename: "test.pdf",
			SourceID: "src:1",
		}
		items := splitPages("Hello world", req, "application/pdf", "test")

		Expect(items).To(HaveLen(1))
		Expect(items[0].Content).To(Equal("Hello world"))
		Expect(items[0].ID).To(Equal("src:1:page:1"))
		Expect(items[0].Metadata["parser"]).To(Equal("test"))
		Expect(items[0].Metadata["mime_type"]).To(Equal("application/pdf"))
	})

	It("splits on page markers into multiple items", func() {
		text := "Intro\n--- PAGE 1 ---\nFirst page\n--- PAGE 2 ---\nSecond page"
		req := ParseRequest{
			Filename: "doc.docx",
			SourceID: "src:2",
		}
		items := splitPages(text, req, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "gemini")

		Expect(items).To(HaveLen(3))
		Expect(items[0].ID).To(Equal("src:2:page:1"))
		Expect(items[1].ID).To(Equal("src:2:page:2"))
		Expect(items[2].ID).To(Equal("src:2:page:3"))
	})

	It("skips empty pages between markers", func() {
		text := "Content\n--- PAGE 1 ---\n--- PAGE 2 ---\nReal content"
		req := ParseRequest{
			Filename: "test.pdf",
			SourceID: "src:3",
		}
		items := splitPages(text, req, "application/pdf", "gemini")

		for _, item := range items {
			Expect(item.Content).NotTo(BeEmpty(), "empty pages should be skipped")
		}
	})
})
