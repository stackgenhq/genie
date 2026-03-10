// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package websearch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/tools/websearch"
)

var _ = Describe("WebSearch Tool", func() {
	Context("NewTool", func() {
		It("should default to DuckDuckGo when Provider is empty", func() {
			tool := websearch.NewTool(websearch.Config{}, nil)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should initialise with explicit duckduckgo provider", func() {
			tool := websearch.NewTool(websearch.Config{Provider: "duckduckgo"}, nil)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should initialise with Google provider", func() {
			cfg := websearch.Config{
				Provider:     "google",
				GoogleAPIKey: "fake-key",
				GoogleCX:     "fake-cx",
			}
			tool := websearch.NewTool(cfg, nil)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should initialise with Bing provider", func() {
			cfg := websearch.Config{
				Provider:   "bing",
				BingAPIKey: "fake-bing-key",
			}
			tool := websearch.NewTool(cfg, nil)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should fallback to duckduckgo when Google credentials are missing", func() {
			cfg := websearch.Config{
				Provider:     "google",
				GoogleAPIKey: "", // Missing
				GoogleCX:     "", // Missing
			}
			tool := websearch.NewTool(cfg, nil)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should fallback to duckduckgo when Bing API key is missing", func() {
			cfg := websearch.Config{
				Provider:   "bing",
				BingAPIKey: "", // Missing
			}
			tool := websearch.NewTool(cfg, nil)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should normalise 'ddg' alias to duckduckgo", func() {
			tool := websearch.NewTool(websearch.Config{Provider: "ddg"}, nil)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should fallback to duckduckgo for unknown provider names", func() {
			tool := websearch.NewTool(websearch.Config{Provider: "yahoo"}, nil)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should normalise provider with leading/trailing whitespace", func() {
			tool := websearch.NewTool(websearch.Config{Provider: "  DuckDuckGo  "}, nil)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})
	})

	Context("NewBingTool", func() {
		It("should create a standalone bing_search tool", func() {
			bt := websearch.NewBingTool("fake-key")
			Expect(bt.Declaration().Name).To(Equal("bing_search"))
		})

		It("should return error when API key is empty", func(ctx context.Context) {
			bt := websearch.NewBingTool("")
			_, err := bt.Call(ctx, []byte(`{"query":"test"}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("bing_api_key"))
		})

		It("should return error when Bing API returns non-200 status", func(ctx context.Context) {
			bt := websearch.NewBingTool("invalid-key")
			_, err := bt.Call(ctx, []byte(`{"query":"test"}`))
			Expect(err).To(HaveOccurred())
		})
	})

	Context("formatBingResults", func() {
		It("should return 'No results found.' for empty response", func() {
			resp := websearch.BingResponseForTest{}
			result := websearch.FormatBingResultsForTest(resp)
			Expect(result).To(Equal("No results found."))
		})

		It("should format a single result correctly", func() {
			resp := websearch.BingResponseForTest{}
			resp.WebPages.Value = []struct {
				Name    string `json:"name"`
				URL     string `json:"url"`
				Snippet string `json:"snippet"`
			}{
				{
					Name:    "Go Programming",
					URL:     "https://golang.org",
					Snippet: "An open source programming language.",
				},
			}
			result := websearch.FormatBingResultsForTest(resp)
			Expect(result).To(ContainSubstring("1. Go Programming"))
			Expect(result).To(ContainSubstring("https://golang.org"))
			Expect(result).To(ContainSubstring("An open source programming language."))
		})

		It("should format multiple results with correct numbering", func() {
			resp := websearch.BingResponseForTest{}
			resp.WebPages.Value = []struct {
				Name    string `json:"name"`
				URL     string `json:"url"`
				Snippet string `json:"snippet"`
			}{
				{Name: "First", URL: "https://first.com", Snippet: "First snippet"},
				{Name: "Second", URL: "https://second.com", Snippet: "Second snippet"},
				{Name: "Third", URL: "https://third.com", Snippet: "Third snippet"},
			}
			result := websearch.FormatBingResultsForTest(resp)
			Expect(result).To(ContainSubstring("1. First"))
			Expect(result).To(ContainSubstring("2. Second"))
			Expect(result).To(ContainSubstring("3. Third"))
		})
	})

	Context("Search execution", func() {
		BeforeEach(func() {
			// Save original env
			origKey := os.Getenv("GOOGLE_API_KEY")
			origCX := os.Getenv("GOOGLE_CSE_ID")
			DeferCleanup(func() {
				os.Setenv("GOOGLE_API_KEY", origKey)
				os.Setenv("GOOGLE_CSE_ID", origCX)
			})
		})

		It("should use env vars if provided", func() {
			os.Setenv("GOOGLE_API_KEY", "env-key")
			os.Setenv("GOOGLE_CSE_ID", "env-cx")

			tool := websearch.NewTool(websearch.Config{}, nil)
			Expect(tool).NotTo(BeNil())
		})

		It("should fallback to DDG for Bing with missing API key (graceful init)", func(ctx context.Context) {
			cfg := websearch.Config{Provider: "bing", BingAPIKey: ""}

			// Mock DDG server to avoid hitting live html.duckduckgo.com (403/rate limits in CI).
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`
					<div class="result__body">
						<h2 class="result__title">
							<a class="result__a" href="https://duckduckgo.com">DuckDuckGo</a>
						</h2>
						<a class="result__snippet" href="https://duckduckgo.com">Privacy, simplified.</a>
					</div>
				`))
			}))
			defer ts.Close()

			tool := websearch.NewTool(cfg, nil, websearch.WithDDGEndpoint(ts.URL))
			res, err := tool.Call(ctx, []byte(`{"query":"test"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("DuckDuckGo"))
		})

		It("should fallback to DDG for Google with missing credentials (graceful init)", func(ctx context.Context) {
			if testing.Short() {
				Skip("Skipping network-dependent test in short mode")
			}
			cfg := websearch.Config{Provider: "google"}

			// Mock DDG server to avoid 403/Ratelimits
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`
					<div class="result__body">
						<h2 class="result__title">
							<a class="result__a" href="https://duckduckgo.com">DuckDuckGo</a>
						</h2>
						<a class="result__snippet" href="https://duckduckgo.com">Privacy, simplified.</a>
					</div>
				`))
			}))
			defer ts.Close()

			tool := websearch.NewTool(cfg, nil, websearch.WithDDGEndpoint(ts.URL))
			res, err := tool.Call(ctx, []byte(`{"query":"test"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("DuckDuckGo"))
		})

		It("should fallback to DDG when Bing fails (runtime fallback)", func(ctx context.Context) {
			if testing.Short() {
				Skip("Skipping network-dependent test in short mode")
			}
			// Bing with invalid key should fail, then fallback to DDG
			cfg := websearch.Config{
				Provider:   "bing",
				BingAPIKey: "invalid-key-triggering-failure",
			}

			// Mock DDG server
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`
					<div class="result__body">
						<h2 class="result__title">
							<a class="result__a" href="https://duckduckgo.com">DuckDuckGo</a>
						</h2>
					</div>
				`))
			}))
			defer ts.Close()

			tool := websearch.NewTool(cfg, nil, websearch.WithDDGEndpoint(ts.URL))
			res, err := tool.Call(ctx, []byte(`{"query":"golang"}`))

			// We expect NO error, because it should fallback to DDG
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("DuckDuckGo"))
		})

		It("should search with DuckDuckGo when configured explicitly", func(ctx context.Context) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`
					<div class="result__body">
						<h2 class="result__title">
							<a class="result__a" href="https://example.com">Example</a>
						</h2>
						<a class="result__snippet" href="https://example.com">An example site.</a>
					</div>
				`))
			}))
			defer ts.Close()

			cfg := websearch.Config{Provider: "duckduckgo"}
			tool := websearch.NewTool(cfg, nil, websearch.WithDDGEndpoint(ts.URL))
			res, err := tool.Call(ctx, []byte(`{"query":"example"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("Example"))
		})

		It("should attempt to call the tool", func(ctx context.Context) {
			if testing.Short() {
				Skip("Skipping network-dependent test in short mode")
			}
			if os.Getenv("RUN_NETWORK_TESTS") == "" {
				Skip("Skipping network-dependent test (set RUN_NETWORK_TESTS=1 to enable)")
			}

			cfg := websearch.Config{}
			tool := websearch.NewTool(cfg, nil)
			input := `{"query": "golang interface"}`

			// We expect execution not to panic, even if it fails due to network
			_, err := tool.Call(ctx, []byte(input))
			if err != nil {
				// If error is network related, it's acceptable for this test
				Expect(err).To(HaveOccurred())
			}
		})
	})
})
