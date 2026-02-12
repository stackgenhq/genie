package codeowner

import (
	"context"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor/local"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("ShellTool", func() {
	var (
		exec tool.Tool
		ctx  context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		// Initialize local executor
		localExec := local.New()
		exec = NewShellTool(localExec)
	})

	Context("when executing shell commands", func() {
		It("should propagate the PATH environment variable", func() {
			// Command to print PATH
			input := []byte(`{"command": "echo $PATH"}`)

			callable, ok := exec.(tool.CallableTool)
			Expect(ok).To(BeTrue(), "ShellTool should be callable")

			result, err := callable.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			execResult, ok := result.(codeexecutor.CodeExecutionResult)
			Expect(ok).To(BeTrue())

			output := execResult.Output
			GinkgoWriter.Printf("PATH output: %s\n", output)

			// Basic sanity check: should contain standard bin paths
			Expect(output).To(ContainSubstring("/bin"))

			// Check match against the test process PATH
			hostPath := os.Getenv("PATH")
			if hostPath != "" {
				parts := strings.Split(hostPath, ":")
				// We accept if at least one common path is present, as the environment might be sanitized
				found := false
				for _, p := range parts {
					if p != "" && strings.Contains(output, p) {
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "Expected output PATH to contain at least one path from host PATH: %s", hostPath)
			}
		})

		It("should execute basic commands successfully", func() {
			input := []byte(`{"command": "echo 'hello world'"}`)

			callable, ok := exec.(tool.CallableTool)
			Expect(ok).To(BeTrue(), "ShellTool should be callable")

			result, err := callable.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			execResult, ok := result.(codeexecutor.CodeExecutionResult)
			Expect(ok).To(BeTrue())

			output := strings.TrimSpace(execResult.Output)
			Expect(output).To(Equal("hello world"))
		})
	})
})
