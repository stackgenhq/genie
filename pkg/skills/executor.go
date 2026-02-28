package skills

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/logger"
)

// Executor executes skill scripts.
// This interface exists to abstract script execution and enable testing with fake implementations.
// Without this interface, we would be tightly coupled to local script execution.
//
//counterfeiter:generate . Executor
type Executor interface {
	Execute(ctx context.Context, req ExecuteRequest) (ExecuteResponse, error)
}

// ExecutorConfig contains configuration for the executor.
// This struct exists to provide configurable resource limits.
// Without this struct, we couldn't control resource usage.
type ExecutorConfig struct {
	// MaxWorkspaceSize is the maximum total size of workspace files in bytes.
	// 0 means unlimited. Default: 100MB
	MaxWorkspaceSize int64
	// MaxOutputFileSize is the maximum size of a single output file in bytes.
	// 0 means unlimited. Default: 10MB
	MaxOutputFileSize int64
	// DefaultTimeout is the default execution timeout if not specified in request.
	// 0 means no default timeout. Default: 5 minutes
	DefaultTimeout time.Duration
}

// DefaultExecutorConfig returns sensible default configuration.
func DefaultExecutorConfig() ExecutorConfig {
	return ExecutorConfig{
		MaxWorkspaceSize:  100 * 1024 * 1024, // 100MB
		MaxOutputFileSize: 10 * 1024 * 1024,  // 10MB
		DefaultTimeout:    5 * time.Minute,
	}
}

// ExecuteRequest contains parameters for script execution.
// This struct exists to encapsulate all execution parameters in one place.
// Without this struct, Execute() would have many individual parameters.
type ExecuteRequest struct {
	SkillPath   string            // Absolute path to skill directory
	ScriptPath  string            // Relative path to script within skill
	Args        []string          // Command-line arguments for script
	InputFiles  map[string]string // Input files (name -> content)
	Environment map[string]string // Additional environment variables
	Timeout     time.Duration     // Execution timeout (0 = use default)
}

// ExecuteResponse contains the results of script execution.
// This struct exists to return all execution results in one place.
// Without this struct, Execute() would need multiple return values.
type ExecuteResponse struct {
	Output      string            // Combined stdout/stderr
	Error       string            // Error message if execution failed
	ExitCode    int               // Script exit code
	OutputFiles map[string]string // Output files (name -> content)
}

// LocalExecutor implements Executor for local script execution.
// This struct exists to provide local script execution with workspace isolation.
// Without this struct, we would not be able to execute skills locally.
type LocalExecutor struct {
	baseWorkDir string
	config      ExecutorConfig
}

// NewLocalExecutor creates a new LocalExecutor with default configuration.
// This function exists to initialize the executor with a base workspace directory.
// Without this function, we could not create executor instances.
func NewLocalExecutor(baseWorkDir string) *LocalExecutor {
	return NewLocalExecutorWithConfig(baseWorkDir, DefaultExecutorConfig())
}

// NewLocalExecutorWithConfig creates a new LocalExecutor with custom configuration.
// This function exists to allow customization of resource limits.
// Without this function, users could not configure executor behavior.
func NewLocalExecutorWithConfig(baseWorkDir string, config ExecutorConfig) *LocalExecutor {
	return &LocalExecutor{
		baseWorkDir: baseWorkDir,
		config:      config,
	}
}

// Execute implements Executor.
// This method exists to execute skill scripts with proper workspace isolation and cleanup.
// Without this method, we could not run skill scripts.
func (e *LocalExecutor) Execute(ctx context.Context, req ExecuteRequest) (ExecuteResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "LocalExecutor.Execute", "script", req.ScriptPath)

	// Create workspace
	workspace, err := e.prepareWorkspace(ctx, req)
	if err != nil {
		return ExecuteResponse{}, fmt.Errorf("failed to prepare workspace: %w", err)
	}
	defer func() {
		if err := e.cleanupWorkspace(workspace); err != nil {
			logr.Error("failed to cleanup workspace", "error", err)
		}
	}()

	// Build script path
	scriptPath := filepath.Join(req.SkillPath, req.ScriptPath)
	if _, err := os.Stat(scriptPath); err != nil {
		return ExecuteResponse{}, fmt.Errorf("script not found: %w", err)
	}

	// Determine script interpreter
	interpreter, interpreterArgs := determineInterpreter(scriptPath)

	// Build command
	cmdArgs := append(interpreterArgs, scriptPath)
	cmdArgs = append(cmdArgs, req.Args...)

	// Apply timeout (use default if not specified)
	execCtx := ctx
	timeout := req.Timeout
	if timeout == 0 && e.config.DefaultTimeout > 0 {
		timeout = e.config.DefaultTimeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
		logr.Debug("applying execution timeout", "timeout", timeout)
	}

	cmd := exec.CommandContext(execCtx, interpreter, cmdArgs...)
	cmd.Dir = workspace

	// Set environment
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("SKILL_PATH=%s", req.SkillPath))
	cmd.Env = append(cmd.Env, fmt.Sprintf("WORKSPACE=%s", workspace))
	cmd.Env = append(cmd.Env, fmt.Sprintf("OUTPUT_DIR=%s", filepath.Join(workspace, "output")))
	for k, v := range req.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	logr.Info("executing script", "interpreter", interpreter, "args", cmdArgs)

	// Capture output
	output, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		// Check if it's a context timeout error
		if execCtx.Err() == context.DeadlineExceeded {
			return ExecuteResponse{}, fmt.Errorf("script execution timed out: %w", context.DeadlineExceeded)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return ExecuteResponse{}, fmt.Errorf("failed to execute script: %w", err)
		}
	}

	// Collect output files
	outputFiles, err := e.collectOutputFiles(workspace)
	if err != nil {
		logr.Error("failed to collect output files", "error", err)
	}

	response := ExecuteResponse{
		Output:      string(output),
		ExitCode:    exitCode,
		OutputFiles: outputFiles,
	}

	if exitCode != 0 {
		response.Error = fmt.Sprintf("script exited with code %d", exitCode)
	}

	logr.Info("execution completed", "exit_code", exitCode)
	return response, nil
}

