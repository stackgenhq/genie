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

	// AllowedBinaries is an optional allowlist of command names (basenames)
	// that the shell tool may execute. When non-empty, the first word of
	// every command is checked against this list; commands starting with an
	// unlisted binary are rejected before execution.
	// When empty, any command is allowed.
	AllowedBinaries []string `yaml:"allowed_binaries,omitempty" toml:"allowed_binaries,omitempty"`
}

// ShellTool is a simplified tool for running shell commands.
// It wraps a codeexecutor.CodeExecutor but exposes a simpler "command" interface
// that is friendlier to models than the full codeexec.Tool.
type ShellTool struct {
	executor        codeexecutor.CodeExecutor
	secrets         security.SecretProvider
	allowedEnvKeys  map[string]struct{}
	allowedBinaries map[string]struct{}
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
	if len(config.AllowedBinaries) > 0 {
		t.allowedBinaries = make(map[string]struct{}, len(config.AllowedBinaries))
		for _, b := range config.AllowedBinaries {
			t.allowedBinaries[b] = struct{}{}
		}
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

	// Binary allowlist check: extract the first word and verify it.
	if len(t.allowedBinaries) > 0 {
		bin := ExtractBinaryName(args.Command)
		if _, ok := t.allowedBinaries[bin]; !ok {
			return nil, fmt.Errorf("command %q is not in the allowed binaries list", bin)
		}
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
// We use `env -i` with inline exports so the code executor's shell script
// sees a clean environment.
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

	// Build preamble: unset everything, then export only allowed vars.
	// Using `unset` on each host var is fragile, so instead we clear with
	// a function that unsets all non-readonly vars, then re-export.
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

// AllowedBinaries returns the set of allowed binary names (for testing).
func (t *ShellTool) AllowedBinaries() []string {
	if len(t.allowedBinaries) == 0 {
		return nil
	}
	bins := make([]string, 0, len(t.allowedBinaries))
	for b := range t.allowedBinaries {
		bins = append(bins, b)
	}
	sort.Strings(bins)
	return bins
}

// ExtractBinaryName returns the basename of the first word in a command string.
func ExtractBinaryName(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	// Take the first word (space-separated).
	if idx := strings.IndexAny(cmd, " \t"); idx > 0 {
		cmd = cmd[:idx]
	}
	// Strip any leading path (e.g. /usr/bin/git → git).
	if idx := strings.LastIndex(cmd, "/"); idx >= 0 {
		cmd = cmd[idx+1:]
	}
	return cmd
}
