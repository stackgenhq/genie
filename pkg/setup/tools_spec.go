// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package setup: tools_spec loads optional-tool definitions from embedded JSON
// and applies collected answers to config.GenieConfig so the setup wizard can
// offer dynamic, data-driven forms for email, web search, calendar, etc.
package setup

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/stackgenhq/genie/pkg/config"
	"github.com/stackgenhq/genie/pkg/tools/email"
	"github.com/stackgenhq/genie/pkg/tools/google/gdrive"
	"github.com/stackgenhq/genie/pkg/tools/pm"
	"github.com/stackgenhq/genie/pkg/tools/scm"
	"github.com/stackgenhq/genie/pkg/tools/websearch"
)

//go:embed tools.json
var toolsJSON []byte

// ToolSpec describes one optional tool and its setup questions.
// It is loaded from tools.json so devs can add tools without changing wizard code.
type ToolSpec struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Questions   []QuestionSpec `json:"questions"`
}

// QuestionSpec describes a single question in a tool's form.
type QuestionSpec struct {
	Key         string       `json:"key"`
	Label       string       `json:"label"`
	Description string       `json:"description"`
	Type        string       `json:"type"` // "input", "password", "select", "number"
	Options     []OptionSpec `json:"options,omitempty"`
	Default     string       `json:"default,omitempty"`
	Placeholder string       `json:"placeholder,omitempty"`
	Optional    bool         `json:"optional,omitempty"`
}

// OptionSpec is one option in a select question.
type OptionSpec struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// toolsSchema is the root of tools.json.
type toolsSchema struct {
	Tools []ToolSpec `json:"tools"`
}

// LoadSetupTools returns the list of optional tools and their questions from
// embedded tools.json. Add or edit tools there to change the wizard without
// changing Go code.
func LoadSetupTools() ([]ToolSpec, error) {
	var s toolsSchema
	if err := json.Unmarshal(toolsJSON, &s); err != nil {
		return nil, fmt.Errorf("parse tools.json: %w", err)
	}
	return s.Tools, nil
}

// ApplyToolAnswers applies collected tool answers to cfg. Only tools present
// in answers are configured; others are left as zero values. Call this after
// building the base GenieConfig so optional tools are merged in.
func ApplyToolAnswers(cfg *config.GenieConfig, answers map[string]map[string]string) {
	if cfg == nil || len(answers) == 0 {
		return
	}
	for toolID, m := range answers {
		applyOne(cfg, toolID, m)
	}
}

func applyOne(cfg *config.GenieConfig, toolID string, m map[string]string) {
	get := func(k string) string { return strings.TrimSpace(m[k]) }
	getInt := func(k string) int {
		s := get(k)
		if s == "" {
			return 0
		}
		n, _ := strconv.Atoi(s)
		return n
	}

	switch toolID {
	case "email":
		cfg.Email = email.Config{
			Provider: get("provider"),
			Host:     get("host"),
			Port:     getInt("port"),
			Username: get("username"),
			Password: get("password"),
			IMAPHost: get("imap_host"),
			IMAPPort: getInt("imap_port"),
		}
	case "web_search":
		cfg.WebSearch = websearch.Config{
			Provider:     get("provider"),
			GoogleAPIKey: get("google_api_key"),
			GoogleCX:     get("google_cx"),
			BingAPIKey:   get("bing_api_key"),
		}
	case "google_drive":
		cfg.GDrive = gdrive.Config{
			CredentialsFile: get("credentials_file"),
		}
	case "project_management":
		cfg.ProjectManagement = pm.Config{
			Provider: get("provider"),
			APIToken: get("api_token"),
			BaseURL:  get("base_url"),
			Email:    get("email"),
		}
	case "scm":
		cfg.SCM = scm.Config{
			Provider: get("provider"),
			Token:    get("token"),
			BaseURL:  get("base_url"),
		}
	}
}
