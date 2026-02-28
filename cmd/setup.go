/*
Copyright © 2026 StackGen, Inc.
*/
package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/osutils"
	"github.com/stackgenhq/genie/pkg/security/keyring"
	"github.com/stackgenhq/genie/pkg/setup"
	"github.com/stackgenhq/genie/pkg/tools/google/oauth"
)

type setupCmdOption struct {
	configPath string
}

type setupCmd struct {
	opts setupCmdOption
}

func newSetupCommand() *cobra.Command {
	setup := &setupCmd{
		opts: setupCmdOption{},
	}
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Create config (quick 3-step setup)",
		Long:  `Setup creates a .genie.toml config file. Three steps: AI key, where to chat (messenger), and optional Google sign-in.`,
		RunE:  setup.runSetup,
	}
	cmd.Flags().StringVar(&setup.opts.configPath, "config", "", "path for config file (default: .genie.toml in current directory)")
	return cmd
}

func (s setupCmd) runSetup(cmd *cobra.Command, args []string) error {
	wd, err := osutils.Getwd()
	if err != nil {
		return fmt.Errorf("working directory: %w", err)
	}
	defaultPath := filepath.Join(wd, ".genie.toml")
	if s.opts.configPath != "" {
		if err := setup.ValidateConfigPath(s.opts.configPath); err != nil {
			return fmt.Errorf("config path: %w", err)
		}
		defaultPath, err = filepath.Abs(s.opts.configPath)
		if err != nil {
			return fmt.Errorf("config path: %w", err)
		}
	}
	// Scope keyring so this instance's Google OAuth (and other secrets) do not collide with others.
	keyring.SetKeyringScopeFromConfigPath(defaultPath)

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	c := setup.Collected{
		ConfigPath:  defaultPath,
		SkillsRoots: setup.DefaultSkillsRoots,
	}
	detectedProvider := setup.DetectAIKeyProvider()
	if detectedProvider != "" {
		c.ModelProvider = detectedProvider
	}
	// When no API key is set, offer Ollama as a provider if it is reachable locally.
	ollamaAvailable := detectedProvider == "" && modelprovider.OllamaReachable(ctx, "")
	var ollamaModels []string
	if ollamaAvailable {
		if models, err := modelprovider.ListModels(ctx, ""); err == nil && len(models) > 0 {
			ollamaModels = models
		}
	}

	// Step 0: Name your agent (personality; used for default audit log path)
	agentName := "Genie"
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Name your agent").
				Description("Give your agent a name. It will develop a personality and learn about you over time. Audit logs are stored under ~/.genie/{name}.<date>.ndjson.").
				Placeholder("e.g. Genie, Alfred, Jarvis").
				Value(&agentName).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("agent name is required")
					}
					return nil
				}),
		).Title("Your agent").Description("A name helps your agent feel like a consistent assistant."),
	).WithTheme(huh.ThemeCharm()).WithAccessible(os.Getenv("ACCESSIBLE") != "").Run(); err != nil {
		return err
	}
	c.AgentName = strings.TrimSpace(agentName)
	printStepSuccess()

	// Step 1: LLM key (skip entirely if key already in env)
	if detectedProvider == "" {
		var modelProvider, apiKey string
		modelProvider = "openai"
		providerOptions := []huh.Option[string]{
			huh.NewOption("OpenAI (ChatGPT)", "openai"),
			huh.NewOption("Google (Gemini)", "gemini"),
			huh.NewOption("Anthropic (Claude)", "anthropic"),
		}
		if ollamaAvailable {
			providerOptions = append(providerOptions, huh.NewOption("Ollama (local)", "ollama"))
		}
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Which AI do you use?").
					Options(providerOptions...).
					Value(&modelProvider),
				huh.NewInput().
					Title("Paste your API key").
					Description("OpenAI: https://platform.openai.com/api-keys | Gemini: https://aistudio.google.com/api-keys | Anthropic: https://platform.claude.com/settings/keys. Leave empty for Ollama.").
					Value(&apiKey).
					EchoMode(huh.EchoModePassword).
					Validate(func(s string) error {
						if strings.TrimSpace(modelProvider) != "ollama" && strings.TrimSpace(s) == "" {
							return fmt.Errorf("required")
						}
						return nil
					}),
			).Title("Your AI key").Description("We need an API key so Genie can talk to your AI."),
		).WithTheme(huh.ThemeCharm()).WithAccessible(os.Getenv("ACCESSIBLE") != "").Run(); err != nil {
			return err
		}
		c.ModelProvider = strings.TrimSpace(modelProvider)
		if k := strings.TrimSpace(apiKey); k != "" {
			c.APIKey = k
		}
		// When user chose Ollama, let them pick a model if we have a list; otherwise use Mac-friendly default.
		if c.ModelProvider == "ollama" {
			if len(ollamaModels) > 0 {
				var chosenModel string
				modelOptions := make([]huh.Option[string], 0, len(ollamaModels)+1)
				modelOptions = append(modelOptions, huh.NewOption("Use default (llama3.2:3b — runs well on Mac)", modelprovider.DefaultOllamaModelForSetup))
				for _, m := range ollamaModels {
					modelOptions = append(modelOptions, huh.NewOption(m, m))
				}
				if err := huh.NewForm(
					huh.NewGroup(
						huh.NewSelect[string]().
							Title("Which Ollama model?").
							Description("Default runs well on Mac. Or pick one already on your machine.").
							Options(modelOptions...).
							Value(&chosenModel),
					).Title("Ollama model"),
				).WithTheme(huh.ThemeCharm()).WithAccessible(os.Getenv("ACCESSIBLE") != "").Run(); err != nil {
					return err
				}
				c.ModelName = strings.TrimSpace(chosenModel)
			}
			if c.ModelName == "" {
				c.ModelName = modelprovider.DefaultOllamaModelForSetup
			}
			// Pull the model behind the scenes if not already available.
			alreadyHave := false
			for _, m := range ollamaModels {
				if m == c.ModelName {
					alreadyHave = true
					break
				}
			}
			if !alreadyHave {
				fmt.Fprintln(os.Stderr, "Pulling Ollama model", c.ModelName, "... (this may take a few minutes)")
				pullCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
				defer cancel()
				if err := modelprovider.PullModel(pullCtx, "", c.ModelName); err != nil {
					hint := pullErrorHint(err)
					return fmt.Errorf("pull Ollama model %q: %w%s", c.ModelName, err, hint)
				}
				fmt.Fprintln(os.Stderr, "Model", c.ModelName, "ready.")
			}
		}
	}
	printStepSuccess()

	// Step 2: Platform (messenger)
	var platform string
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("How do you want to talk to your agent?").
				Description("Built-in web = open Genie in your browser.").
				Options(
					huh.NewOption("Built-in web", setup.DefaultPlatform),
					huh.NewOption("Telegram", "telegram"),
					huh.NewOption("Slack", "slack"),
					huh.NewOption("Discord", "discord"),
					huh.NewOption("Microsoft Teams", "teams"),
					huh.NewOption("WhatsApp", "whatsapp"),
					huh.NewOption("Google Chat", "googlechat"),
				).
				Value(&platform),
		).Title("Where to chat"),
	).WithTheme(huh.ThemeCharm()).WithAccessible(os.Getenv("ACCESSIBLE") != "").Run(); err != nil {
		return err
	}
	c.Platform = strings.TrimSpace(platform)
	if c.Platform == "" {
		c.Platform = setup.DefaultPlatform
	}
	if err := runPlatformTokenStep(&c); err != nil {
		return err
	}

	// When using built-in web (AGUI), offer to secure the chat with a password (stored in keyring per agent).
	if c.Platform == setup.DefaultPlatform {
		var aguiPasswordChoice string
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Secure the web chat with a password?").
					Description("Users will need to enter this password to connect from the browser. Stored in system keyring (one per agent).").
					Options(
						huh.NewOption("Yes, require a password to connect", setup.ChoiceYes),
						huh.NewOption("No, don't require a password (local browser only)", setup.ChoiceNo),
					).
					Value(&aguiPasswordChoice),
			).Title("Web chat password"),
		).WithTheme(huh.ThemeCharm()).WithAccessible(os.Getenv("ACCESSIBLE") != "").Run(); err != nil {
			return err
		}
		if strings.TrimSpace(aguiPasswordChoice) == setup.ChoiceYes {
			var aguiPassword, aguiPasswordConfirm string
			if err := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Password for web chat").
						Description("Users must enter this when connecting via the browser.").
						Value(&aguiPassword).
						EchoMode(huh.EchoModePassword).
						Validate(func(s string) error {
							if len(strings.TrimSpace(s)) < 1 {
								return fmt.Errorf("password is required")
							}
							return nil
						}),
					huh.NewInput().
						Title("Confirm password").
						Description("Re-enter the same password to confirm.").
						Value(&aguiPasswordConfirm).
						EchoMode(huh.EchoModePassword).
						Validate(func(s string) error {
							if strings.TrimSpace(s) != strings.TrimSpace(aguiPassword) {
								return fmt.Errorf("passwords do not match")
							}
							return nil
						}),
				).Title("Set password"),
			).WithTheme(huh.ThemeCharm()).WithAccessible(os.Getenv("ACCESSIBLE") != "").Run(); err != nil {
				return err
			}
			pwd := strings.TrimSpace(aguiPassword)
			if pwd != "" && pwd == strings.TrimSpace(aguiPasswordConfirm) {
				if err := keyring.KeyringSet(keyring.AccountAGUIPassword, []byte(pwd)); err != nil {
					return fmt.Errorf("store AGUI password in keyring: %w", err)
				}
				c.AGUIPasswordProtected = true
			}
		}
	}
	printStepSuccess()

	// Learn from your data (no dependency on Google; asked for everyone).
	var learnChoice string
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Learn from your data?").
				Description("Genie will gather data from your Gmail (inbox) and Calendar, then learn from it: sync runs in the background and a knowledge graph (people, projects, relationships) is built after the first sync. Data is summarized and stored locally; you can turn this off anytime in config.").
				Options(
					huh.NewOption("Yes, gather data and learn from it", setup.ChoiceYes),
					huh.NewOption("No, skip for now", setup.ChoiceNo),
				).
				Value(&learnChoice),
		).Title("Learn"),
	).WithTheme(huh.ThemeCharm()).WithAccessible(os.Getenv("ACCESSIBLE") != "").Run(); err != nil {
		return err
	}
	c.Learn = strings.TrimSpace(learnChoice)
	if c.Learn == "" {
		c.Learn = setup.ChoiceNo
	}
	if c.Learn == setup.ChoiceYes {
		var keywordsInput string
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("Keywords to look for (optional)").
					Description("Enter up to 10 keywords, comma-separated (e.g. project name, customer, product). Genie will only index items that mention any of these. Leave empty to index everything.").
					Placeholder("Acme, Q4 roadmap, onboarding").
					Value(&keywordsInput),
			).Title("Search keywords"),
		).WithTheme(huh.ThemeCharm()).WithAccessible(os.Getenv("ACCESSIBLE") != "").Run(); err != nil {
			return err
		}
		c.DataSourcesKeywords = strings.TrimSpace(keywordsInput)
	}

	// Step 3: Google (last step so users set up agent name, AI, messenger, and learn preference first).
	var googleChoice string
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Do you want to connect with Google?").
				Description("Calendar, Contacts, Gmail – browser will open to sign in.").
				Options(
					huh.NewOption("Yes, open browser to sign in", setup.ChoiceYes),
					huh.NewOption("No, skip", setup.ChoiceNo),
				).
				Value(&googleChoice),
		).Title("Connect Google"),
	).WithTheme(huh.ThemeCharm()).WithAccessible(os.Getenv("ACCESSIBLE") != "").Run(); err != nil {
		return err
	}
	c.ManageGoogleServices = strings.TrimSpace(googleChoice)

	var googleFlowRan bool
	if c.ManageGoogleServices == setup.ChoiceYes {
		if oauth.CanRunBrowserFlow() {
			if err := oauth.RunBrowserFlow(context.Background(), oauth.EmbeddedCredentialsJSON()); err != nil {
				return fmt.Errorf("google sign-in: %w", err)
			}
			googleFlowRan = true
		} else {
			fmt.Fprintln(os.Stderr, "Google sign-in skipped: set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET (or build with them) to open the browser here. You can run 'genie grant' after starting Genie to connect later.")
		}
	}
	printStepSuccess()

	absPath, err := setup.ConfigPathFromCollected(&c, defaultPath)
	if err != nil {
		return fmt.Errorf("config path: %w", err)
	}
	inputs := setup.ApplyCollectedToInputs(&c, detectedProvider)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	var vectorOverride *vector.Config
	if modelprovider.OllamaReachable(ctx, "") {
		vectorOverride = &vector.Config{
			EmbeddingProvider:   "ollama",
			OllamaURL:           modelprovider.DefaultOllamaURL,
			OllamaModel:         "nomic-embed-text",
			VectorStoreProvider: "inmemory",
		}
	}
	if err := setup.WriteConfigFile(absPath, inputs, nil, vectorOverride); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	printSetupSuccess(absPath, inputs, googleFlowRan, vectorOverride != nil)
	if err := runGrantWithConfig(ctx, absPath, wd); err != nil {
		return fmt.Errorf("config saved to %s; grant failed: %w (run 'genie grant' or start Genie to connect later)", absPath, err)
	}
	return nil
}

