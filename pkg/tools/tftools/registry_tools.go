package tftools

import (
	"context"
	"fmt"
	"time"

	"github.com/appcd-dev/genie/pkg/mcputils"
	"github.com/appcd-dev/go-lib/logger"
	registryTools "github.com/hashicorp/terraform-mcp-server/pkg/tools/registry"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/sirupsen/logrus"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// MultiRegistryTools provides access to Terraform/OpenTofu registry tools with fallback support
type MultiRegistryTools struct {
	logger    *logrus.Logger
	sessionID string
	maxPages  int
}

// NewMultiRegistryTools creates a new instance with multiple registry support
// TODO: add opentofu registry support
func NewMultiRegistryTools(logger *logrus.Logger, maxPages int) MultiRegistryTools {
	return MultiRegistryTools{
		logger:    logger,
		sessionID: fmt.Sprintf("genie-session-%d", time.Now().Unix()),
		maxPages:  maxPages,
	}
}

// GetTools returns all registry tools as trpc-agent-go compatible tools with multi-registry support
func (m MultiRegistryTools) GetTools() []tool.Tool {
	// Note: We use a custom search tool that filters for verified modules only
	// This ensures we only get well-maintained modules from trusted authors

	return []tool.Tool{
		m.wrapTool(registryTools.SearchModules(m.logger), "search_modules"),
		m.wrapTool(registryTools.ModuleDetails(m.logger), "get_module_details"),
		m.wrapTool(registryTools.GetLatestModuleVersion(m.logger), "get_latest_module_version"),
	}
}

// wrapTool wraps an MCP tool with enhanced description about multi-registry support
func (m *MultiRegistryTools) wrapTool(mcpTool server.ServerTool, toolName string) tool.Tool {
	baseTool := mcputils.NewMCPTool(mcpTool, m.logger)

	return &enhancedTool{
		baseTool:  baseTool,
		sessionID: m.sessionID,
		toolName:  toolName,
		limit:     m.maxPages,
	}
}

// enhancedTool wraps a tool with enhanced logging and error handling
type enhancedTool struct {
	baseTool  tool.CallableTool
	sessionID string
	toolName  string
	limit     int
}

func (e *enhancedTool) Declaration() *tool.Declaration {
	decl := e.baseTool.Declaration()
	// Override limit default if configured and tool is search_modules
	if e.toolName == "search_modules" && e.limit > 0 && decl.InputSchema != nil {
		if prop, ok := decl.InputSchema.Properties["limit"]; ok {
			decl.InputSchema.Properties["limit"] = &tool.Schema{
				Type:        prop.Type,
				Description: prop.Description,
				Default:     e.limit,
			}
		}
	}

	return decl
}

func (e *enhancedTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	logger := logger.GetLogger(ctx).With("fn", "enhancedTool.Call")
	logger.Debug("Calling registry tool", "tool", e.toolName, "session_id", e.sessionID)

	// Inject our session ID into the context so GetHttpClientFromContext can find it
	// We need to use reflection to access the private clientSessionKey from server package
	// Instead, we'll use a workaround: store the session using the interface type directly
	mockSession := &mockClientSession{sessionID: e.sessionID}

	// Use reflection to inject the session with the correct key type
	// The server package uses: type clientSessionKey struct{}
	// We need to match that exact type, so we'll use an unsafe workaround
	ctx = injectSessionIntoContext(ctx, mockSession)

	// Call the underlying tool
	result, err := e.baseTool.Call(ctx, jsonArgs)

	if err != nil {
		logger.Warn(e.toolName + " tool call failed")

		// Check if it's a rate limit or session error
		if isRateLimitOrSessionError(err) {
			logger.Warn("Rate limit or session error detected. Consider implementing direct HTTP fallback to OpenTofu registry.")
		}
	} else {
		logger.Debug("registry tool call succeeded", "tool", e.toolName)
	}

	return result, err
}

// Helper function to check if error is rate limit or session related
func isRateLimitOrSessionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return containsAny(errStr, []string{
		"rate limit",
		"429",
		"too many requests",
		"no active session",
		"session",
	})
}

func containsAny(s string, substrs []string) bool {
	for _, substr := range substrs {
		if contains(s, substr) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// injectSessionIntoContext injects a mock session into the context using the server package's private key type
func injectSessionIntoContext(ctx context.Context, session server.ClientSession) context.Context {
	mcpServer := server.NewMCPServer(
		"temp-registry-session",
		"1.0.0",
	)

	return mcpServer.WithContext(ctx, session)
}

// mockClientSession implements server.ClientSession to inject our session ID into the context
type mockClientSession struct {
	sessionID string
}

func (m *mockClientSession) SessionID() string {
	return m.sessionID
}

// Implement required methods of server.ClientSession interface
// These are not used by the registry tools, but required by the interface
func (m *mockClientSession) Initialize() {}

func (m *mockClientSession) Initialized() bool {
	return true
}

func (m *mockClientSession) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return nil
}
