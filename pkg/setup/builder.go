/*
Copyright © 2026 StackGen, Inc.
*/

// Package setup provides config generation for the day-1 onboarding wizard.
// It builds config.GenieConfig from wizard answers and encodes it with the
// TOML library so CLI-generated config is compatible with config.LoadGenieConfig.
// The web Config Builder is at https://stackgenhq.github.io/genie/config-builder.html.
package setup

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/stackgenhq/genie/pkg/config"
	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/memory/graph"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/pii"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/security/keyring"
	"github.com/stackgenhq/genie/pkg/tools"
)

// WizardInputs holds the raw answers from the setup wizard (env var names,
// platform choice, paths). It is not a duplicate of config structs; it is
// only the form data used to build config.GenieConfig.
type WizardInputs struct {
	// AgentName is the user-chosen name for the agent (personality and default audit path).
	AgentName string
	// Messenger
	Platform             string // "", "slack", "telegram", "teams", "googlechat", "whatsapp", "discord"
	TelegramTokenEnv     string // env var name when token not in keyring
	TelegramTokenLiteral string // pasted token stored in keyring when set
	SlackAppTokenEnv     string
	SlackBotTokenEnv     string
	DiscordBotTokenEnv   string
	TeamsAppIDEnv        string
	TeamsAppPassEnv      string
	TeamsListenAddr      string
	GoogleChatCreds      string

	// Model provider (first provider)
	ModelProvider             string // "openai", "gemini", "anthropic"
	ModelName                 string // default model for that provider
	ModelProviderTokenEnv     string // env var name when not pasting key
	ModelProviderTokenLiteral string // pasted API key (stored in config when set)
	// Skills
	SkillsRoots []string
	// ManageGoogleServices is true when the user chose to connect Google (Calendar, Contacts, Gmail) during setup.
	ManageGoogleServices bool
	// Learn is true when the user wants Genie to gather data (Gmail, Calendar) and learn from it: sync runs in the background and a knowledge graph is built after the first sync.
	Learn bool
	// DataSourceKeywords are up to MaxSearchKeywords terms (e.g. project names, customers) that Genie should look for when indexing; only items matching any keyword are embedded. From setup wizard or config.
	DataSourceKeywords []string

	// AGUIPasswordProtected is true when the user chose to secure the built-in web chat with a password (stored in keyring per agent).
	AGUIPasswordProtected bool
}

