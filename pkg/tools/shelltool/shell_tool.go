package shelltool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ShellTool is a simplified tool for running shell commands.
// It wraps a codeexecutor.CodeExecutor but exposes a simpler "command" interface
// that is friendlier to models than the full codeexec.Tool.
type ShellTool struct {
	executor codeexecutor.CodeExecutor
}

func NewShellTool(executor codeexecutor.CodeExecutor) tool.Tool {
	return &ShellTool{executor: executor}
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

	// Prepend common paths to PATH for robustness
	// This ensures tools like terraform are found even if the agent's environment is restricted
	// or missing user specific paths.
	fullCommand := fmt.Sprintf(`export PATH="/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:$PATH"; %s`, args.Command)

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
