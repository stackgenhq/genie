// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/adhocore/gronx"
	"github.com/google/uuid"
	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/db"
	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const ToolName = "create_recurring_task"

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
		function.WithName(ToolName),
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
	// NOTE: The notify-on-failure instruction is injected at dispatch-time
	// by the scheduler (not persisted), to avoid duplication on upsert.
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

const ListToolName = "list_recurring_tasks"

type listRecurringTasksRequest struct {
	EnabledOnly bool `json:"enabled_only" jsonschema:"description=If true, only returns enabled tasks,default=false"`
}

type listRecurringTasksResponse struct {
	Tasks []recurringTaskView `json:"tasks"`
}

type recurringTaskView struct {
	TaskID     string `json:"task_id"`
	Name       string `json:"name"`
	Expression string `json:"expression"`
	Action     string `json:"action"`
	Enabled    bool   `json:"enabled"`
	NextRunAt  string `json:"next_run_at,omitempty"`
}

type listRecurringTasksTool struct {
	store ICronStore
}

func NewListRecurringTasksTool(store ICronStore) tool.Tool {
	t := &listRecurringTasksTool{store: store}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(ListToolName),
		function.WithDescription(
			"List all recurring scheduled tasks. Use this tool to see what tasks are currently configured to run on a schedule."),
	)
}

func (t *listRecurringTasksTool) execute(ctx context.Context, req listRecurringTasksRequest) (listRecurringTasksResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "listRecurringTasksTool.execute")
	tasks, err := t.store.ListTasks(ctx, ListTasksRequest(req))
	if err != nil {
		logr.Error("Failed to list recurring tasks", "error", err)
		return listRecurringTasksResponse{}, fmt.Errorf("failed to list recurring tasks: %w", err)
	}

	views := make([]recurringTaskView, len(tasks))
	for i, task := range tasks {
		nextRun := ""
		if task.NextRunAt != nil {
			nextRun = task.NextRunAt.Format(time.RFC3339)
		}
		views[i] = recurringTaskView{
			TaskID:     task.ID.String(),
			Name:       task.Name,
			Expression: task.Expression,
			Action:     task.Action,
			Enabled:    task.Enabled,
			NextRunAt:  nextRun,
		}
	}

	return listRecurringTasksResponse{Tasks: views}, nil
}

const DeleteToolName = "delete_recurring_task"

type deleteRecurringTaskRequest struct {
	TaskID string `json:"task_id" jsonschema:"description=The unique identifier (UUID) of the recurring task to delete,required"`
}

type deleteRecurringTaskResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type deleteRecurringTaskTool struct {
	store ICronStore
}

func NewDeleteRecurringTaskTool(store ICronStore) tool.Tool {
	t := &deleteRecurringTaskTool{store: store}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(DeleteToolName),
		function.WithDescription(
			"Delete a recurring scheduled task by its TaskID. Use list_recurring_tasks first if you do not know the TaskID."),
	)
}

func (t *deleteRecurringTaskTool) execute(ctx context.Context, req deleteRecurringTaskRequest) (deleteRecurringTaskResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "deleteRecurringTaskTool.execute")

	id, err := uuid.Parse(req.TaskID)
	if err != nil {
		return deleteRecurringTaskResponse{}, fmt.Errorf("invalid task_id format: %w", err)
	}

	err = t.store.DeleteTask(ctx, DeleteTaskRequest{ID: id})
	if err != nil {
		logr.Error("Failed to delete recurring task", "task_id", req.TaskID, "error", err)
		return deleteRecurringTaskResponse{}, fmt.Errorf("failed to delete task: %w", err)
	}

	return deleteRecurringTaskResponse{Success: true, Message: fmt.Sprintf("Successfully deleted task with ID %s", req.TaskID)}, nil
}

const HistoryToolName = "history_recurring_task"

type historyRecurringTaskRequest struct {
	TaskID string `json:"task_id" jsonschema:"description=The unique identifier (UUID) of the recurring task to check history for,required"`
	Limit  int    `json:"limit" jsonschema:"description=The maximum number of recent runs to return,default=5"`
}

type historyRecurringTaskResponse struct {
	Runs []recurringTaskRunView `json:"runs"`
}

