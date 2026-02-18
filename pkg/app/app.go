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
	"time"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/audit"
	"github.com/appcd-dev/genie/pkg/browser"
	"github.com/appcd-dev/genie/pkg/codeowner"
	"github.com/appcd-dev/genie/pkg/config"
	"github.com/appcd-dev/genie/pkg/cron"
	geniedb "github.com/appcd-dev/genie/pkg/db"
	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/mcp"
	"github.com/appcd-dev/genie/pkg/messenger"
	messengerhitl "github.com/appcd-dev/genie/pkg/messenger/hitl"
	"github.com/appcd-dev/genie/pkg/skills"
	"github.com/appcd-dev/genie/pkg/tools/email"
	"github.com/appcd-dev/genie/pkg/tools/networking"
	"github.com/appcd-dev/genie/pkg/tools/pm"
	"github.com/appcd-dev/genie/pkg/tools/scm"
	"github.com/appcd-dev/genie/pkg/tools/websearch"
	"gorm.io/gorm"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
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

	// --- Messenger (best-effort) ---
	a.msgr, err = a.cfg.Messenger.InitMessenger(ctx)
	if err != nil {
		log.Warn("failed to initialize messenger, continuing without", "error", err)
	}
	if a.msgr != nil {
		log.Info("Messenger initialized", "platform", a.msgr.Platform())
	}

	// --- Tools ---
	tools := a.initToolDeps(ctx)

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
	if a.msgr != nil {
		a.notifierStore = messengerhitl.NewNotifierStore(a.approvalStore, a.msgr)
		a.approvalStore = a.notifierStore
	}

	// Recover approvals orphaned by a previous server instance.
	// Approvals older than 30 min are marked "expired"; recent ones get
	// waiter channels re-registered so they can still be resolved.
	if res, err := a.approvalStore.RecoverPending(ctx, 30*time.Minute); err != nil {
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

	// --- Messenger tool ---
	if a.msgr != nil {
		tools = append(tools, messenger.NewSendMessageTool(a.msgr))
	}

	// --- Cron store + tool ---
	a.cronStore = cron.NewStore(a.db)
	tools = append(tools, cron.NewCreateRecurringTaskTool(a.cronStore))

	// --- CodeOwner ---
	a.codeOwner, err = codeowner.NewCodeOwner(
		ctx,
		a.cfg.ModelConfig.NewEnvBasedModelProvider(),
		a.workingDir,
		tools,
		vectorStore,
		a.auditor,
		a.approvalStore,
		memorySvc,
		a.cfg.Runbook,
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

	// --- Chat handler ---
	chatHandler := a.buildChatHandler()
	httpHandler := agui.NewChatHandlerFromCodeOwner(a.codeOwner.Resume, chatHandler)

	// --- Background worker ---
	bgWorker := agui.NewBackgroundWorker(httpHandler, runtime.NumCPU())

	// Spawn replay goroutines for recoverable pending approvals.
	// Each goroutine waits for its approval to be resolved, then replays
	// the original user question through the background worker.
	for _, ra := range a.pendingReplays {
		ra := ra // capture loop variable
		go a.replayOnApproval(ctx, ra, bgWorker)
	}
	a.pendingReplays = nil // don't replay twice

	// --- Cron scheduler (dispatcher wired via constructor) ---
	dispatcher := func(ctx context.Context, req agui.EventRequest) (string, error) {
		req.Type = agui.EventTypeWebhook
		return bgWorker.HandleEvent(ctx, req)
	}
	cronScheduler := a.cfg.Cron.NewScheduler(a.cronStore, dispatcher)

	// --- AG-UI server ---
	aguiServer := a.cfg.AGUI.NewServer(httpHandler, a.approvalStore, bgWorker, cronScheduler)

	// --- Messenger receive loop ---
	if a.msgr != nil {
		a.startMessengerLoop(ctx)
	}

	// --- Startup banner ---
	fmt.Fprintf(os.Stderr, "\n🧞 Genie AG-UI server starting on :%d\n", a.cfg.AGUI.Port)
	fmt.Fprintf(os.Stderr, "   Working directory: %s\n", a.workingDir)
	fmt.Fprintf(os.Stderr, "   Database:          %s\n", a.cfg.DBConfig.DBFile)
	fmt.Fprintf(os.Stderr, "   Audit log:         %s\n", a.auditPath)
	fmt.Fprintf(os.Stderr, "   Health check:      http://localhost:%d/health\n", a.cfg.AGUI.Port)
	fmt.Fprintf(os.Stderr, "   UI:                http://localhost:%d/ui/\n", a.cfg.AGUI.Port)
	fmt.Fprintf(os.Stderr, "   Resume:            http://localhost:%d/api/v1/resume\n\n", a.cfg.AGUI.Port)

	log.Info("Starting Genie", "version", a.version)

	return aguiServer.Start(ctx)
}

// Close releases all resources acquired during Bootstrap. Safe to call
// even if Bootstrap was only partially successful.
func (a *Application) Close() {
	if a.auditor != nil {
		_ = a.auditor.Close()
	}
	if a.codeOwner != nil {
		if err := a.codeOwner.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to close code owner: %v\n", err)
		}
	}
	if a.mcpClient != nil {
		a.mcpClient.Close()
	}
	if a.browser != nil {
		a.browser.Close()
	}
	if a.msgr != nil {
		if err := a.msgr.Disconnect(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to disconnect messenger: %v\n", err)
		}
	}
	if a.db != nil {
		if err := geniedb.Close(a.db); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to close database: %v\n", err)
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

// initToolDeps builds the tool slice from GenieConfig. Each dependency is
// best-effort — failures are logged as warnings, not fatal. Resources like
// the MCP client and browser are stored on the Application struct and
// released by Close().
func (a *Application) initToolDeps(ctx context.Context) []tool.Tool {
	log := logger.GetLogger(ctx).With("fn", "app.initToolDeps")

	var tools []tool.Tool

	// WebSearch — always available (falls back to DuckDuckGo)
	tools = append(tools, websearch.NewTool(a.cfg.WebSearch))
	log.Debug("WebSearch tool initialized")

	// HTTP request tool — always available
	tools = append(tools, networking.NewTool())
	log.Debug("HTTP request tool initialized")

	// MCP tools
	mcpClient, err := mcp.NewClient(ctx, a.cfg.MCP)
	if err != nil {
		log.Warn("failed to initialize MCP client, skipping MCP tools", "error", err)
	}
	if mcpClient != nil {
		a.mcpClient = mcpClient
		tools = append(tools, mcpClient.GetTools()...)
		log.Info("MCP client initialized", "server_count", len(a.cfg.MCP.Servers))
	}

	// Skills
	if len(a.cfg.SkillsRoots) != 0 {
		repo, err := skill.NewFSRepository(a.cfg.SkillsRoots...)
		if err != nil {
			log.Warn("failed to initialize skills repository, skipping skill tools", "error", err)
		}
		if repo != nil {
			executor := skills.NewLocalExecutor(a.workingDir)
			skillTools := skills.CreateAllSkillTools(repo, executor)
			tools = append(tools, skillTools...)
			log.Info("Skills initialized", "tool_count", len(skillTools))
		}
	}

	// Browser tools
	bw, err := browser.New(
		browser.WithHeadless(true),
		browser.WithBlockedDomains(a.cfg.Browser.BlockedDomains),
	)
	if err != nil {
		log.Warn("failed to start browser, skipping browser tools", "error", err)
	}
	if bw != nil {
		a.browser = bw
		for _, bt := range browser.AllTools(bw) {
			tools = append(tools, bt)
		}
		log.Info("Browser initialized", "headless", true)
	}

	// SCM tools
	if scmSvc, err := scm.New(a.cfg.SCM); err == nil {
		if vErr := scmSvc.Validate(ctx); vErr != nil {
			log.Warn("SCM health check failed — token or endpoint may be invalid", "error", vErr)
		}
		tools = append(tools, scm.AllTools(scmSvc)...)
	}
	log.Debug("SCM tools initialized")

	// PM tools
	if pmSvc, err := pm.New(a.cfg.ProjectManagement); err == nil {
		if vErr := pmSvc.Validate(ctx); vErr != nil {
			log.Warn("PM health check failed — token or endpoint may be invalid", "error", vErr)
		}
		tools = append(tools, pm.AllTools(pmSvc)...)
	}
	log.Debug("PM tools initialized")

	// Email tools
	if emailSvc, err := a.cfg.Email.New(); err == nil {
		tools = append(tools, email.AllTools(emailSvc)...)
	}
	log.Debug("Email tools initialized")

	return tools
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
	messengerCh := messenger.ReceiveWithReconnect(ctx, a.msgr, 1*time.Second, 30*time.Second)
	go func() {
		for {
			select {
			case msg, ok := <-messengerCh:
				if !ok {
					return
				}
				go a.handleMessengerInput(ctx, msg, eventChan)
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
	if msg.Content.Text == "" {
		return
	}

	log := logger.GetLogger(ctx).With("fn", "app.handleMessengerInput")

	senderCtx := msg.String()

	// Check for interactive HITL reply (Yes/No/Revisit)
	normalized := strings.TrimSpace(strings.ToLower(msg.Content.Text))
	isApproval := normalized == "yes" || normalized == "y"
	isRejection := normalized == "no" || normalized == "n"

	if a.notifierStore != nil {
		if approvalID, found := a.notifierStore.GetPending(senderCtx); found {
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
				a.notifierStore.RemovePending(senderCtx)
			}

			_, _ = a.msgr.Send(ctx, messenger.SendRequest{
				Channel:  msg.Channel,
				ThreadID: msg.ThreadID,
				Content:  messenger.MessageContent{Text: replyText},
			})
			return
		}
	}

	// Emit attributed user bubble
	senderLabel := fmt.Sprintf("%s (%s)", msg.Sender.DisplayName, msg.Platform)
	agui.EmitAgentMessage(ctx, eventChan, senderLabel, msg.Content.Text)

	log.Info("received messenger message",
		"platform", msg.Platform,
		"sender", msg.Sender.DisplayName,
		"channel", msg.Channel.ID,
		"senderContext", senderCtx,
	)

	agentThoughts := make(chan string)
	go func() {
		defer close(agentThoughts)

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

		if err := a.codeOwner.Chat(ctx, codeowner.CodeQuestion{
			Question:      msg.Content.Text,
			OutputDir:     a.workingDir,
			EventChan:     eventChan,
			SenderContext: senderCtx,
			ExcludeTools:  []string{"send_message"},
			BrowserTab:    tabCtx,
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
func (a *Application) replayOnApproval(ctx context.Context, ra hitl.ReplayableApproval, bgWorker *agui.BackgroundWorker) {
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