// pullErrorHint returns a short hint to append to pull errors so users know what to do.
func pullErrorHint(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return " Download was interrupted; re-run setup to resume."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return " Download timed out; re-run setup to resume."
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return " Ensure Ollama is running (e.g. run 'ollama serve') and try again."
	}
	if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "no such host") {
		return " Ensure Ollama is running and you have network access; then re-run setup."
	}
	return ""
}

// runGrantWithConfig runs the grant command in-process with the given config path
// and working directory so the user connects their chat immediately after setup.
// It sets the grant command's args explicitly so it does not inherit os.Args from
// "genie setup --log-level ...", which would cause "unknown flag: --log-level"
// because the standalone grant command does not have the root's persistent flags.
func runGrantWithConfig(ctx context.Context, configPath, workingDir string) error {
	rootOpts := &rootCmdOption{
		cfgFile:    configPath,
		workingDir: workingDir,
		logLevel:   "info",
	}
	root := rootCmd{opts: *rootOpts}
	if err := root.init(ctx); err != nil {
		return fmt.Errorf("init: %w", err)
	}
	g := NewGrantCommand(rootOpts)
	grantCobraCmd, err := g.command()
	if err != nil {
		return fmt.Errorf("grant command: %w", err)
	}
	// Grant command has no root parent here, so it must not see os.Args from
	// "genie setup --log-level ...". Pass no args; rootOpts are already set.
	grantCobraCmd.SetArgs([]string{})
	return grantCobraCmd.ExecuteContext(ctx)
}

