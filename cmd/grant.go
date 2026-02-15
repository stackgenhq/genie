/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/audit"
	"github.com/appcd-dev/genie/pkg/browser"
	"github.com/appcd-dev/genie/pkg/codeowner"
	"github.com/appcd-dev/genie/pkg/config"
	geniedb "github.com/appcd-dev/genie/pkg/db"
	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/mcp"
	"github.com/appcd-dev/genie/pkg/memory/vector"
	"github.com/appcd-dev/genie/pkg/messenger"
	messengerhitl "github.com/appcd-dev/genie/pkg/messenger/hitl"
	"github.com/appcd-dev/genie/pkg/skills"
	"github.com/appcd-dev/genie/pkg/tools/email"
	"github.com/appcd-dev/genie/pkg/tools/pm"
	"github.com/appcd-dev/genie/pkg/tools/scm"
	"github.com/appcd-dev/genie/pkg/tools/websearch"
	"github.com/appcd-dev/go-lib/logger"
	"github.com/appcd-dev/go-lib/osutils"
	"github.com/aquasecurity/trivy/pkg/log"
	"github.com/spf13/cobra"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	// Register messenger adapter factories via init().
	_ "github.com/appcd-dev/genie/pkg/messenger/googlechat"
	_ "github.com/appcd-dev/genie/pkg/messenger/slack"
	_ "github.com/appcd-dev/genie/pkg/messenger/teams"
	_ "github.com/appcd-dev/genie/pkg/messenger/telegram"
	_ "github.com/appcd-dev/genie/pkg/messenger/whatsapp"
)

type grantCmdOption struct {
	// AuditLogPath
	AuditLogPath string
}

type grantCmd struct {
	opts          grantCmdOption
	rootOpts      *rootCmdOption
	codeOwner     codeowner.CodeOwner
	mcpClient     *mcp.Client         // held for cleanup
	msgr          messenger.Messenger // held for chat loop receive
	browser       *browser.Browser    // factory for creating isolated tabs
	notifierStore *messengerhitl.NotifierStore
}

func NewGrantCommand(rootOpts *rootCmdOption) *grantCmd {
	return &grantCmd{
		rootOpts: rootOpts,
	}
}

func (g *grantCmd) command() (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:   "grant",
		Short: "Interactive Intent-to-Infrastructure wizard",
		Long: `Grant your infrastructure wish! This command launches an interactive wizard
that helps you describe your application requirements and generates production-ready
infrastructure code using Stackgen's agentic intelligence.

Examples:
  genie grant`,
		RunE: g.run,
	}
	cwd, err := filepath.Abs(osutils.Getwd())
	if err != nil {
		return nil, fmt.Errorf("error getting the current working directory: %w", err)
	}

	// Bind flags to the grant command options
	cmd.Flags().StringVar(&g.rootOpts.workingDir, "working-dir", cwd, "working directory")
	cmd.Flags().StringVar(&g.opts.AuditLogPath, "audit-log-path", "", "path to the audit log file")

	return cmd, nil
}

