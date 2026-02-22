/*
Copyright © 2026 StackGen, Inc.
*/

// Package app provides the Application service layer that owns the full
// Genie lifecycle: config → bootstrap → start → close. It is the single
// place where all dependencies (DB, tools, codeowner, messenger, cron,
// background worker, HTTP server) are wired together.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/audit"
	"github.com/appcd-dev/genie/pkg/browser"
	"github.com/appcd-dev/genie/pkg/clarify"
	"github.com/appcd-dev/genie/pkg/codeowner"
	"github.com/appcd-dev/genie/pkg/config"
	"github.com/appcd-dev/genie/pkg/cron"
	geniedb "github.com/appcd-dev/genie/pkg/db"
	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/mcp"
	"github.com/appcd-dev/genie/pkg/memory/vector"
	"github.com/appcd-dev/genie/pkg/messenger"
	messengeragui "github.com/appcd-dev/genie/pkg/messenger/agui" // register adapter + server types
	_ "github.com/appcd-dev/genie/pkg/messenger/discord"          // register adapter
	_ "github.com/appcd-dev/genie/pkg/messenger/googlechat"       // register adapter
	messengerhitl "github.com/appcd-dev/genie/pkg/messenger/hitl"
	"github.com/appcd-dev/genie/pkg/messenger/media"
	_ "github.com/appcd-dev/genie/pkg/messenger/slack"    // register adapter
	_ "github.com/appcd-dev/genie/pkg/messenger/teams"    // register adapter
	_ "github.com/appcd-dev/genie/pkg/messenger/telegram" // register adapter
	_ "github.com/appcd-dev/genie/pkg/messenger/whatsapp" // register adapter
	"github.com/appcd-dev/genie/pkg/runbook"
	"github.com/appcd-dev/genie/pkg/skills"
	"github.com/appcd-dev/genie/pkg/tools"
	"github.com/appcd-dev/genie/pkg/tools/atlassian"
	"github.com/appcd-dev/genie/pkg/tools/bigquery"
	"github.com/appcd-dev/genie/pkg/tools/email"
	"github.com/appcd-dev/genie/pkg/tools/gdrive"
	"github.com/appcd-dev/genie/pkg/tools/hubspot"
	"github.com/appcd-dev/genie/pkg/tools/networking"
	"github.com/appcd-dev/genie/pkg/tools/pm"
	"github.com/appcd-dev/genie/pkg/tools/salesforce"
	"github.com/appcd-dev/genie/pkg/tools/scm"
	"github.com/appcd-dev/genie/pkg/tools/slacktools"
	"github.com/appcd-dev/genie/pkg/tools/snowflake"

	"github.com/appcd-dev/genie/pkg/tools/websearch"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
	"trpc.group/trpc-go/trpc-agent-go/skill"
)

// Params holds the inputs required to create an Application.
type Params struct {
	Config     config.GenieConfig
	WorkingDir string
	AuditPath  string
	Version    string
}

// Application orchestrates the Genie lifecycle. Create one with
// NewApplication, call Bootstrap to initialise all dependencies, then Start
// to run the HTTP server. Call Close to release resources.
type Application struct {
	cfg        config.GenieConfig
	workingDir string
	auditPath  string
	version    string

	// --- populated during Bootstrap ---
	db            *gorm.DB
	codeOwner     codeowner.CodeOwner
	approvalStore hitl.ApprovalStore
	cronStore     cron.ICronStore
	msgr          messenger.Messenger
	notifierStore *messengerhitl.NotifierStore
	browser       *browser.Browser
	mcpClient     *mcp.Client
	auditor       audit.Auditor

	// pendingReplays holds approvals recovered during Bootstrap that can be
	// replayed once the chat handler is available in Start().
	pendingReplays []hitl.ReplayableApproval

	// replayWG tracks outstanding replayOnApproval goroutines for graceful drain.
	replayWG sync.WaitGroup

	// clarifyStore is shared between the ask_clarifying_question tool and
	// the AG-UI server's /api/v1/clarify endpoint.
	clarifyStore clarify.Store

	// shortMemory is the DB-backed, TTL-bounded generic key-value store.
	// Used for message dedup, cooldowns, pending clarifications, and
	// reaction ledger correlation. Replaces ad-hoc in-memory TTL maps.
	shortMemory *geniedb.ShortMemoryStore
}

