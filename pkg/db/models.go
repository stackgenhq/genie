package db

import (
	"fmt"
	"time"

	"github.com/adhocore/gronx"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CronStatus string

const (
	CronStatusRunning CronStatus = "running"
	CronStatusSuccess CronStatus = "success"
	CronStatusFailed  CronStatus = "failed"
)

// Approval is the GORM model for the approvals table.
// Each non-readonly tool call creates one Approval row before execution.
type Approval struct {
	ID            string     `gorm:"primaryKey;type:text" json:"id"`
	ThreadID      string     `gorm:"type:text;not null;index:idx_approvals_thread" json:"thread_id"`
	RunID         string     `gorm:"type:text;not null" json:"run_id"`
	ToolName      string     `gorm:"type:text;not null" json:"tool_name"`
	Args          string     `gorm:"type:text;not null;default:''" json:"args"`
	Status        string     `gorm:"type:text;not null;default:'pending';index:idx_approvals_status" json:"status"`
	CreatedAt     time.Time  `gorm:"not null" json:"created_at"`
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy    string     `gorm:"type:text;default:''" json:"resolved_by,omitempty"`
	Feedback      string     `gorm:"type:text;default:''" json:"feedback,omitempty"`
	SenderContext string     `gorm:"type:text;default:''" json:"sender_context,omitempty"` // who sent the original request (e.g. "slack:U12345:C67890")
	Question      string     `gorm:"type:text;default:''" json:"question,omitempty"`       // original user question for replay-on-resume
}

// TableName overrides the default GORM table name.
func (Approval) TableName() string {
	return "approvals"
}

// Memory is the GORM model for the memories table.
// It stores conversation history and episodic memory.
type Memory struct {
	ID        uuid.UUID `gorm:"primaryKey;type:text" json:"id"`
	AppName   string    `gorm:"type:text;not null;index:idx_memories_key" json:"app_name"`
	UserID    string    `gorm:"type:text;not null;index:idx_memories_key" json:"user_id"`
	Content   string    `gorm:"type:text;not null" json:"content"`
	Topics    string    `gorm:"type:text;not null;default:'[]'" json:"topics"` // JSON encoded string array
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
}

// TableName overrides the default GORM table name.
func (Memory) TableName() string {
	return "memories"
}

// CronTask is the GORM model for the cron_tasks table.
// Each row represents a recurring task that the scheduler evaluates.
// Tasks can originate from configuration or be created dynamically
// via the create_recurring_task tool.
type CronTask struct {
	ID              uuid.UUID  `gorm:"primaryKey;type:text" json:"id"`
	Name            string     `gorm:"type:text;not null;uniqueIndex" json:"name"`
	Expression      string     `gorm:"type:text;not null" json:"expression"`
	Action          string     `gorm:"type:text;not null" json:"action"`
	Enabled         bool       `gorm:"not null;default:true" json:"enabled"`
	Source          string     `gorm:"type:text;not null;default:'config'" json:"source"` // "config" or "tool"
	LastTriggeredAt *time.Time `json:"last_triggered_at,omitempty"`                       // last successful dispatch
	NextRunAt       *time.Time `gorm:"index:idx_cron_tasks_next_run" json:"next_run_at"`  // pre-computed next due time
	CreatedAt       time.Time  `gorm:"not null" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"not null" json:"updated_at"`
}

func (c CronTask) String() string {
	return fmt.Sprintf("%s:%s:%s", c.Name, c.Expression, c.Action)
}

func (c *CronTask) BeforeCreate(tx *gorm.DB) (err error) {
	c.ID = uuid.NewSHA1(uuid.Nil, []byte(c.Name))
	c.CreatedAt = time.Now()
	c.UpdatedAt = time.Now()
	gron := gronx.New()
	if !gron.IsValid(c.Expression) {
		return fmt.Errorf("invalid cron expression %q", c.Expression)
	}
	return nil
}

// TableName overrides the default GORM table name.
func (CronTask) TableName() string {
	return "cron_tasks"
}

// CronHistory is the GORM model for the cron_history table.
// Every cron execution is logged here for audit and health-check purposes.
type CronHistory struct {
	ID         uuid.UUID  `gorm:"primaryKey;type:text" json:"id"`
	TaskID     uuid.UUID  `gorm:"type:text;not null;index:idx_cron_history_task" json:"task_id"`
	TaskName   string     `gorm:"type:text;not null" json:"task_name"`
	StartedAt  time.Time  `gorm:"not null" json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Status     CronStatus `gorm:"type:text;not null;default:'running';index:idx_cron_history_status" json:"status"` // running, success, failed
	Error      string     `gorm:"type:text;default:''" json:"error,omitempty"`
	RunID      string     `gorm:"type:text;default:''" json:"run_id,omitempty"` // BackgroundWorker run ID
}

// TableName overrides the default GORM table name.
func (CronHistory) TableName() string {
	return "cron_history"
}

// ShortMemory is the GORM model for the short_memories table.
// It is a generic, TTL-bounded key-value store that different subsystems
// can use for short-lived data by setting a unique MemoryType. This avoids
// creating per-feature tables for transient data (e.g. reaction ledger,
// cooldown trackers, pending confirmations).
type ShortMemory struct {
	// Key is the primary lookup key (e.g. a message ID for reaction correlation).
	Key string `gorm:"primaryKey;type:text" json:"key"`
	// MemoryType logically separates different subsystems sharing this table
	// (e.g. "reaction_ledger", "cooldown_tracker").
	MemoryType string `gorm:"primaryKey;type:text;index:idx_short_memory_type" json:"memory_type"`
	// Value stores the subsystem-specific data as JSON.
	Value string `gorm:"type:text;not null" json:"value"`
	// ExpiresAt is when this entry should be considered expired.
	// Queries should filter WHERE expires_at > NOW().
	ExpiresAt time.Time `gorm:"not null;index:idx_short_memory_expires" json:"expires_at"`
	// CreatedAt tracks when the entry was first created.
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
}

// TableName overrides the default GORM table name.
func (ShortMemory) TableName() string {
	return "short_memories"
}

// ClarifyStatus represents the lifecycle state of a [Clarification] row.
type ClarifyStatus string

const (
	// ClarifyStatusPending indicates the question is waiting for a user answer.
	ClarifyStatusPending ClarifyStatus = "pending"
	// ClarifyStatusAnswered indicates the user has provided an answer.
	ClarifyStatusAnswered ClarifyStatus = "answered"
	// ClarifyStatusExpired indicates the question was not answered within the
	// allowed window and was expired during startup recovery.
	ClarifyStatusExpired ClarifyStatus = "expired"
)

// Clarification is the GORM model for the "clarifications" table.
// Each invocation of the ask_clarifying_question tool creates one row.
// Rows are durable across server restarts, enabling [clarify.RecoverPending]
// to re-register waiter channels for questions still awaiting answers.
type Clarification struct {
	ID            uuid.UUID     `gorm:"primaryKey;type:text" json:"id"`
	Question      string        `gorm:"type:text;not null" json:"question"`                                                 // the question shown to the user
	Context       string        `gorm:"type:text;default:''" json:"context,omitempty"`                                      // optional LLM-provided context
	Answer        string        `gorm:"type:text;default:''" json:"answer,omitempty"`                                       // the user's response (empty until answered)
	Status        ClarifyStatus `gorm:"type:text;not null;default:'pending';index:idx_clarifications_status" json:"status"` // pending → answered | expired
	SenderContext string        `gorm:"type:text;default:''" json:"sender_context,omitempty"`                               // opaque platform sender ID (e.g. "slack:U1234:C5678")
	CreatedAt     time.Time     `gorm:"not null" json:"created_at"`
	AnsweredAt    *time.Time    `json:"answered_at,omitempty"` // set when status transitions to answered or expired
}

// TableName overrides the default GORM table name.
func (Clarification) TableName() string {
	return "clarifications"
}
