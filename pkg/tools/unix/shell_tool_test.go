package unix_test

import (
	"context"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor/local"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/stackgenhq/genie/pkg/tools/unix"
)

var _ = Describe("ShellTool", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	// --- Construction & Config ---

	Context("construction", func() {
		It("defaults to only PATH in allowed env keys", func() {
			t := unix.NewShellTool(local.New(), unix.ShellToolConfig{})
			st := t.(*unix.ShellTool)
			Expect(st.AllowedEnvKeys()).To(Equal([]string{"PATH"}))
		})

		It("adds configured env vars to the allowed set", func() {
			t := unix.NewShellTool(local.New(), unix.ShellToolConfig{
				AllowedEnv: []string{"HOME", "USER"},
			})
			st := t.(*unix.ShellTool)
			keys := st.AllowedEnvKeys()
			Expect(keys).To(ContainElements("HOME", "USER", "PATH"))
			Expect(keys).To(HaveLen(3))
		})

		It("normalises env var keys to uppercase", func() {
			t := unix.NewShellTool(local.New(), unix.ShellToolConfig{
				AllowedEnv: []string{"home", "gOPATH"},
			})
			st := t.(*unix.ShellTool)
			Expect(st.AllowedEnvKeys()).To(ContainElements("HOME", "GOPATH", "PATH"))
		})

		It("returns nil allowed binaries when none are set", func() {
			t := unix.NewShellTool(local.New(), unix.ShellToolConfig{})
			st := t.(*unix.ShellTool)
			Expect(st.AllowedBinaries()).To(BeNil())
		})

		It("records allowed binaries when configured", func() {
			t := unix.NewShellTool(local.New(), unix.ShellToolConfig{
				AllowedBinaries: []string{"ls", "git"},
			})
			st := t.(*unix.ShellTool)
			Expect(st.AllowedBinaries()).To(Equal([]string{"git", "ls"}))
		})
	})

	// --- Declaration ---

	Context("declaration", func() {
		It("returns a tool named run_shell", func() {
			t := unix.NewShellTool(local.New(), unix.ShellToolConfig{})
			decl := t.(tool.Tool).Declaration()
			Expect(decl.Name).To(Equal("run_shell"))
			Expect(decl.InputSchema.Required).To(ContainElement("command"))
		})
	})

	// --- Call: basic execution ---

	Context("basic execution", func() {
		It("executes a simple echo command", func() {
			t := unix.NewShellTool(local.New(), unix.ShellToolConfig{})
			callable := t.(tool.CallableTool)
			result, err := callable.Call(ctx, []byte(`{"command": "echo hello"}`))
			Expect(err).NotTo(HaveOccurred())
			execResult, ok := result.(codeexecutor.CodeExecutionResult)
			Expect(ok).To(BeTrue())
			Expect(strings.TrimSpace(execResult.Output)).To(Equal("hello"))
		})

		It("rejects empty command", func() {
			t := unix.NewShellTool(local.New(), unix.ShellToolConfig{})
			callable := t.(tool.CallableTool)
			_, err := callable.Call(ctx, []byte(`{"command": ""}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("command is required"))
		})

		It("rejects invalid JSON", func() {
			t := unix.NewShellTool(local.New(), unix.ShellToolConfig{})
			callable := t.(tool.CallableTool)
			_, err := callable.Call(ctx, []byte(`not json`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse arguments"))
		})
	})

	// --- Call: binary allowlist ---

	Context("binary allowlist", func() {
		It("allows a whitelisted command", func() {
			t := unix.NewShellTool(local.New(), unix.ShellToolConfig{
				AllowedBinaries: []string{"echo", "ls"},
			})
			callable := t.(tool.CallableTool)
			result, err := callable.Call(ctx, []byte(`{"command": "echo allowed"}`))
			Expect(err).NotTo(HaveOccurred())
			execResult := result.(codeexecutor.CodeExecutionResult)
			Expect(strings.TrimSpace(execResult.Output)).To(Equal("allowed"))
		})

		It("blocks a non-whitelisted command", func() {
			t := unix.NewShellTool(local.New(), unix.ShellToolConfig{
				AllowedBinaries: []string{"ls"},
			})
			callable := t.(tool.CallableTool)
			_, err := callable.Call(ctx, []byte(`{"command": "rm -rf /"}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not in the allowed binaries list"))
		})

		It("allows any command when no allowlist is set", func() {
			t := unix.NewShellTool(local.New(), unix.ShellToolConfig{})
			callable := t.(tool.CallableTool)
			result, err := callable.Call(ctx, []byte(`{"command": "echo no-filter"}`))
			Expect(err).NotTo(HaveOccurred())
			execResult := result.(codeexecutor.CodeExecutionResult)
			Expect(strings.TrimSpace(execResult.Output)).To(Equal("no-filter"))
		})
	})

	// --- ExtractBinaryName ---

	Context("ExtractBinaryName", func() {
		It("extracts simple command", func() {
			Expect(unix.ExtractBinaryName("ls -la")).To(Equal("ls"))
		})

		It("extracts command with full path", func() {
			Expect(unix.ExtractBinaryName("/usr/bin/git status")).To(Equal("git"))
		})

		It("handles command with no arguments", func() {
			Expect(unix.ExtractBinaryName("pwd")).To(Equal("pwd"))
		})

		It("handles leading whitespace", func() {
			Expect(unix.ExtractBinaryName("  docker ps")).To(Equal("docker"))
		})

		It("handles tab-separated args", func() {
			Expect(unix.ExtractBinaryName("make\tbuild")).To(Equal("make"))
		})
	})
})
