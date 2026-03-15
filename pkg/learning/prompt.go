// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package learning

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed distillation.tmpl
var distillationPromptRaw string

// distillationPromptTemplate is the Go text/template for the distillation
// prompt. Using text/template instead of fmt.Sprintf gives us named variables,
// cleaner multi-line templates, and avoids %-escaping issues in prompts.
var distillationPromptTemplate = template.Must(template.New("distillation").Parse(distillationPromptRaw))

// distillationVars holds the variables for the distillation prompt template.
type distillationVars struct {
	Goal           string
	Result         string
	ToolsList      string
	ToolTrace      string
	ExistingSkills string
}

// buildDistillationPrompt constructs the LLM prompt that asks the model
// to evaluate a completed task and, if worthy, produce a structured skill
// proposal in JSON format following the agentskills.io standard.
func buildDistillationPrompt(req LearnRequest, existingSkills string) (string, error) {
	toolsList := "none"
	if len(req.ToolsUsed) > 0 {
		toolsList = strings.Join(req.ToolsUsed, ", ")
	}

	var buf bytes.Buffer
	if err := distillationPromptTemplate.Execute(&buf, distillationVars{
		Goal:           req.Goal,
		Result:         req.Output,
		ToolsList:      toolsList,
		ToolTrace:      req.ToolTrace,
		ExistingSkills: existingSkills,
	}); err != nil {
		return "", fmt.Errorf("distillation template execution: %w", err)
	}
	return buf.String(), nil
}

// buildRetryPrompt constructs a strict JSON-only re-prompt that includes
// a summarized version of the original task. This is necessary because
// expert.Do() calls are stateless — without context, the LLM wouldn't
// know what task to evaluate on retry.
func buildRetryPrompt(req LearnRequest) string {
	goalSummary := req.Goal
	if len(goalSummary) > 200 {
		goalSummary = goalSummary[:200] + "..."
	}
	resultSummary := req.Output
	if len(resultSummary) > 300 {
		resultSummary = resultSummary[:300] + "..."
	}

	return fmt.Sprintf(`Your previous response was not valid JSON. You MUST respond with ONLY a raw JSON object.
No markdown headers, no code fences, no analysis — ONLY the JSON object.

Task to evaluate:
- Goal: %s
- Result: %s

Respond with exactly one of these two formats:

If the task IS worth distilling:
{"should_create": true, "novelty_score": 8, "name": "skill-name", "description": "One-line description", "instructions": "Full markdown instructions", "update_existing": ""}

If the task is NOT worth distilling:
{"should_create": false, "novelty_score": 3, "name": "", "description": "", "instructions": "", "update_existing": ""}

Respond with ONLY JSON:`, goalSummary, resultSummary)
}
