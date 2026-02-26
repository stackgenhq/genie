/*
Copyright © 2026 StackGen, Inc.
*/

// Package setup provides config generation for the day-1 onboarding wizard.
// It builds config.GenieConfig from wizard answers and encodes it with the
// TOML library so CLI-generated config is compatible with config.LoadGenieConfig.
// The web Config Builder is at https://appcd-dev.github.io/stackgen-genie/config-builder.html.
package setup

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/stackgenhq/genie/pkg/config"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/pii"
	"github.com/stackgenhq/genie/pkg/security"
)

// WizardInputs holds the raw answers from the setup wizard (env var names,
// platform choice, paths). It is not a duplicate of config structs; it is
// only the form data used to build config.GenieConfig.
type WizardInputs struct {
	// Messenger
	Platform           string // "", "slack", "telegram", "teams", "googlechat", "whatsapp", "discord"
	TelegramTokenEnv   string
	SlackAppTokenEnv   string
	SlackBotTokenEnv   string
	DiscordBotTokenEnv string
	TeamsAppIDEnv      string
	TeamsAppPassEnv    string
	TeamsListenAddr    string
	GoogleChatCreds    string
	GoogleChatListen   string
	WhatsAppStorePath  string

	// Model provider (first provider)
	ModelProvider             string // "openai", "gemini", "anthropic"
	ModelName                 string // default model for that provider
	ModelProviderTokenEnv     string // env var name when not pasting key
	ModelProviderTokenLiteral string // pasted API key (stored in config when set)
	// Skills
	SkillsRoots []string
	// ManageGoogleServices is true when the user chose to connect Google (Calendar, Contacts, Gmail) during setup.
	ManageGoogleServices bool
}

// DefaultWizardInputs returns inputs with the same defaults as the web Config Builder.
func DefaultWizardInputs() WizardInputs {
	return WizardInputs{
		ModelProviderTokenEnv: "OPENAI_API_KEY",
		SkillsRoots:           []string{"./skills"},
		TeamsListenAddr:       ":3978",
		GoogleChatListen:      ":8080",
	}
}

// BuildGenieConfig builds a config.GenieConfig from wizard inputs. When
// securitySecrets is non-nil, the config uses [security.secrets] with filevar
// URLs and token is "${name}"; otherwise when ModelProviderTokenLiteral is set
// the token is literal, or "${ENV_VAR}" when using env. Optional toolAnswers
// (from the tools step) are applied via ApplyToolAnswers when non-nil.
func BuildGenieConfig(in WizardInputs, securitySecrets map[string]string, toolAnswers map[string]map[string]string) config.GenieConfig {
	provider := in.ModelProvider
	if provider == "" {
		provider = "openai"
	}
	modelName := in.ModelName
	if modelName == "" {
		modelName = DefaultModelForProvider(provider)
	}
	var token string
	secretName := SecretNameForProvider(provider)
	if securitySecrets != nil && securitySecrets[secretName] != "" {
		token = envPlaceholder(secretName)
	} else if in.ModelProviderTokenLiteral != "" {
		token = in.ModelProviderTokenLiteral
	} else {
		token = envPlaceholder(in.ModelProviderTokenEnv)
	}
	cfg := config.GenieConfig{
		ModelConfig: modelprovider.ModelConfig{
			Providers: modelprovider.ProviderConfigs{
				{
					Provider:    provider,
					ModelName:   modelName,
					Variant:     "default",
					Token:       token,
					GoodForTask: modelprovider.TaskEfficiency,
				},
			},
		},
		SkillsRoots: in.SkillsRoots,
		Messenger:   buildMessengerConfig(in),
		PII:         pii.DefaultConfig(),
	}
	if len(securitySecrets) > 0 {
		cfg.Security = security.Config{Secrets: securitySecrets}
	}
	if len(toolAnswers) > 0 {
		ApplyToolAnswers(&cfg, toolAnswers)
	}
	return cfg
}

