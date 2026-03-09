package ghcli_test

import (
	"context"
	"encoding/json"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/tools/ghcli"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("GH CLI Tool Call", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Call", func() {
		It("returns error for empty command", func() {
			provider := ghcli.New(ctx, ghcli.Config{Token: "dummy"})
			if provider == nil {
				Skip("gh binary not available on PATH")
			}
			tools := provider.GetTools()
			Expect(tools).To(HaveLen(1))

			callable, ok := tools[0].(tool.CallableTool)
			Expect(ok).To(BeTrue(), "tool should implement CallableTool")

			input, _ := json.Marshal(map[string]string{"command": ""})
			_, err := callable.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("command is required"))
		})

		It("returns error for whitespace-only command", func() {
			provider := ghcli.New(ctx, ghcli.Config{Token: "dummy"})
			if provider == nil {
				Skip("gh binary not available on PATH")
			}
			tools := provider.GetTools()
			callable, ok := tools[0].(tool.CallableTool)
			Expect(ok).To(BeTrue())

			input, _ := json.Marshal(map[string]string{"command": "   "})
			_, err := callable.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("command is required"))
		})

		It("returns error for invalid JSON input", func() {
			provider := ghcli.New(ctx, ghcli.Config{Token: "dummy"})
			if provider == nil {
				Skip("gh binary not available on PATH")
			}
			tools := provider.GetTools()
			callable, ok := tools[0].(tool.CallableTool)
			Expect(ok).To(BeTrue())

			_, err := callable.Call(ctx, []byte("not-json"))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse"))
		})

		It("executes a valid gh command successfully", func() {
			if _, err := exec.LookPath("gh"); err != nil {
				Skip("gh binary not on PATH")
			}

			provider := ghcli.New(ctx, ghcli.Config{Token: "dummy-token-for-test"})
			if provider == nil {
				Skip("gh tool not available")
			}
			tools := provider.GetTools()
			callable, ok := tools[0].(tool.CallableTool)
			Expect(ok).To(BeTrue())

			// Use --help as a safe command that should always succeed
			input, _ := json.Marshal(map[string]string{"command": "--help"})
			result, err := callable.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			resultStr, ok := result.(string)
			Expect(ok).To(BeTrue())
			Expect(resultStr).To(ContainSubstring("gh"))
		})

		It("returns error for invalid gh command", func() {
			if _, err := exec.LookPath("gh"); err != nil {
				Skip("gh binary not on PATH")
			}

			provider := ghcli.New(ctx, ghcli.Config{Token: "dummy-token"})
			if provider == nil {
				Skip("gh tool not available")
			}
			tools := provider.GetTools()
			callable, ok := tools[0].(tool.CallableTool)
			Expect(ok).To(BeTrue())

			input, _ := json.Marshal(map[string]string{"command": "nonexistent-subcommand-abc123"})
			_, err := callable.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("gh command failed"))
		})

		// --- Shell metacharacter injection prevention ---

		DescribeTable("rejects commands containing shell metacharacters",
			func(cmd string) {
				provider := ghcli.New(ctx, ghcli.Config{Token: "dummy"})
				if provider == nil {
					Skip("gh binary not available on PATH")
				}
				tools := provider.GetTools()
				callable, ok := tools[0].(tool.CallableTool)
				Expect(ok).To(BeTrue())

				input, _ := json.Marshal(map[string]string{"command": cmd})
				_, err := callable.Call(ctx, input)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("disallowed shell metacharacters"))
			},
			Entry("pipe", "run list | cat"),
			Entry("semicolon", "run list; echo pwned"),
			Entry("backtick", "run list `whoami`"),
			Entry("dollar-paren", "run list $(whoami)"),
			Entry("backslash", "run list \\n"),
			Entry("hash comment", "run list # ignore rest"),
			Entry("redirect", "run list > /tmp/out"),
			Entry("ampersand", "run list & background"),
		)
	})
})
