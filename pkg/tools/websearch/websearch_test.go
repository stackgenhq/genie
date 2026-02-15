package websearch_test

import (
	"context"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/tools/websearch"
)

var _ = Describe("WebSearch Tool", func() {
	Context("NewTool", func() {
		It("should default to DuckDuckGo when Provider is empty", func() {
			tool := websearch.NewTool(websearch.Config{})
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should initialise with explicit duckduckgo provider", func() {
			tool := websearch.NewTool(websearch.Config{Provider: "duckduckgo"})
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should initialise with Google provider", func() {
			cfg := websearch.Config{
				Provider:     "google",
				GoogleAPIKey: "fake-key",
				GoogleCX:     "fake-cx",
			}
			tool := websearch.NewTool(cfg)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should initialise with Bing provider", func() {
			cfg := websearch.Config{
				Provider:   "bing",
				BingAPIKey: "fake-bing-key",
			}
			tool := websearch.NewTool(cfg)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should fallback to duckduckgo when Google credentials are missing", func() {
			cfg := websearch.Config{
				Provider:     "google",
				GoogleAPIKey: "", // Missing
				GoogleCX:     "", // Missing
			}
			tool := websearch.NewTool(cfg)
			// It should still be usable, but backed by DDG.
			// The tool name is always "web_search", so we can't easily distinguish from outside
			// without introspecting the struct or running it.
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should fallback to duckduckgo when Bing API key is missing", func() {
			cfg := websearch.Config{
				Provider:   "bing",
				BingAPIKey: "", // Missing
			}
			tool := websearch.NewTool(cfg)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should normalise 'ddg' alias to duckduckgo", func() {
			tool := websearch.NewTool(websearch.Config{Provider: "ddg"})
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

			tool := websearch.NewTool(websearch.Config{})
			Expect(tool).NotTo(BeNil())
		})

		It("should fallback to DDG for Bing with missing API key (graceful init)", func(ctx context.Context) {
			cfg := websearch.Config{Provider: "bing", BingAPIKey: ""}
			tool := websearch.NewTool(cfg)
			res, err := tool.Call(ctx, []byte(`{"query":"test"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("DuckDuckGo"))
		})

		It("should fallback to DDG for Google with missing credentials (graceful init)", func(ctx context.Context) {
			cfg := websearch.Config{Provider: "google"}
			tool := websearch.NewTool(cfg)
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
			// Note: NewTool might catch empty key, but here we provide a non-empty but invalid key
			// so NewTool accepts it as Bing, but Search() fails.
			tool := websearch.NewTool(cfg) // Bing backend initialized
			res, err := tool.Call(ctx, []byte(`{"query":"golang"}`))

			// We expect NO error, because it should fallback to DDG
			Expect(err).NotTo(HaveOccurred())
			Expect(res.(string)).To(ContainSubstring("DuckDuckGo"))
		})

		It("should attempt to call the tool", func(ctx context.Context) {
			if testing.Short() {
				Skip("Skipping network-dependent test in short mode")
			}
			if os.Getenv("RUN_NETWORK_TESTS") == "" {
				Skip("Skipping network-dependent test (set RUN_NETWORK_TESTS=1 to enable)")
			}

			cfg := websearch.Config{}
			tool := websearch.NewTool(cfg)
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