type recurringTaskRunView struct {
	RunID     string `json:"run_id"`
	StartedAt string `json:"started_at"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

type historyRecurringTaskTool struct {
	store ICronStore
}

func NewHistoryRecurringTaskTool(store ICronStore) tool.Tool {
	t := &historyRecurringTaskTool{store: store}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(HistoryToolName),
		function.WithDescription(
			"Get the execution history (past runs) for a specific recurring scheduled task. This shows when the task ran, its status, and any errors."),
	)
}

func (t *historyRecurringTaskTool) execute(ctx context.Context, req historyRecurringTaskRequest) (historyRecurringTaskResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "historyRecurringTaskTool.execute")

	id, err := uuid.Parse(req.TaskID)
	if err != nil {
		return historyRecurringTaskResponse{}, fmt.Errorf("invalid task_id format: %w", err)
	}

	limit := req.Limit
	if limit <= 0 || limit > 50 {
		limit = 5
	}

	runs, err := t.store.RecentRuns(ctx, RecentRunsRequest{TaskID: id, Limit: limit})
	if err != nil {
		logr.Error("Failed to get task history", "task_id", req.TaskID, "error", err)
		return historyRecurringTaskResponse{}, fmt.Errorf("failed to get task history: %w", err)
	}

	views := make([]recurringTaskRunView, len(runs))
	for i, run := range runs {
		views[i] = recurringTaskRunView{
			RunID:     run.RunID,
			StartedAt: run.StartedAt.Format(time.RFC3339),
			Status:    string(run.Status),
			Error:     run.Error,
		}
	}

	return historyRecurringTaskResponse{Runs: views}, nil
}

const ToggleToolName = "toggle_recurring_task"

type toggleRecurringTaskRequest struct {
	TaskID  string `json:"task_id" jsonschema:"description=The unique identifier (UUID) of the recurring task to toggle,required"`
	Enabled bool   `json:"enabled" jsonschema:"description=Set to true to enable (resume) or false to disable (pause) the task,required"`
}

type toggleRecurringTaskResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type toggleRecurringTaskTool struct {
	store ICronStore
}

func NewToggleRecurringTaskTool(store ICronStore) tool.Tool {
	t := &toggleRecurringTaskTool{store: store}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(ToggleToolName),
		function.WithDescription(
			"Enable or disable a recurring scheduled task without deleting it. "+
				"Use this to temporarily pause a task during maintenance windows "+
				"or resume a previously paused task. Use list_recurring_tasks to find the task_id."),
	)
}

func (t *toggleRecurringTaskTool) execute(ctx context.Context, req toggleRecurringTaskRequest) (toggleRecurringTaskResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "toggleRecurringTaskTool.execute")

	id, err := uuid.Parse(req.TaskID)
	if err != nil {
		return toggleRecurringTaskResponse{}, fmt.Errorf("invalid task_id format: %w", err)
	}

	err = t.store.SetEnabled(ctx, id, req.Enabled)
	if err != nil {
		logr.Error("Failed to toggle recurring task", "task_id", req.TaskID, "enabled", req.Enabled, "error", err)
		return toggleRecurringTaskResponse{}, fmt.Errorf("failed to toggle task: %w", err)
	}

	action := "paused"
	if req.Enabled {
		action = "resumed"
	}
	return toggleRecurringTaskResponse{
		Success: true,
		Message: fmt.Sprintf("Successfully %s task %s", action, req.TaskID),
	}, nil
}

const TriggerToolName = "trigger_recurring_task"

type triggerRecurringTaskRequest struct {
	TaskID string `json:"task_id" jsonschema:"description=The unique identifier (UUID) of the recurring task to trigger immediately,required"`
}

type triggerRecurringTaskResponse struct {
	Success bool   `json:"success"`
	RunID   string `json:"run_id,omitempty"`
	Message string `json:"message"`
}

type triggerRecurringTaskTool struct {
	store      ICronStore
	dispatcher EventDispatcher
}

func NewTriggerRecurringTaskTool(store ICronStore, dispatcher EventDispatcher) tool.Tool {
	t := &triggerRecurringTaskTool{store: store, dispatcher: dispatcher}
	return function.NewFunctionTool(
		t.execute,
		function.WithName(TriggerToolName),
		function.WithDescription(
			"Immediately trigger a recurring scheduled task outside its normal schedule. "+
				"Use this to test a newly created task, re-run after a fix, or run on demand. "+
				"Use list_recurring_tasks to find the task_id."),
	)
}

func (t *triggerRecurringTaskTool) execute(ctx context.Context, req triggerRecurringTaskRequest) (triggerRecurringTaskResponse, error) {
	logr := logger.GetLogger(ctx).With("fn", "triggerRecurringTaskTool.execute")

	id, err := uuid.Parse(req.TaskID)
	if err != nil {
		return triggerRecurringTaskResponse{Success: false, Message: fmt.Sprintf("invalid task_id format: %v", err)}, nil
	}

	task, err := t.store.GetTask(ctx, id)
	if err != nil {
		logr.Error("Failed to get task for triggering", "task_id", req.TaskID, "error", err)
		return triggerRecurringTaskResponse{Success: false, Message: fmt.Sprintf("task not found: %v", err)}, nil
	}

	if t.dispatcher == nil {
		return triggerRecurringTaskResponse{Success: false, Message: "dispatcher not available — cron scheduler may not be running"}, nil
	}

	// Record the run.
	history, recordErr := t.store.RecordRun(ctx, RecordRunRequest{
		TaskID:   task.ID,
		TaskName: task.Name,
		Status:   "running",
	})
	if recordErr != nil {
		logr.Warn("Failed to record manual trigger run", "error", recordErr)
	}

	// Dispatch.
	payload, _ := json.Marshal(map[string]string{
		"task_name":  task.Name,
		"action":     task.Action,
		"expression": task.Expression,
		"message":    fmt.Sprintf("Manual trigger of cron task [%s]: %s", task.Name, task.Action),
	})
	runID, dispatchErr := t.dispatcher(ctx, agui.EventRequest{
		Source:  "cron:manual:" + task.Name,
		Payload: payload,
	})

	// Update history.
	if history != nil {
		status := db.CronStatusSuccess
		errMsg := ""
		if dispatchErr != nil {
			status = db.CronStatusFailed
			errMsg = dispatchErr.Error()
		}
		_ = t.store.UpdateRun(ctx, UpdateRunRequest{
			HistoryID: history.ID,
			Status:    status,
			Error:     errMsg,
			RunID:     runID,
		})
	}

	if dispatchErr != nil {
		logr.Error("Failed to trigger task", "task_id", req.TaskID, "error", dispatchErr)
		return triggerRecurringTaskResponse{Success: false, Message: fmt.Sprintf("dispatch failed: %v", dispatchErr)}, nil
	}

	return triggerRecurringTaskResponse{
		Success: true,
		RunID:   runID,
		Message: fmt.Sprintf("Successfully triggered task '%s' (run_id: %s)", task.Name, runID),
	}, nil
}