// DefaultWizardInputs returns inputs with the same defaults as the web Config Builder.
func DefaultWizardInputs() WizardInputs {
	return WizardInputs{
		ModelProviderTokenEnv: keyring.AccountOpenAIAPIKey,
		SkillsRoots:           []string{"./skills"},
		TeamsListenAddr:       ":3978",
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
	var token, host string
	if provider == "ollama" {
		token = ""
		host = modelprovider.DefaultOllamaURL
	} else {
		secretName := SecretNameForProvider(provider)
		if securitySecrets != nil && securitySecrets[secretName] != "" {
			token = envPlaceholder(secretName)
		} else if in.ModelProviderTokenLiteral != "" {
			token = in.ModelProviderTokenLiteral
		} else {
			token = envPlaceholder(in.ModelProviderTokenEnv)
		}
	}
	cfg := config.GenieConfig{
		AgentName: strings.TrimSpace(in.AgentName),
		ModelConfig: modelprovider.ModelConfig{
			Providers: modelprovider.ProviderConfigs{
				{
					Provider:    provider,
					ModelName:   modelName,
					Variant:     "default",
					Token:       token,
					Host:        host,
					GoodForTask: modelprovider.TaskEfficiency,
				},
			},
		},
		SkillLoadConfig: tools.SkillLoadConfig{SkillsRoots: in.SkillsRoots},
		Messenger:       buildMessengerConfig(in, securitySecrets),
		PII:             pii.DefaultConfig(),
	}
	if len(securitySecrets) > 0 {
		cfg.Security = security.Config{Secrets: securitySecrets}
	}
	if in.Learn {
		cfg.DataSources = defaultDataSourcesConfigForOnboarding()
		cfg.DataSources.SearchKeywords = trimKeywords(in.DataSourceKeywords, datasource.MaxSearchKeywords)
	}
	if len(toolAnswers) > 0 {
		ApplyToolAnswers(&cfg, toolAnswers)
	}
	return cfg
}

// trimKeywords returns up to max non-empty, trimmed keywords (no duplicate keys).
func trimKeywords(keywords []string, max int) []string {
	if max <= 0 || len(keywords) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, k := range keywords {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		lower := strings.ToLower(k)
		if _, ok := seen[lower]; ok {
			continue
		}
		seen[lower] = struct{}{}
		out = append(out, k)
		if len(out) >= max {
			break
		}
	}
	return out
}

// defaultDataSourcesConfigForOnboarding returns a DataSources config that
// enables learning from Gmail (INBOX) and Drive when the user opts in during
// setup. Calendar is left disabled until the app wires a Calendar connector
// into the sync path. Drive has no folders so users can add folder IDs in config.
func defaultDataSourcesConfigForOnboarding() datasource.Config {
	return datasource.Config{
		Enabled:      true,
		SyncInterval: 15 * time.Minute,
		Gmail:        &datasource.GmailSourceConfig{Enabled: true, LabelIDs: []string{"INBOX"}},
		Calendar:     &datasource.CalendarSourceConfig{Enabled: false, CalendarIDs: []string{"primary"}},
		GDrive:       &datasource.GDriveSourceConfig{Enabled: true, FolderIDs: []string{}},
	}
}

// SecretNameForProvider returns the conventional env/secret name for the provider.
// Uses keyring account constants so keyring and config stay in sync.
func SecretNameForProvider(provider string) string {
	switch strings.ToLower(provider) {
	case "gemini":
		return keyring.AccountGoogleAPIKey
	case "anthropic":
		return keyring.AccountAnthropicAPIKey
	default:
		return keyring.AccountOpenAIAPIKey
	}
}

// DefaultModelForProvider returns a default model name for the given provider.
func DefaultModelForProvider(provider string) string {
	switch provider {
	case "gemini":
		return modelprovider.DefaultGeminiModel
	case "anthropic":
		return modelprovider.DefaultAnthropicModel
	case "openai":
		return modelprovider.DefaultOpenAIModel
	case "ollama":
		return modelprovider.DefaultOllamaModelForSetup
	case "huggingface":
		return modelprovider.DefaultHuggingFaceModel
	default:
		return modelprovider.DefaultOpenAIModel
	}
}

func envPlaceholder(name string) string {
	if name == "" {
		return ""
	}
	return "${" + name + "}"
}

func buildMessengerConfig(in WizardInputs, securitySecrets map[string]string) messenger.Config {
	agui := messenger.DefaultAGUIConfig()
	agui.RateLimit = 30 // 30 requests per second per IP (wizard default)
	agui.RateBurst = 60 // burst allowance for 30 req/s
	agui.CORSOrigins = []string{"https://stackgenhq.github.io"}
	agui.Auth.Password.Enabled = in.AGUIPasswordProtected
	mc := messenger.Config{
		AGUI: agui,
	}
	if in.Platform == "" {
		return mc
	}
	mc.Platform = messenger.Platform(in.Platform)
	// When a secret is in securitySecrets it was stored in keyring; config references it by name so the secret provider resolves via keyringvar.
	useKeyring := func(account string) string {
		if securitySecrets != nil && securitySecrets[account] != "" {
			return envPlaceholder(account)
		}
		return ""
	}
	switch in.Platform {
	case "slack":
		if s := useKeyring(keyring.AccountSlackAppToken); s != "" {
			mc.Slack.AppToken = s
		} else {
			mc.Slack.AppToken = envPlaceholder(in.SlackAppTokenEnv)
		}
		if s := useKeyring(keyring.AccountSlackBotToken); s != "" {
			mc.Slack.BotToken = s
		} else {
			mc.Slack.BotToken = envPlaceholder(in.SlackBotTokenEnv)
		}
	case "discord":
		if s := useKeyring(keyring.AccountDiscordBotToken); s != "" {
			mc.Discord.BotToken = s
		} else {
			mc.Discord.BotToken = envPlaceholder(in.DiscordBotTokenEnv)
		}
	case "telegram":
		if s := useKeyring(keyring.AccountTelegramBotToken); s != "" {
			mc.Telegram.Token = s
		} else {
			mc.Telegram.Token = envPlaceholder(in.TelegramTokenEnv)
		}
	case "teams":
		if s := useKeyring(keyring.AccountTeamsAppID); s != "" {
			mc.Teams.AppID = s
		} else {
			mc.Teams.AppID = envPlaceholder(in.TeamsAppIDEnv)
		}
		if s := useKeyring(keyring.AccountTeamsAppPassword); s != "" {
			mc.Teams.AppPassword = s
		} else {
			mc.Teams.AppPassword = envPlaceholder(in.TeamsAppPassEnv)
		}
		mc.Teams.ListenAddr = in.TeamsListenAddr
	case "googlechat":
		// Google Chat uses the logged-in user OAuth token (SecretProvider); no config fields.
	}
	return mc
}

// EncodeTOML encodes cfg to TOML and writes it to w using the same library
// (github.com/BurntSushi/toml) and structs as config.LoadGenieConfig.
func EncodeTOML(w io.Writer, cfg config.GenieConfig) error {
	return toml.NewEncoder(w).Encode(cfg)
}

// keyringURL returns the runtimevar URL for a keyring-backed secret so the
// secret provider (and keyringvar) can resolve it at runtime.
func keyringURL(account string) string {
	return "keyring:///" + account + "?decoder=string"
}

// storePlatformSecretsInKeyring reads platform token env vars from the
// environment and stores any non-empty values in the keyring, then adds
// keyring URLs to securitySecrets so config references them and the secret
// provider resolves via keyringvar.
func storePlatformSecretsInKeyring(in WizardInputs, securitySecrets *map[string]string) error {
	ensure := func() {
		if *securitySecrets == nil {
			*securitySecrets = make(map[string]string)
		}
	}
	switch in.Platform {
	case "slack":
		if v := os.Getenv(in.SlackAppTokenEnv); v != "" {
			if err := keyring.KeyringSet(keyring.AccountSlackAppToken, []byte(v)); err != nil {
				return fmt.Errorf("store Slack app token in keyring: %w", err)
			}
			ensure()
			(*securitySecrets)[keyring.AccountSlackAppToken] = keyringURL(keyring.AccountSlackAppToken)
		}
		if v := os.Getenv(in.SlackBotTokenEnv); v != "" {
			if err := keyring.KeyringSet(keyring.AccountSlackBotToken, []byte(v)); err != nil {
				return fmt.Errorf("store Slack bot token in keyring: %w", err)
			}
			ensure()
			(*securitySecrets)[keyring.AccountSlackBotToken] = keyringURL(keyring.AccountSlackBotToken)
		}
	case "discord":
		if v := os.Getenv(in.DiscordBotTokenEnv); v != "" {
			if err := keyring.KeyringSet(keyring.AccountDiscordBotToken, []byte(v)); err != nil {
				return fmt.Errorf("store Discord bot token in keyring: %w", err)
			}
			ensure()
			(*securitySecrets)[keyring.AccountDiscordBotToken] = keyringURL(keyring.AccountDiscordBotToken)
		}
	case "teams":
		if v := os.Getenv(in.TeamsAppIDEnv); v != "" {
			if err := keyring.KeyringSet(keyring.AccountTeamsAppID, []byte(v)); err != nil {
				return fmt.Errorf("store Teams app ID in keyring: %w", err)
			}
			ensure()
			(*securitySecrets)[keyring.AccountTeamsAppID] = keyringURL(keyring.AccountTeamsAppID)
		}
		if v := os.Getenv(in.TeamsAppPassEnv); v != "" {
			if err := keyring.KeyringSet(keyring.AccountTeamsAppPassword, []byte(v)); err != nil {
				return fmt.Errorf("store Teams app password in keyring: %w", err)
			}
			ensure()
			(*securitySecrets)[keyring.AccountTeamsAppPassword] = keyringURL(keyring.AccountTeamsAppPassword)
		}
	}
	return nil
}

// WriteConfigFile builds config from inputs and writes it to path. Every
// secret collected (pasted or from env) is stored in the system keyring and
// [security.secrets] is set to keyring:// URLs so the secret provider
// resolves them via keyringvar at runtime. The config file never contains
// raw secrets. Optional toolAnswers from the tools step are merged when non-nil.
// If vectorMemoryOverride is non-nil, it is used as the [vector_memory] config
// (e.g. when setup detects a local Ollama for embeddings).
func WriteConfigFile(path string, in WizardInputs, toolAnswers map[string]map[string]string, vectorMemoryOverride *vector.Config) error {
	var securitySecrets map[string]string
	if in.ModelProviderTokenLiteral != "" {
		name := SecretNameForProvider(in.ModelProvider)
		if err := keyring.KeyringSet(name, []byte(in.ModelProviderTokenLiteral)); err != nil {
			return fmt.Errorf("store API key in keyring: %w", err)
		}
		if securitySecrets == nil {
			securitySecrets = make(map[string]string)
		}
		securitySecrets[name] = keyringURL(name)
	}
	if in.TelegramTokenLiteral != "" {
		if err := keyring.KeyringSet(keyring.AccountTelegramBotToken, []byte(in.TelegramTokenLiteral)); err != nil {
			return fmt.Errorf("store Telegram token in keyring: %w", err)
		}
		if securitySecrets == nil {
			securitySecrets = make(map[string]string)
		}
		securitySecrets[keyring.AccountTelegramBotToken] = keyringURL(keyring.AccountTelegramBotToken)
	}
	// Store any platform tokens present in the environment so config can reference keyring; secret provider will resolve via keyringvar.
	if err := storePlatformSecretsInKeyring(in, &securitySecrets); err != nil {
		return err
	}
	if len(securitySecrets) > 0 {
		_, _ = fmt.Fprintf(os.Stderr, "Note: Secrets have been stored in the system keyring. Use 'genie back-to-bottle' to clear them.\n")
	}
	cfg := BuildGenieConfig(in, securitySecrets, toolAnswers)
	if vectorMemoryOverride != nil {
		cfg.VectorMemory = *vectorMemoryOverride
	}
	var buf bytes.Buffer
	if err := EncodeTOML(&buf, cfg); err != nil {
		return err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		return err
	}
	// When user opted into Learn, write pending flag so the app runs one
	// graph-learn pass after the first successful data sources sync.
	if in.Learn && strings.TrimSpace(in.AgentName) != "" {
		agentDir := graph.DataDirForAgent(in.AgentName)
		if err := os.MkdirAll(agentDir, 0755); err != nil {
			return fmt.Errorf("create agent dir for graph learn pending: %w", err)
		}
		pendingPath := graph.PendingGraphLearnPath(in.AgentName)
		if err := os.WriteFile(pendingPath, []byte("1"), 0600); err != nil {
			return fmt.Errorf("write graph learn pending flag: %w", err)
		}
	}
	return nil
}
