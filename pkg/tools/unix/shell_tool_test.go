package unix_test

import (
	"context"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor/local"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/security/securityfakes"
	"github.com/stackgenhq/genie/pkg/tools/unix"
)

// envSecretProvider returns a FakeSecretProvider that resolves secrets from
// os.Getenv — mimicking a real env-backed provider for integration-style tests.
func envSecretProvider() *securityfakes.FakeSecretProvider {
	fake := &securityfakes.FakeSecretProvider{}
	fake.GetSecretStub = func(_ context.Context, req security.GetSecretRequest) (string, error) {
		return os.Getenv(req.Name), nil
	}
	return fake
}

var _ = Describe("ShellTool", func() {
	var (
		ctx     context.Context
		secrets *securityfakes.FakeSecretProvider
	)

	BeforeEach(func() {
		ctx = context.Background()
		secrets = envSecretProvider()
	})

	// --- Construction & Config ---

	Context("construction", func() {
		It("defaults to base Unix env keys", func() {
			t := unix.NewShellTool(local.New(), secrets, unix.ShellToolConfig{})
			st := t.(*unix.ShellTool)
			keys := st.AllowedEnvKeys()
			Expect(keys).To(ContainElements("PATH", "HOME", "USER", "TMPDIR", "LANG", "TERM", "SHELL"))
		})

		It("adds configured env vars to the allowed set", func() {
			t := unix.NewShellTool(local.New(), secrets, unix.ShellToolConfig{
				AllowedEnv: []string{"MY_CUSTOM_VAR", "ANOTHER_VAR"},
			})
			st := t.(*unix.ShellTool)
			keys := st.AllowedEnvKeys()
			// Base keys + 2 custom keys
			Expect(keys).To(ContainElements("PATH", "HOME", "MY_CUSTOM_VAR", "ANOTHER_VAR"))
		})

		It("normalises env var keys to uppercase", func() {
			t := unix.NewShellTool(local.New(), secrets, unix.ShellToolConfig{
				AllowedEnv: []string{"my_var", "gOPATH"},
			})
			st := t.(*unix.ShellTool)
			Expect(st.AllowedEnvKeys()).To(ContainElements("MY_VAR", "GOPATH", "PATH", "HOME"))
		})
	})

	// --- Declaration ---

	Context("declaration", func() {
		It("returns a tool named run_shell", func() {
			t := unix.NewShellTool(local.New(), secrets, unix.ShellToolConfig{})
			decl := t.Declaration()
			Expect(decl.Name).To(Equal("run_shell"))
			Expect(decl.InputSchema.Required).To(ContainElement("command"))
		})
	})

	// --- Call: basic execution ---

	Context("basic execution", func() {
		It("executes a simple echo command", func() {
			t := unix.NewShellTool(local.New(), secrets, unix.ShellToolConfig{})
			callable := t.(tool.CallableTool)
			result, err := callable.Call(ctx, []byte(`{"command": "echo hello"}`))
			Expect(err).NotTo(HaveOccurred())
			execResult, ok := result.(codeexecutor.CodeExecutionResult)
			Expect(ok).To(BeTrue())
			Expect(strings.TrimSpace(execResult.Output)).To(Equal("hello"))
		})

		It("executes piped commands", func() {
			t := unix.NewShellTool(local.New(), secrets, unix.ShellToolConfig{})
			callable := t.(tool.CallableTool)
			result, err := callable.Call(ctx, []byte(`{"command": "echo hello world | wc -w"}`))
			Expect(err).NotTo(HaveOccurred())
			execResult := result.(codeexecutor.CodeExecutionResult)
			Expect(strings.TrimSpace(execResult.Output)).To(Equal("2"))
		})

		It("rejects empty command", func() {
			t := unix.NewShellTool(local.New(), secrets, unix.ShellToolConfig{})
			callable := t.(tool.CallableTool)
			_, err := callable.Call(ctx, []byte(`{"command": ""}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("command is required"))
		})

		It("rejects invalid JSON", func() {
			t := unix.NewShellTool(local.New(), secrets, unix.ShellToolConfig{})
			callable := t.(tool.CallableTool)
			_, err := callable.Call(ctx, []byte(`not json`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse arguments"))
		})
	})

	// --- SecretProvider integration ---

	Context("SecretProvider integration", func() {
		It("resolves env vars through the secret provider", func() {
			fakeSP := &securityfakes.FakeSecretProvider{}
			fakeSP.GetSecretStub = func(_ context.Context, req security.GetSecretRequest) (string, error) {
				if req.Name == "MY_SECRET" {
					return "s3cr3t", nil
				}
				if req.Name == "PATH" {
					return os.Getenv("PATH"), nil
				}
				return "", nil
			}
			t := unix.NewShellTool(local.New(), fakeSP, unix.ShellToolConfig{
				AllowedEnv: []string{"MY_SECRET"},
			})
			callable := t.(tool.CallableTool)
			result, err := callable.Call(ctx, []byte(`{"command": "echo $MY_SECRET"}`))
			Expect(err).NotTo(HaveOccurred())
			execResult := result.(codeexecutor.CodeExecutionResult)
			Expect(strings.TrimSpace(execResult.Output)).To(Equal("s3cr3t"))
		})

		It("audits which secrets were requested", func() {
			fakeSP := &securityfakes.FakeSecretProvider{}
			fakeSP.GetSecretStub = func(_ context.Context, req security.GetSecretRequest) (string, error) {
				return os.Getenv(req.Name), nil
			}
			t := unix.NewShellTool(local.New(), fakeSP, unix.ShellToolConfig{
				AllowedEnv: []string{"HOME"},
			})
			callable := t.(tool.CallableTool)
			_, err := callable.Call(ctx, []byte(`{"command": "echo test"}`))
			Expect(err).NotTo(HaveOccurred())
			// Should have called GetSecret for all base keys + HOME
			Expect(fakeSP.GetSecretCallCount()).To(BeNumerically(">=", 11))
		})
	})
})
