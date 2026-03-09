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
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/browser"
	"github.com/stackgenhq/genie/pkg/clarify"
	"github.com/stackgenhq/genie/pkg/config"
	"github.com/stackgenhq/genie/pkg/cron"
	"github.com/stackgenhq/genie/pkg/datasource"
	geniedb "github.com/stackgenhq/genie/pkg/db"
	"github.com/stackgenhq/genie/pkg/hitl"
	"github.com/stackgenhq/genie/pkg/httputil"
	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/mcp"
	"github.com/stackgenhq/genie/pkg/memory/graph"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/messenger"
	messengeragui "github.com/stackgenhq/genie/pkg/messenger/agui" // register adapter + server types
	_ "github.com/stackgenhq/genie/pkg/messenger/discord"          // register adapter
	_ "github.com/stackgenhq/genie/pkg/messenger/googlechat"       // register adapter
	messengerhitl "github.com/stackgenhq/genie/pkg/messenger/hitl"
	"github.com/stackgenhq/genie/pkg/messenger/media"
	_ "github.com/stackgenhq/genie/pkg/messenger/slack"    // register adapter
	_ "github.com/stackgenhq/genie/pkg/messenger/teams"    // register adapter
	_ "github.com/stackgenhq/genie/pkg/messenger/telegram" // register adapter
	_ "github.com/stackgenhq/genie/pkg/messenger/whatsapp" // register adapter
	"github.com/stackgenhq/genie/pkg/orchestrator"
	"github.com/stackgenhq/genie/pkg/orchestrator/orchestratorcontext"
	"github.com/stackgenhq/genie/pkg/pii"
	"github.com/stackgenhq/genie/pkg/report/activityreport"
	"github.com/stackgenhq/genie/pkg/semanticrouter"

	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/security/authcontext"

	"github.com/stackgenhq/genie/pkg/tools"
	"github.com/stackgenhq/genie/pkg/tools/codeskim"
	"github.com/stackgenhq/genie/pkg/tools/datetime"
	"github.com/stackgenhq/genie/pkg/tools/doctool"
	"github.com/stackgenhq/genie/pkg/tools/email"
	"github.com/stackgenhq/genie/pkg/tools/encodetool"
	"github.com/stackgenhq/genie/pkg/tools/ghcli"
	"github.com/stackgenhq/genie/pkg/tools/google/calendar"
	"github.com/stackgenhq/genie/pkg/tools/google/contacts"
	"github.com/stackgenhq/genie/pkg/tools/google/gdrive"
	"github.com/stackgenhq/genie/pkg/tools/google/gmail"
	"github.com/stackgenhq/genie/pkg/tools/google/tasks"
	"github.com/stackgenhq/genie/pkg/tools/jsontool"
	mathtool "github.com/stackgenhq/genie/pkg/tools/math"
	"github.com/stackgenhq/genie/pkg/tools/networking"
	"github.com/stackgenhq/genie/pkg/tools/ocrtool"
	"github.com/stackgenhq/genie/pkg/tools/pm"
	"github.com/stackgenhq/genie/pkg/tools/regextool"
	"github.com/stackgenhq/genie/pkg/tools/scm"
	"github.com/stackgenhq/genie/pkg/tools/sqltool"
	"github.com/stackgenhq/genie/pkg/tools/webfetch"
	"github.com/stackgenhq/genie/pkg/tools/websearch"
	"github.com/stackgenhq/genie/pkg/tools/youtubetranscript"
	"github.com/stackgenhq/genie/pkg/toolwrap"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	oteltrace "go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/trace"
)

// Application orchestrates the Genie lifecycle. Create one with
// NewApplication, call Bootstrap to initialise all dependencies, then Start
// to run the HTTP server. Call Close to release resources.
type Application struct {
	cfg        config.GenieConfig
	workingDir string
	auditPath  string

	// --- populated during Bootstrap ---
	db            *gorm.DB
	toolRegistry  *tools.Registry
	codeOwner     orchestrator.Orchestrator
	approvalStore hitl.ApprovalStore
	cronStore     cron.ICronStore
	msgr          messenger.Messenger
	notifierStore *messengerhitl.NotifierStore
	browser       *browser.Browser
	mcpClient     *mcp.Client
	auditor       audit.Auditor
	sp            security.SecretProvider // same provider used everywhere; audits lookups when [security.secrets] set

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

	// vectorStore is the vector store for memory_search and data sources sync.
	// Set during Bootstrap; may be nil if vector store init failed.
	vectorStore vector.IStore

	// graphStore is the knowledge graph store for graph_* tools. Set during
	// Bootstrap when [graph] is enabled; nil otherwise. Interface-driven so
	// implementation can be swapped (in-memory now; etc.).
	graphStore graph.IStore

	// reportGenerator runs the built-in activity report (genie:report cron action).
	// Set during Bootstrap when cron is used; may be nil.
	reportGenerator *activityreport.Generator

	// approveList is the in-memory temporary allowlist for "approve for X mins".
	// Shared between the HITL middleware and the AG-UI server's /approve endpoint.
	approveList *toolwrap.ApproveList
}

// NewApplication creates a new Application with validated parameters.
// No side-effects — heavy initialisation happens in Bootstrap.
func NewApplication(
	config config.GenieConfig,
	workingDir string,
) (*Application, error) {
	if workingDir == "" {
		return nil, fmt.Errorf("working directory is required")
	}
	auditPath := config.AuditPath
	if auditPath == "" {
		auditPath = audit.DefaultAuditPath(config.AgentName)
	}
	return &Application{
		cfg:        config,
		workingDir: workingDir,
		auditPath:  auditPath,
	}, nil
}

// displayName returns the agent's display name for UI-facing messages.
// Falls back to "Genie" when agent_name is not configured.
func (a *Application) displayName() string {
	if a.cfg.AgentName != "" {
		return a.cfg.AgentName
	}
	return orchestratorcontext.DefaultAgentName
}

// loadAgentsGuide reads persona/coding-standards content for the agent.
// When personaFile is non-empty, that path is used directly (resolved
// relative to dir if not absolute).
// Returns an empty string if the file does not exist or cannot be read
// (best-effort, non-fatal).
func (a *Application) persona(ctx context.Context) string {
	if a.cfg.Persona.File == "" {
		return ""
	}

	personaPath := a.cfg.Persona.File
	if !filepath.IsAbs(personaPath) && a.workingDir != "" {
		personaPath = filepath.Join(a.workingDir, personaPath)
	}
	data, err := os.ReadFile(personaPath)
	if err != nil {
		return ""
	}

	content := string(data)

	// Check against token threshold (roughly 4 chars per token)
	approxTokens := len(content) / 4
	threshold := a.cfg.PersonaTokenThreshold
	if threshold == 0 {
		threshold = 2000 // Fallback if defaults weren't applied
	}
	if approxTokens > threshold {
		logger.GetLogger(ctx).Warn(
			"persona exceeds threshold tokens — consider moving domain knowledge to skills",
			"approx_tokens", approxTokens,
			"threshold", threshold,
		)
	}

	return content
}