// NewApplication creates a new Application with validated parameters.
// No side-effects — heavy initialisation happens in Bootstrap.
func NewApplication(p Params) (*Application, error) {
	if p.WorkingDir == "" {
		return nil, fmt.Errorf("working directory is required")
	}
	if p.AuditPath == "" {
		p.AuditPath = filepath.Join(p.WorkingDir, "genie_audit.ndjson")
	}
	return &Application{
		cfg:        p.Config,
		workingDir: p.WorkingDir,
		auditPath:  p.AuditPath,
		version:    p.Version,
	}, nil
}

// Bootstrap initialises all dependencies: database, tools, vector memory,
// audit logger, HITL approval store, cron store, messenger, and the
// CodeOwner agent. Call exactly once after NewApplication.
func (a *Application) Bootstrap(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("fn", "app.Bootstrap")

	// --- Database ---
	var err error
	a.db, err = geniedb.Open(a.cfg.DBConfig.DBFile)
	if err != nil {
		return fmt.Errorf("database open: %w", err)
	}
	if err := geniedb.AutoMigrate(a.db); err != nil {
		return fmt.Errorf("database migrate: %w", err)
	}
	log.Info("Central database opened", "path", a.cfg.DBConfig.DBFile)

	// --- Messenger ---
	// Try to initialize the configured external messenger (Slack, Discord, etc.).
	// When none is configured, InitMessenger defaults to AGUI so a.msgr
	// is never nil.
	a.msgr, err = a.cfg.Messenger.InitMessenger(ctx)
	if err != nil {
		return fmt.Errorf("messenger init: %w", err)
	}
	if a.msgr == nil {
		return fmt.Errorf("messenger not initialized: configure one in .genie.toml")
	}
	a.msgr = messenger.WithLogging(ctx, a.msgr)
	log.Info("Messenger initialized", "platform", a.msgr.Platform())

	// --- Clarify store (DB-backed, survives restarts) ---
	a.clarifyStore = clarify.NewStore(a.db)

	// Recover clarifications orphaned by a previous server instance.
	if res, err := a.clarifyStore.RecoverPending(ctx, 10*time.Minute); err != nil {
		log.Warn("failed to recover pending clarifications", "error", err)
	} else if res.Expired+res.Recovered > 0 {
		log.Info("Recovered pending clarifications from previous run",
			"expired", res.Expired, "recovered", res.Recovered)
	}

	// --- Short memory store (generic TTL k/v) ---
	a.shortMemory = geniedb.NewShortMemoryStore(a.db)

	// --- Vector memory ---
	vectorStore, err := a.cfg.VectorMemory.NewStore(ctx)
	if err != nil {
		log.Warn("failed to initialize vector store, skipping memory tools", "error", err)
	} else {
		log.Info("Vector memory initialized")
	}

	// --- Audit ---
	a.auditor, err = audit.NewFileAuditor(a.auditPath)
	if err != nil {
		return fmt.Errorf("audit init: %w", err)
	}
	log.Info("Audit logger initialized", "path", a.auditPath)

	// --- HITL ---
	a.approvalStore = a.cfg.HITL.NewStore(a.db)
	a.notifierStore = messengerhitl.NewNotifierStore(a.approvalStore, a.msgr)
	a.approvalStore = a.notifierStore

	// Recover approvals orphaned by a previous server instance.
	// Approvals older than 10 min are marked "expired"; recent ones get
	// waiter channels re-registered so they can still be resolved.
	if res, err := a.approvalStore.RecoverPending(ctx, 10*time.Minute); err != nil {
		log.Warn("failed to recover pending approvals", "error", err)
	} else {
		if res.Expired+res.Recovered > 0 {
			log.Info("Recovered pending HITL approvals from previous run",
				"expired", res.Expired, "recovered", res.Recovered,
				"replayable", len(res.Replayable))
		}
		a.pendingReplays = res.Replayable
	}

	// --- Memory service ---
	memorySvc := geniedb.NewMemoryService(a.db)

	// --- Cron store ---
	a.cronStore = cron.NewStore(a.db)

	// --- Tool Registry (centralized tool creation + denied-tool filtering) ---
	registry := a.initToolRegistry(ctx, vectorStore)

	// --- Session store (persistent conversation history) ---
	sessionStore := geniedb.NewSessionStore(a.db)
	log.Info("GORM-backed session store initialized for persistent chat history")

	// --- CodeOwner ---
	a.codeOwner, err = codeowner.NewCodeOwner(
		ctx,
		a.cfg.ModelConfig.NewEnvBasedModelProvider(),
		a.workingDir,
		registry,
		vectorStore,
		a.auditor,
		a.approvalStore,
		memorySvc,
		a.cfg.Runbook,
		sessionStore,
	)
	if err != nil {
		return fmt.Errorf("codeowner init: %w", err)
	}
	log.Info("CodeOwner agent initialized")

	return nil
}

