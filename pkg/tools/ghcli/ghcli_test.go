package ghcli_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/tools/ghcli"
)

var _ = Describe("GH CLI Tool Provider", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("New", func() {
		It("returns nil when token is empty", func() {
			provider := ghcli.New(ctx, ghcli.Config{Token: ""})
			Expect(provider).To(BeNil())
		})

		It("returns nil when gh binary is not on PATH", func() {
			GinkgoT().Setenv("PATH", "/nonexistent")
			provider := ghcli.New(ctx, ghcli.Config{Token: "test-token"})
			Expect(provider).To(BeNil())
		})
	})

	Describe("GetTools", func() {
		It("returns a single tool named gh_cli", func() {
			provider := ghcli.New(ctx, ghcli.Config{Token: "dummy"})
			if provider == nil {
				Skip("gh binary not available on PATH, skipping GetTools test")
			}
			tools := provider.GetTools()
			Expect(tools).To(HaveLen(1))
			Expect(tools[0].Declaration().Name).To(Equal("gh_cli"))
		})
	})

	Describe("Tool Declaration", func() {
		It("has required command input parameter", func() {
			provider := ghcli.New(ctx, ghcli.Config{Token: "dummy"})
			if provider == nil {
				Skip("gh binary not available on PATH")
			}
			decl := provider.GetTools()[0].Declaration()
			Expect(decl.InputSchema.Required).To(ContainElement("command"))
			Expect(decl.InputSchema.Properties).To(HaveKey("command"))
		})
	})
})
