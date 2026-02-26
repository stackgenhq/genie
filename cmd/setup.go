/*
Copyright © 2026 StackGen, Inc.
*/
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/stackgenhq/genie/pkg/osutils"
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

	collected := map[string]string{
		"config_path":  defaultPath,
		"skills_roots": "./skills",
	}
	detectedProvider := setup.DetectAIKeyProvider()
	if detectedProvider != "" {
		collected["model_provider"] = detectedProvider
	}

	// Step 1: LLM key (skip entirely if key already in env)
	if detectedProvider == "" {
		var modelProvider, apiKey string
		modelProvider = "openai"
		if err := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Which AI do you use?").
					Options(
						huh.NewOption("OpenAI (ChatGPT)", "openai"),
						huh.NewOption("Google (Gemini)", "gemini"),
						huh.NewOption("Anthropic (Claude)", "anthropic"),
					).
					Value(&modelProvider),
				huh.NewInput().
					Title("Paste your API key").
					Description("Get one: OpenAI → platform.openai.com | Google → aistudio.google.com | Anthropic → console.anthropic.com").
					Value(&apiKey).
					EchoMode(huh.EchoModePassword).
					Validate(func(s string) error {
						if strings.TrimSpace(s) == "" {
							return fmt.Errorf("required")
						}
						return nil
					}),
			).Title("Your AI key").Description("We need an API key so Genie can talk to your AI."),
		).WithTheme(huh.ThemeCharm()).WithAccessible(os.Getenv("ACCESSIBLE") != "").Run(); err != nil {
			return err
		}
		collected["model_provider"] = strings.TrimSpace(modelProvider)
		if k := strings.TrimSpace(apiKey); k != "" {
			collected["api_key"] = k
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
					huh.NewOption("Built-in web", "agui"),
					huh.NewOption("Telegram", "telegram"),
					huh.NewOption("Slack", "slack"),
					huh.NewOption("Discord", "discord"),
					huh.NewOption("Microsoft Teams", "teams"),
					huh.NewOption("Google Chat", "googlechat"),
					huh.NewOption("WhatsApp", "whatsapp"),
				).
				Value(&platform),
		).Title("Where to chat"),
	).WithTheme(huh.ThemeCharm()).WithAccessible(os.Getenv("ACCESSIBLE") != "").Run(); err != nil {
		return err
	}
	collected["platform"] = strings.TrimSpace(platform)
	if collected["platform"] == "" {
		collected["platform"] = "agui"
	}
	// Platform-specific token env (minimal: just one field for telegram/slack/discord/teams)
	if err := runPlatformTokenStep(collected); err != nil {
		return err
	}
	printStepSuccess()

	// Step 3: Google
	var googleChoice string
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Do you want to connect with Google?").
				Description("Calendar, Contacts, Gmail – browser will open to sign in.").
				Options(
					huh.NewOption("Yes, open browser to sign in", "yes"),
					huh.NewOption("No, skip", "no"),
				).
				Value(&googleChoice),
		).Title("Connect Google"),
	).WithTheme(huh.ThemeCharm()).WithAccessible(os.Getenv("ACCESSIBLE") != "").Run(); err != nil {
		return err
	}
	collected["manage_google_services"] = strings.TrimSpace(googleChoice)
	var googleFlowRan bool
	if collected["manage_google_services"] == "yes" {
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

	absPath, err := setup.ConfigPathFromCollected(collected, defaultPath)
	if err != nil {
		return fmt.Errorf("config path: %w", err)
	}
	inputs := setup.ApplyCollectedToInputs(collected, detectedProvider)
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	if err := setup.WriteConfigFile(absPath, inputs, nil); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	printSetupSuccess(absPath, inputs, googleFlowRan)
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	return runGrantWithConfig(ctx, absPath, wd)
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

func runPlatformTokenStep(collected map[string]string) error {
	platform := collected["platform"]
	switch platform {
	case "telegram":
		var v string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Telegram bot token env var").Description("e.g. TELEGRAM_BOT_TOKEN").Value(&v).Placeholder("TELEGRAM_BOT_TOKEN"),
		).Title("Telegram")).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return err
		}
		if s := strings.TrimSpace(v); s != "" {
			collected["telegram_token_env"] = s
		} else {
			collected["telegram_token_env"] = "TELEGRAM_BOT_TOKEN"
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
			collected["slack_app_token_env"] = s
		} else {
			collected["slack_app_token_env"] = "SLACK_APP_TOKEN"
		}
		if s := strings.TrimSpace(bot); s != "" {
			collected["slack_bot_token_env"] = s
		} else {
			collected["slack_bot_token_env"] = "SLACK_BOT_TOKEN"
		}
	case "discord":
		var v string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Discord bot token env").Value(&v).Placeholder("DISCORD_BOT_TOKEN"),
		).Title("Discord")).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return err
		}
		if s := strings.TrimSpace(v); s != "" {
			collected["discord_bot_token_env"] = s
		} else {
			collected["discord_bot_token_env"] = "DISCORD_BOT_TOKEN"
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
			collected["teams_app_id_env"] = s
		} else {
			collected["teams_app_id_env"] = "TEAMS_APP_ID"
		}
		if s := strings.TrimSpace(appPass); s != "" {
			collected["teams_app_pass_env"] = s
		} else {
			collected["teams_app_pass_env"] = "TEAMS_APP_PASSWORD"
		}
	case "googlechat":
		var v string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Path to Google key file (optional)").Value(&v),
		).Title("Google Chat")).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return err
		}
		if s := strings.TrimSpace(v); s != "" {
			collected["google_chat_creds"] = s
		}
	case "whatsapp":
		var v string
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("WhatsApp session path (optional)").Value(&v),
		).Title("WhatsApp")).WithTheme(huh.ThemeCharm()).Run(); err != nil {
			return err
		}
		if s := strings.TrimSpace(v); s != "" {
			collected["whatsapp_store_path"] = s
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

func printSetupSuccess(configPath string, inputs setup.WizardInputs, googleFlowRan bool) {
	fmt.Println()
	fmt.Println(setupSuccessTitle.Render("✨ You're all set!"))
	fmt.Println(setupSuccessBody.Render("Config saved to: " + configPath))
	if inputs.ModelProviderTokenLiteral != "" {
		secretsDir := filepath.Join(filepath.Dir(configPath), "secrets")
		fmt.Println(setupSuccessBody.Render("Your API key is stored in: " + secretsDir))
	}
	if inputs.ManageGoogleServices && !googleFlowRan {
		fmt.Println(setupSuccessBody.Render("To connect Google (Calendar, Contacts, Gmail): sign in when Genie prompts you."))
	}
	fmt.Println(setupSuccessCTA.Render("Starting Genie..."))
	fmt.Println()
}
