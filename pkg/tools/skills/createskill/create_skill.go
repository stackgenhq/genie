// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package createskill

import (
	"context"
	"fmt"

	"github.com/stackgenhq/genie/pkg/skills"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

type skillInput struct {
	// Name is the skill identifier (lowercase alphanumeric + hyphens/underscores).
	Name string `json:"name" jsonschema:"description=Unique skill name (lowercase alphanumeric with hyphens/underscores),required"`

	// Description is a one-line summary of the skill.
	Description string `json:"description" jsonschema:"description=One-line summary of what this skill does,required"`

	// Instructions is the markdown body of the SKILL.md file.
	Instructions string `json:"instructions" jsonschema:"description=Full markdown instructions for this skill (the SKILL.md body),required"`

	// Scripts maps filename to plain text script content.
	// Scripts are written with executable permissions (e.g. Python, bash).
	Scripts map[string]string `json:"scripts,omitempty" jsonschema:"description=Optional map of script filename to content (written with exec permissions)"`

	// Docs maps filename to base64-encoded binary content.
	// Used for reference documents (PDF, docx, xlsx, pptx, etc.).
	Docs map[string]string `json:"docs,omitempty" jsonschema:"description=Optional map of doc filename to base64-encoded content (PDF/docx/xlsx/pptx)"`
}

type deleteSkillInput struct {
	// Name is the skill to delete.
	Name string `json:"name" jsonschema:"description=Name of the skill to delete,required"`
}

// createHandler wraps the MutableRepository for create operations.
type createHandler struct {
	repo *skills.MutableRepository
}

func (h *createHandler) handle(_ context.Context, input skillInput) (string, error) {
	err := h.repo.Add(toAddRequest(input))
	if err != nil {
		return "", fmt.Errorf("create_skill: %w", err)
	}
	return formatMessage("created", input), nil
}

// updateHandler wraps the MutableRepository for update operations.
type updateHandler struct {
	repo *skills.MutableRepository
}

func (h *updateHandler) handle(_ context.Context, input skillInput) (string, error) {
	err := h.repo.Update(toAddRequest(input))
	if err != nil {
		return "", fmt.Errorf("update_skill: %w", err)
	}
	return formatMessage("updated", input), nil
}

// deleteHandler wraps the MutableRepository for delete operations.
type deleteHandler struct {
	repo *skills.MutableRepository
}

func (h *deleteHandler) handle(_ context.Context, input deleteSkillInput) (string, error) {
	err := h.repo.Delete(input.Name)
	if err != nil {
		return "", fmt.Errorf("delete_skill: %w", err)
	}
	return fmt.Sprintf("Successfully deleted skill %q.", input.Name), nil
}

func toAddRequest(input skillInput) skills.AddSkillRequest {
	return skills.AddSkillRequest{
		Name:         input.Name,
		Description:  input.Description,
		Instructions: input.Instructions,
		Scripts:      input.Scripts,
		Docs:         input.Docs,
	}
}

func formatMessage(verb string, input skillInput) string {
	msg := fmt.Sprintf("Successfully %s skill %q. ", verb, input.Name)
	msg += "The skill is now available via discover_skills and can be loaded with load_skill."

	if len(input.Scripts) > 0 {
		msg += fmt.Sprintf(" Scripts: %d.", len(input.Scripts))
	}
	if len(input.Docs) > 0 {
		msg += fmt.Sprintf(" Reference docs: %d.", len(input.Docs))
	}
	return msg
}

// NewCreateSkillTool creates the create_skill tool backed by the given MutableRepository.
func NewCreateSkillTool(repo *skills.MutableRepository) tool.Tool {
	h := &createHandler{repo: repo}
	return function.NewFunctionTool(
		h.handle,
		function.WithName("create_skill"),
		function.WithDescription(
			"Create a new skill on disk. The skill becomes immediately available "+
				"for discovery and loading. Provide a name, description, and markdown "+
				"instructions. Optionally include scripts (Python, bash) and reference "+
				"documents (PDF, docx, xlsx, pptx as base64-encoded content).",
		),
	)
}

// NewUpdateSkillTool creates the update_skill tool backed by the given MutableRepository.
func NewUpdateSkillTool(repo *skills.MutableRepository) tool.Tool {
	h := &updateHandler{repo: repo}
	return function.NewFunctionTool(
		h.handle,
		function.WithName("update_skill"),
		function.WithDescription(
			"Update an existing skill by replacing its content. Provide the full new "+
				"name, description, instructions, scripts, and docs. The old content "+
				"(scripts, docs) is removed and replaced completely.",
		),
	)
}

// NewDeleteSkillTool creates the delete_skill tool backed by the given MutableRepository.
func NewDeleteSkillTool(repo *skills.MutableRepository) tool.Tool {
	h := &deleteHandler{repo: repo}
	return function.NewFunctionTool(
		h.handle,
		function.WithName("delete_skill"),
		function.WithDescription(
			"Delete an existing skill from disk. The skill will no longer be "+
				"discoverable or loadable. This action is permanent.",
		),
	)
}
