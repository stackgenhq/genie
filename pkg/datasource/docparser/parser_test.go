// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package docparser_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/datasource/docparser"
	"github.com/stackgenhq/genie/pkg/security"
)

var _ = Describe("DocParser", func() {
	var (
		ctx context.Context
		sp  security.SecretProvider
	)

	BeforeEach(func() {
		ctx = context.Background()
		sp = security.NewEnvProvider()
	})

	Describe("Config.New", func() {
		It("returns error for empty provider", func() {
			cfg := docparser.Config{}
			_, err := cfg.New(ctx, sp)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("provider is required"))
		})

		It("returns error for unknown provider", func() {
			cfg := docparser.Config{Provider: "unknown_parser"}
			_, err := cfg.New(ctx, sp)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown provider"))
			Expect(err.Error()).To(ContainSubstring("unknown_parser"))
		})

		It("creates a docling provider with default base URL", func() {
			cfg := docparser.Config{Provider: "docling"}
			p, err := cfg.New(ctx, sp)
			Expect(err).NotTo(HaveOccurred())
			Expect(p).NotTo(BeNil())
		})

		It("creates a docling provider with custom base URL", func() {
			cfg := docparser.Config{
				Provider: "docling",
				Docling:  docparser.DoclingConfig{BaseURL: "http://custom:9999"},
			}
			p, err := cfg.New(ctx, sp)
			Expect(err).NotTo(HaveOccurred())
			Expect(p).NotTo(BeNil())
		})

		It("creates a docling provider case-insensitively", func() {
			cfg := docparser.Config{Provider: "  DOCLING  "}
			p, err := cfg.New(ctx, sp)
			Expect(err).NotTo(HaveOccurred())
			Expect(p).NotTo(BeNil())
		})

		It("returns error for gemini without API key", func() {
			// Use a fake SecretProvider that always returns empty.
			fakeSP := &fakeEmptySecretProvider{}
			cfg := docparser.Config{Provider: "gemini"}
			_, err := cfg.New(ctx, fakeSP)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("GEMINI_API_KEY"))
		})
	})

	Describe("Docling Provider", func() {
		It("parses a multi-page response", func() {
			// Fake Docling Serve returning two pages.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/v1/convert/file"))
				Expect(r.Method).To(Equal(http.MethodPost))
				Expect(r.Header.Get("Content-Type")).To(ContainSubstring("multipart/form-data"))

				resp := map[string]interface{}{
					"document": map[string]interface{}{
						"pages": []map[string]interface{}{
							{"page_no": 1, "text": "Page one content", "tables": []interface{}{}},
							{"page_no": 2, "text": "Page two content", "tables": []interface{}{}},
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errcheck
			}))
			defer srv.Close()

			cfg := docparser.Config{
				Provider: "docling",
				Docling:  docparser.DoclingConfig{BaseURL: srv.URL},
			}
			p, err := cfg.New(ctx, sp)
			Expect(err).NotTo(HaveOccurred())

			items, err := p.Parse(ctx, docparser.ParseRequest{
				Reader:   strings.NewReader("fake pdf content"),
				Filename: "test.pdf",
				SourceID: "test:doc1",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(2))

			Expect(items[0].ID).To(Equal("test:doc1:page:1"))
			Expect(items[0].Content).To(Equal("Page one content"))
			Expect(items[0].Source).To(Equal("docparser"))
			Expect(items[0].Metadata["parser"]).To(Equal("docling"))
			Expect(items[0].Metadata["mime_type"]).To(Equal("application/pdf"))
			Expect(items[0].Metadata["page_number"]).To(Equal("1"))
			Expect(items[0].Metadata["element_type"]).To(Equal("text"))

			Expect(items[1].ID).To(Equal("test:doc1:page:2"))
			Expect(items[1].Content).To(Equal("Page two content"))
			Expect(items[1].Metadata["page_number"]).To(Equal("2"))
		})

		It("parses a response with tables", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := map[string]interface{}{
					"document": map[string]interface{}{
						"pages": []map[string]interface{}{
							{
								"page_no": 1,
								"text":    "Intro text",
								"tables": []map[string]interface{}{
									{"text": "| Col A | Col B |"},
								},
							},
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errcheck
			}))
			defer srv.Close()

			cfg := docparser.Config{
				Provider: "docling",
				Docling:  docparser.DoclingConfig{BaseURL: srv.URL},
			}
			p, err := cfg.New(ctx, sp)
			Expect(err).NotTo(HaveOccurred())

			items, err := p.Parse(ctx, docparser.ParseRequest{
				Reader:   strings.NewReader("fake"),
				Filename: "report.pdf",
				SourceID: "src:1",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Content).To(ContainSubstring("Intro text"))
			Expect(items[0].Content).To(ContainSubstring("[Table]"))
			Expect(items[0].Content).To(ContainSubstring("Col A"))
			Expect(items[0].Metadata["element_type"]).To(Equal("text"))
		})

		It("creates table element_type when only tables and no text", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := map[string]interface{}{
					"document": map[string]interface{}{
						"pages": []map[string]interface{}{
							{
								"page_no": 1,
								"text":    "",
								"tables":  []map[string]interface{}{{"text": "| A | B |"}},
							},
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errcheck
			}))
			defer srv.Close()

			cfg := docparser.Config{
				Provider: "docling",
				Docling:  docparser.DoclingConfig{BaseURL: srv.URL},
			}
			p, err := cfg.New(ctx, sp)
			Expect(err).NotTo(HaveOccurred())

			items, err := p.Parse(ctx, docparser.ParseRequest{
				Reader:   strings.NewReader("f"),
				Filename: "table.xlsx",
				SourceID: "src:2",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Metadata["element_type"]).To(Equal("table"))
		})

		It("falls back to markdown export when no pages", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := map[string]interface{}{
					"document": map[string]interface{}{
						"pages":      []interface{}{},
						"md_content": "# Markdown Content\nHello",
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errcheck
			}))
			defer srv.Close()

			cfg := docparser.Config{
				Provider: "docling",
				Docling:  docparser.DoclingConfig{BaseURL: srv.URL},
			}
			p, err := cfg.New(ctx, sp)
			Expect(err).NotTo(HaveOccurred())

			items, err := p.Parse(ctx, docparser.ParseRequest{
				Reader:   strings.NewReader("f"),
				Filename: "doc.docx",
				SourceID: "src:3",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].Content).To(ContainSubstring("Markdown Content"))
			Expect(items[0].ID).To(Equal("src:3:page:1"))
		})

		It("returns nil when no pages and no markdown", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				resp := map[string]interface{}{
					"document": map[string]interface{}{
						"pages":      []interface{}{},
						"md_content": "",
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp) //nolint:errcheck
			}))
			defer srv.Close()

			cfg := docparser.Config{
				Provider: "docling",
				Docling:  docparser.DoclingConfig{BaseURL: srv.URL},
			}
			p, err := cfg.New(ctx, sp)
			Expect(err).NotTo(HaveOccurred())

			items, err := p.Parse(ctx, docparser.ParseRequest{
				Reader:   strings.NewReader("f"),
				Filename: "empty.pdf",
				SourceID: "src:4",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeNil())
		})

		It("returns error on HTTP error from Docling", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, "internal error")
			}))
			defer srv.Close()

			cfg := docparser.Config{
				Provider: "docling",
				Docling:  docparser.DoclingConfig{BaseURL: srv.URL},
			}
			p, err := cfg.New(ctx, sp)
			Expect(err).NotTo(HaveOccurred())

			_, err = p.Parse(ctx, docparser.ParseRequest{
				Reader:   strings.NewReader("f"),
				Filename: "bad.pdf",
				SourceID: "src:5",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unexpected status 500"))
		})

		It("returns error on invalid JSON from Docling", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, "not json")
			}))
			defer srv.Close()

			cfg := docparser.Config{
				Provider: "docling",
				Docling:  docparser.DoclingConfig{BaseURL: srv.URL},
			}
			p, err := cfg.New(ctx, sp)
			Expect(err).NotTo(HaveOccurred())

			_, err = p.Parse(ctx, docparser.ParseRequest{
				Reader:   strings.NewReader("f"),
				Filename: "bad.pdf",
				SourceID: "src:6",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("decode response"))
		})

		It("returns error when Docling server is unreachable", func() {
			cfg := docparser.Config{
				Provider: "docling",
				Docling:  docparser.DoclingConfig{BaseURL: "http://127.0.0.1:1"},
			}
			p, err := cfg.New(ctx, sp)
			Expect(err).NotTo(HaveOccurred())

			_, err = p.Parse(ctx, docparser.ParseRequest{
				Reader:   strings.NewReader("f"),
				Filename: "doc.pdf",
				SourceID: "src:7",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("request failed"))
		})
	})

	Describe("splitPages via SplitOnPageMarkers", func() {
		It("returns single element for text without markers", func() {
			pages := docparser.SplitOnPageMarkers("just some text")
			Expect(pages).To(HaveLen(1))
			Expect(pages[0]).To(ContainSubstring("just some text"))
		})

		It("splits on multiple page markers", func() {
			text := "intro\n--- PAGE 1 ---\nfirst page\n--- PAGE 2 ---\nsecond page"
			pages := docparser.SplitOnPageMarkers(text)
			Expect(pages).To(HaveLen(3))
			Expect(pages[0]).To(ContainSubstring("intro"))
			Expect(pages[1]).To(ContainSubstring("first page"))
			Expect(pages[2]).To(ContainSubstring("second page"))
		})

		It("handles case-insensitive markers", func() {
			text := "--- page 1 ---\nContent A\n--- Page 2 ---\nContent B"
			pages := docparser.SplitOnPageMarkers(text)
			Expect(pages).To(HaveLen(2))
			Expect(pages[0]).To(ContainSubstring("Content A"))
			Expect(pages[1]).To(ContainSubstring("Content B"))
		})

		It("returns empty text as-is", func() {
			pages := docparser.SplitOnPageMarkers("")
			Expect(pages).To(HaveLen(1))
		})
	})

	Describe("DetectMIME", func() {
		It("detects DOC files", func() {
			Expect(docparser.DetectMIME("file.doc")).To(Equal("application/msword"))
		})

		It("detects PPT files", func() {
			Expect(docparser.DetectMIME("file.ppt")).To(Equal("application/vnd.ms-powerpoint"))
		})

		It("detects XLS files", func() {
			Expect(docparser.DetectMIME("file.xls")).To(Equal("application/vnd.ms-excel"))
		})

		It("detects .markdown files", func() {
			Expect(docparser.DetectMIME("file.markdown")).To(Equal("text/markdown"))
		})

		It("uses stdlib fallback for known types", func() {
			mime := docparser.DetectMIME("index.html")
			Expect(mime).To(ContainSubstring("text/html"))
		})

		It("returns octet-stream for unknown extension", func() {
			Expect(docparser.DetectMIME("file.zzz")).To(Equal("application/octet-stream"))
		})
	})
})

// fakeEmptySecretProvider is a test helper that always returns empty strings.
type fakeEmptySecretProvider struct{}

func (f *fakeEmptySecretProvider) GetSecret(_ context.Context, _ security.GetSecretRequest) (string, error) {
	return "", nil
}

// Ensure fakeEmptySecretProvider satisfies the interface at compile time.
var _ security.SecretProvider = (*fakeEmptySecretProvider)(nil)
