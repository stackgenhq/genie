// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

/*
Copyright © 2026 StackGen, Inc.
*/

package setup

import (
	"path/filepath"
	"strings"

	"github.com/stackgenhq/genie/pkg/datasource"
)

// ApplyCollectedToInputs maps collected setup answers to WizardInputs for
// writing config. When APIKey is empty and detectedProvider is set, the config
// uses the env var placeholder for that provider.
func ApplyCollectedToInputs(c *Collected, detectedProvider string) WizardInputs {
	in := DefaultWizardInputs()
	get := func(s string) string { return strings.TrimSpace(s) }
	in.AgentName = get(c.AgentName)
	in.Platform = get(c.Platform)
	if in.Platform == "" {
		in.Platform = DefaultPlatform
	}
	in.TelegramTokenLiteral = get(c.TelegramToken)
	in.TelegramTokenEnv = get(c.TelegramTokenEnv)
	if in.TelegramTokenEnv == "" && in.Platform == "telegram" {
		in.TelegramTokenEnv = "TELEGRAM_BOT_TOKEN"
	}
	in.SlackAppTokenEnv = get(c.SlackAppTokenEnv)
	if in.SlackAppTokenEnv == "" && in.Platform == "slack" {
		in.SlackAppTokenEnv = "SLACK_APP_TOKEN"
	}
	in.SlackBotTokenEnv = get(c.SlackBotTokenEnv)
	if in.SlackBotTokenEnv == "" && in.Platform == "slack" {
		in.SlackBotTokenEnv = "SLACK_BOT_TOKEN"
	}
	in.DiscordBotTokenEnv = get(c.DiscordBotTokenEnv)
	if in.DiscordBotTokenEnv == "" && in.Platform == "discord" {
		in.DiscordBotTokenEnv = "DISCORD_BOT_TOKEN"
	}
	in.TeamsAppIDEnv = get(c.TeamsAppIDEnv)
	if in.TeamsAppIDEnv == "" && in.Platform == "teams" {
		in.TeamsAppIDEnv = "TEAMS_APP_ID"
	}
	in.TeamsAppPassEnv = get(c.TeamsAppPassEnv)
	if in.TeamsAppPassEnv == "" && in.Platform == "teams" {
		in.TeamsAppPassEnv = "TEAMS_APP_PASSWORD"
	}
	in.TeamsListenAddr = ":3978"
	in.GoogleChatCreds = get(c.GoogleChatCreds)
	in.ModelProvider = get(c.ModelProvider)
	if in.ModelProvider == "" {
		in.ModelProvider = "openai"
	}
	if get(c.ModelName) != "" {
		in.ModelName = get(c.ModelName)
	} else {
		in.ModelName = DefaultModelForProvider(in.ModelProvider)
	}
	apiKey := get(c.APIKey)
	if apiKey != "" {
		in.ModelProviderTokenLiteral = apiKey
	} else {
		in.ModelProviderTokenEnv = SecretNameForProvider(in.ModelProvider)
	}
	skillsLine := get(c.SkillsRoots)
	if skillsLine != "" {
		in.SkillsRoots = splitCSVStatic(skillsLine)
	}
	if len(in.SkillsRoots) == 0 {
		in.SkillsRoots = []string{DefaultSkillsRoots}
	}
	in.ManageGoogleServices = get(c.ManageGoogleServices) == ChoiceYes
	in.Learn = get(c.Learn) == ChoiceYes
	keywordsLine := get(c.DataSourcesKeywords)
	if keywordsLine != "" {
		in.DataSourceKeywords = splitCSVStatic(keywordsLine)
		if len(in.DataSourceKeywords) > datasource.MaxSearchKeywords {
			in.DataSourceKeywords = in.DataSourceKeywords[:datasource.MaxSearchKeywords]
		}
	}
	in.AGUIPasswordProtected = c.AGUIPasswordProtected
	return in
}

// ConfigPathFromCollected returns the absolute config path from collected or default.
func ConfigPathFromCollected(c *Collected, configPathDefault string) (string, error) {
	p := strings.TrimSpace(c.ConfigPath)
	if p == "" {
		p = configPathDefault
	}
	return filepath.Abs(p)
}

func splitCSVStatic(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