// SecretNameForProvider returns the conventional env/secret name for the provider.
func SecretNameForProvider(provider string) string {
	switch strings.ToLower(provider) {
	case "gemini":
		return "GOOGLE_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	default:
		return "OPENAI_API_KEY"
	}
}

// DefaultModelForProvider returns a default model name for the given provider.
func DefaultModelForProvider(provider string) string {
	switch provider {
	case "gemini":
		return "gemini-2.5-flash"
	case "anthropic":
		return "claude-sonnet-4"
	default:
		return "gpt-5.2"
	}
}

func envPlaceholder(name string) string {
	if name == "" {
		return ""
	}
	return "${" + name + "}"
}

func buildMessengerConfig(in WizardInputs) messenger.Config {
	agui := messenger.DefaultAGUIConfig()
	agui.RateLimit = 30 // 30 requests per second per IP (wizard default)
	agui.RateBurst = 60 // burst allowance for 30 req/s
	agui.CORSOrigins = []string{"https://appcd-dev.github.io"}
	mc := messenger.Config{
		AGUI: agui,
	}
	if in.Platform == "" {
		return mc
	}
	mc.Platform = messenger.Platform(in.Platform)
	switch in.Platform {
	case "slack":
		mc.Slack.AppToken = envPlaceholder(in.SlackAppTokenEnv)
		mc.Slack.BotToken = envPlaceholder(in.SlackBotTokenEnv)
	case "discord":
		mc.Discord.BotToken = envPlaceholder(in.DiscordBotTokenEnv)
	case "telegram":
		mc.Telegram.Token = envPlaceholder(in.TelegramTokenEnv)
	case "teams":
		mc.Teams.AppID = envPlaceholder(in.TeamsAppIDEnv)
		mc.Teams.AppPassword = envPlaceholder(in.TeamsAppPassEnv)
		mc.Teams.ListenAddr = in.TeamsListenAddr
	case "googlechat":
		mc.GoogleChat.CredentialsFile = in.GoogleChatCreds
		mc.GoogleChat.ListenAddr = in.GoogleChatListen
	case "whatsapp":
		mc.WhatsApp.StorePath = in.WhatsAppStorePath
	}
	return mc
}

// EncodeTOML encodes cfg to TOML and writes it to w using the same library
// (github.com/BurntSushi/toml) and structs as config.LoadGenieConfig.
func EncodeTOML(w io.Writer, cfg config.GenieConfig) error {
	return toml.NewEncoder(w).Encode(cfg)
}

// WriteConfigFile builds config from inputs and writes it to path. When
// ModelProviderTokenLiteral is set, the pasted key is written to a separate
// secrets directory (configDir/secrets) and [security.secrets] is set to
// filevar URLs so gocloud.dev/runtimevar/filevar loads them at runtime.
// The main config file never contains the raw secret. Optional toolAnswers
// from the tools step are merged into the config when non-nil.
func WriteConfigFile(path string, in WizardInputs, toolAnswers map[string]map[string]string) error {
	var securitySecrets map[string]string
	if in.ModelProviderTokenLiteral != "" {
		configDir := filepath.Dir(path)
		secretsDir := filepath.Join(configDir, "secrets")
		if err := os.MkdirAll(secretsDir, 0700); err != nil {
			return err
		}
		name := SecretNameForProvider(in.ModelProvider)
		secretPath := filepath.Join(secretsDir, name)
		if err := os.WriteFile(secretPath, []byte(in.ModelProviderTokenLiteral), 0600); err != nil {
			return err
		}
		absPath, err := filepath.Abs(secretPath)
		if err != nil {
			return err
		}
		// filevar URL: file:///path?decoder=string (gocloud.dev/runtimevar/filevar)
		slash := filepath.ToSlash(absPath)
		if strings.HasPrefix(slash, "/") {
			slash = "file://" + slash
		} else {
			slash = "file:///" + slash
		}
		fileURL := slash + "?decoder=string"
		securitySecrets = map[string]string{name: fileURL}
	}
	cfg := BuildGenieConfig(in, securitySecrets, toolAnswers)
	var buf bytes.Buffer
	if err := EncodeTOML(&buf, cfg); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0600)
}