// Bootstrap initialises all dependencies: database, tools, vector memory,
// audit logger, HITL approval store, cron store, messenger, and the
// CodeOwner agent. Call exactly once after NewApplication.
func (a *Application) Bootstrap(ctx context.Context) error {
	// Inject agent identity into the context so every downstream call
	// (orchestrator, AGUI server, audit, etc.) can access it.
	ctx = orchestratorcontext.WithAgent(ctx, orchestratorcontext.Agent{
		Name: a.displayName(),
	})
	log := logger.GetLogger(ctx).With("fn", "app.Bootstrap")

	// --- Crypto / TLS (NIST 2030 defaults: TLS 1.2+, optional cipher restriction) ---
	httputil.SetDefaultTLSConfig(a.cfg.Security.Crypto.TLSConfig())

	// --- Database ---
	var err error
	a.db, err = geniedb.OpenConfig(a.cfg.DBConfig)
	if err != nil {
		return fmt.Errorf("database open: %w", err)
	}
	if err := geniedb.AutoMigrate(a.db); err != nil {
		return fmt.Errorf("database migrate: %w", err)
	}
	log.Info("Central database opened", "path", a.cfg.DBConfig.DisplayPath())

	// --- Audit (before Messenger so secret provider can audit lookups) ---
	if a.cfg.AuditPath != "" {
		a.auditor, err = audit.NewFixedPathAuditor(a.auditPath)
	} else {
		a.auditor, err = audit.NewRotatingFileAuditor(a.cfg.AgentName)
	}
	if err != nil {
		return fmt.Errorf("audit init: %w", err)
	}
	log.Info("Audit logger initialized", "path", a.auditPath)
	if a.cfg.AuditPath != "" {
		// Ensure the fixed-path audit file exists on disk (it is created on first Log).
		a.auditor.Log(ctx, audit.LogRequest{EventType: audit.EventCommand, Actor: "app", Action: "bootstrap"})
	}

	// --- Messenger ---
	// Try to initialize the configured external messenger (Slack, Discord, etc.).
	// When none is configured, InitMessenger defaults to AGUI so a.msgr
	// is never nil. Create secret provider with optional audit callback so every
	// secret lookup is recorded (Manager and keyring). Stored on app for initToolRegistry and data sources sync.
	a.sp = a.secretProviderWithAudit(ctx)
	a.msgr, err = a.cfg.Messenger.InitMessenger(ctx, messenger.WithSecretProvider(a.sp))
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

	// --- Vector memory (requires sp for optional secret-backed config) ---
	vectorStore, err := a.cfg.VectorMemory.NewStore(ctx)
	if err != nil {
		log.Warn("failed to initialize vector store, skipping memory tools", "error", err)
	} else {
		a.vectorStore = vectorStore
		log.Info("Vector memory initialized")
	}

	// --- Graph memory (interface-driven; in-memory or vector-backed) ---
	if !a.cfg.Graph.Disabled {
		if a.cfg.Graph.IsVectorStoreBackend() && vectorStore != nil {
			// Reuse the configured vector store (Qdrant) for graph storage.
			graphStore, gErr := graph.NewVectorBackedStore(vectorStore)
			if gErr != nil {
				log.Warn("failed to initialize vector-backed graph store, skipping graph tools", "error", gErr)
			} else {
				a.graphStore = graphStore
				log.Info("Graph memory initialized (vector-backed)")
			}
		} else {
			// Default: in-memory with optional gob+zstd persistence.
			dataDir := a.cfg.Graph.DataDir
			opts := []graph.InMemoryStoreOption{}
			if dataDir != "" {
				opts = append(opts, graph.WithPersistenceDir(dataDir))
			}
			graphStore, gErr := graph.NewInMemoryStore(opts...)
			if gErr != nil {
				log.Warn("failed to initialize graph store, skipping graph tools", "error", gErr)
			} else {
				a.graphStore = graphStore
				log.Info("Graph memory initialized", "data_dir", dataDir)
			}
		}
	}

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
	a.toolRegistry = a.initToolRegistry(ctx, vectorStore)

	// --- Session store (persistent conversation history) ---
	sessionStore := geniedb.NewSessionStore(ctx, a.db)
	log.Info("GORM-backed session store initialized for persistent chat history")

	// In-memory approve list so users can "approve for X mins" from the chat UI.
	a.approveList = toolwrap.NewApproveList()
	modelProvider := a.cfg.ModelConfig.NewEnvBasedModelProvider()
	// Always instantiate the gatekeeper Router. It handles its own enablement toggle.
	semRouter, err := semanticrouter.New(ctx, a.cfg.SemanticRouter, modelProvider)
	if err != nil {
		return fmt.Errorf("failed to initialize semantic router gatekeeper: %w", err)
	}

	orchestratorOpts := []orchestrator.OrchestratorOption{
		orchestrator.WithToolwrapOptions(
			toolwrap.WithApprovalCacheTTL(a.cfg.HITL.CacheTTL),
			toolwrap.WithApproveList(a.approveList),
		),
		orchestrator.WithDisableResume(a.cfg.Persona.DisableResume),
		orchestrator.WithHalGuardConfig(a.cfg.HalGuard),
		orchestrator.WithSemanticRouter(semRouter),
	}

	// If a skill provider exists, we allow dynamic skills
	a.codeOwner, err = orchestrator.NewOrchestrator(
		ctx,
		modelProvider,
		a.toolRegistry,
		vectorStore,
		a.auditor,
		a.approvalStore,
		memorySvc,
		sessionStore,
		a.persona(ctx),
		orchestratorOpts...,
	)
	if err != nil {
		return fmt.Errorf("codeowner init: %w", err)
	}
	log.Info("CodeOwner agent initialized")

	// --- Activity report generator (for genie:report cron tasks) ---
	a.reportGenerator = activityreport.NewGenerator(a.cfg.AgentName, a.vectorStore, 0, a.auditor)

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
	bgExpert := messengeragui.NewChatHandler(a.codeOwner.Resume, chatHandler, a.codeOwner.InjectFeedback)
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
		// Built-in activity report: run report generator instead of the agent.
		var payload struct {
			TaskName   string `json:"task_name"`
			Action     string `json:"action"`
			Expression string `json:"expression"`
			Message    string `json:"message"`
		}
		if len(req.Payload) > 0 {
			_ = json.Unmarshal(req.Payload, &payload)
		}
		if payload.Action == activityreport.ActionReport && a.reportGenerator != nil {
			reportName := payload.TaskName
			if reportName == "" {
				reportName = "report"
			}
			runID := fmt.Sprintf("report-%d", time.Now().UnixNano())
			if err := a.reportGenerator.Generate(ctx, reportName, time.Now()); err != nil {
				return "", fmt.Errorf("activity report: %w", err)
			}
			return runID, nil
		}
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
			Capabilities: &messengeragui.CapabilitiesStance{
				ToolNames:     a.toolRegistry.ToolNames(),
				AlwaysAllowed: a.cfg.HITL.AlwaysAllowed,
				DeniedTools:   a.cfg.HITL.DeniedTools,
			},
			ApproveList: a.approveList,
			AgentName:   a.displayName(),
		})
		log.Info("AG-UI server configured on AGUI messenger")
	}

	// Connect the messenger — each adapter returns an optional http.Handler.
	// HTTP-push adapters (Teams, Google Chat, AGUI) return a non-nil handler
	// that needs to be mounted on the application's HTTP mux.
	// Outbound adapters (Slack Socket Mode, Discord, Telegram) return nil.
	messengerHandler, err := a.msgr.Connect(ctx)
	if err != nil {
		return fmt.Errorf("messenger connect: %w", err)
	}

	// Mount the handler returned by Connect() on an HTTP server.
	// HTTP-push adapters (AGUI, Teams, Google Chat) return a non-nil handler.
	// BindAddr defaults to empty; we use ":port" so the server is reachable from other hosts/containers (e.g. Docker, webhooks).
	// Set [messenger.agui] bind_addr to "127.0.0.1:port" for localhost-only binding.
	if messengerHandler != nil {
		addr := aguiCfg.BindAddr
		if addr == "" {
			addr = fmt.Sprintf(":%d", aguiCfg.Port)
		}
		wrapped := securityHeadersMiddleware(messengerHandler)
		if err := startHTTPServer(ctx, addr, wrapped); err != nil {
			return err
		}
	}

	// --- Messenger receive loop (external messengers only) ---
	if a.msgr.Platform() != messenger.PlatformAGUI {
		a.startMessengerLoop(ctx)
	}

	// --- Data sources sync (learn from Gmail, Calendar, etc. when user opted in during setup) ---
	if a.cfg.DataSources.Enabled && a.vectorStore != nil {
		a.replayWG.Add(1)
		go func() {
			defer a.replayWG.Done()
			a.runDataSourcesSync(ctx)
		}()
		log.Info("Data sources sync started; learning from your data in the background")
	}

	// --- Startup banner ---
	fmt.Fprintf(os.Stderr, "\n🧞 Genie starting\n")
	fmt.Fprintf(os.Stderr, "   Working directory: %s\n", a.workingDir)
	fmt.Fprintf(os.Stderr, "   Database:          %s\n", a.cfg.DBConfig.DisplayPath())
	fmt.Fprintf(os.Stderr, "   Audit log:         %s\n", a.auditPath)
	fmt.Fprintf(os.Stderr, "   Messenger:         %s\n", a.msgr.Platform())
	if a.msgr.Platform() == messenger.PlatformAGUI {
		fmt.Fprintf(os.Stderr, "   Health check:      http://localhost:%d/health\n", aguiCfg.Port)
		fmt.Fprintf(os.Stderr, "   Chat UI:           http://localhost:%d/ui/chat.html\n", aguiCfg.Port)
	}
	fmt.Fprintf(os.Stderr, "   Connection Info:   %s\n\n", a.msgr.ConnectionInfo())

	log.Info("Starting Genie", "version", config.Version)

	<-ctx.Done()

	// Graceful shutdown: stop cron, wait for background tasks.
	if err := cronScheduler.Stop(); err != nil {
		log.Warn("Failed to stop cron scheduler", "error", err)
	}
	bgWorker.WaitForCompletion()

	return nil
}