func runPlatformTokenStep(c *setup.Collected) error {
	switch c.Platform {
	case "telegram":
		var v string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().
				Title("Telegram bot token").
				Description("From @BotFather on Telegram (message /newbot to create one). Stored in system keyring.").
				Value(&v).
				EchoMode(huh.EchoModePassword),
		).Title("Telegram")).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return err
		}
		if s := strings.TrimSpace(v); s != "" {
			c.TelegramToken = s
		}
	case "slack":
		var app, bot string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Slack app token env").Value(&app).Placeholder("SLACK_APP_TOKEN"),
			huh.NewInput().Title("Slack bot token env").Value(&bot).Placeholder("SLACK_BOT_TOKEN"),
		).Title("Slack")).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return err
		}
		if s := strings.TrimSpace(app); s != "" {
			c.SlackAppTokenEnv = s
		} else {
			c.SlackAppTokenEnv = "SLACK_APP_TOKEN"
		}
		if s := strings.TrimSpace(bot); s != "" {
			c.SlackBotTokenEnv = s
		} else {
			c.SlackBotTokenEnv = "SLACK_BOT_TOKEN"
		}
	case "discord":
		var v string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Discord bot token env").Value(&v).Placeholder("DISCORD_BOT_TOKEN"),
		).Title("Discord")).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return err
		}
		if s := strings.TrimSpace(v); s != "" {
			c.DiscordBotTokenEnv = s
		} else {
			c.DiscordBotTokenEnv = "DISCORD_BOT_TOKEN"
		}
	case "teams":
		var appID, appPass string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Teams app ID env").Value(&appID).Placeholder("TEAMS_APP_ID"),
			huh.NewInput().Title("Teams app password env").Value(&appPass).Placeholder("TEAMS_APP_PASSWORD"),
		).Title("Microsoft Teams")).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return err
		}
		if s := strings.TrimSpace(appID); s != "" {
			c.TeamsAppIDEnv = s
		} else {
			c.TeamsAppIDEnv = "TEAMS_APP_ID"
		}
		if s := strings.TrimSpace(appPass); s != "" {
			c.TeamsAppPassEnv = s
		} else {
			c.TeamsAppPassEnv = "TEAMS_APP_PASSWORD"
		}
	case "googlechat":
		var v string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Path to Google key file (optional)").Value(&v),
		).Title("Google Chat")).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return err
		}
		if s := strings.TrimSpace(v); s != "" {
			c.GoogleChatCreds = s
		}
	case "whatsapp":
		var v string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("WhatsApp session path (optional)").Value(&v),
		).Title("WhatsApp")).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return err
		}
	}
	return nil
}

