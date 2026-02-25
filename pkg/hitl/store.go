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

	"github.com/stackgenhq/genie/pkg/clarify"
	"github.com/stackgenhq/genie/pkg/messenger"
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
	ToolName      string         `json:"tool_name"`
	Args          string         `json:"args"`
	Status        ApprovalStatus `json:"status"`
	Feedback      string         `json:"feedback,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	ResolvedAt    *time.Time     `json:"resolved_at,omitempty"`
	ResolvedBy    string         `json:"resolved_by,omitempty"`
	SenderContext string         `json:"sender_context,omitempty"` // originating sender for replay
	Question      string         `json:"question,omitempty"`       // original user question for replay
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
	sb.WriteString("Reply **Yes** to approve, **No** to reject, or send any other message as feedback to have the agent revisit its approach.")
	return sb.String()
}

// CreateRequest contains the fields needed to create a new approval request.
type CreateRequest struct {
	ThreadID      string
	RunID         string
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

	// ListPending returns all approval requests currently in "pending" state.
	// Without this, external systems (e.g. the GUILD API) would have no way
	// to discover which tool calls are waiting for human approval.
	ListPending(ctx context.Context) ([]ApprovalRequest, error)

	// Close releases any resources held by the store.
	Close() error

	IsAllowed(toolName string) bool
}

// defaultReadOnlyTools is the set of tool names that do NOT require human approval.
// These tools only read data and have no side effects.
var defaultReadOnlyTools = []string{
	"read_file",
	"list_file",
	"read_multiple_files",
	"memory_search",
	"summarize_content",
	"web_search",
	"search_content",
	"search_runbook",
	"search_file",
	messenger.ToolName,
	"create_agent",   // orchestration — sub-agent tools get their own HITL
	clarify.ToolName, // informational — asks user a question, no side effects
	"check_budget",   // Pensieve — pure read, reports context event counts
	"read_notes",     // Pensieve — pure read, lists saved notes
}
