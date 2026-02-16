// Package hitl implements human-in-the-loop approval for non-readonly tool calls.
// When a mutating tool is invoked, execution pauses until a human approves or
// rejects the call. Without this package, the agent would execute all tool calls
// autonomously, which may be undesirable for destructive operations like file
// writes or shell commands.
package hitl

import (
	"context"
	"time"
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
)

// ApprovalRequest represents a pending or resolved tool approval.
// Each non-readonly tool call creates one ApprovalRequest before execution.
// Without this struct the system would have no way to track or persist
// individual approval decisions across requests.
type ApprovalRequest struct {
	ID         string         `json:"id"`
	ThreadID   string         `json:"thread_id"`
	RunID      string         `json:"run_id"`
	ToolName   string         `json:"tool_name"`
	Args       string         `json:"args"`
	Status     ApprovalStatus `json:"status"`
	Feedback   string         `json:"feedback,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	ResolvedAt *time.Time     `json:"resolved_at,omitempty"`
	ResolvedBy string         `json:"resolved_by,omitempty"`
}

// CreateRequest contains the fields needed to create a new approval request.
type CreateRequest struct {
	ThreadID string
	RunID    string
	ToolName string
	Args     string
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
	"search_file",
	"create_agent", // orchestration — sub-agent tools get their own HITL
}
