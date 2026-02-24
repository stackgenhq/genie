package cron

import (
	"context"
	"fmt"
	"time"

	"github.com/adhocore/gronx"
	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// createRecurringTaskRequest is the input schema for the create_recurring_task tool.
type createRecurringTaskRequest struct {
	Name       string `json:"name" jsonschema:"description=Human-readable name for this recurring task,required"`
	Expression string `json:"expression" jsonschema:"description=Cron expression (e.g. '*/5 * * * *' for every 5 minutes or '0 9 * * *' for daily at 9 AM),required"`
	Action     string `json:"action" jsonschema:"description=What the agent should do each time this task triggers. This is sent as a prompt to the agent,required"`
}

// createRecurringTaskResponse is the output of the create_recurring_task tool.
type createRecurringTaskResponse struct {
	TaskID     string `json:"task_id"`
	Name       string `json:"name"`
	Expression string `json:"expression"`
	NextRunAt  string `json:"next_run_at"`
	Status     string `json:"status"`
	Message    string `json:"message"`
}

// createRecurringTaskTool holds the dependencies for the tool.
type createRecurringTaskTool struct {
	store ICronStore
}

// NewCreateRecurringTaskTool creates a tool that allows LLM agents to
// dynamically schedule recurring tasks. The tool validates the cron
// expression, persists the task to the database, computes the first
// NextRunAt, and the DB ticker will pick it up on the next cycle.
func NewCreateRecurringTaskTool(store ICronStore) tool.Tool {
	t := &createRecurringTaskTool{store: store}
	return function.NewFunctionTool(
		t.execute,
		function.WithName("create_recurring_task"),
		function.WithDescription(
			"Create a recurring scheduled task. The task will run automatically "+
				"based on the provided cron expression. Each execution sends the action "+
				"as a prompt to the Genie agent. Use standard cron expressions "+
				"(minute hour day-of-month month day-of-week). "+
				"Examples: '*/5 * * * *' (every 5 min), '0 9 * * *' (daily 9 AM), "+
				"'0 0 * * 1' (every Monday midnight)."),
	)
}

// execute handles the create_recurring_task tool invocation.
func (t *createRecurringTaskTool) execute(ctx context.Context, req createRecurringTaskRequest) (createRecurringTaskResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "createRecurringTaskTool.execute")

	// Persist the task (upserts on name conflict).
	task, err := t.store.CreateTask(ctx, CreateTaskRequest{
		Name:       req.Name,
		Expression: req.Expression,
		Action:     req.Action,
		Source:     "tool",
	})
	if err != nil {
		return createRecurringTaskResponse{}, fmt.Errorf("failed to create recurring task: %w", err)
	}

	// Compute and persist the first NextRunAt so the ticker picks it up.
	// If this fails, warn but don't error — the scheduler's Start() method
	// will compute NextRunAt for tasks that are missing it, avoiding orphans.
	nextTick, err := gronx.NextTick(req.Expression, false)
	if err == nil {
		if setErr := t.store.SetNextRun(ctx, task.ID, nextTick); setErr != nil {
			logr.Warn("Task created but failed to schedule next run — scheduler will retry",
				"task", task.Name, "error", setErr)
		}
	}

	nextRunStr := ""
	if err == nil {
		nextRunStr = nextTick.Format(time.RFC3339)
	}

	// Determine if this was a new creation or an upsert of an existing task.
	status := "created"
	if task.UpdatedAt.After(task.CreatedAt) {
		status = "updated_existing"
	}

	msg := fmt.Sprintf(
		"SUCCESS: Recurring task '%s' is now scheduled with cron expression '%s'. "+
			"Next execution at %s. Do NOT call this tool again — the task is already persisted and the scheduler will execute it automatically.",
		task.Name, task.Expression, nextRunStr)

	logr.Info("Recurring task created", "name", task.Name, "expression", task.Expression, "next_run", nextRunStr, "status", status)

	return createRecurringTaskResponse{
		TaskID:     task.ID.String(),
		Name:       task.Name,
		Expression: task.Expression,
		NextRunAt:  nextRunStr,
		Status:     status,
		Message:    msg,
	}, nil
}
