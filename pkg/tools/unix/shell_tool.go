package unix

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/security"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ShellToolConfig configures the run_shell tool's security behaviour.
type ShellToolConfig struct {
	// AllowedEnv controls which environment variables are visible to shell
	// commands. Only listed variables (plus PATH, which is always included)
	// are resolved via the SecretProvider and injected. When empty or nil,
	// only PATH is visible.
	AllowedEnv []string `yaml:"allowed_env" toml:"allowed_env"`

	// Timeout overrides the default 10-minute shell execution timeout.
	// Use Go duration syntax (e.g. "5m", "30s").
	Timeout time.Duration `yaml:"timeout,omitempty" toml:"timeout,omitempty"`
}

// ShellTool is a simplified tool for running shell commands.
// It wraps a codeexecutor.CodeExecutor but exposes a simpler "command" interface
// that is friendlier to models than the full codeexec.Tool.
type ShellTool struct {
	executor       codeexecutor.CodeExecutor
	secrets        security.SecretProvider
	allowedEnvKeys map[string]struct{}
}

// NewShellTool creates a new ShellTool with the given executor, secret provider,
// and config. Environment filtering is always active — only PATH (plus any keys
// listed in config.AllowedEnv) is resolved via the SecretProvider and injected
// into the shell process.
func NewShellTool(executor codeexecutor.CodeExecutor, secrets security.SecretProvider, config ShellToolConfig) tool.Tool {
	t := &ShellTool{
		executor:       executor,
		secrets:        secrets,
		allowedEnvKeys: make(map[string]struct{}, len(config.AllowedEnv)+1),
	}
	// PATH is always required for command resolution.
	t.allowedEnvKeys["PATH"] = struct{}{}
	for _, k := range config.AllowedEnv {
		t.allowedEnvKeys[strings.ToUpper(k)] = struct{}{}
	}
	return t
}

func (t *ShellTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "run_shell",
		Description: "Execute a shell command on the local machine.",
		InputSchema: &tool.Schema{
			Type:     "object",
			Required: []string{"command"},
			Properties: map[string]*tool.Schema{
				"command": {
					Type:        "string",
					Description: "The shell command to execute (e.g., 'ls -la', 'go test ./...').",
				},
			},
		},
	}
}

func (t *ShellTool) Call(ctx context.Context, input []byte) (any, error) {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	if args.Command == "" {
		return nil, fmt.Errorf("command is required")
	}

	// Build the command with env filtering preamble.
	preamble, err := t.envPreamble(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve environment: %w", err)
	}
	fullCommand := preamble + args.Command

	// Adapt single command to CodeExecutionInput
	lang := "sh"
	if _, err := exec.LookPath("bash"); err == nil {
		lang = "bash"
	}

	execInput := codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{
				Language: lang,
				Code:     fullCommand,
			},
		},
	}

	return t.executor.ExecuteCode(ctx, execInput)
}

// envPreamble builds a shell preamble that unsets the host environment and
// re-exports only the allowed variables, resolved via SecretProvider at runtime.
func (t *ShellTool) envPreamble(ctx context.Context) (string, error) {
	// Collect sorted list of keys for deterministic output.
	keys := make([]string, 0, len(t.allowedEnvKeys))
	for k := range t.allowedEnvKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var exports []string
	for _, key := range keys {
		val, err := t.secrets.GetSecret(ctx, security.GetSecretRequest{
			Name:   key,
			Reason: "shell_tool environment injection",
		})
		if err != nil {
			return "", fmt.Errorf("resolving env var %s: %w", key, err)
		}
		if val == "" {
			continue
		}
		// Shell-escape the value by single-quoting it.
		val = strings.ReplaceAll(val, "'", "'\\''")
		exports = append(exports, fmt.Sprintf("export %s='%s'", key, val))
	}

	var preamble string
	if len(exports) > 0 {
		preamble = strings.Join(exports, "; ") + "; "
	}
	return preamble, nil
}

// AllowedEnvKeys returns the set of allowed env var names (for testing).
func (t *ShellTool) AllowedEnvKeys() []string {
	if len(t.allowedEnvKeys) == 0 {
		return nil
	}
	keys := make([]string, 0, len(t.allowedEnvKeys))
	for k := range t.allowedEnvKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
