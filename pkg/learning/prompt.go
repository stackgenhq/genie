// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package learning

import (
	"bytes"
	_ "embed"
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