// prepareWorkspace creates a workspace directory with input files.
// This method exists to set up the execution environment for scripts.
// Without this method, scripts would not have access to input files.
func (e *LocalExecutor) prepareWorkspace(ctx context.Context, req ExecuteRequest) (string, error) {
	// Create unique workspace directory
	timestamp := time.Now().UnixNano()
	workspace := filepath.Join(e.baseWorkDir, fmt.Sprintf("skill_%d", timestamp))
	if err := os.MkdirAll(workspace, 0755); err != nil {
		return "", fmt.Errorf("failed to create workspace: %w", err)
	}

	// Create input directory
	inputDir := filepath.Join(workspace, "input")
	if err := os.MkdirAll(inputDir, 0755); err != nil {
		_ = os.RemoveAll(workspace) // Cleanup on error
		return "", fmt.Errorf("failed to create input directory: %w", err)
	}

	// Create output directory
	outputDir := filepath.Join(workspace, "output")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		_ = os.RemoveAll(workspace) // Cleanup on error
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}

	// Stage input files with size checking
	var totalSize int64
	for filename, content := range req.InputFiles {
		fileSize := int64(len(content))
		totalSize += fileSize

		// Check workspace size limit
		if e.config.MaxWorkspaceSize > 0 && totalSize > e.config.MaxWorkspaceSize {
			_ = os.RemoveAll(workspace) // Cleanup on error
			return "", fmt.Errorf("input files exceed workspace size limit (%d bytes > %d bytes)",
				totalSize, e.config.MaxWorkspaceSize)
		}

		filePath := filepath.Join(inputDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			_ = os.RemoveAll(workspace) // Cleanup on error
			return "", fmt.Errorf("failed to write input file %q: %w", filename, err)
		}
	}

	return workspace, nil
}

// cleanupWorkspace removes the workspace directory.
// This method exists to clean up temporary files after script execution.
// Without this method, we would accumulate temporary workspace directories.
func (e *LocalExecutor) cleanupWorkspace(workspace string) error {
	return os.RemoveAll(workspace)
}

// collectOutputFiles collects output files from the workspace.
// This method exists to gather script output files for return to the caller.
// Without this method, callers could not receive files generated by scripts.
func (e *LocalExecutor) collectOutputFiles(workspace string) (map[string]string, error) {
	outputDir := filepath.Join(workspace, "output")
	files := make(map[string]string)
	var totalSize int64

	err := filepath.WalkDir(outputDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		// Check file size limit
		info, err := d.Info()
		if err != nil {
			return err
		}
		fileSize := info.Size()

		if e.config.MaxOutputFileSize > 0 && fileSize > e.config.MaxOutputFileSize {
			return fmt.Errorf("output file %q exceeds size limit (%d bytes > %d bytes)",
				d.Name(), fileSize, e.config.MaxOutputFileSize)
		}

		totalSize += fileSize
		if e.config.MaxWorkspaceSize > 0 && totalSize > e.config.MaxWorkspaceSize {
			return fmt.Errorf("total output size exceeds workspace limit (%d bytes > %d bytes)",
				totalSize, e.config.MaxWorkspaceSize)
		}

		relPath, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}

		// Read file with size limit
		content, err := readFileWithLimit(path, e.config.MaxOutputFileSize)
		if err != nil {
			return err
		}

		files[relPath] = content
		return nil
	})

	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to collect output files: %w", err)
	}

	return files, nil
}

// determineInterpreter determines the appropriate interpreter for a script.
// This function exists to automatically detect the script type and select the right interpreter.
// Without this function, we would need to manually specify interpreters for each script.
func determineInterpreter(scriptPath string) (string, []string) {
	ext := strings.ToLower(filepath.Ext(scriptPath))
	switch ext {
	case ".py":
		return "python3", nil
	case ".sh":
		return "bash", nil
	case ".js":
		return "node", nil
	case ".rb":
		return "ruby", nil
	default:
		// Try to make it executable and run directly
		_ = os.Chmod(scriptPath, 0755)
		return scriptPath, nil
	}
}
