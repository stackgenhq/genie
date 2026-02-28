package skills

import (
	"context"
	"encoding/json"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// SkillLoadTool provides the skill_load tool for loading skill instructions.
// This struct exists to enable agents to discover and load skill instructions.
// Without this struct, agents could not access skill documentation.
type SkillLoadTool struct {
	repo skill.Repository
}

// NewSkillLoadTool creates a new SkillLoadTool.
// This function exists to initialize the skill_load tool with a repository.
// Without this function, we could not create the skill_load tool.
func NewSkillLoadTool(repo skill.Repository) tool.Tool {
	return function.NewFunctionTool(
		(&SkillLoadTool{repo: repo}).execute,
		function.WithName("skill_load"),
		function.WithDescription(
			"Load a skill's instructions and documentation. "+
				"Use this tool to discover what a skill does and how to use it. "+
				"After loading a skill, you can execute it using the skill_run tool.",
		),
	)
}

// SkillLoadRequest is the request for skill_load.
// This struct exists to define the input schema for the skill_load tool.
// Without this struct, agents would not know how to call the tool.
type SkillLoadRequest struct {
	SkillName string `json:"skill_name" jsonschema:"description=Name of the skill to load,required"`
}

// SkillLoadResponse is the response for skill_load.
// This struct exists to return skill instructions to the agent.
// Without this struct, we could not return structured skill information.
type SkillLoadResponse struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Instructions string   `json:"instructions"`
	Documents    []string `json:"documents,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// execute implements the skill_load tool logic.
// This method exists to load and return skill instructions.
// Without this method, the skill_load tool would not function.
func (t *SkillLoadTool) execute(ctx context.Context, req SkillLoadRequest) (SkillLoadResponse, error) {
	skill, err := t.repo.Get(req.SkillName)
	if err != nil {
		return SkillLoadResponse{
			Error: fmt.Sprintf("Failed to load skill: %v", err),
		}, nil
	}

	docs := make([]string, len(skill.Docs))
	for i, doc := range skill.Docs {
		docs[i] = fmt.Sprintf("%s:\n%s", doc.Path, doc.Content)
	}

	return SkillLoadResponse{
		Name:         skill.Summary.Name,
		Description:  skill.Summary.Description,
		Instructions: skill.Body,
		Documents:    docs,
	}, nil
}

// SkillRunTool provides the skill_run tool for executing skills.
// This struct exists to enable agents to execute skill scripts.
// Without this struct, agents could not run skills.
type SkillRunTool struct {
	repo     skill.Repository
	executor Executor
}

// NewSkillRunTool creates a new SkillRunTool.
// This function exists to initialize the skill_run tool with a repository and executor.
// Without this function, we could not create the skill_run tool.
func NewSkillRunTool(repo skill.Repository, executor Executor) tool.Tool {
	return function.NewFunctionTool(
		(&SkillRunTool{repo: repo, executor: executor}).execute,
		function.WithName("skill_run"),
		function.WithDescription(
			"Execute a skill's script with the provided arguments and input files. "+
				"Use this tool after loading a skill with skill_load to run its functionality. "+
				"The skill will execute in an isolated workspace with access to input files.",
		),
	)
}

// SkillRunRequest is the request for skill_run.
// This struct exists to define the input schema for the skill_run tool.
// Without this struct, agents would not know how to call the tool.
type SkillRunRequest struct {
	SkillName   string            `json:"skill_name" jsonschema:"description=Name of the skill to run,required"`
	ScriptPath  string            `json:"script_path" jsonschema:"description=Relative path to the script within the skill directory (e.g. scripts/run.py),required"`
	Args        []string          `json:"args,omitempty" jsonschema:"description=Arguments to pass to the script"`
	InputFiles  map[string]string `json:"input_files,omitempty" jsonschema:"description=Map of filename to content for input files"`
	Environment map[string]string `json:"environment,omitempty" jsonschema:"description=Additional environment variables"`
}

// SkillRunResponse is the response for skill_run.
// This struct exists to return execution results to the agent.
// Without this struct, we could not return structured execution results.
type SkillRunResponse struct {
	Output      string            `json:"output"`
	Error       string            `json:"error,omitempty"`
	ExitCode    int               `json:"exit_code"`
	OutputFiles map[string]string `json:"output_files,omitempty"`
}

// execute implements the skill_run tool logic.
// This method exists to execute skill scripts and return results.
// Without this method, the skill_run tool would not function.
func (t *SkillRunTool) execute(ctx context.Context, req SkillRunRequest) (SkillRunResponse, error) {
	// Get skill path
	skillPath, err := t.repo.Path(req.SkillName)
	if err != nil {
		return SkillRunResponse{
			Error:    fmt.Sprintf("Failed to find skill: %v", err),
			ExitCode: 1,
		}, nil
	}

	// Execute skill
	execReq := ExecuteRequest{
		SkillPath:   skillPath,
		ScriptPath:  req.ScriptPath,
		Args:        req.Args,
		InputFiles:  req.InputFiles,
		Environment: req.Environment,
	}

	execResp, err := t.executor.Execute(ctx, execReq)
	if err != nil {
		return SkillRunResponse{
			Error:    fmt.Sprintf("Failed to execute skill: %v", err),
			ExitCode: 1,
		}, nil
	}

	return SkillRunResponse(execResp), nil
}

// CreateSkillTools creates both skill_load and skill_run tools.
// This function exists to provide a convenient way to create both skill tools at once.
// Without this function, callers would need to create each tool separately.
func CreateSkillTools(repo skill.Repository, executor Executor) []tool.Tool {
	return []tool.Tool{
		NewSkillLoadTool(repo),
		NewSkillRunTool(repo, executor),
	}
}

// ListSkillsTool provides a tool for listing available skills.
// This struct exists to enable agents to discover what skills are available.
// Without this struct, agents would not know which skills they can use.
type ListSkillsTool struct {
	repo skill.Repository
}

// NewListSkillsTool creates a new ListSkillsTool.
// This function exists to initialize the list_skills tool with a repository.
// Without this function, we could not create the list_skills tool.
func NewListSkillsTool(repo skill.Repository) tool.Tool {
	return function.NewFunctionTool(
		(&ListSkillsTool{repo: repo}).execute,
		function.WithName("list_skills"),
		function.WithDescription(
			"List all available skills with their names and descriptions. "+
				"Use this tool to discover what skills are available before loading or running them.",
		),
	)
}

// ListSkillsRequest is the request for list_skills.
// This struct exists to define the input schema for the list_skills tool.
// Without this struct, the tool would not have a proper schema.
type ListSkillsRequest struct {
	// No parameters needed for listing skills
}

// ListSkillsResponse is the response for list_skills.
// This struct exists to return the list of available skills.
// Without this struct, we could not return structured skill summaries.
type ListSkillsResponse struct {
	Skills []SkillSummary `json:"skills"`
	Count  int            `json:"count"`
}

// SkillSummary represents a skill summary in the list response.
// This is an alias for skill.Summary from trpc-agent-go.
type SkillSummary = skill.Summary

// execute implements the list_skills tool logic.
// This method exists to list all available skills.
// Without this method, the list_skills tool would not function.
func (t *ListSkillsTool) execute(ctx context.Context, req ListSkillsRequest) (ListSkillsResponse, error) {
	summaries := t.repo.Summaries()
	return ListSkillsResponse{
		Skills: summaries,
		Count:  len(summaries),
	}, nil
}

// CreateAllSkillTools creates all skill tools including list, load, and run.
// This function exists to provide a convenient way to create all skill tools at once.
// Without this function, callers would need to create each tool separately.
func CreateAllSkillTools(repo skill.Repository, executor Executor) []tool.Tool {
	return []tool.Tool{
		NewListSkillsTool(repo),
		NewSkillLoadTool(repo),
		NewSkillRunTool(repo, executor),
	}
}

// MarshalJSON implements custom JSON marshaling for tool responses.
// This is needed because the trpc-agent-go function tool expects JSON responses.
func (r SkillLoadResponse) MarshalJSON() ([]byte, error) {
	type Alias SkillLoadResponse
	return json.Marshal(&struct{ *Alias }{Alias: (*Alias)(&r)})
}

// MarshalJSON implements custom JSON marshaling for tool responses.
// This is needed because the trpc-agent-go function tool expects JSON responses.
func (r SkillRunResponse) MarshalJSON() ([]byte, error) {
	type Alias SkillRunResponse
	return json.Marshal(&struct{ *Alias }{Alias: (*Alias)(&r)})
}

// MarshalJSON implements custom JSON marshaling for tool responses.
// This is needed because the trpc-agent-go function tool expects JSON responses.
func (r ListSkillsResponse) MarshalJSON() ([]byte, error) {
	type Alias ListSkillsResponse
	return json.Marshal(&struct{ *Alias }{Alias: (*Alias)(&r)})
}
