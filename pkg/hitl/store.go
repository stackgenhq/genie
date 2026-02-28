// Package hitl implements human-in-the-loop approval for non-readonly tool calls.
// When a mutating tool is invoked, execution pauses until a human approves or
// rejects the call. Without this package, the agent would execute all tool calls
// autonomously, which may be undesirable for destructive operations like file
// writes or shell commands.
package hitl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/memory/graph"
)

//go:generate go tool counterfeiter -generate

// ApprovalStatus represents the current state of an approval request.
type ApprovalStatus string

const (
	// StatusPending means the request is waiting for a human decision.
	StatusPending ApprovalStatus = "pending"
	// StatusApproved means the human approved the tool call.
	StatusApproved ApprovalStatus = "approved"
	// StatusRejected means the human rejected the tool call.
	StatusRejected ApprovalStatus = "rejected"
	// StatusExpired means the request was orphaned by a server restart and
	// automatically expired during startup recovery.
	StatusExpired ApprovalStatus = "expired"
)

func (a ApprovalStatus) String() string {
	return string(a)
}

// ApprovalRequest represents a pending or resolved tool approval.
// Each non-readonly tool call creates one ApprovalRequest before execution.
// Without this struct the system would have no way to track or persist
// individual approval decisions across requests.
type ApprovalRequest struct {
	ID            string         `json:"id"`
	ThreadID      string         `json:"thread_id"`
	RunID         string         `json:"run_id"`
	TenantID      string         `json:"tenant_id,omitempty"`
	ToolName      string         `json:"tool_name"`
	Args          string         `json:"args"`
	Status        ApprovalStatus `json:"status"`
	Feedback      string         `json:"feedback,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	ExpiresAt     *time.Time     `json:"expires_at,omitempty"`
	ResolvedAt    *time.Time     `json:"resolved_at,omitempty"`
	ResolvedBy    string         `json:"resolved_by,omitempty"`
	SenderContext string         `json:"sender_context,omitempty"`
	Question      string         `json:"question,omitempty"`
}

func (a ApprovalRequest) String() string {
	// Pretty-print JSON args for readability.
	prettyArgs := a.Args
	if json.Valid([]byte(a.Args)) {
		var buf bytes.Buffer
		if err := json.Indent(&buf, []byte(a.Args), "", "  "); err == nil {
			prettyArgs = buf.String()
		}
	}
	var sb strings.Builder
	sb.WriteString("⚠️ **Approval Required**\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Fprintf(&sb, "🔧 **Tool**: `%s`\n\n", a.ToolName)

	if a.Feedback != "" {
		fmt.Fprintf(&sb, "💡 **Why**: %s\n\n", a.Feedback)
	}

	sb.WriteString("📋 **Arguments**:\n")
	sb.WriteString("```\n")
	sb.WriteString(prettyArgs)
	sb.WriteString("\n```\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString("Reply **Yes** to approve, **No** to reject, or send any other message as feedback to have the agent revisit its approach. On platforms that support reactions, you can also react with 👍 to approve or 👎 to reject.")
	return sb.String()
}

// CreateRequest contains the fields needed to create a new approval request.
type CreateRequest struct {
	ThreadID      string
	RunID         string
	TenantID      string
	ToolName      string
	Args          string
	SenderContext string // originating sender (e.g. "slack:U123:C456")
	Question      string // original user question — needed for replay-on-resume
}

// ReplayableApproval describes a recovered pending approval that can be
// replayed through the chat handler once it's resolved.
type ReplayableApproval struct {
	ApprovalID    string
	Question      string
	SenderContext string
}

// RecoverResult holds the outcome of RecoverPending.
type RecoverResult struct {
	Expired    int                  // approvals that were too old and marked expired
	Recovered  int                  // approvals that had waiter channels re-registered
	Replayable []ReplayableApproval // recent approvals with a saved question, eligible for replay
}

// ResolveRequest contains the fields needed to resolve (approve/reject) a request.
type ResolveRequest struct {
	ApprovalID string
	Decision   ApprovalStatus
	ResolvedBy string
	Feedback   string
}

// ApprovalStore is the interface for persisting and coordinating tool approvals.
// Implementations must be safe for concurrent use.
//
//counterfeiter:generate . ApprovalStore
type ApprovalStore interface {
	// Create persists a new pending approval and returns it with a generated ID.
	Create(ctx context.Context, req CreateRequest) (ApprovalRequest, error)

	// Resolve updates the approval's status to approved or rejected.
	// It unblocks any goroutine waiting in WaitForResolution for the same ID.
	Resolve(ctx context.Context, req ResolveRequest) error

	// WaitForResolution blocks until the given approval is resolved or the
	// context is cancelled. Returns the resolved ApprovalRequest.
	WaitForResolution(ctx context.Context, approvalID string) (ApprovalRequest, error)

	// RecoverPending handles approvals left in "pending" state after a restart.
	// Approvals older than maxAge are marked as expired; recent ones get their
	// waiter channels re-registered so they can still be resolved via the API.
	RecoverPending(ctx context.Context, maxAge time.Duration) (RecoverResult, error)

	// ListPending returns all approval requests currently in "pending" state
	// whose deadline has not expired.
	// Without this, external systems (e.g. the GUILD API) would have no way
	// to discover which tool calls are waiting for human approval.
	ListPending(ctx context.Context) ([]ApprovalRequest, error)

	// Get returns the approval request by ID (pending or resolved).
	// Used by the approve endpoint to read tool name and args when adding to the approve list.
	Get(ctx context.Context, approvalID string) (ApprovalRequest, error)

	// ExpireStale marks all pending approvals past their expires_at as expired.
	// Returns the number of rows affected. Called periodically by a background
	// reaper goroutine.
	ExpireStale(ctx context.Context) (int64, error)

	// Close releases any resources held by the store.
	Close() error

	IsAllowed(toolName string) bool
}

// defaultReadOnlyTools lists tool names that are auto-approved (no HITL gate).
// Includes read-only tools and selected orchestration/mutation tools (e.g. create_agent,
// graph_store_entity) that are intentionally exempt. Excluded from this list:
// send_message, sql_sql_query (writes), browser_navigate, delete_context, note (mutable).
var defaultReadOnlyTools = []string{
	"ask_clarifying_question",
	"assist_with_regular_expressions",
	"browser_read_html",
	"browser_read_text",
	"calculator",
	"check_budget",
	"code_skim",
	"datetime",
	"email_read",
	"encode_string",
	"google_calendar_find_time",
	"google_calendar_free_busy",
	"google_calendar_list_events",
	"google_calendar_next_events",
	"google_contacts_list_contacts",
	"google_contacts_search_contacts",
	"google_drive_get_file",
	"google_drive_list_folder",
	"google_drive_read_file",
	"google_drive_search",
	"google_gmail_get_message",
	"google_gmail_list_messages",
	"google_tasks_list_task_lists",
	"google_tasks_list_tasks",
	"list_file",
	"list_skills",
	"math",
	"memory_search",
	"ocr_extract_text",
	"parse_document",
	"read_file",
	"read_multiple_files",
	"read_notes",
	"search_content",
	"search_file",
	"search_runbook",
	"skill_load",
	"util_json",
	"web_fetch",
	"web_search",
	"wikipedia_search",
	"youtube_transcript",
	"create_agent",
	graph.StoreEntityToolName,
	graph.StoreRelationToolName,
	graph.GraphQueryToolName,
	graph.GetEntityToolName,
	graph.ShortestPathToolName,
}
