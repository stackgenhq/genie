// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package websearch_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/tools/websearch"
)

var _ = Describe("SerpAPI Tool", func() {
	Context("NewSerpAPITool", func() {
		It("should create a serpapi_search tool", func() {
			cfg := websearch.SerpAPIConfig{APIKey: "fake-key"}
			tool := websearch.NewSerpAPITool(cfg)
			Expect(tool.Declaration().Name).To(Equal("serpapi_search"))
		})

		It("should return error when API key is empty", func(ctx context.Context) {
			cfg := websearch.SerpAPIConfig{APIKey: ""}
			tool := websearch.NewSerpAPITool(cfg)
			_, err := tool.Call(ctx, []byte(`{"query":"test"}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("serpapi_api_key"))
		})

		It("should return error for empty query", func(ctx context.Context) {
			cfg := websearch.SerpAPIConfig{APIKey: "fake-key"}
			tool := websearch.NewSerpAPITool(cfg)
			_, err := tool.Call(ctx, []byte(`{"query":""}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("empty search query"))
		})
	})

	Context("NewSerpAPINewsTool", func() {
		It("should create a google_news_search tool", func() {
			cfg := websearch.SerpAPIConfig{APIKey: "fake-key"}
			tool := websearch.NewSerpAPINewsTool(cfg)
			Expect(tool.Declaration().Name).To(Equal("google_news_search"))
		})
	})

	Context("NewSerpAPIScholarTool", func() {
		It("should create a google_scholar_search tool", func() {
			cfg := websearch.SerpAPIConfig{APIKey: "fake-key"}
			tool := websearch.NewSerpAPIScholarTool(cfg)
			Expect(tool.Declaration().Name).To(Equal("google_scholar_search"))
		})
	})

	Context("SerpAPI Google Search execution", func() {
		It("should return formatted organic results from mock server", func(ctx context.Context) {
			mockResp := map[string]interface{}{
				"organic_results": []map[string]interface{}{
					{
						"position": 1,
						"title":    "Go Programming Language",
						"link":     "https://go.dev",
						"snippet":  "Go is an open source programming language.",
					},
					{
						"position": 2,
						"title":    "Go Tutorial",
						"link":     "https://go.dev/doc/tutorial",
						"snippet":  "Learn how to program in Go.",
					},
				},
			}

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Query().Get("engine")).To(Equal("google"))
				Expect(r.URL.Query().Get("q")).To(Equal("golang"))
				Expect(r.URL.Query().Get("api_key")).To(Equal("test-key"))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(mockResp)
			}))
			defer ts.Close()

			cfg := websearch.SerpAPIConfig{APIKey: "test-key"}
			tool := websearch.NewSerpAPITool(cfg, websearch.WithSerpAPIEndpoint(ts.URL))
			res, err := tool.Call(ctx, []byte(`{"query":"golang"}`))
			Expect(err).NotTo(HaveOccurred())
			result := res.(string)
			Expect(result).To(ContainSubstring("Go Programming Language"))
			Expect(result).To(ContainSubstring("https://go.dev"))
			Expect(result).To(ContainSubstring("SerpAPI / Google"))
		})

		It("should include answer box when present", func(ctx context.Context) {
			mockResp := map[string]interface{}{
				"answer_box": map[string]interface{}{
					"title":   "What is Go?",
					"answer":  "Go is a programming language.",
					"snippet": "Go was designed at Google.",
					"link":    "https://go.dev",
				},
				"organic_results": []map[string]interface{}{},
			}

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(mockResp)
			}))
			defer ts.Close()

			cfg := websearch.SerpAPIConfig{APIKey: "test-key"}
			tool := websearch.NewSerpAPITool(cfg, websearch.WithSerpAPIEndpoint(ts.URL))
			res, err := tool.Call(ctx, []byte(`{"query":"what is go"}`))
			Expect(err).NotTo(HaveOccurred())
			result := res.(string)
			Expect(result).To(ContainSubstring("[Answer Box]"))
			Expect(result).To(ContainSubstring("Go is a programming language."))
		})

		It("should include knowledge graph when present", func(ctx context.Context) {
			mockResp := map[string]interface{}{
				"knowledge_graph": map[string]interface{}{
					"title":       "Go",
					"type":        "Programming Language",
					"description": "Go is a statically typed, compiled language.",
				},
				"organic_results": []map[string]interface{}{
					{
						"position": 1,
						"title":    "Go Programming",
						"link":     "https://go.dev",
						"snippet":  "Go is an open source programming language.",
					},
				},
			}

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(mockResp)
			}))
			defer ts.Close()

			cfg := websearch.SerpAPIConfig{APIKey: "test-key"}
			tool := websearch.NewSerpAPITool(cfg, websearch.WithSerpAPIEndpoint(ts.URL))
			res, err := tool.Call(ctx, []byte(`{"query":"go programming"}`))
			Expect(err).NotTo(HaveOccurred())
			result := res.(string)
			Expect(result).To(ContainSubstring("[Knowledge Graph: Go (Programming Language)]"))
			Expect(result).To(ContainSubstring("statically typed"))
		})

		It("should return 'No results found.' for empty organic results", func(ctx context.Context) {
			mockResp := map[string]interface{}{
				"organic_results": []map[string]interface{}{},
			}

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(mockResp)
			}))
			defer ts.Close()

			cfg := websearch.SerpAPIConfig{APIKey: "test-key"}
			tool := websearch.NewSerpAPITool(cfg, websearch.WithSerpAPIEndpoint(ts.URL))
			res, err := tool.Call(ctx, []byte(`{"query":"xyznonexistent123"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(Equal("No results found."))
		})

		It("should pass location and language parameters", func(ctx context.Context) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Query().Get("location")).To(Equal("Austin, Texas"))
				Expect(r.URL.Query().Get("gl")).To(Equal("us"))
				Expect(r.URL.Query().Get("hl")).To(Equal("en"))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"organic_results": []map[string]interface{}{},
				})
			}))
			defer ts.Close()

			cfg := websearch.SerpAPIConfig{
				APIKey:   "test-key",
				Location: "Austin, Texas",
				GL:       "us",
				HL:       "en",
			}
			tool := websearch.NewSerpAPITool(cfg, websearch.WithSerpAPIEndpoint(ts.URL))
			_, err := tool.Call(ctx, []byte(`{"query":"test"}`))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error for non-200 status", func(ctx context.Context) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":"Invalid API key"}`))
			}))
			defer ts.Close()

			cfg := websearch.SerpAPIConfig{APIKey: "bad-key"}
			tool := websearch.NewSerpAPITool(cfg, websearch.WithSerpAPIEndpoint(ts.URL))
			_, err := tool.Call(ctx, []byte(`{"query":"test"}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("HTTP 403"))
		})
	})

	Context("SerpAPI Google News execution", func() {
		It("should return formatted news results from mock server", func(ctx context.Context) {
			mockResp := map[string]interface{}{
				"news_results": []map[string]interface{}{
					{
						"position": 1,
						"title":    "Go 1.22 Released",
						"link":     "https://go.dev/blog/go1.22",
						"source":   "Go Blog",
						"date":     "2 hours ago",
						"snippet":  "The Go team has released Go 1.22.",
					},
					{
						"position": 2,
						"title":    "Go Tops Developer Survey",
						"link":     "https://example.com/survey",
						"source":   "Tech News",
						"date":     "1 day ago",
						"snippet":  "Go is now the most wanted language.",
					},
				},
			}

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Query().Get("engine")).To(Equal("google_news"))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(mockResp)
			}))
			defer ts.Close()

			cfg := websearch.SerpAPIConfig{APIKey: "test-key"}
			tool := websearch.NewSerpAPINewsTool(cfg, websearch.WithSerpAPIEndpoint(ts.URL))
			res, err := tool.Call(ctx, []byte(`{"query":"golang"}`))
			Expect(err).NotTo(HaveOccurred())
			result := res.(string)
			Expect(result).To(ContainSubstring("Go 1.22 Released"))
			Expect(result).To(ContainSubstring("Source: Go Blog"))
			Expect(result).To(ContainSubstring("SerpAPI / Google News"))
		})

		It("should return 'No news results found.' for empty response", func(ctx context.Context) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"news_results": []map[string]interface{}{},
				})
			}))
			defer ts.Close()

			cfg := websearch.SerpAPIConfig{APIKey: "test-key"}
			tool := websearch.NewSerpAPINewsTool(cfg, websearch.WithSerpAPIEndpoint(ts.URL))
			res, err := tool.Call(ctx, []byte(`{"query":"xyznonexistent123"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(Equal("No news results found."))
		})
	})

	Context("SerpAPI Google Scholar execution", func() {
		It("should return formatted scholar results from mock server", func(ctx context.Context) {
			mockResp := map[string]interface{}{
				"organic_results": []map[string]interface{}{
					{
						"position": 1,
						"title":    "Attention Is All You Need",
						"link":     "https://arxiv.org/abs/1706.03762",
						"snippet":  "The dominant sequence transduction models...",
						"publication_info": map[string]interface{}{
							"summary": "A Vaswani, N Shazeer, N Parmar... - Advances in neural information processing systems, 2017",
							"authors": []map[string]interface{}{
								{"name": "A Vaswani"},
								{"name": "N Shazeer"},
							},
						},
						"inline_links": map[string]interface{}{
							"cited_by": map[string]interface{}{
								"total": 95000,
								"link":  "https://scholar.google.com/cited",
							},
						},
					},
				},
			}

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Query().Get("engine")).To(Equal("google_scholar"))
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(mockResp)
			}))
			defer ts.Close()

			cfg := websearch.SerpAPIConfig{APIKey: "test-key"}
			tool := websearch.NewSerpAPIScholarTool(cfg, websearch.WithSerpAPIEndpoint(ts.URL))
			res, err := tool.Call(ctx, []byte(`{"query":"attention is all you need"}`))
			Expect(err).NotTo(HaveOccurred())
			result := res.(string)
			Expect(result).To(ContainSubstring("Attention Is All You Need"))
			Expect(result).To(ContainSubstring("A Vaswani"))
			Expect(result).To(ContainSubstring("Cited by: 95000"))
			Expect(result).To(ContainSubstring("SerpAPI / Google Scholar"))
		})

		It("should return 'No scholar results found.' for empty response", func(ctx context.Context) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"organic_results": []map[string]interface{}{},
				})
			}))
			defer ts.Close()

			cfg := websearch.SerpAPIConfig{APIKey: "test-key"}
			tool := websearch.NewSerpAPIScholarTool(cfg, websearch.WithSerpAPIEndpoint(ts.URL))
			res, err := tool.Call(ctx, []byte(`{"query":"xyznonexistent123"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(Equal("No scholar results found."))
		})
	})

	Context("SerpAPI as web_search provider", func() {
		It("should initialise with serpapi provider", func() {
			cfg := websearch.Config{
				Provider: "serpapi",
				SerpAPI:  websearch.SerpAPIConfig{APIKey: "fake-key"},
			}
			tool := websearch.NewTool(cfg, nil)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should normalise 'serp' alias to serpapi", func() {
			cfg := websearch.Config{
				Provider: "serp",
				SerpAPI:  websearch.SerpAPIConfig{APIKey: "fake-key"},
			}
			tool := websearch.NewTool(cfg, nil)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should fallback to duckduckgo when serpapi API key is missing", func() {
			cfg := websearch.Config{
				Provider: "serpapi",
				SerpAPI:  websearch.SerpAPIConfig{APIKey: ""},
			}
			tool := websearch.NewTool(cfg, nil)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})
	})

	Context("ToolProvider with SerpAPI", func() {
		It("should include news and scholar tools when serpapi is configured", func() {
			cfg := websearch.Config{
				Provider: "serpapi",
				SerpAPI:  websearch.SerpAPIConfig{APIKey: "fake-key"},
			}
			provider := websearch.NewToolProvider(cfg)
			tools := provider.GetTools()

			names := make([]string, 0, len(tools))
			for _, t := range tools {
				names = append(names, t.Declaration().Name)
			}

			Expect(names).To(ContainElement("web_search"))
			Expect(names).To(ContainElement("wikipedia_search"))
			Expect(names).To(ContainElement("google_news_search"))
			Expect(names).To(ContainElement("google_scholar_search"))
		})

		It("should not include news and scholar tools when serpapi is not the provider", func() {
			cfg := websearch.Config{
				Provider: "duckduckgo",
			}
			provider := websearch.NewToolProvider(cfg)
			tools := provider.GetTools()

			names := make([]string, 0, len(tools))
			for _, t := range tools {
				names = append(names, t.Declaration().Name)
			}

			Expect(names).To(ContainElement("web_search"))
			Expect(names).To(ContainElement("wikipedia_search"))
			Expect(names).NotTo(ContainElement("google_news_search"))
			Expect(names).NotTo(ContainElement("google_scholar_search"))
		})
	})

	Context("formatSerpAPIOrganicResults", func() {
		It("should format organic results correctly", func() {
			body := `{
				"organic_results": [
					{"position": 1, "title": "First Result", "link": "https://first.com", "snippet": "First snippet"},
					{"position": 2, "title": "Second Result", "link": "https://second.com", "snippet": "Second snippet"}
				]
			}`
			result, err := websearch.FormatSerpAPIOrganicResultsForTest([]byte(body))
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(ContainSubstring("1. First Result"))
			Expect(result).To(ContainSubstring("2. Second Result"))
			Expect(result).To(ContainSubstring("https://first.com"))
		})

		It("should return 'No results found.' for empty organic results", func() {
			body := `{"organic_results": []}`
			result, err := websearch.FormatSerpAPIOrganicResultsForTest([]byte(body))
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("No results found."))
		})
	})

	Context("formatSerpAPINewsResults", func() {
		It("should format news results with source and date", func() {
			body := `{
				"news_results": [
					{"position": 1, "title": "Breaking News", "link": "https://news.com", "source": "News Corp", "date": "1 hour ago", "snippet": "Important news."}
				]
			}`
			result, err := websearch.FormatSerpAPINewsResultsForTest([]byte(body))
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(ContainSubstring("Breaking News"))
			Expect(result).To(ContainSubstring("Source: News Corp"))
			Expect(result).To(ContainSubstring("Date: 1 hour ago"))
		})
	})

	Context("formatSerpAPIScholarResults", func() {
		It("should format scholar results with authors and citations", func() {
			body := `{
				"organic_results": [{
					"position": 1,
					"title": "A Great Paper",
					"link": "https://arxiv.org/abs/1234",
					"snippet": "We present a novel approach.",
					"publication_info": {
						"summary": "J Doe, J Smith - Journal of Example, 2024",
						"authors": [{"name": "J Doe"}, {"name": "J Smith"}]
					},
					"inline_links": {
						"cited_by": {"total": 42, "link": "https://scholar.google.com"}
					}
				}]
			}`
			result, err := websearch.FormatSerpAPIScholarResultsForTest([]byte(body))
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(ContainSubstring("A Great Paper"))
			Expect(result).To(ContainSubstring("Authors: J Doe, J Smith"))
			Expect(result).To(ContainSubstring("Cited by: 42"))
		})
	})
})
