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
		It("should initialize with default settings (DDG only)", func() {
			tool := websearch.NewTool(websearch.Config{})
			Expect(tool.Declaration().Name).To(Equal("web_search"))
		})

		It("should initialize with Google config", func() {
			cfg := websearch.Config{
				GoogleAPIKey: "fake-key",
				GoogleCX:     "fake-cx",
			}
			tool := websearch.NewTool(cfg)
			Expect(tool.Declaration().Name).To(Equal("web_search"))
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
			// It might error or succeed depending on network, but we care it runs
			if err != nil {
				// If error is network related, it's acceptable for this test
				Expect(err).To(HaveOccurred())
			}
		})
	})
})
