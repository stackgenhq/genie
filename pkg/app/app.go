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
	aguimsg "github.com/appcd-dev/genie/pkg/messenger/agui"
	messengerhitl "github.com/appcd-dev/genie/pkg/messenger/hitl"
	"github.com/appcd-dev/genie/pkg/messenger/media"
	"github.com/appcd-dev/genie/pkg/tools"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"gorm.io/gorm"
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

	// aguiAdapter is the in-process messenger adapter for the AG-UI SSE
	// server. It runs alongside the primary messenger (Slack, etc.) and
	// allows AG-UI web chat messages to participate in messenger-level
	// features (HITL notifications, send_message tool, sender allowlists).
	aguiAdapter *aguimsg.Messenger
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
		a.msgr = messenger.WithLogging(ctx, a.msgr)
		log.Info("Messenger initialized", "platform", a.msgr.Platform())
	}

	// --- AGUI messenger adapter (always created, runs alongside primary messenger) ---
	a.aguiAdapter = aguimsg.New(aguimsg.Config{})
	if err := a.aguiAdapter.Connect(ctx); err != nil {
		log.Warn("failed to connect AGUI messenger adapter", "error", err)
	} else {
		log.Info("AGUI messenger adapter connected (in-process)")
	}

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
	if a.msgr != nil {
		a.notifierStore = messengerhitl.NewNotifierStore(a.approvalStore, a.msgr)
		a.approvalStore = a.notifierStore
	}

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
		registry.AllTools(),
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
		a.replayWG.Add(1)
		go func() {
			defer a.replayWG.Done()
			a.replayOnApproval(ctx, ra, bgWorker)
		}()
	}
	a.pendingReplays = nil // don't replay twice

	// --- Cron scheduler (dispatcher wired via constructor) ---
	dispatcher := func(ctx context.Context, req agui.EventRequest) (string, error) {
		req.Type = agui.EventTypeWebhook
		return bgWorker.HandleEvent(ctx, req)
	}
	cronScheduler := a.cfg.Cron.NewScheduler(a.cronStore, dispatcher)

	// --- AG-UI server ---
	aguiServer := a.cfg.AGUI.NewServer(httpHandler, a.approvalStore, a.clarifyStore, bgWorker, cronScheduler)

	// Wire the AGUI messenger adapter as a bridge so handleRun injects
	// messages into the messenger pipeline for HITL + send_message routing.
	if a.aguiAdapter != nil {
		aguiServer.SetMessengerBridge(a.aguiAdapter)
		log.Info("AGUI messenger bridge wired to AG-UI server")
	}

	// Wire the framework runner wrapping the same chat pipeline.
	// This provides the structured trunner.Runner interface for features
	// like run registration, cancellation, and session tracking.
	aguiServer.SetRunner(agui.NewRunner(chatHandler))
	log.Info("Framework runner wired to AG-UI server")

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
		a.mcpClient.Close()
	}
	if a.browser != nil {
		a.browser.Close()
	}
	if a.msgr != nil {
		if err := a.msgr.Disconnect(context.Background()); err != nil {
			logger.Warn("failed to disconnect messenger", "error", err)
		}
	}
	if a.aguiAdapter != nil {
		if err := a.aguiAdapter.Disconnect(context.Background()); err != nil {
			logger.Warn("failed to disconnect AGUI messenger adapter", "error", err)
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
func (a *Application) initToolRegistry(ctx context.Context, vectorStore vector.IStore) *tools.Registry {
	log := logger.GetLogger(ctx).With("fn", "app.initToolRegistry")

	// MCP client — lifecycle owned by Application
	mcpClient, err := mcp.NewClient(ctx, a.cfg.MCP)
	if err != nil {
		log.Warn("failed to initialize MCP client, skipping MCP tools", "error", err)
	}
	if mcpClient != nil {
		a.mcpClient = mcpClient
		log.Info("MCP client initialized", "server_count", len(a.cfg.MCP.Servers))
	}

	// Browser — lifecycle owned by Application
	bw, err := browser.New(
		browser.WithHeadless(true),
		browser.WithBlockedDomains(a.cfg.Browser.BlockedDomains),
	)
	if err != nil {
		log.Warn("failed to start browser, skipping browser tools", "error", err)
	}
	if bw != nil {
		a.browser = bw
		log.Info("Browser initialized", "headless", true)
	}

	// Clarify emitter — bridges clarify → AG-UI + messenger.
	// Stays here because it references Application fields.
	emitter := func(ctx context.Context, evt clarify.ClarificationEvent) error {
		// AG-UI path: emit to the event channel for the SSE stream.
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

		// Messenger path: forward question to the chat platform.
		if a.msgr != nil {
			senderCtx := messenger.SenderContextFrom(ctx)
			if senderCtx != "" {
				parts := strings.SplitN(senderCtx, ":", 3)
				if len(parts) == 3 {
					channelID := parts[2]
					sendReq := messenger.SendRequest{
						Channel: messenger.Channel{ID: channelID},
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
					_ = a.shortMemory.Set(ctx, "pending_clarification", senderCtx, evt.RequestID, 10*time.Minute)
				}
			}
		}

		return nil
	}

	return tools.NewRegistry(ctx, tools.Deps{
		WorkingDir:     a.workingDir,
		VectorStore:    vectorStore,
		WebSearch:      a.cfg.WebSearch,
		SCMConfig:      a.cfg.SCM,
		PMConfig:       a.cfg.ProjectManagement,
		EmailConfig:    a.cfg.Email,
		SkillsRoots:    a.cfg.SkillsRoots,
		HITLCfg:        a.cfg.HITL,
		Messenger:      a.msgr,
		CronStore:      a.cronStore,
		MCPClient:      mcpClient,
		Browser:        bw,
		ClarifyStore:   a.clarifyStore,
		ClarifyEmitter: emitter,
		EnablePensieve: a.cfg.EnablePensieve,
	})
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
	origin := &messenger.MessageOrigin{
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

	if a.notifierStore != nil {
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