// Start creates the BackgroundWorker, wires the cron dispatcher through it,
// creates the AG-UI HTTP server, starts the cron scheduler, sets up the
// messenger receive loop, and blocks until the context is cancelled.
func (a *Application) Start(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("fn", "app.Start")

	// --- Chat handler (platform-agnostic) ---
	chatHandler := a.buildChatHandler()

	// The BackgroundWorker needs an Expert (Handle + Resume). For all
	// platforms we use a lightweight wrapper that just calls codeOwner.
	bgExpert := messengeragui.NewChatHandlerFromCodeOwner(a.codeOwner.Resume, chatHandler)
	bgWorker := messengeragui.NewBackgroundWorker(bgExpert, runtime.NumCPU())

	// Spawn replay goroutines for recoverable pending approvals.
	for _, ra := range a.pendingReplays {
		ra := ra
		a.replayWG.Add(1)
		go func() {
			defer a.replayWG.Done()
			a.replayOnApproval(ctx, ra, bgWorker)
		}()
	}
	a.pendingReplays = nil

	// --- Cron scheduler (platform-agnostic) ---
	dispatcher := func(ctx context.Context, req agui.EventRequest) (string, error) {
		req.Type = agui.EventTypeWebhook
		return bgWorker.HandleEvent(ctx, req)
	}
	cronScheduler := a.cfg.Cron.NewScheduler(a.cronStore, dispatcher)

	if err := cronScheduler.Start(ctx); err != nil {
		log.Warn("Failed to start cron scheduler", "error", err)
	}

	// --- AGUI server wiring (only when AGUI is the platform) ---
	aguiCfg := a.cfg.Messenger.AGUI

	if aguiMsgr := unwrapAGUI(a.msgr); aguiMsgr != nil {
		aguiMsgr.ConfigureServer(messengeragui.ServerConfig{
			AGUIConfig:    aguiCfg,
			ChatHandler:   bgExpert,
			ApprovalStore: a.approvalStore,
			ClarifyStore:  a.clarifyStore,
			BGWorker:      bgWorker,
			Workers:       []agui.BGWorker{cronScheduler},
			ChatFunc:      chatHandler,
		})
		log.Info("AG-UI server configured on AGUI messenger")
	}

	// Connect the messenger — for AGUI this starts the HTTP server,
	// for external platforms this opens the connection to the platform API.
	if err := a.msgr.Connect(ctx); err != nil {
		return fmt.Errorf("messenger connect: %w", err)
	}

	// --- Messenger receive loop (external messengers only) ---
	if a.msgr.Platform() != messenger.PlatformAGUI {
		a.startMessengerLoop(ctx)
	}

	// --- Startup banner ---
	fmt.Fprintf(os.Stderr, "\n🧞 Genie starting\n")
	fmt.Fprintf(os.Stderr, "   Working directory: %s\n", a.workingDir)
	fmt.Fprintf(os.Stderr, "   Database:          %s\n", a.cfg.DBConfig.DBFile)
	fmt.Fprintf(os.Stderr, "   Audit log:         %s\n", a.auditPath)
	fmt.Fprintf(os.Stderr, "   Messenger:         %s\n", a.msgr.Platform())
	if a.msgr.Platform() == messenger.PlatformAGUI {
		fmt.Fprintf(os.Stderr, "   Health check:      http://localhost:%d/health\n", aguiCfg.Port)
	}
	fmt.Fprintf(os.Stderr, "   Connection Info:   %s\n\n", a.msgr.ConnectionInfo())

	log.Info("Starting Genie", "version", a.version)

	<-ctx.Done()

	// Graceful shutdown: stop cron, wait for background tasks.
	if err := cronScheduler.Stop(); err != nil {
		log.Warn("Failed to stop cron scheduler", "error", err)
	}
	bgWorker.WaitForCompletion()

	return nil
}

// unwrapAGUI extracts the *messengeragui.Messenger from the given messenger,
// unwrapping LoggingMessenger if necessary. Returns nil if not AGUI.
func unwrapAGUI(m messenger.Messenger) *messengeragui.Messenger {
	if m.Platform() != messenger.PlatformAGUI {
		return nil
	}
	if am, ok := m.(*messengeragui.Messenger); ok {
		return am
	}
	if lm, ok := m.(*messenger.LoggingMessenger); ok {
		if am, ok := lm.Unwrap().(*messengeragui.Messenger); ok {
			return am
		}
	}
	return nil
}

