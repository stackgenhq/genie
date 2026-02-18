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
