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
	// commands. Only listed variables (plus base Unix variables like PATH,
	// HOME, TMPDIR, etc.) are resolved via the SecretProvider and injected.
	// When empty or nil, only the base Unix variables are visible.
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
		allowedEnvKeys: make(map[string]struct{}, len(config.AllowedEnv)+len(baseEnvKeys)),
	}
	// Always inject essential Unix environment variables so that tools
	// like aws, git, kubectl, and npm work correctly even though env -i
	// clears the inherited environment. Without these, subprocesses fail
	// with errors like "RuntimeError: HOME not set" or write to "/" instead
	// of the user's home directory.
	for _, k := range baseEnvKeys {
		t.allowedEnvKeys[k] = struct{}{}
	}
	for _, k := range config.AllowedEnv {
		t.allowedEnvKeys[strings.ToUpper(k)] = struct{}{}
	}
	return t
}

// baseEnvKeys are always passed through env -i to ensure a functioning
// Unix environment. These are read-only identifiers and paths — no secrets.
var baseEnvKeys = []string{
	"PATH",   // command resolution
	"HOME",   // ~/ expansion, config dirs, credential caches
	"USER",   // whoami, git commit author
	"TMPDIR", // Go os.TempDir(), Python tempfile, etc.
	"LANG",   // locale (prevents mojibake in tool output)
	"TERM",   // terminal capabilities (tput, colored output)
	"SHELL",  // child process default shell
	// XDG base directories — used by gh CLI, npm, pip, etc.
	"XDG_CONFIG_HOME",
	"XDG_CACHE_HOME",
	"XDG_DATA_HOME",
	"XDG_RUNTIME_DIR",
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
	prefix, suffix, err := t.envPreamble(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve environment: %w", err)
	}
	// Escape single quotes in the user command so it can be safely wrapped
	// inside the single-quoted sh -c '...' block.
	escapedCmd := strings.ReplaceAll(args.Command, "'", "'\\''")
	fullCommand := prefix + escapedCmd + suffix

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

// envPreamble builds a shell preamble that clears the host environment using
// `env -i` and re-exports only the allowed variables, resolved via SecretProvider
// at runtime. The user's command is wrapped in a sub-shell so that shell
// variable references (e.g. $MY_VAR) are expanded correctly:
//
//	env -i PATH='/usr/bin:...' MY_VAR='value' sh -c '<user_command>'
//
// When allowedEnvKeys is empty (or all resolve to empty), the preamble still
// wraps the command with `env -i sh -c '...'` to ensure a clean environment.
//
// The user command's single quotes are escaped so the wrapping is injection-safe.
func (t *ShellTool) envPreamble(ctx context.Context) (prefix string, suffix string, err error) {
	// Collect sorted list of keys for deterministic output.
	keys := make([]string, 0, len(t.allowedEnvKeys))
	for k := range t.allowedEnvKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var envAssignments []string
	for _, key := range keys {
		val, err := t.secrets.GetSecret(ctx, security.GetSecretRequest{
			Name:   key,
			Reason: "shell_tool environment injection",
		})
		if err != nil {
			return "", "", fmt.Errorf("resolving env var %s: %w", key, err)
		}
		if val == "" {
			continue
		}
		// Shell-escape the value by single-quoting it.
		val = strings.ReplaceAll(val, "'", "'\\''")
		envAssignments = append(envAssignments, fmt.Sprintf("%s='%s'", key, val))
	}

	// Build: env -i KEY='val' ... sh -c '<user_command>'
	// The caller inserts the user command between prefix and suffix.
	prefix = "env -i "
	if len(envAssignments) > 0 {
		prefix += strings.Join(envAssignments, " ") + " "
	}
	prefix += "sh -c '"
	suffix = "'"
	return prefix, suffix, nil
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