var (
	setupSuccessTitle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#04B575")).MarginBottom(0)
	setupSuccessBody  = lipgloss.NewStyle().Foreground(lipgloss.Color("#8B949E")).MarginLeft(2)
	setupSuccessCTA   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#A371F7")).MarginTop(1).MarginLeft(2)
)

func printStepSuccess() {
	fmt.Println()
	fmt.Println(setupSuccessTitle.Render("✓ Success"))
	fmt.Println()
}

func printSetupSuccess(configPath string, inputs setup.WizardInputs, googleFlowRan, usedOllamaForVector bool) {
	fmt.Println()
	fmt.Println(setupSuccessTitle.Render("✨ You're all set!"))
	fmt.Println(setupSuccessBody.Render("Config saved to: " + configPath))
	if inputs.ModelProviderTokenLiteral != "" {
		fmt.Println(setupSuccessBody.Render("Your API key is stored in the system keyring (keyring://genie/...)."))
	}
	if usedOllamaForVector {
		fmt.Println(setupSuccessBody.Render("Vector memory will use your local Ollama (nomic-embed-text) for embeddings."))
	}
	if inputs.ManageGoogleServices && !googleFlowRan {
		fmt.Println(setupSuccessBody.Render("To connect Google (Calendar, Contacts, Gmail): sign in when Genie prompts you."))
	}
	if inputs.Learn {
		fmt.Println(setupSuccessBody.Render("Learn is on: Genie will gather data from Gmail and Calendar, then learn from it and build a knowledge graph after the first sync."))
	}
	fmt.Println(setupSuccessCTA.Render("Starting Genie..."))
	fmt.Println()
}