// Close releases all resources acquired during Bootstrap. Safe to call
// even if Bootstrap was only partially successful.
func (a *Application) Close(ctx context.Context) {
	logger := logger.GetLogger(ctx).With("fn", "app.Close")
	// Drain outstanding replay goroutines before closing resources.
	logger.Info("Waiting for replay goroutines to finish")
	a.replayWG.Wait()

	if a.auditor != nil {
		if err := a.auditor.Close(); err != nil {
			logger.Warn("failed to close auditor", "error", err)
		}
	}
	if a.codeOwner != nil {
		if err := a.codeOwner.Close(); err != nil {
			logger.Warn("failed to close code owner", "error", err)
		}
	}
	if a.mcpClient != nil {
		a.mcpClient.Close(context.Background())
	}
	if a.browser != nil {
		a.browser.Close()
	}
	if a.msgr != nil {
		if err := a.msgr.Disconnect(context.Background()); err != nil {
			logger.Warn("failed to disconnect messenger", "error", err)
		}
	}
	if a.db != nil {
		if err := geniedb.Close(a.db); err != nil {
			logger.Warn("failed to close database", "error", err)
		}
	}
}

// buildChatHandler returns a function matching the shape expected by
// agui.NewChatHandlerFromCodeOwner. It bridges the AG-UI server to the
// underlying codeOwner.Chat() pipeline.
func (a *Application) buildChatHandler() func(ctx context.Context, message string, agentsMessage chan<- interface{}) error {
	return func(ctx context.Context, message string, agentsMessage chan<- interface{}) error {
		outputChan := make(chan string)
		chatDone := make(chan struct{})
		go func() {
			defer close(chatDone)
			defer close(outputChan)

			var tabCtx context.Context
			if a.browser != nil {
				var cancel context.CancelFunc
				var err error
				tabCtx, cancel, err = a.browser.NewTab(ctx)
				if err != nil {
					agui.EmitError(ctx, agentsMessage, err, "failed to create browser tab")
					return
				}
				defer cancel()
			}

			if err := a.codeOwner.Chat(ctx, codeowner.CodeQuestion{
				Question:      message,
				OutputDir:     a.workingDir,
				EventChan:     agentsMessage,
				SenderContext: "agui:http",
				BrowserTab:    tabCtx,
			}, outputChan); err != nil {
				agui.EmitError(ctx, agentsMessage, err, "while processing AG-UI chat message")
			}
		}()
		for output := range outputChan {
			if output != "" {
				agui.EmitAgentMessage(ctx, agentsMessage, "genie", output)
			}
		}
		<-chatDone
		return nil
	}
}

