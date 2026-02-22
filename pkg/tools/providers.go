package tools

import (
	"context"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/codeexecutor/local"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	ctxtools "trpc.group/trpc-go/trpc-agent-go/tool/context"
	"trpc.group/trpc-go/trpc-agent-go/tool/file"
)

// Slice is a convenience adapter that wraps a plain []tool.Tool into a
// ToolProviders conformer. Useful for ad-hoc tool collections that don't
// belong to a dedicated package (e.g. the codeowner's orchestration-only
// tool set).
type Tools []tool.Tool

// GetTools returns the wrapped slice of tools.
func (s Tools) GetTools() []tool.Tool { return s }

// FileToolProvider wraps a trpc-agent-go file.ToolSet and satisfies the
// ToolProviders interface. The tools are pre-computed at construction time
// because file.ToolSet.Tools requires a context.
type FileToolProvider struct {
	tools Tools
}

// NewFileToolProvider creates a ToolProvider for file tools scoped to a
// working directory. Returns nil if the ToolSet fails to initialise.
func NewFileToolProvider(ctx context.Context, workingDir string) *FileToolProvider {
	ts, err := file.NewToolSet(file.WithBaseDir(workingDir))
	if err != nil {
		return nil
	}
	return &FileToolProvider{tools: ts.Tools(ctx)}
}

// GetTools returns the pre-computed file tools.
func (p *FileToolProvider) GetTools() []tool.Tool {
	return p.tools
}

// ShellToolProvider wraps the shell tool and satisfies the ToolProviders
// interface. It encapsulates code executor configuration.
type ShellToolProvider struct {
	workingDir string
	timeout    time.Duration
}

// NewShellToolProvider creates a ToolProvider for the shell_exec tool.
func NewShellToolProvider(workingDir string) *ShellToolProvider {
	return &ShellToolProvider{
		workingDir: workingDir,
		timeout:    10 * time.Minute,
	}
}

// GetTools returns the shell tool backed by a local code executor.
func (p *ShellToolProvider) GetTools() []tool.Tool {
	exec := local.New(
		local.WithWorkDir(p.workingDir),
		local.WithTimeout(p.timeout),
		local.WithCleanTempFiles(true),
	)
	return Tools{NewShellTool(exec)}
}

// PensieveToolProvider wraps the Pensieve context management tools
// (delete_context, check_budget, note, read_notes) and satisfies the
// ToolProviders interface. Gated behind EnablePensieve in config.
type PensieveToolProvider struct{}

// NewPensieveToolProvider creates a ToolProvider for the Pensieve tools.
func NewPensieveToolProvider() *PensieveToolProvider {
	return &PensieveToolProvider{}
}

// GetTools returns the context management tools.
func (p *PensieveToolProvider) GetTools() []tool.Tool {
	return ctxtools.Tools()
}
