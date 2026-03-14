// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package learning

import (
	"bytes"
	"strings"
	"text/template"
)

// distillationPromptTemplate is the Go text/template for the distillation
// prompt. Using text/template instead of fmt.Sprintf gives us named variables,
// cleaner multi-line templates, and avoids %-escaping issues in prompts.
var distillationPromptTemplate = template.Must(template.New("distillation").Parse(`You are a knowledge distillation engine. Your job is to review the following
completed task and decide whether it should be turned into a reusable skill.

## Completed Task

**Goal:** {{.Goal}}

**Result:** {{.Result}}

**Tools Used:** {{.ToolsList}}

## Instructions

1. Evaluate novelty on a 1-10 scale:
   - 1-3: Routine / trivial (e.g. simple lookup, greeting)
   - 4-6: Moderately useful but common knowledge
   - 7-10: Novel workflow, multi-step process, or domain-specific technique

2. If novelty_score >= 7, produce a skill proposal. The instructions field
   MUST follow the agentskills.io standard with these sections:
   - **What it can do**: One-paragraph capability summary
   - **How it did it**: Step-by-step recipe the agent followed
   - **What worked**: Techniques, tool combinations, or approaches that succeeded
   - **What did not work**: Dead ends, errors encountered, or approaches abandoned

3. The skill name must be lowercase with hyphens only (e.g. "deploy-k8s-service").

4. Respond with ONLY a JSON object (no markdown fences, no extra text):

{"should_create": true, "novelty_score": 8, "name": "skill-name", "description": "One-line description", "instructions": "Full markdown instructions with the four sections above"}

If the task is NOT worth distilling, respond with:

{"should_create": false, "novelty_score": 3, "name": "", "description": "", "instructions": ""}`))

// distillationVars holds the variables for the distillation prompt template.
type distillationVars struct {
	Goal      string
	Result    string
	ToolsList string
}

// buildDistillationPrompt constructs the LLM prompt that asks the model
// to evaluate a completed task and, if worthy, produce a structured skill
// proposal in JSON format following the agentskills.io standard.
func buildDistillationPrompt(req LearnRequest) string {
	toolsList := "none"
	if len(req.ToolsUsed) > 0 {
		toolsList = strings.Join(req.ToolsUsed, ", ")
	}

	var buf bytes.Buffer
	_ = distillationPromptTemplate.Execute(&buf, distillationVars{
		Goal:      req.Goal,
		Result:    req.Output,
		ToolsList: toolsList,
	})
	return buf.String()
}