// securityHeadersMiddleware wraps a handler to add OWASP-recommended security
// response headers: X-Frame-Options (clickjacking), X-Content-Type-Options (MIME sniffing),
// and Referrer-Policy to limit referrer leakage.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// httpRunTimeout is the maximum duration for a single HTTP request (read + write).
// Must be long enough for long-lived flows (e.g. AG-UI SSE runs, sub-agents). Short
// timeouts (e.g. 15s) cancel the request context and cause "context canceled" / run failures.
const httpRunTimeout = 15 * time.Minute

// startHTTPServer binds to addr and serves handler until ctx is cancelled.
// It returns an error if the bind fails (e.g. address already in use).
// Caller must cancel ctx to stop the server. It obtains its logger from ctx
// via logger.GetLogger(ctx); do not pass a logger.
// The server uses ReadTimeout, WriteTimeout, and IdleTimeout to mitigate
// Slowloris and resource exhaustion (per Go security best practices).
func startHTTPServer(ctx context.Context, addr string, handler http.Handler) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("HTTP server: %w", err)
	}
	log := logger.GetLogger(ctx).With("fn", "startHTTPServer")
	srv := &http.Server{
		Handler:           handler,
		ReadTimeout:       httpRunTimeout,
		WriteTimeout:      httpRunTimeout,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Warn("HTTP server shutdown error", "error", err)
		}
	}()
	go func() {
		log.Info("Starting HTTP server", "addr", addr)
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Error("HTTP server error", "error", err)
		}
	}()
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

// secretProviderWithAudit returns a SecretProvider that audits every successful
// secret lookup (env or Manager). Used so the audit log records whenever a
// secret is accessed (compliance and debugging).
func (a *Application) secretProviderWithAudit(ctx context.Context) security.SecretProvider {
	if len(a.cfg.Security.Secrets) == 0 {
		return security.NewEnvProvider(security.WithSecretLookupAuditEnv(a.auditSecretLookup))
	}
	return security.NewManager(ctx, a.cfg.Security, security.WithSecretLookupAudit(a.auditSecretLookup))
}

// auditSecretLookup is the callback passed to Manager for secret-access auditing.
// It logs the logical secret name only; the value is never recorded.
func (a *Application) auditSecretLookup(ctx context.Context, req security.GetSecretRequest) {
	if a.auditor == nil {
		return
	}
	a.auditor.Log(ctx, audit.LogRequest{
		EventType: audit.EventSecretAccess,
		Actor:     "system",
		Action:    "secret_lookup",
		Metadata: map[string]any{
			"secret_name": req.Name,
			"reason":      req.Reason,
		},
	})
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
	if a.graphStore != nil {
		if err := a.graphStore.Close(ctx); err != nil {
			logger.Warn("failed to close graph store", "error", err)
		}
	}
	if a.db != nil {
		if err := geniedb.Close(a.db); err != nil {
			logger.Warn("failed to close database", "error", err)
		}
	}
}

