// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package setup

// Choice values for yes/no setup questions.
const (
	ChoiceYes = "yes"
	ChoiceNo  = "no"
)

// Default platform when user does not pick one.
const DefaultPlatform = "agui"

// Default skills roots path offered in the wizard.
const DefaultSkillsRoots = "./skills"

// Collected holds all answers from the setup wizard. The CLI populates it
// step-by-step; ApplyCollectedToInputs and ConfigPathFromCollected consume it
// to build WizardInputs and resolve the config path.
type Collected struct {
	ConfigPath    string
	SkillsRoots   string
	ModelProvider string
	ModelName     string // optional; when set (e.g. user picked Ollama model) used for that provider
	AgentName     string
	APIKey        string
	Platform      string

	// Messenger tokens / env (platform-specific)
	TelegramToken      string
	TelegramTokenEnv   string
	SlackAppTokenEnv   string
	SlackBotTokenEnv   string
	DiscordBotTokenEnv string
	TeamsAppIDEnv      string
	TeamsAppPassEnv    string
	GoogleChatCreds    string

	// Google and data sources
	ManageGoogleServices string // ChoiceYes or ChoiceNo
	// Learn is ChoiceYes when the user wants Genie to gather data (Gmail, Calendar) and learn from it (sync + knowledge graph after first sync).
	Learn               string
	DataSourcesKeywords string

	// AGUI (built-in web): when true, the web chat is secured with a password (stored in keyring per agent during setup).
	AGUIPasswordProtected bool
}
