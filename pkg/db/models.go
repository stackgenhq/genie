package db

import (
	"time"

	"github.com/google/uuid"
)

// Approval is the GORM model for the approvals table.
// Each non-readonly tool call creates one Approval row before execution.
type Approval struct {
	ID         string     `gorm:"primaryKey;type:text" json:"id"`
	ThreadID   string     `gorm:"type:text;not null;index:idx_approvals_thread" json:"thread_id"`
	RunID      string     `gorm:"type:text;not null" json:"run_id"`
	ToolName   string     `gorm:"type:text;not null" json:"tool_name"`
	Args       string     `gorm:"type:text;not null;default:''" json:"args"`
	Status     string     `gorm:"type:text;not null;default:'pending';index:idx_approvals_status" json:"status"`
	CreatedAt  time.Time  `gorm:"not null" json:"created_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy string     `gorm:"type:text;default:''" json:"resolved_by,omitempty"`
	Feedback   string     `gorm:"type:text;default:''" json:"feedback,omitempty"`
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