// initToolRegistry creates the centralized tool registry using toolreg.
// Dependencies with lifecycle (MCP client, browser) are created here and
// stored on Application so they can be released by Close().
// Each tool group is constructed as a ToolProviders conformer and passed
// to tools.NewRegistry, which simply collects and optionally filters them.
func (a *Application) initToolRegistry(ctx context.Context, vectorStore vector.IStore) *tools.Registry {
	log := logger.GetLogger(ctx).With("fn", "app.initToolRegistry")

	providers := []tools.ToolProviders{
		websearch.NewToolProvider(a.cfg.WebSearch),
		networking.NewToolProvider(),
	}

	// --- MCP tools ---
	mcpClient, err := mcp.NewClient(ctx, a.cfg.MCP)
	if err != nil {
		log.Warn("failed to initialize MCP client, skipping MCP tools", "error", err)
	}
	if mcpClient != nil {
		a.mcpClient = mcpClient
		providers = append(providers, mcpClient) // *mcp.Client already satisfies ToolProviders
		log.Info("MCP client initialized", "server_count", len(a.cfg.MCP.Servers))
	}

	// --- Skills ---
	if len(a.cfg.SkillsRoots) != 0 {
		repo, err := skill.NewFSRepository(a.cfg.SkillsRoots...)
		if err != nil {
			log.Warn("failed to initialize skills repository", "error", err)
		}
		if repo != nil {
			executor := skills.NewLocalExecutor(a.workingDir)
			providers = append(providers, skills.NewToolProvider(repo, executor))
			log.Info("Skills tool provider added")
		}
	}

	// --- Browser tools ---
	bw, err := browser.New(
		browser.WithHeadless(true),
		browser.WithBlockedDomains(a.cfg.Browser.BlockedDomains),
	)
	if err != nil {
		log.Warn("failed to start browser, skipping browser tools", "error", err)
	}
	if bw != nil {
		a.browser = bw
		providers = append(providers, bw) // *browser.Browser satisfies ToolProviders
		log.Info("Browser initialized", "headless", true)
	}

	// --- SCM tools ---
	if scmSvc, err := scm.New(a.cfg.SCM); err == nil {
		if vErr := scmSvc.Validate(ctx); vErr != nil {
			log.Warn("SCM health check failed", "error", vErr)
		}
		providers = append(providers, scm.NewToolProvider(scmSvc))
		log.Debug("SCM tool provider added")
	}

	// --- PM tools ---
	if pmSvc, err := pm.New(a.cfg.ProjectManagement); err == nil {
		if vErr := pmSvc.Validate(ctx); vErr != nil {
			log.Warn("PM health check failed", "error", vErr)
		}
		providers = append(providers, pm.NewToolProvider(pmSvc))
	}
	log.Debug("PM tool provider added")

	// --- Email tools ---
	if emailSvc, err := a.cfg.Email.New(); err == nil {
		providers = append(providers, email.NewToolProvider(emailSvc))
	}
	log.Debug("Email tool provider added")

	// --- Slack tools (search/read — distinct from messenger) ---
	if slSvc, err := slacktools.New(a.cfg.Enterprise.SlackTools); err == nil {
		if vErr := slSvc.Validate(ctx); vErr != nil {
			log.Warn("Slack tools health check failed", "error", vErr)
		}
		providers = append(providers, slacktools.NewToolProvider(slSvc))
		log.Debug("Slack tools provider added")
	}

	// --- Salesforce tools ---
	if sforceSvc, err := salesforce.New(a.cfg.Enterprise.Salesforce); err == nil {
		if vErr := sforceSvc.Validate(ctx); vErr != nil {
			log.Warn("Salesforce health check failed", "error", vErr)
		}
		providers = append(providers, salesforce.NewToolProvider(sforceSvc))
		log.Debug("Salesforce tool provider added")
	}

	// --- Atlassian tools (Jira + Confluence) ---
	if atlSvc, err := atlassian.New(a.cfg.Enterprise.Atlassian); err == nil {
		if vErr := atlSvc.Validate(ctx); vErr != nil {
			log.Warn("Atlassian health check failed", "error", vErr)
		}
		providers = append(providers, atlassian.NewToolProvider(atlSvc))
		log.Debug("Atlassian tool provider added")
	}

	// --- Snowflake tools ---
	if sfSvc, err := snowflake.New(a.cfg.Enterprise.Snowflake); err == nil {
		if vErr := sfSvc.Validate(ctx); vErr != nil {
			log.Warn("Snowflake health check failed", "error", vErr)
		}
		providers = append(providers, snowflake.NewToolProvider(sfSvc))
		log.Debug("Snowflake tool provider added")
	}

	// --- BigQuery tools ---
	if bqSvc, err := bigquery.New(a.cfg.Enterprise.BigQuery); err == nil {
		if vErr := bqSvc.Validate(ctx); vErr != nil {
			log.Warn("BigQuery health check failed", "error", vErr)
		}
		providers = append(providers, bigquery.NewToolProvider(bqSvc))
		log.Debug("BigQuery tool provider added")
	}

	// --- Google Drive tools ---
	if gdSvc, err := gdrive.New(a.cfg.Enterprise.GDrive); err == nil {
		if vErr := gdSvc.Validate(ctx); vErr != nil {
			log.Warn("Google Drive health check failed", "error", vErr)
		}
		providers = append(providers, gdrive.NewToolProvider(gdSvc))
		log.Debug("Google Drive tool provider added")
	}

	// --- HubSpot tools ---
	if hsSvc, err := hubspot.New(a.cfg.Enterprise.HubSpot); err == nil {
		if vErr := hsSvc.Validate(ctx); vErr != nil {
			log.Warn("HubSpot health check failed", "error", vErr)
		}
		providers = append(providers, hubspot.NewToolProvider(hsSvc))
		log.Debug("HubSpot tool provider added")
	}

	// --- Clarify tool ---
	// Clarify emitter bridges clarify → AG-UI + messenger.
	// Stays here because it references Application fields.
	emitter := func(ctx context.Context, evt clarify.ClarificationEvent) error {
		evChan := agui.EventChanFromContext(ctx)
		if evChan != nil {
			select {
			case evChan <- agui.ClarificationRequestMsg{
				Type:      agui.EventClarificationRequest,
				RequestID: evt.RequestID,
				Question:  evt.Question,
				Context:   evt.Context,
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		origin := messenger.MessageOriginFrom(ctx)
		sendReq := messenger.SendRequest{
			Channel: origin.Channel,
			Content: messenger.MessageContent{
				Text: fmt.Sprintf("❓ **Question from Genie**:\n%s\n\n_Reply with your answer._", evt.Question),
			},
		}
		sendReq = a.msgr.FormatClarification(sendReq, messenger.ClarificationInfo{
			RequestID: evt.RequestID,
			Question:  evt.Question,
			Context:   evt.Context,
		})
		if _, err := a.msgr.Send(ctx, sendReq); err != nil {
			log.Warn("failed to send clarification via messenger", "error", err)
		}
		_ = a.shortMemory.Set(ctx, "pending_clarification", origin.String(), evt.RequestID, 10*time.Minute)

		return nil
	}

	providers = append(providers, clarify.NewToolProvider(a.clarifyStore, emitter))
	log.Debug("Clarify tool provider added")

	// --- Messenger tool ---
	providers = append(providers, messenger.NewToolProvider(a.msgr))
	log.Debug("Messenger send_message tool provider added")

	// --- Cron tool ---
	providers = append(providers, cron.NewToolProvider(a.cronStore))
	log.Debug("Cron tool provider added")

	// --- File tools (scoped to working directory) ---
	if fp := tools.NewFileToolProvider(ctx, a.workingDir); fp != nil {
		providers = append(providers, fp)
		log.Debug("File tool provider added")
	}

	// --- Shell tool ---
	providers = append(providers, tools.NewShellToolProvider(a.workingDir))
	log.Debug("Shell tool provider added")

	// --- Summarizer tool ---
	// Note: Summarizer is not injected at the app level — sub-agents create
	// their own summarizers. This keeps the tool registry self-contained.

	// --- Vector memory + Runbook tools ---
	if vectorStore != nil {
		providers = append(providers, vector.NewToolProvider(vectorStore))
		providers = append(providers, runbook.NewToolProvider(vectorStore))
		log.Debug("Vector memory and runbook tool providers added")
	}

	// --- Context management tools (Pensieve paradigm) ---
	if a.cfg.EnablePensieve {
		providers = append(providers, tools.NewPensieveToolProvider())
		log.Debug("Context management tool provider added (Pensieve paradigm)")
	}

	// --- Build registry and filter denied tools ---
	return tools.NewRegistry(ctx, providers...).FilterDenied(ctx, a.cfg.HITL)
}

// startMessengerLoop starts listening for incoming messenger messages
// (Slack, Teams, Telegram, Google Chat, Discord) and dispatches them to
// the codeowner agent.
func (a *Application) startMessengerLoop(ctx context.Context) {
	eventChan := make(chan interface{}, 100)
	// Drain eventChan — there is no UI consumer on the messenger path,
	// so without this goroutine downstream tool/chat sends would block
	// once the buffer fills, stalling the messenger handlers.
	go func() {
		for {
			select {
			case _, ok := <-eventChan:
				if !ok {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// seenMessages deduplicates incoming messages by ID.
	// WhatsApp can deliver the same message multiple times:
	//   - Two goroutines reading from the channel simultaneously
	//   - Reconnect replaying buffered/pending messages
	// We track seen IDs for 60 seconds, which is well beyond any replay window.
	// Backed by ShortMemoryStore for restart persistence and no manual eviction.
	const seenMemoryType = "seen_messages"
	const seenTTL = 60 * time.Second

	messengerCh := messenger.ReceiveWithReconnect(ctx, a.msgr, 1*time.Second, 30*time.Second)
	go func() {
		for {
			select {
			case msg, ok := <-messengerCh:
				if !ok {
					return
				}
				// Deduplicate by message ID when available.
				if msg.ID != "" {
					if _, found, _ := a.shortMemory.Get(ctx, seenMemoryType, msg.ID); found {
						continue // already processing this message
					}
					_ = a.shortMemory.Set(ctx, seenMemoryType, msg.ID, "1", seenTTL)
				}
				go a.handleMessengerInput(context.WithoutCancel(ctx), msg, eventChan)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// handleMessengerInput processes an incoming message from a remote messaging
// platform (Slack, Teams, Telegram, Google Chat, Discord). The response is
// sent back to the same channel/thread via messenger.Send().
func (a *Application) handleMessengerInput(ctx context.Context, msg messenger.IncomingMessage, eventChan chan<- interface{}) {
	if msg.Content.Text == "" && len(msg.Content.Attachments) == 0 {
		return
	}

	// Synthesize or augment message text with attachment info so the LLM
	// knows about any files sent. Includes file paths when available so the
	// LLM can use read_file to access the content.
	if len(msg.Content.Attachments) > 0 {
		desc := media.DescribeAttachments(msg.Content.Attachments, a.workingDir)
		if msg.Content.Text == "" {
			msg.Content.Text = desc
		} else {
			msg.Content.Text = msg.Content.Text + "\n\n" + desc
		}
	}

	log := logger.GetLogger(ctx).With("fn", "app.handleMessengerInput")

	// Check sender allowlist — if configured, only respond to listed senders.
	if !a.cfg.Messenger.IsSenderAllowed(msg.Sender.ID) {
		log.Debug("ignoring message from non-allowed sender",
			"sender", msg.Sender.ID,
			"displayName", msg.Sender.DisplayName,
		)
		return
	}

	senderCtx := msg.String()

	// Create structured origin for reply routing.
	origin := messenger.MessageOrigin{
		Platform:  msg.Platform,
		Channel:   msg.Channel,
		Sender:    msg.Sender,
		ThreadID:  msg.ThreadID,
		MessageID: msg.ID,
	}
	messengerCtx := messenger.WithMessageOrigin(ctx, origin)

	// Check for pending clarification reply first.
	const clarifyMemoryType = "pending_clarification"
	if reqID, found, _ := a.shortMemory.Get(ctx, clarifyMemoryType, senderCtx); found {
		if err := a.clarifyStore.Respond(reqID, msg.Content.Text); err != nil {
			log.Error("failed to respond to clarification via messenger", "error", err)
			_, _ = a.msgr.Send(messengerCtx, messenger.SendRequest{
				Channel:  msg.Channel,
				ThreadID: msg.ThreadID,
				Content:  messenger.MessageContent{Text: fmt.Sprintf("⚠️ Failed to submit answer: %s", err)},
			})
		} else {
			_ = a.shortMemory.Delete(ctx, clarifyMemoryType, senderCtx)
			// React with 👍 instead of sending a confirmation message.
			_, _ = a.msgr.Send(ctx, messenger.SendRequest{
				Type:             messenger.SendTypeReaction,
				Channel:          msg.Channel,
				ReplyToMessageID: msg.ID,
				Emoji:            "👍",
			})
		}
		return
	}

	// Check for interactive HITL reply (Yes/No/Revisit)
	normalized := strings.TrimSpace(strings.ToLower(msg.Content.Text))
	isApproval := normalized == "yes" || normalized == "y"
	isRejection := normalized == "no" || normalized == "n"

	// Try reply-to routing first: if the user replied to a specific
	// approval notification, resolve that exact approval.
	var approvalID string
	var found bool
	var usedReplyTo bool

	if qmid, ok := msg.Metadata["quoted_message_id"].(string); ok && qmid != "" {
		approvalID, found = a.notifierStore.GetPendingByMessageID(messengerCtx, qmid)
		usedReplyTo = found
	}
	// Fallback to FIFO (oldest pending for this sender).
	if !found {
		approvalID, found = a.notifierStore.GetPending(ctx, senderCtx)
	}

	if found {

		status := hitl.StatusApproved
		replyText := "✅ **Approved**"
		var feedback string

		if isRejection {
			status = hitl.StatusRejected
			replyText = "❌ **Rejected**"
		}
		if !isApproval && !isRejection {
			status = hitl.StatusRejected
			feedback = msg.Content.Text
			replyText = "🔄 **Sent back for revision** — " + feedback
		}

		err := a.notifierStore.Resolve(ctx, hitl.ResolveRequest{
			ApprovalID: approvalID,
			Decision:   status,
			ResolvedBy: msg.Sender.DisplayName,
			Feedback:   feedback,
		})

		if err != nil {
			log.Error("failed to resolve approval via messenger", "error", err)
			replyText = fmt.Sprintf("⚠️ Failed to resolve: %s", err)
		}
		if err == nil {
			if usedReplyTo {
				a.notifierStore.RemovePendingByApprovalID(ctx, senderCtx, approvalID)
			} else {
				a.notifierStore.RemovePending(ctx, senderCtx)
			}
		}

		_, _ = a.msgr.Send(ctx, messenger.SendRequest{
			Channel:  msg.Channel,
			ThreadID: msg.ThreadID,
			Content:  messenger.MessageContent{Text: replyText},
		})
		return
	}

	// Emit attributed user bubble
	senderLabel := fmt.Sprintf("%s (%s)", msg.Sender.DisplayName, msg.Platform)
	agui.EmitAgentMessage(ctx, eventChan, senderLabel, msg.Content.Text)

	log.Info("received messenger message",
		"platform", msg.Platform,
		"senderID", msg.Sender.ID,
		"senderName", msg.Sender.DisplayName,
		"channel", msg.Channel.ID,
		"senderContext", senderCtx,
	)

	agentThoughts := make(chan string)
	go func() {
		defer close(agentThoughts)

		// Create a root OTel span for this message so Langfuse traces get
		// populated with input, tags, name, userId, and sessionId.
		tracer := otel.Tracer("genie")
		traceCtx, span := tracer.Start(messengerCtx, "handle_message")
		span.SetAttributes(
			attribute.String("langfuse.trace.name", fmt.Sprintf("%s message", msg.Platform)),
			attribute.String("langfuse.trace.input", msg.Content.Text),
			attribute.String("langfuse.user.id", msg.Sender.ID),
			attribute.String("langfuse.session.id", senderCtx),
			attribute.StringSlice("langfuse.trace.tags", []string{
				string(msg.Platform),
				"messenger",
			}),
		)
		defer span.End()

		var tabCtx context.Context
		if a.browser != nil {
			var cancel context.CancelFunc
			var err error
			tabCtx, cancel, err = a.browser.NewTab(ctx)
			if err != nil {
				agui.EmitError(ctx, eventChan, err, "failed to create browser tab")
				return
			}
			defer cancel()
		}

		if err := a.codeOwner.Chat(traceCtx, codeowner.CodeQuestion{
			Question:      msg.Content.Text,
			OutputDir:     a.workingDir,
			EventChan:     eventChan,
			SenderContext: senderCtx,
			BrowserTab:    tabCtx,
			Attachments:   msg.Content.Attachments,
		}, agentThoughts); err != nil {
			agui.EmitError(ctx, eventChan, err, "while processing messenger message")
		}
	}()

	for response := range agentThoughts {
		agui.EmitAgentMessage(ctx, eventChan, "Genie", response)

		if _, err := a.msgr.Send(ctx, messenger.SendRequest{
			Channel:  msg.Channel,
			ThreadID: msg.ThreadID,
			Content:  messenger.MessageContent{Text: response},
		}); err != nil {
			log.Warn("failed to reply on messenger", "error", err)
		}
	}
}

// replayOnApproval waits for a recovered pending approval to be resolved.
// If the approval is approved, it replays the original user question through
// the background worker so the agent can re-execute the task with fresh LLM
// context (but with durable conversation memory for recall).
//
// This is the core of the "replay-on-resume" pattern for Phase 3:
//   - The user sends a question that triggers a mutating tool call.
//   - A HITL approval is created, saving the original question.
//   - The server restarts, killing the agent goroutine.
//   - On restart, RecoverPending re-registers the approval's waiter channel.
//   - This goroutine blocks until the approval is resolved.
//   - On approval, it dispatches the original question as a background task.
func (a *Application) replayOnApproval(ctx context.Context, ra hitl.ReplayableApproval, bgWorker *messengeragui.BackgroundWorker) {
	log := logger.GetLogger(ctx).With("fn", "replayOnApproval", "approvalID", ra.ApprovalID)

	// Use a generous timeout so we don't block forever if the approval is
	// never resolved — the user may have moved on after a restart.
	replayCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	log.Info("Waiting for recovered approval to be resolved before replaying",
		"question", truncateForLog(ra.Question, 80))

	resolved, err := a.approvalStore.WaitForResolution(replayCtx, ra.ApprovalID)
	if err != nil {
		log.Warn("Replay waiter cancelled or failed", "error", err)
		return
	}

	if resolved.Status != hitl.StatusApproved {
		log.Info("Recovered approval was not approved, skipping replay",
			"status", resolved.Status)
		return
	}

	log.Info("Recovered approval approved — replaying original question",
		"question", truncateForLog(ra.Question, 120),
		"senderContext", ra.SenderContext)

	// Build the replay payload. We pass the sender context and original
	// question so the chat handler can re-execute with the right identity.
	payload, _ := json.Marshal(map[string]string{
		"question":       ra.Question,
		"sender_context": ra.SenderContext,
		"replay_source":  "hitl-recovery",
		"approval_id":    ra.ApprovalID,
	})

	_, err = bgWorker.HandleEvent(ctx, agui.EventRequest{
		Type:    agui.EventTypeWebhook,
		Source:  fmt.Sprintf("hitl-replay:%s", ra.ApprovalID),
		Payload: payload,
	})
	if err != nil {
		log.Error("Failed to dispatch replayed question to background worker", "error", err)
	}
}

// truncateForLog shortens a string for log output.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
