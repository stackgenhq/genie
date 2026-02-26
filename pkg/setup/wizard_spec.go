/*
Copyright © 2026 StackGen, Inc.
*/

package setup

import (
	"path/filepath"
	"strings"
)

// ApplyCollectedToInputs maps collected setup answers (key -> value) to
// WizardInputs for writing config. When api_key is empty and a provider was
// detected from env, the config uses the env var placeholder.
func ApplyCollectedToInputs(collected map[string]string, detectedProvider string) WizardInputs {
	in := DefaultWizardInputs()
	get := func(k string) string { return strings.TrimSpace(collected[k]) }
	in.Platform = get("platform")
	if in.Platform == "" {
		in.Platform = "agui"
	}
	in.TelegramTokenEnv = get("telegram_token_env")
	if in.TelegramTokenEnv == "" && in.Platform == "telegram" {
		in.TelegramTokenEnv = "TELEGRAM_BOT_TOKEN"
	}
	in.SlackAppTokenEnv = get("slack_app_token_env")
	if in.SlackAppTokenEnv == "" && in.Platform == "slack" {
		in.SlackAppTokenEnv = "SLACK_APP_TOKEN"
	}
	in.SlackBotTokenEnv = get("slack_bot_token_env")
	if in.SlackBotTokenEnv == "" && in.Platform == "slack" {
		in.SlackBotTokenEnv = "SLACK_BOT_TOKEN"
	}
	in.DiscordBotTokenEnv = get("discord_bot_token_env")
	if in.DiscordBotTokenEnv == "" && in.Platform == "discord" {
		in.DiscordBotTokenEnv = "DISCORD_BOT_TOKEN"
	}
	in.TeamsAppIDEnv = get("teams_app_id_env")
	if in.TeamsAppIDEnv == "" && in.Platform == "teams" {
		in.TeamsAppIDEnv = "TEAMS_APP_ID"
	}
	in.TeamsAppPassEnv = get("teams_app_pass_env")
	if in.TeamsAppPassEnv == "" && in.Platform == "teams" {
		in.TeamsAppPassEnv = "TEAMS_APP_PASSWORD"
	}
	in.TeamsListenAddr = ":3978"
	in.GoogleChatCreds = get("google_chat_creds")
	in.GoogleChatListen = ":8080"
	in.WhatsAppStorePath = get("whatsapp_store_path")
	in.ModelProvider = get("model_provider")
	if in.ModelProvider == "" {
		in.ModelProvider = "openai"
	}
	in.ModelName = DefaultModelForProvider(in.ModelProvider)
	apiKey := get("api_key")
	if apiKey != "" {
		in.ModelProviderTokenLiteral = apiKey
	} else {
		in.ModelProviderTokenEnv = SecretNameForProvider(in.ModelProvider)
	}
	skillsLine := get("skills_roots")
	if skillsLine != "" {
		in.SkillsRoots = splitCSVStatic(skillsLine)
	}
	if len(in.SkillsRoots) == 0 {
		in.SkillsRoots = []string{"./skills"}
	}
	in.ManageGoogleServices = get("manage_google_services") == "yes"
	return in
}

// ConfigPathFromCollected returns the absolute config path from collected or default.
func ConfigPathFromCollected(collected map[string]string, configPathDefault string) (string, error) {
	p := strings.TrimSpace(collected["config_path"])
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