// buildChatHandler returns a function matching the shape expected by
// agui.NewChatHandler. It bridges the AG-UI server to the
// underlying codeOwner.Chat() pipeline.
func (a *Application) buildChatHandler() func(ctx context.Context, message string, agentsMessage chan<- interface{}) error {
	return func(ctx context.Context, message string, agentsMessage chan<- interface{}) error {
		if a.cfg.AgentName != "" {
			ctx = audit.WithAgentName(ctx, a.cfg.AgentName)
			ctx = orchestratorcontext.WithAgent(ctx, orchestratorcontext.Agent{Name: a.cfg.AgentName})
		}

		// Stamp trace-level Langfuse tags so every AG-UI chat trace
		// carries the persona name for filtering/grouping in the dashboard.
		//
		// We use OTel Baggage so that the baggageBatchSpanProcessor in the
		// langfuse exporter automatically copies these onto EVERY span
		// created downstream (including trpc-agent-go internal spans).
		// Span attributes alone only work if this specific span happens to
		// be the Langfuse trace root, which is often not the case.
		ctx = withLangfuseTraceBaggage(ctx, a.displayName(), "agui")
		ctx = withPrincipalBaggage(ctx)

		principal := authcontext.GetPrincipal(ctx)
		ctx, aguiSpan := trace.Tracer.Start(ctx, a.displayName(), oteltrace.WithAttributes(
			attribute.String("langfuse.trace.name", a.displayName()),
			attribute.String("langfuse.trace.input", pii.Redact(message)),
			attribute.String("langfuse.user.id", principal.ID),
			attribute.StringSlice("langfuse.trace.tags", []string{
				a.displayName(),
				"agui",
			}),
		))
		defer aguiSpan.End()
		logger := logger.GetLogger(ctx).With("fn", "app.buildChatHandler")
		outputChan := make(chan string)
		chatDone := make(chan struct{})
		go func() {
			defer close(chatDone)
			defer close(outputChan)

			var tabCtx context.Context
			if a.browser != nil {
				var cancel context.CancelFunc
				var err error
				// Use the request context so tab creation respects client disconnects
				// and request cancellation (e.g. cross-origin fetch, navigation, or
				// quick disconnect).
				tabCtx, cancel, err = a.browser.NewTab(ctx)
				if err != nil {
					logger.Warn("failed to create browser tab", "error", err)
					tabCtx = nil
				} else {
					defer cancel()
					// Wire request cancellation so the tab is closed when the client disconnects.
					go func() {
						<-ctx.Done()
						cancel()
					}()
				}
			}

			if err := a.codeOwner.Chat(ctx, orchestrator.CodeQuestion{
				Question:    message,
				BrowserTab:  tabCtx,
				Attachments: messengeragui.AttachmentsFromContext(ctx),
			}, outputChan); err != nil {
				agui.EmitError(ctx, err, "while processing AG-UI chat message")
			}
		}()
		// For AG-UI, the reply is already streamed via EventAdapter (TEXT_MESSAGE_*).
		// Emitting the full output again would create a duplicate bubble in the chat UI.
		origin := messenger.MessageOriginFrom(ctx)
		skipEmitFullOutput := origin.Platform == messenger.PlatformAGUI
		for output := range outputChan {
			if output != "" && !skipEmitFullOutput {
				agui.EmitAgentMessage(ctx, orchestratorcontext.AgentFromContext(ctx).Name, output)
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

	// --- Secret provider (same as Bootstrap; audits lookups when [security.secrets] set) ---
	sp := a.sp

	providers := []tools.ToolProviders{
		websearch.NewToolProvider(a.cfg.WebSearch, websearch.WithSecretProvider(sp)),
		networking.NewToolProvider(),

		// --- Utility tools (stateless, no external dependencies) ---
		mathtool.NewToolProvider(),
		datetime.NewToolProvider(),
		encodetool.NewToolProvider(a.cfg.Security.Crypto),
		jsontool.NewToolProvider(),
		regextool.NewToolProvider(),
		webfetch.NewToolProvider(),
		youtubetranscript.NewToolProvider(),
		doctool.NewToolProvider(),
		codeskim.NewToolProvider(),
		ocrtool.NewToolProvider(),
		tools.Tools(sqltool.NewToolProvider(sp).GetTools("sql")),
		tools.Tools(calendar.NewToolProvider(sp).GetTools("google_calendar")),
	}

	// --- Google Contacts (conditional — only when OAuth credentials are available) ---
	if contactsSvc, err := contacts.NewFromSecretProvider(ctx, sp, "google_contacts"); err == nil {
		providers = append(providers, tools.Tools(contacts.NewToolProvider(contactsSvc).GetTools("google_contacts")))
		log.Info("Google Contacts tool provider added (OAuth)")
	}

	// --- MCP tools ---
	mcpClient, err := mcp.NewClient(ctx, a.cfg.MCP, mcp.WithSecretProvider(sp))
	if err != nil {
		log.Warn("failed to initialize MCP client, skipping MCP tools", "error", err)
	}
	if mcpClient != nil {
		a.mcpClient = mcpClient
		providers = append(providers, mcpClient) // *mcp.Client already satisfies ToolProviders
		log.Info("MCP client initialized", "server_count", len(a.cfg.MCP.Servers))
	}

	// --- Skills ---
	if len(a.cfg.SkillLoadConfig.SkillsRoots) != 0 {
		skillProvider, err := tools.NewSkillToolProvider(a.workingDir, a.cfg.SkillLoadConfig)
		if err != nil {
			log.Warn("failed to initialize skills tool provider", "error", err)
		} else {
			providers = append(providers, skillProvider)
			log.Info("Skills tool provider added")
		}
	}

	// --- Browser tools ---
	bw, err := browser.New(ctx,
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
			log.Warn("SCM health check failed, tools still registered", "error", vErr)
		}
		providers = append(providers, scm.NewToolProvider(scmSvc))
		log.Info("SCM tool provider added", "provider", a.cfg.SCM.Provider)
	} else if a.cfg.SCM.Provider != "" {
		log.Warn("failed to initialize SCM service, skipping SCM tools", "provider", a.cfg.SCM.Provider, "error", err)
	}

	// --- GitHub CLI (gh) tool ---
	if ghProvider := ghcli.New(ctx, a.cfg.GHCli); ghProvider != nil {
		providers = append(providers, ghProvider)
		log.Info("gh CLI tool provider added")
	}

	// --- PM tools ---
	if pmSvc, err := pm.New(a.cfg.ProjectManagement); err == nil {
		if vErr := pmSvc.Validate(ctx); vErr != nil {
			log.Warn("PM health check failed, tools still registered", "error", vErr)
		}
		providers = append(providers, pm.NewToolProvider(pmSvc))
		log.Info("PM tool provider added", "provider", a.cfg.ProjectManagement.Provider)
	} else if a.cfg.ProjectManagement.Provider != "" {
		log.Warn("failed to initialize PM service, skipping PM tools", "provider", a.cfg.ProjectManagement.Provider, "error", err)
	}

	// --- Email tools ---
	emailCfg := a.cfg.Email
	emailCfg.IMAPTLSConfig = a.cfg.Security.Crypto.TLSConfig()
	if emailSvc, err := emailCfg.New(); err == nil {
		if vErr := emailSvc.Validate(ctx); vErr != nil {
			log.Warn("Email health check failed, tools still registered", "error", vErr)
		}
		providers = append(providers, email.NewToolProvider(emailSvc))
		log.Info("Email tool provider added", "provider", a.cfg.Email.Provider)
	} else if a.cfg.Email.Provider != "" {
		log.Warn("failed to initialize email service, skipping email tools", "provider", a.cfg.Email.Provider, "error", err)
	}

	// --- Google Drive tools (same logged-in user token as Gmail/Calendar via secret provider) ---
	if gdSvc, err := gdrive.NewFromSecretProvider(ctx, sp); err == nil {
		if vErr := gdSvc.Validate(ctx); vErr != nil {
			log.Warn("Google Drive health check failed, tools still registered", "error", vErr)
		}
		providers = append(providers, tools.Tools(gdrive.NewToolProvider(gdSvc).GetTools("google_drive")))
		log.Info("Google Drive tool provider added (OAuth)")
	} else if a.cfg.GDrive.CredentialsFile != "" {
		log.Warn("failed to initialize Google Drive service, skipping gdrive tools", "error", err)
	}

	// --- Gmail tools (shared OAuth with calendar/contacts/drive) ---
	if gmailSvc, err := gmail.NewFromSecretProvider(ctx, sp); err == nil {
		if vErr := gmailSvc.Validate(ctx); vErr != nil {
			log.Warn("Gmail health check failed, tools still registered", "error", vErr)
		}
		providers = append(providers, tools.Tools(gmail.NewToolProvider(gmailSvc).GetTools("google_gmail")))
		log.Info("Gmail tool provider added (OAuth)")
	}

	// --- Google Tasks (shared OAuth with calendar/contacts/drive/gmail) ---
	if tasksSvc, err := tasks.NewFromSecretProvider(ctx, sp); err == nil {
		if vErr := tasksSvc.Validate(ctx); vErr != nil {
			log.Warn("Google Tasks health check failed, tools still registered", "error", vErr)
		}
		providers = append(providers, tools.Tools(tasks.NewToolProvider(tasksSvc).GetTools("google_tasks")))
		log.Info("Google Tasks tool provider added (OAuth)")
	}

	// --- Clarify tool ---
	// Clarify emitter bridges clarify → AG-UI + messenger.
	// Stays here because it references Application fields.
	// For AG-UI we only emit ClarificationRequestMsg (above); calling Send as well
	// would duplicate the question in the UI and duplicate "sending outgoing message" logs.
	emitter := func(ctx context.Context, evt clarify.ClarificationEvent) error {
		origin := messenger.MessageOriginFrom(ctx)

		agui.Emit(ctx, agui.ClarificationRequestMsg{
			Type:      agui.EventClarificationRequest,
			RequestID: evt.RequestID,
			Question:  evt.Question,
			Context:   evt.Context,
		})

		if a.msgr.Platform() != messenger.PlatformAGUI {
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
	providers = append(providers, tools.NewShellToolProvider(a.workingDir, a.sp, a.cfg.ShellTool))
	log.Debug("Shell tool provider added")

	// --- Summarizer tool ---
	// Note: Summarizer is not injected at the app level — sub-agents create
	// their own summarizers. This keeps the tool registry self-contained.

	// --- Vector memory tools ---
	if vectorStore != nil {
		providers = append(providers, vector.NewToolProvider(vectorStore, &a.cfg.VectorMemory))
		log.Debug("Vector memory tool provider added")
	}

	// --- Graph memory tools (interface-driven; only when graph store is enabled) ---
	if a.graphStore != nil {
		providers = append(providers, graph.NewToolProvider(a.graphStore))
		log.Debug("Graph memory tool provider added")
	}

	// --- Context management tools (Pensieve paradigm) ---
	if !a.cfg.DisablePensieve {
		providers = append(providers, tools.NewPensieveToolProvider())
		log.Debug("Context management tool provider added (Pensieve paradigm)")
	}

	// --- Build registry and filter denied tools ---
	return tools.NewRegistry(ctx, providers...).FilterDenied(ctx, a.cfg.HITL)
}

// graphLearnPrompt is the internal prompt used for the one-time graph-learn
// pass after the first successful data sources sync when the user opted in
// during setup. The agent uses memory_search and graph_* tools to populate
// the knowledge graph in an open-ended way; output is drained and not shown to the user.
//
//go:embed graph_learn_prompt.txt
var graphLearnPrompt string

// maybeRunGraphLearn checks for the graph-learn pending file written by setup.
// If present and the app has a graph store and orchestrator, it runs a one-off
// graph-learn pass in a goroutine (so the sync loop is not blocked). The file
// is removed immediately so only one pass can run even if called concurrently.
// ctx is used so the goroutine is cancelled when the app shuts down. AG-UI
// emissions are suppressed so the internal task does not leak into user UI.
func (a *Application) maybeRunGraphLearn(ctx context.Context) {
	if a.graphStore == nil || a.codeOwner == nil || strings.TrimSpace(a.cfg.AgentName) == "" {
		return
	}
	pendingPath := graph.PendingGraphLearnPath(a.cfg.AgentName)
	if _, err := os.Stat(pendingPath); err != nil {
		return
	}
	// Remove immediately so a concurrent caller won't also start a learn pass.
	if err := os.Remove(pendingPath); err != nil {
		logger.GetLogger(ctx).Warn("Could not remove graph learn pending file", "path", pendingPath, "error", err)
		return
	}
	go func() {
		learnCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		defer cancel()
		learnCtx = agui.WithSuppressEmit(learnCtx)
		log := logger.GetLogger(learnCtx).With("fn", "app.maybeRunGraphLearn")
		log.Info("Starting one-time graph learn from synced data")
		outputChan := make(chan string, 64)
		var learnErr error
		go func() {
			defer close(outputChan)
			learnErr = a.codeOwner.Chat(learnCtx, orchestrator.CodeQuestion{
				Question:           graphLearnPrompt,
				SkipClassification: true,
			}, outputChan)
			if learnErr != nil {
				log.Warn("Graph learn pass failed", "error", learnErr)
			}
		}()
		for range outputChan {
			// Drain output; not sent to any user.
		}
		if learnErr != nil {
			// Re-create pending file so the next successful sync can retry the learn pass.
			if wErr := os.WriteFile(pendingPath, []byte{}, 0o600); wErr != nil {
				log.Warn("Could not re-create graph learn pending file for retry", "path", pendingPath, "error", wErr)
			}
			return
		}
		log.Info("Graph learn pass completed")
	}()
}

// runDataSourcesSync runs the data sources sync loop when the user opted in
// during setup (Gmail, Calendar, Drive). It builds connectors from the app's
// secret provider, lists items per source, and upserts into the vector store
// so memory_search can return more relevant results. Runs until ctx is cancelled.
// On sync failures (e.g. rate limiting), exponential backoff is applied before
// the next run to avoid hammering external APIs.
func (a *Application) runDataSourcesSync(ctx context.Context) {
	cfg := &a.cfg.DataSources
	interval := cfg.SyncInterval
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	sp := a.sp
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	const maxBackoff = 15 * time.Minute
	consecutiveFailures := 0
	// Run one sync immediately, then on interval (with backoff on failures).
	hadErrors := a.runOneDataSourcesSync(ctx, cfg, sp)
	if !hadErrors {
		a.maybeRunGraphLearn(ctx)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hadErrors = a.runOneDataSourcesSync(ctx, cfg, sp)
			if hadErrors {
				consecutiveFailures++
				backoff := interval * time.Duration(1<<uint(consecutiveFailures))
				if backoff > maxBackoff || backoff <= 0 {
					backoff = maxBackoff
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
					// wait before next tick to avoid hammering APIs
				}
			} else {
				consecutiveFailures = 0
				a.maybeRunGraphLearn(ctx)
			}
		}
	}
}

// runOneDataSourcesSync runs a single sync pass: for each enabled source,
// list items (or only items updated since last sync when the connector
// supports ListItemsSince) and upsert into the vector store. Last sync time
// per source is persisted so the next run only fetches and re-embeds new data.
// Returns true if any source had list/upsert errors (caller may apply backoff).
func (a *Application) runOneDataSourcesSync(ctx context.Context, cfg *datasource.Config, sp security.SecretProvider) (hadErrors bool) { //nolint:cyclop // per-source switch is clear
	log := logger.GetLogger(ctx).With("fn", "app.runOneDataSourcesSync")
	names := cfg.EnabledSourceNames()
	if len(names) == 0 {
		return false
	}
	state, err := datasource.LoadSyncState(a.workingDir)
	if err != nil {
		log.Warn("data sources: could not load sync state (missing or invalid file), performing full sync for all sources", "error", err)
		hadErrors = true
	}
	if state == nil {
		state = make(datasource.SyncState)
	}
	for _, name := range names {
		scope := cfg.ScopeFromConfig(name)
		var conn datasource.DataSource
		switch name {
		case "gmail":
			gmailSvc, err := gmail.NewFromSecretProvider(ctx, sp)
			if err != nil {
				log.Warn("data sources: skip gmail, failed to create service", "error", err)
				hadErrors = true
				continue
			}
			conn = gmail.NewGmailConnector(gmailSvc)
		case "gdrive":
			gdSvc, err := gdrive.NewFromSecretProvider(ctx, sp)
			if err != nil {
				log.Warn("data sources: skip gdrive, failed to create service", "error", err)
				hadErrors = true
				continue
			}
			conn = gdrive.NewGDriveConnector(gdSvc)
		case "github", "gitlab":
			if a.cfg.SCM.Provider != name {
				log.Debug("data sources: skip SCM source, provider mismatch", "source", name, "provider", a.cfg.SCM.Provider)
				continue
			}
			scmSvc, err := scm.New(a.cfg.SCM)
			if err != nil {
				log.Warn("data sources: skip "+name+", failed to create SCM service", "error", err)
				hadErrors = true
				continue
			}
			conn = scm.NewSCMConnector(scmSvc, name)
		case "calendar":
			// Calendar connector requires *gcal.Service; calendar package does not expose
			// a constructor that returns it from SecretProvider. Skip until we add one.
			log.Debug("data sources: calendar sync not yet wired, skipping")
			continue
		default:
			log.Debug("data sources: sync not implemented for source, skipping", "source", name)
			continue
		}
		lastSync := state.LastSync(name)
		var items []datasource.NormalizedItem
		if inc, ok := conn.(datasource.ListItemsSince); ok && !lastSync.IsZero() {
			items, err = inc.ListItemsSince(ctx, scope, lastSync)
		} else {
			items, err = conn.ListItems(ctx, scope)
		}
		if err != nil {
			log.Warn("data sources: list failed", "source", name, "error", err)
			hadErrors = true
			continue
		}
		if len(items) == 0 {
			continue
		}
		keywords := cfg.SearchKeywordsTrimmed()
		batch := make([]vector.BatchItem, 0, len(items))
		for _, item := range items {
			if !datasource.ItemMatchesKeywords(&item, keywords) {
				continue
			}
			chunks := vector.ChunkTextForEmbedding(item.Content)
			if len(chunks) == 0 {
				continue
			}
			for i, chunkText := range chunks {
				meta := make(map[string]string, len(item.Metadata)+6)
				meta["source"] = item.Source
				meta["source_type"] = item.Source
				meta["source_ref_id"] = item.SourceRefID()
				meta["updated_at"] = item.UpdatedAt.Format(time.RFC3339)
				if len(chunks) > 1 {
					meta["chunk_index"] = fmt.Sprintf("%d", i)
					meta["chunk_total"] = fmt.Sprintf("%d", len(chunks))
				}
				for k, v := range item.Metadata {
					meta[k] = v
				}
				chunkID := item.ID
				if len(chunks) > 1 {
					chunkID = fmt.Sprintf("%s:chunk:%d", item.ID, i)
				}
				batch = append(batch, vector.BatchItem{ID: chunkID, Text: chunkText, Metadata: meta})
			}
		}
		if err := a.vectorStore.Upsert(ctx, batch...); err != nil {
			log.Warn("data sources: upsert failed", "source", name, "error", err)
			hadErrors = true
			continue
		}
		state.SetLastSync(name, time.Now().UTC())
		if saveErr := datasource.SaveSyncState(a.workingDir, state); saveErr != nil {
			log.Warn("data sources: could not save sync state", "source", name, "error", saveErr)
			hadErrors = true
		}
		log.Info("data sources: synced", "source", name, "items_listed", len(items), "chunks_upserted", len(batch))
	}
	return hadErrors
}

// startMessengerLoop starts listening for incoming messenger messages
// (Slack, Teams, Telegram, Google Chat, Discord) and dispatches them to
// the codeowner agent.
func (a *Application) startMessengerLoop(ctx context.Context) {
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
				go a.handleMessengerInput(context.WithoutCancel(ctx), msg)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// handleMessengerInput processes an incoming message from a remote messaging
// platform (Slack, Teams, Telegram, Google Chat, Discord). The response is
// sent back to the same channel/thread via messenger.Send().
func (a *Application) handleMessengerInput(ctx context.Context, msg messenger.IncomingMessage) {
	// Create a per-message drain channel. Each message gets its own
	// channel registered on the event bus so concurrent messages don't
	// overwrite each other's bus registrations.
	eventChan := make(chan interface{}, 100)
	go func() {
		for range eventChan {
			// drain — no UI consumer on the messenger path
		}
	}()
	// Route interactive actions (button clicks) through a dedicated handler
	// before any text-based processing. Interactions carry structured data
	// (ActionID, ActionValue) and should never be processed as text messages.
	if msg.Type == messenger.MessageTypeInteraction {
		a.handleInteractiveAction(ctx, msg)
		return
	}

	// Allow reaction-only messages through so they can be treated as yes/no
	// for pending clarifying questions (thumbs up = yes, thumbs down = no).
	if msg.Content.Text == "" && len(msg.Content.Attachments) == 0 && msg.Type != messenger.MessageTypeReaction {
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

	// Register a per-message drain channel on the event bus so downstream
	// code (HITL middleware, Emit* helpers) can find it via MessageOrigin.
	agui.Register(origin, eventChan)
	defer func() {
		agui.Deregister(origin)
		close(eventChan)
	}()

	// Check for pending clarification reply first.
	// Accept text answers or thumbs up/down on the clarifying message (yes/no).
	const clarifyMemoryType = "pending_clarification"
	if reqID, found, _ := a.shortMemory.Get(ctx, clarifyMemoryType, senderCtx); found {
		responseText := msg.Content.Text
		if msg.Type == messenger.MessageTypeReaction {
			switch msg.ReactionEmoji {
			case "👍":
				responseText = "yes"
			case "👎":
				responseText = "no"
			default:
				log.Debug("reaction on clarifying message ignored (use 👍 or 👎 for yes/no)",
					"emoji", msg.ReactionEmoji,
				)
				return
			}
		}
		if err := a.clarifyStore.Respond(reqID, responseText); err != nil {
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

	// Treat thumbs up/down on an approval notification message as approve/reject.
	// ReactedMessageID is set when the user reacts to the bot's approval message (e.g. Telegram, WhatsApp).
	if msg.Type == messenger.MessageTypeReaction && msg.ReactedMessageID != "" {
		approvalID, foundByReaction := a.notifierStore.GetPendingByMessageID(messengerCtx, msg.ReactedMessageID)
		if foundByReaction {
			outcome, ok := reactionEmojiToOutcome(msg.ReactionEmoji)
			if !ok {
				log.Debug("reaction on approval message ignored (use 👍 to approve or 👎 to reject)",
					"emoji", msg.ReactionEmoji,
				)
				return
			}
			a.resolveApprovalAndNotify(ctx, msg.Channel, msg.ThreadID, msg.ReactedMessageID, approvalID, senderCtx, msg.Sender.DisplayName, "", outcome, true, nil)
			return
		}
	}

	// Reaction-only messages with no pending clarification or approval are not processed as chat.
	if msg.Type == messenger.MessageTypeReaction {
		return
	}

	// Check for interactive HITL reply (Yes/No/Revisit, optionally with "allow for X min" / "when args contain X")
	parsed := parseApprovalTextReply(msg.Content.Text)

	// Try reply-to routing first: if the user replied to a specific
	// approval notification, resolve that exact approval.
	var approvalID string
	var found bool
	var usedReplyTo bool

	if qmid, ok := msg.Metadata[messenger.QuotedMessageID].(string); ok && qmid != "" {
		approvalID, found = a.notifierStore.GetPendingByMessageID(messengerCtx, qmid)
		usedReplyTo = found
	}
	// Fallback to FIFO (oldest pending for this sender).
	if !found {
		approvalID, found = a.notifierStore.GetPending(ctx, senderCtx)
	}

	if found {
		originalMsgID := ""
		if qmid, ok := msg.Metadata[messenger.QuotedMessageID].(string); ok && qmid != "" {
			originalMsgID = qmid
		}
		var addToApprove *approveListAfterResolve
		if parsed.IsApproval && (parsed.AllowForMins > 0 || len(parsed.AllowWhenArgsContain) > 0) {
			validMins := map[int]bool{5: true, 10: true, 30: true, 60: true}
			if validMins[parsed.AllowForMins] || len(parsed.AllowWhenArgsContain) > 0 {
				if parsed.AllowForMins <= 0 {
					parsed.AllowForMins = 10
				}
				approval, err := a.approvalStore.Get(ctx, approvalID)
				if err == nil {
					addToApprove = &approveListAfterResolve{
						ToolName:             approval.ToolName,
						AllowForMins:         parsed.AllowForMins,
						AllowWhenArgsContain: parsed.AllowWhenArgsContain,
					}
				}
			}
		}
		feedback := parsed.Feedback
		outcome := textReplyToOutcome(parsed.IsApproval, parsed.IsRejection, feedback)
		a.resolveApprovalAndNotify(ctx, msg.Channel, msg.ThreadID, originalMsgID, approvalID, senderCtx, msg.Sender.DisplayName, feedback, outcome, usedReplyTo, addToApprove)
		return
	}

	// Emit attributed user bubble
	senderLabel := fmt.Sprintf("%s (%s)", msg.Sender.DisplayName, msg.Platform)
	agui.EmitAgentMessage(ctx, senderLabel, msg.Content.Text)

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
		//
		// Set baggage so the baggageBatchSpanProcessor propagates tags
		// to all child spans (including trpc-agent-go internal spans).
		messengerCtx = withLangfuseTraceBaggage(messengerCtx, a.displayName(), string(msg.Platform), "messenger")
		messengerCtx = withPrincipalBaggage(messengerCtx)

		traceCtx, span := trace.Tracer.Start(messengerCtx, a.displayName(), oteltrace.WithAttributes(
			attribute.String("langfuse.trace.name", a.displayName()),
			attribute.String("langfuse.trace.input", pii.Redact(msg.Content.Text)),
			attribute.String("langfuse.user.id", msg.Sender.ID),
			attribute.String("langfuse.session.id", senderCtx),
			attribute.StringSlice("langfuse.trace.tags", []string{
				a.displayName(),
				string(msg.Platform),
				"messenger",
			}),
		))
		defer span.End()

		if a.cfg.AgentName != "" {
			traceCtx = audit.WithAgentName(traceCtx, a.cfg.AgentName)
			traceCtx = orchestratorcontext.WithAgent(traceCtx, orchestratorcontext.Agent{Name: a.cfg.AgentName})
		}

		var tabCtx context.Context
		if a.browser != nil {
			var cancel context.CancelFunc
			var err error
			// Use the request context so tab creation respects client disconnects
			// and request cancellation (e.g. client disconnect).
			tabCtx, cancel, err = a.browser.NewTab(ctx)
			if err != nil {
				agui.EmitError(ctx, err, "failed to create browser tab")
				return
			}
			defer cancel()
			go func() {
				<-ctx.Done()
				cancel()
			}()
		}

		if err := a.codeOwner.Chat(traceCtx, orchestrator.CodeQuestion{
			Question:    msg.Content.Text,
			BrowserTab:  tabCtx,
			Attachments: msg.Content.Attachments,
		}, agentThoughts); err != nil {
			agui.EmitError(ctx, err, "while processing messenger message")
		}
	}()

	for response := range agentThoughts {
		agui.EmitAgentMessage(ctx, orchestratorcontext.AgentFromContext(ctx).Name, response)

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

// handleInteractiveAction processes a MessageTypeInteraction incoming message
// (e.g. a Slack button click on an approval notification). It parses the
// ActionID to determine the action type (approve/reject/revisit) and resolves
// the corresponding approval. After resolution, it updates the original
// message to disarm the interactive buttons.
//
// ActionID conventions:
//   - "approve_{approvalID}" → approve the request
//   - "reject_{approvalID}"  → reject the request
//   - "revisit_{approvalID}" → reject with feedback (revisit)
//
// Without this handler, button clicks from FormatApproval-rendered messages
// would be silently discarded after the adapter acknowledges them.
func (a *Application) handleInteractiveAction(ctx context.Context, msg messenger.IncomingMessage) {
	log := logger.GetLogger(ctx).With("fn", "app.handleInteractiveAction")

	action := msg.Interaction
	if action == nil {
		log.Warn("received interaction message with nil InteractionData")
		return
	}

	log.Info("processing interactive action",
		"actionID", action.ActionID,
		"actionValue", action.ActionValue,
		"blockID", action.BlockID,
		"sender", msg.Sender.ID,
		"channel", msg.Channel.ID,
	)

	approvalID, outcome, feedback, ok := parseApprovalActionFromInteraction(action.ActionID, action.ActionValue, msg.Sender.DisplayName)
	if !ok {
		if approvalID == "" {
			log.Warn("interactive action has empty approval ID", "actionID", action.ActionID)
		} else {
			log.Debug("unrecognized interactive action, ignoring", "actionID", action.ActionID)
		}
		return
	}

	senderCtx := msg.String()
	a.resolveApprovalAndNotify(ctx, msg.Channel, msg.ThreadID, msg.ID, approvalID, senderCtx, msg.Sender.DisplayName, feedback, outcome, true, nil)
}

// parseApprovalActionFromInteraction parses ActionID and ActionValue from an
// interactive message (e.g. Slack button click) into an approval ID, outcome,
// and optional feedback. displayName is used in the reply text.
// Returns ok false when approvalID is empty or actionID is not approve_/reject_/revisit_.
func parseApprovalActionFromInteraction(actionID, actionValue, displayName string) (approvalID string, outcome approvalOutcome, feedback string, ok bool) {
	if actionValue == "" {
		return "", approvalOutcome{}, "", false
	}
	approvalID = actionValue
	switch {
	case strings.HasPrefix(actionID, "approve_"):
		return approvalID, approvalOutcome{hitl.StatusApproved, fmt.Sprintf("✅ **Approved** by %s", displayName)}, "", true
	case strings.HasPrefix(actionID, "reject_"):
		return approvalID, approvalOutcome{hitl.StatusRejected, fmt.Sprintf("❌ **Rejected** by %s", displayName)}, "", true
	case strings.HasPrefix(actionID, "revisit_"):
		feedback = "Sent back for revision via button click"
		return approvalID, approvalOutcome{hitl.StatusRejected, fmt.Sprintf("🔄 **Sent back for revision** by %s — %s", displayName, feedback)}, feedback, true
	default:
		return approvalID, approvalOutcome{}, "", false
	}
}

// approvalOutcome holds status and the reply text for a single resolution.
type approvalOutcome struct {
	Status    hitl.ApprovalStatus
	ReplyText string
}

var (
	approvalOutcomeApproved = approvalOutcome{hitl.StatusApproved, "✅ **Approved**"}
	approvalOutcomeRejected = approvalOutcome{hitl.StatusRejected, "❌ **Rejected**"}
)

func approvalOutcomeRevision(feedback string) approvalOutcome {
	return approvalOutcome{hitl.StatusRejected, "🔄 **Sent back for revision** — " + feedback}
}

// reactionEmojiToOutcome maps a reaction emoji to an approval outcome. ok is false for unknown emoji.
func reactionEmojiToOutcome(emoji string) (approvalOutcome, bool) {
	switch emoji {
	case "👍", "✅":
		return approvalOutcomeApproved, true
	case "👎":
		return approvalOutcomeRejected, true
	default:
		return approvalOutcome{}, false
	}
}

// textReplyToOutcome maps text reply (yes/no/feedback) to an approval outcome.
func textReplyToOutcome(isApproval, isRejection bool, feedback string) approvalOutcome {
	if isApproval {
		return approvalOutcomeApproved
	}
	if isRejection {
		return approvalOutcomeRejected
	}
	return approvalOutcomeRevision(feedback)
}

// parsedApprovalReply holds the result of parsing a messenger text reply for HITL.
type parsedApprovalReply struct {
	IsApproval           bool
	IsRejection          bool
	Feedback             string
	AllowForMins         int
	AllowWhenArgsContain []string
}

// parseApprovalTextReply parses a user's text reply to an approval request (e.g. from Slack/Telegram).
// It recognizes "yes"/"y"/"approve" (optionally with "allow for 5/10/30/60 min(s)" and "when args contain X"),
// and "no"/"n"/"reject". Returns approval/rejection flags, feedback for revision, and optional allow-list options.
func parseApprovalTextReply(text string) parsedApprovalReply {
	normalized := strings.TrimSpace(strings.ToLower(text))
	out := parsedApprovalReply{}

	// Exact rejection first.
	switch normalized {
	case "no", "n", "reject":
		out.IsRejection = true
		return out
	}

	// Exact approval.
	if normalized == "yes" || normalized == "y" || normalized == "approve" {
		out.IsApproval = true
		return out
	}

	// Approval with optional "allow for X min" / "when args contain X" in the same message.
	approvalPrefixes := []string{"yes ", "yes,", "y ", "y,", "approve ", "approve,"}
	for _, p := range approvalPrefixes {
		if strings.HasPrefix(normalized, p) {
			out.IsApproval = true
			rest := strings.TrimSpace(normalized[len(p):])
			out.AllowForMins, out.AllowWhenArgsContain = parseAllowOptionsFromReply(rest)
			return out
		}
	}
	// "allow for 10 mins" or "approve for 30 min" without leading yes
	if strings.HasPrefix(normalized, "allow for ") {
		mins, substrings := parseAllowOptionsFromReply(normalized[len("allow for "):])
		if mins > 0 || len(substrings) > 0 {
			out.IsApproval = true
			out.AllowForMins = mins
			out.AllowWhenArgsContain = substrings
			return out
		}
	}
	if strings.HasPrefix(normalized, "approve for ") {
		mins, substrings := parseAllowOptionsFromReply(normalized[len("approve for "):])
		if mins > 0 || len(substrings) > 0 {
			out.IsApproval = true
			out.AllowForMins = mins
			out.AllowWhenArgsContain = substrings
			return out
		}
	}

	// Otherwise treat as feedback (revisit).
	out.Feedback = text
	return out
}

// allowForMinsRe parses "5 min", "10 mins", "30 min", "60 mins" etc. from HITL reply text.
var allowForMinsRe = regexp.MustCompile(`(?:for\s+)?(5|10|30|60)\s*min(?:ute)?s?`)

// parseAllowOptionsFromReply extracts allowForMins (5/10/30/60) and allowWhenArgsContain from reply text.
// Parses the allow-for duration first, then extracts the "when args contain" segment so that text like
// "when args contain /tmp, allow for 10 mins" does not put "allow for 10 mins" into the args list.
func parseAllowOptionsFromReply(rest string) (allowForMins int, allowWhenArgsContain []string) {
	if m := allowForMinsRe.FindStringSubmatch(rest); len(m) > 1 {
		switch m[1] {
		case "5":
			allowForMins = 5
		case "10":
			allowForMins = 10
		case "30":
			allowForMins = 30
		case "60":
			allowForMins = 60
		}
	}
	// "when args contain X" or "only when args contain X" — take only the args segment (stop at " allow for " / " approve for ").
	for _, prefix := range []string{"when args contain ", "only when args contain "} {
		if idx := strings.Index(rest, prefix); idx >= 0 {
			after := strings.TrimSpace(rest[idx+len(prefix):])
			// Stop at next clause so "allow for 10 mins" is not included in args (e.g. "when args contain /tmp, allow for 10 mins").
			for _, stop := range []string{" allow for ", " approve for "} {
				if i := strings.Index(after, stop); i >= 0 {
					after = strings.TrimSpace(after[:i])
					break
				}
			}
			for _, s := range strings.Split(after, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					allowWhenArgsContain = append(allowWhenArgsContain, s)
				}
			}
			break
		}
	}
	return allowForMins, allowWhenArgsContain
}

// approveListAfterResolve is optional data to add the tool to the approve list only after Resolve succeeds.
// Used so the in-memory allowlist is not updated if persistence fails.
type approveListAfterResolve struct {
	ToolName             string
	AllowForMins         int
	AllowWhenArgsContain []string
}

// resolveApprovalAndNotify resolves the approval in the store, removes it from pending,
// and notifies the user (reaction on original message when possible, else reply text).
// usedReplyTo means the approval was identified by quoted message ID; when true,
// RemovePendingByApprovalID is used, otherwise RemovePending(senderCtx).
// If addToApprove is non-nil and Resolve succeeds, the tool is added to the approve list so
// future calls are auto-approved; adding only after success avoids leaving the tool
// auto-approved when the approval was not persisted.
func (a *Application) resolveApprovalAndNotify(ctx context.Context, ch messenger.Channel, threadID, originalMessageID, approvalID, senderCtx, resolvedBy, feedback string, outcome approvalOutcome, usedReplyTo bool, addToApprove *approveListAfterResolve) {
	err := a.notifierStore.Resolve(ctx, hitl.ResolveRequest{
		ApprovalID: approvalID,
		Decision:   outcome.Status,
		ResolvedBy: resolvedBy,
		Feedback:   feedback,
	})
	replyText := outcome.ReplyText
	if err != nil {
		logger.GetLogger(ctx).Error("failed to resolve approval", "error", err)
		replyText = fmt.Sprintf("⚠️ Failed to resolve: %s", err)
	}
	if err == nil {
		if addToApprove != nil && a.approveList != nil && addToApprove.AllowForMins > 0 {
			duration := time.Duration(addToApprove.AllowForMins) * time.Minute
			if len(addToApprove.AllowWhenArgsContain) > 0 {
				a.approveList.AddWithArgsFilter(addToApprove.ToolName, addToApprove.AllowWhenArgsContain, duration)
			} else {
				a.approveList.AddBlind(addToApprove.ToolName, duration)
			}
		}
		if usedReplyTo {
			a.notifierStore.RemovePendingByApprovalID(ctx, senderCtx, approvalID)
		} else {
			a.notifierStore.RemovePending(ctx, senderCtx)
		}
	}
	a.sendApprovalResolution(ctx, ch, threadID, originalMessageID, outcome.Status, err != nil, replyText)
}

// sendApprovalResolution notifies the user of an approval resolution: when the
// original approval message ID is known and the resolution was successful
// (approve or reject), it adds a checkmark or thumbs-down reaction to that
// message instead of sending a separate reply. Otherwise it sends replyText.
func (a *Application) sendApprovalResolution(ctx context.Context, ch messenger.Channel, threadID, originalMessageID string, status hitl.ApprovalStatus, hadError bool, replyText string) {
	if originalMessageID != "" && !hadError && (status == hitl.StatusApproved || status == hitl.StatusRejected) {
		emoji := "✅"
		if status == hitl.StatusRejected {
			emoji = "👎"
		}
		_, err := a.msgr.Send(ctx, messenger.SendRequest{
			Type:             messenger.SendTypeReaction,
			Channel:          ch,
			ThreadID:         threadID,
			ReplyToMessageID: originalMessageID,
			Emoji:            emoji,
		})
		if err == nil {
			return
		}
		logger.GetLogger(ctx).Debug("failed to add resolution reaction, falling back to text", "error", err)
	}
	if _, err := a.msgr.Send(ctx, messenger.SendRequest{
		Channel:  ch,
		ThreadID: threadID,
		Content:  messenger.MessageContent{Text: replyText},
	}); err != nil {
		logger.GetLogger(ctx).Warn("failed to send approval resolution text", "error", err)
	}
}

// truncateForLog shortens a string for log output.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// withLangfuseTraceBaggage stores `langfuse.trace.tags` in OTel Baggage so
// that the baggageBatchSpanProcessor (registered by the langfuse exporter)
// copies it onto every span created with this context.
//
// This is the recommended Langfuse propagation mechanism:
// https://langfuse.com/docs/integrations/opentelemetry#propagating-attributes
//
// The tags are joined with commas because OTel baggage values are strings.
// The langfuse exporter interprets the comma-separated value as an array.
//
// We use url.PathEscape (not QueryEscape) because OTel baggage treats `+`
// literally — QueryEscape encodes spaces as `+` which is NOT decoded back
// to spaces by the baggage spec, producing garbled values. PathEscape
// encodes spaces as `%20` which is correctly round-tripped.
// Note: PathEscape does not encode `/` — this is acceptable because tags
// and user IDs should not contain bare slashes.
func withLangfuseTraceBaggage(ctx context.Context, tags ...string) context.Context {
	value := url.PathEscape(strings.Join(tags, ","))
	member, err := baggage.NewMember("langfuse.trace.tags", value)
	if err != nil {
		logger.GetLogger(ctx).Warn("failed to create langfuse trace baggage member", "error", err, "tags", tags)
		return ctx
	}
	// Merge into existing baggage instead of replacing it, so upstream
	// baggage values (e.g. from incoming HTTP requests) are preserved.
	bag := baggage.FromContext(ctx)
	bag, err = bag.SetMember(member)
	if err != nil {
		logger.GetLogger(ctx).Warn("failed to set langfuse trace baggage", "error", err, "tags", tags)
		return ctx
	}
	return baggage.ContextWithBaggage(ctx, bag)
}

// withPrincipalBaggage injects the authenticated Principal's identity into
// OTel Baggage so that Langfuse traces carry user-level metadata on every
// span. This enables filtering and grouping by user ID, role, and
// authentication method in the Langfuse dashboard.
//
// Sets the following Langfuse-recognised keys:
//   - langfuse.user.id              → principal.ID
//   - langfuse.trace.metadata.user_name → principal.Name
//   - langfuse.trace.metadata.user_role → principal.Role
//   - langfuse.trace.metadata.auth_method → principal.AuthenticatedVia
//
// Only non-empty values are propagated. If the principal is the demo
// fallback, the keys are still set so Langfuse can distinguish
// unauthenticated traffic.
func withPrincipalBaggage(ctx context.Context) context.Context {
	p := authcontext.GetPrincipal(ctx)
	bag := baggage.FromContext(ctx)

	for _, kv := range []struct{ key, val string }{
		{"langfuse.user.id", p.ID},
		{"langfuse.trace.metadata.user_name", p.Name},
		{"langfuse.trace.metadata.user_role", p.Role},
		{"langfuse.trace.metadata.auth_method", p.AuthenticatedVia},
	} {
		if kv.val == "" {
			continue
		}
		m, err := baggage.NewMember(kv.key, url.PathEscape(kv.val))
		if err != nil {
			logger.GetLogger(ctx).Warn("failed to create principal baggage member",
				"key", kv.key, "error", err)
			continue
		}
		bag, err = bag.SetMember(m)
		if err != nil {
			logger.GetLogger(ctx).Warn("failed to set principal baggage member",
				"key", kv.key, "error", err)
			continue
		}
	}

	return baggage.ContextWithBaggage(ctx, bag)
}