// run executes the grant command logic — starts the AG-UI HTTP/SSE server
// as the primary chat interface and prints server logs to stdout.
func (g *grantCmd) run(cmd *cobra.Command, args []string) error {
	// Wire up signal-based cancellation so Ctrl+C shuts down gracefully.
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	genieCfg, err := g.rootOpts.genieCfg()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	logger := logger.GetLogger(ctx).With("fn", "grantCmd.run")

	// Set up messenger (best-effort, non-fatal).
	g.msgr, err = genieCfg.Messenger.InitMessenger(ctx)
	if err != nil {
		logger.Warn("failed to initialize messenger, continuing without", "error", err)
	} else if g.msgr != nil {
		defer func() {
			if err := g.msgr.Disconnect(context.Background()); err != nil {
				logger.Warn("failed to disconnect messenger", "error", err)
			}
		}()
	}

	modelProvider := genieCfg.ModelConfig.NewEnvBasedModelProvider()

	tools, closeDeps := g.initToolDeps(ctx, genieCfg)
	if closeDeps != nil {
		defer closeDeps()
	}

	// Vector memory
	var store *vector.Store
	if genieCfg.VectorMemory.CanInitialize() {
		store, err = genieCfg.VectorMemory.NewStore(ctx)
		if err != nil {
			log.Warn("failed to initialize vector store, skipping memory tools", "error", err)
		}
	}

	// Initialize audit logger (NoopAuditor when --audit-log-path is not set).
	var auditor audit.Auditor = &audit.NoopAuditor{}
	if g.opts.AuditLogPath != "" {
		fileAuditor, cleanupAudit, auditErr := g.initAuditor()
		if auditErr != nil {
			return fmt.Errorf("audit init: %w", auditErr)
		}
		defer cleanupAudit()
		auditor = fileAuditor
	}

	// Open central database (GORM + SQLite) for persistent features.
	// Must be initialized before NewCodeOwner so the approval store is
	// available for sub-agent HITL gating via create_agent.
	dbPath, err := geniedb.DefaultPath()
	if err != nil {
		return fmt.Errorf("database path: %w", err)
	}
	gormDB, err := geniedb.Open(dbPath)
	if err != nil {
		return fmt.Errorf("database open: %w", err)
	}
	defer func() {
		if err := geniedb.Close(gormDB); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to close database: %v\n", err)
		}
	}()
	if err := geniedb.AutoMigrate(gormDB); err != nil {
		return fmt.Errorf("database migrate: %w", err)
	}
	logger.Info("Central database opened", "path", dbPath)

	// HITL approval store for human-in-the-loop tool approval.
	var approvalStore hitl.ApprovalStore = hitl.NewStore(gormDB)
	if g.msgr != nil {
		g.notifierStore = messengerhitl.NewNotifierStore(approvalStore, g.msgr)
		approvalStore = g.notifierStore
	}

	if g.msgr != nil {
		tools = append(tools, messenger.NewSendMessageTool(g.msgr))
	}

	g.codeOwner, err = codeowner.NewCodeOwner(ctx, modelProvider, g.rootOpts.workingDir, tools, store, auditor, approvalStore)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := g.codeOwner.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to close code owner: %v\n", closeErr)
		}
	}()

	// Build AG-UI chat handler.
	httpHandler := agui.NewChatHandlerFromCodeOwner(func(ctx context.Context, message string, agentsMessage chan<- interface{}) error {
		outputChan := make(chan string)
		chatDone := make(chan struct{})
		go func() {
			defer close(chatDone)
			defer close(outputChan)

			// Create an isolated browser tab for this HTTP session.
			var tabCtx context.Context
			if g.browser != nil {
				var cancel context.CancelFunc
				var err error
				tabCtx, cancel, err = g.browser.NewTab(ctx)
				if err != nil {
					agui.EmitError(agentsMessage, err, "failed to create browser tab")
					return
				}
				defer cancel()
			}

			if err := g.codeOwner.Chat(ctx, codeowner.CodeQuestion{
				Question:      message,
				OutputDir:     g.rootOpts.workingDir,
				EventChan:     agentsMessage,
				SenderContext: "agui:http",
				BrowserTab:    tabCtx,
			}, outputChan); err != nil {
				agui.EmitError(agentsMessage, err, "while processing AG-UI chat message")
			}
		}()
		// Drain the output channel (agent responses).
		for range outputChan {
		}
		// Wait for the chat goroutine to finish so it stops writing
		// to eventChan before NewChatHandlerFromCodeOwner closes it.
		<-chatDone
		return nil
	})

	// Start listening for incoming messenger messages (Slack, Teams, etc.).
	if g.msgr != nil {
		eventChan := make(chan interface{}, 100)
		messengerCh := messenger.ReceiveWithReconnect(ctx, g.msgr, 1*time.Second, 30*time.Second)
		go func() {
			for {
				select {
				case msg, ok := <-messengerCh:
					if !ok {
						return
					}
					go g.handleMessengerInput(ctx, msg, eventChan)
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Print startup banner.
	fmt.Fprintf(os.Stderr, "\n🧞 Genie AG-UI server starting on :%d\n", genieCfg.AGUI.Port)
	fmt.Fprintf(os.Stderr, "   Working directory: %s\n", g.rootOpts.workingDir)
	fmt.Fprintf(os.Stderr, "   Database:          %s\n", dbPath)
	fmt.Fprintf(os.Stderr, "   Health check:      http://localhost:%d/health\n\n", genieCfg.AGUI.Port)

	// Start AG-UI HTTP/SSE server — blocks until context is cancelled.
	aguiServer := genieCfg.AGUI.NewServer(httpHandler, approvalStore)
	return aguiServer.Start(ctx)
}

// initToolDeps builds the codeowner.ToolDeps from GenieConfig.
// Each dependency is best-effort — failures are logged as warnings, not fatal.
// Returns a cleanup func that closes resources (e.g., MCP client).
func (g *grantCmd) initToolDeps(ctx context.Context, cfg config.GenieConfig) ([]tool.Tool, func()) {
	log := logger.GetLogger(ctx).With("fn", "grantcmd.initToolDeps")

	// WebSearch — always available (falls back to DuckDuckGo if no Google API key)
	var tools []tool.Tool
	tools = append(tools, websearch.NewTool(cfg.WebSearch))

	// MCP tools
	mcpClient, err := mcp.NewClient(ctx, cfg.MCP)
	if err != nil {
		log.Warn("failed to initialize MCP client, skipping MCP tools", "error", err)
	} else {
		g.mcpClient = mcpClient
		tools = append(tools, mcpClient.GetTools()...)
	}

	// Skills
	if len(cfg.SkillsRoots) != 0 {
		repo, err := skill.NewFSRepository(cfg.SkillsRoots...)
		if err != nil {
			log.Warn("failed to initialize skills repository, skipping skill tools", "error", err)
		} else {
			executor := skills.NewLocalExecutor(g.rootOpts.workingDir)
			tools = append(tools, skills.CreateAllSkillTools(repo, executor)...)
		}
	}

	// Browser tools — best-effort, non-fatal.
	bw, err := browser.New(
		browser.WithHeadless(true),
		browser.WithBlockedDomains(cfg.Browser.BlockedDomains),
	)
	if err != nil {
		log.Warn("failed to start browser, skipping browser tools", "error", err)
	} else {
		g.browser = bw
		for _, bt := range browser.AllTools(bw) {
			tools = append(tools, bt)
		}
	}

	// SCM tools
	if scmSvc, err := scm.New(cfg.SCM); err == nil {
		tools = append(tools, scm.AllTools(scmSvc)...)
	} else {
		log.Warn("failed to init SCM tools", "error", err)
	}

	// PM tools
	if pmSvc, err := pm.New(cfg.PM); err == nil {
		tools = append(tools, pm.AllTools(pmSvc)...)
	} else {
		log.Warn("failed to init PM tools", "error", err)
	}

	// Email tools
	if emailSvc, err := cfg.Email.New(); err == nil {
		tools = append(tools, email.AllTools(emailSvc)...)
	} else {
		log.Warn("failed to init Email tools", "error", err)
	}

	cleanup := func() {
		if g.mcpClient != nil {
			g.mcpClient.Close()
		}
		if bw != nil {
			bw.Close()
		}
	}
	return tools, cleanup
}

// initAuditor creates the audit logger when a path is configured.
// Returns a nil auditor and nil cleanup func when auditing is disabled.
func (g *grantCmd) initAuditor() (audit.Auditor, func(), error) {
	auditor, err := audit.NewFileAuditor(g.opts.AuditLogPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize audit logger: %w", err)
	}

	cleanup := func() { _ = auditor.Close() }
	return auditor, cleanup, nil
}

// handleMessengerInput processes an incoming message from a remote messaging
// platform (Slack, Teams, Telegram, Google Chat, Discord). The response is
// sent back to the same channel/thread via messenger.Send().
// This method is called in a goroutine for concurrent handling.
func (g *grantCmd) handleMessengerInput(ctx context.Context, msg messenger.IncomingMessage, eventChan chan<- interface{}) {
	if msg.Content.Text == "" {
		return
	}

	logger := logger.GetLogger(ctx).With("fn", "grantCmd.handleMessengerInput")

	senderCtx := msg.String()

	// 1. Check for interactive HITL reply (Yes/No)
	if g.notifierStore != nil {
		normalized := strings.TrimSpace(strings.ToLower(msg.Content.Text))
		isApproval := normalized == "yes" || normalized == "y"
		isRejection := normalized == "no" || normalized == "n"

		if isApproval || isRejection {
			if approvalID, found := g.notifierStore.GetPending(senderCtx); found {
				status := hitl.StatusApproved
				replyText := "✅ **Approved**"
				if isRejection {
					status = hitl.StatusRejected
					replyText = "❌ **Rejected**"
				}

				// Resolve the pending approval
				err := g.notifierStore.Resolve(ctx, hitl.ResolveRequest{
					ApprovalID: approvalID,
					Decision:   status,
					ResolvedBy: msg.Sender.DisplayName,
				})

				if err != nil {
					logger.Error("failed to resolve approval via messenger", "error", err)
					replyText = fmt.Sprintf("⚠️ Failed to resolve: %s", err)
				} else {
					// Cleanup pending map on success
					g.notifierStore.RemovePending(senderCtx)
				}

				// Reply to user
				_, _ = g.msgr.Send(ctx, messenger.SendRequest{
					Channel:  msg.Channel,
					ThreadID: msg.ThreadID,
					Content:  messenger.MessageContent{Text: replyText},
				})
				return
			}
		}
	}

	// Emit attributed user bubble so the TUI shows the sender and platform (#6).
	senderLabel := fmt.Sprintf("%s (%s)", msg.Sender.DisplayName, msg.Platform)
	agui.EmitAgentMessage(eventChan, senderLabel, msg.Content.Text)

	logger.Info("received messenger message",
		"platform", msg.Platform,
		"sender", msg.Sender.DisplayName,
		"channel", msg.Channel.ID,
		"senderContext", senderCtx,
	)

	agentThoughts := make(chan string)
	go func() {
		defer close(agentThoughts)

		// Create an isolated browser tab for this chat session (if browser is enabled)
		var tabCtx context.Context
		if g.browser != nil {
			var cancel context.CancelFunc
			var err error
			tabCtx, cancel, err = g.browser.NewTab(ctx)
			if err != nil {
				agui.EmitError(eventChan, err, "failed to create browser tab")
				return
			}
			defer cancel()
		}

		if err := g.codeOwner.Chat(ctx, codeowner.CodeQuestion{
			Question:      msg.Content.Text,
			OutputDir:     g.rootOpts.workingDir,
			EventChan:     eventChan,
			SenderContext: senderCtx,
			ExcludeTools:  []string{"send_message"},
			BrowserTab:    tabCtx,
		}, agentThoughts); err != nil {
			agui.EmitError(eventChan, err, "while processing messenger message")
		}
	}()

	for response := range agentThoughts {
		// Echo response to the TUI as well.
		agui.EmitAgentMessage(eventChan, "Genie", response)

		// Reply to the originating messenger channel/thread.
		if _, err := g.msgr.Send(ctx, messenger.SendRequest{
			Channel:  msg.Channel,
			ThreadID: msg.ThreadID,
			Content:  messenger.MessageContent{Text: response},
		}); err != nil {
			logger.Warn("failed to reply on messenger", "error", err)
		}
	}
}
