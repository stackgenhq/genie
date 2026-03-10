// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package tasks provides Google Tasks API tools for agents. It enables list,
// create, update, and delete operations using the Tasks API with the shared
// Google OAuth token (Calendar, Contacts, Drive, Gmail). One sign-in can power
// all Google tools.
//
// Available tools (prefixed with google_tasks_ when registered):
//   - google_tasks_list_task_lists — list the user's task lists
//   - google_tasks_list_tasks — list tasks in a task list
//   - google_tasks_create_task — create a task in a task list
//   - google_tasks_update_task — update a task (title, notes, completed)
//   - google_tasks_delete_task — delete a task
//
// Authentication: Same as other pkg/tools/google packages — TokenFile,
// Token/Password, or device keyring; CredentialsFile for OAuth client config.
package tasks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/tools/google/oauth"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	tasksapi "google.golang.org/api/tasks/v1"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	maxTaskLists = 100
	maxTasks     = 100
)

// Tasks API scope: full read/write.
var tasksScopes = []string{"https://www.googleapis.com/auth/tasks"}

// Service provides Google Tasks operations for tools.
type Service interface {
	ListTaskLists(ctx context.Context, maxResults int) ([]*TaskListSummary, error)
	ListTasks(ctx context.Context, taskListID string, showCompleted bool, maxResults int) ([]*TaskSummary, error)
	CreateTask(ctx context.Context, taskListID, title, notes, due string) (*TaskDetail, error)
	UpdateTask(ctx context.Context, taskListID, taskID, title, notes string, completed bool) error
	DeleteTask(ctx context.Context, taskListID, taskID string) error
	Validate(ctx context.Context) error
}

// TaskListSummary is a task list entry for list results.
type TaskListSummary struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// TaskSummary is a task entry for list results.
type TaskSummary struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Notes     string `json:"notes,omitempty"`
	Status    string `json:"status"` // "needsAction" or "completed"
	Due       string `json:"due,omitempty"`
	Updated   string `json:"updated,omitempty"`
	Position  string `json:"position,omitempty"`
	Parent    string `json:"parent,omitempty"`
	Completed string `json:"completed,omitempty"`
}

// TaskDetail is a full task for create/update responses.
type TaskDetail struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Notes     string `json:"notes,omitempty"`
	Status    string `json:"status"`
	Due       string `json:"due,omitempty"`
	Updated   string `json:"updated,omitempty"`
	Completed string `json:"completed,omitempty"`
}

type tasksWrapper struct {
	svc *tasksapi.Service
}

// NewFromSecretProvider creates a Tasks Service using the shared Google OAuth
// token (TokenFile, Token/Password, or device keyring). One sign-in can power
// Calendar, Contacts, Drive, Gmail, and Tasks.
func NewFromSecretProvider(ctx context.Context, sp security.SecretProvider) (Service, error) {
	credsEntry, _ := sp.GetSecret(ctx, security.GetSecretRequest{
		Name:   "CredentialsFile",
		Reason: fmt.Sprintf("Google Tasks tool: %s", toolcontext.GetJustification(ctx)),
	})
	credsJSON, err := oauth.GetCredentials(credsEntry, "Tasks")
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(credsJSON, &raw); err != nil {
		return nil, fmt.Errorf("tasks: invalid credentials JSON: %w", err)
	}
	if typeField, ok := raw["type"]; ok {
		var t string
		if err := json.Unmarshal(typeField, &t); err == nil && t == "service_account" {
			creds, err := google.CredentialsFromJSON(ctx, credsJSON, tasksScopes...) //nolint:staticcheck
			if err != nil {
				return nil, fmt.Errorf("tasks: invalid service account credentials: %w", err)
			}
			svc, err := tasksapi.NewService(ctx, option.WithCredentials(creds))
			if err != nil {
				return nil, fmt.Errorf("tasks: failed to create Tasks service: %w", err)
			}
			return &tasksWrapper{svc: svc}, nil
		}
	}
	tokenJSON, save, err := oauth.GetToken(ctx, sp)
	if err != nil {
		return nil, err
	}
	client, err := oauth.HTTPClient(ctx, credsJSON, tokenJSON, save, tasksScopes)
	if err != nil {
		return nil, err
	}
	svc, err := tasksapi.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("tasks: failed to create Tasks service: %w", err)
	}
	return &tasksWrapper{svc: svc}, nil
}

func (w *tasksWrapper) ListTaskLists(ctx context.Context, maxResults int) ([]*TaskListSummary, error) {
	if maxResults <= 0 || maxResults > maxTaskLists {
		maxResults = maxTaskLists
	}
	resp, err := w.svc.Tasklists.List().Context(ctx).MaxResults(int64(maxResults)).Do()
	if err != nil {
		return nil, fmt.Errorf("tasks list task lists: %w", err)
	}
	out := make([]*TaskListSummary, 0, len(resp.Items))
	for _, it := range resp.Items {
		out = append(out, &TaskListSummary{ID: it.Id, Title: it.Title})
	}
	return out, nil
}

func (w *tasksWrapper) ListTasks(ctx context.Context, taskListID string, showCompleted bool, maxResults int) ([]*TaskSummary, error) {
	if taskListID == "" {
		return nil, fmt.Errorf("task_list_id is required")
	}
	if maxResults <= 0 || maxResults > maxTasks {
		maxResults = maxTasks
	}
	call := w.svc.Tasks.List(taskListID).Context(ctx).MaxResults(int64(maxResults))
	if !showCompleted {
		call = call.ShowCompleted(false)
	}
	resp, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("tasks list tasks: %w", err)
	}
	out := make([]*TaskSummary, 0, len(resp.Items))
	for _, it := range resp.Items {
		s := &TaskSummary{
			ID:       it.Id,
			Title:    it.Title,
			Notes:    it.Notes,
			Status:   it.Status,
			Position: it.Position,
			Parent:   it.Parent,
		}
		if it.Due != "" {
			s.Due = it.Due
		}
		if it.Updated != "" {
			s.Updated = it.Updated
		}
		if it.Completed != nil && *it.Completed != "" {
			s.Completed = *it.Completed
		}
		out = append(out, s)
	}
	return out, nil
}

func (w *tasksWrapper) CreateTask(ctx context.Context, taskListID, title, notes, due string) (*TaskDetail, error) {
	if taskListID == "" || title == "" {
		return nil, fmt.Errorf("task_list_id and title are required")
	}
	t := &tasksapi.Task{Title: title, Notes: notes}
	if due != "" {
		t.Due = due
	}
	created, err := w.svc.Tasks.Insert(taskListID, t).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("tasks create task: %w", err)
	}
	return taskDetailFromAPI(created), nil
}

// UpdateTask patches a task with the given fields. Only non-empty title/notes are set
// so existing values are not cleared; status is always set from completed.
func (w *tasksWrapper) UpdateTask(ctx context.Context, taskListID, taskID, title, notes string, completed bool) error {
	if taskListID == "" || taskID == "" {
		return fmt.Errorf("task_list_id and task_id are required")
	}
	t := &tasksapi.Task{}
	if title != "" {
		t.Title = title
	}
	if notes != "" {
		t.Notes = notes
	}
	if completed {
		t.Status = "completed"
	} else {
		t.Status = "needsAction"
	}
	_, err := w.svc.Tasks.Patch(taskListID, taskID, t).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("tasks update task: %w", err)
	}
	return nil
}

func (w *tasksWrapper) DeleteTask(ctx context.Context, taskListID, taskID string) error {
	if taskListID == "" || taskID == "" {
		return fmt.Errorf("task_list_id and task_id are required")
	}
	if err := w.svc.Tasks.Delete(taskListID, taskID).Context(ctx).Do(); err != nil {
		return fmt.Errorf("tasks delete task: %w", err)
	}
	return nil
}

func (w *tasksWrapper) Validate(ctx context.Context) error {
	_, err := w.svc.Tasklists.List().Context(ctx).MaxResults(1).Do()
	return err
}

func taskDetailFromAPI(t *tasksapi.Task) *TaskDetail {
	d := &TaskDetail{ID: t.Id, Title: t.Title, Notes: t.Notes, Status: t.Status}
	if t.Due != "" {
		d.Due = t.Due
	}
	if t.Updated != "" {
		d.Updated = t.Updated
	}
	if t.Completed != nil && *t.Completed != "" {
		d.Completed = *t.Completed
	}
	return d
}

// ─── Tool request structs ────────────────────────────────────────────────

type listTaskListsRequest struct {
	MaxResults int `json:"max_results,omitempty" jsonschema:"description=Max task lists to return (default 100, max 100)."`
}

type listTasksRequest struct {
	TaskListID    string `json:"task_list_id" jsonschema:"description=ID of the task list (from list_task_lists).,required"`
	ShowCompleted bool   `json:"show_completed,omitempty" jsonschema:"description=Include completed tasks. Default false."`
	MaxResults    int    `json:"max_results,omitempty" jsonschema:"description=Max tasks to return (default 100, max 100)."`
}

type createTaskRequest struct {
	TaskListID string `json:"task_list_id" jsonschema:"description=ID of the task list.,required"`
	Title      string `json:"title" jsonschema:"description=Task title.,required"`
	Notes      string `json:"notes,omitempty" jsonschema:"description=Optional task notes."`
	Due        string `json:"due,omitempty" jsonschema:"description=Optional due date (RFC3339)."`
}

type updateTaskRequest struct {
	TaskListID string `json:"task_list_id" jsonschema:"description=ID of the task list.,required"`
	TaskID     string `json:"task_id" jsonschema:"description=ID of the task (from list_tasks).,required"`
	Title      string `json:"title,omitempty" jsonschema:"description=New task title."`
	Notes      string `json:"notes,omitempty" jsonschema:"description=New task notes."`
	Completed  bool   `json:"completed,omitempty" jsonschema:"description=Mark task as completed."`
}

type deleteTaskRequest struct {
	TaskListID string `json:"task_list_id" jsonschema:"description=ID of the task list.,required"`
	TaskID     string `json:"task_id" jsonschema:"description=ID of the task to delete.,required"`
}

// ─── Tool provider (name-prefixed) ──────────────────────────────────────────

type tasksTools struct {
	name string
	svc  Service
}

func newTasksTools(name string, svc Service) *tasksTools {
	return &tasksTools{name: name, svc: svc}
}

func (c *tasksTools) tools() []tool.CallableTool {
	return []tool.CallableTool{
		function.NewFunctionTool(
			c.handleListTaskLists,
			function.WithName(c.name+"_list_task_lists"),
			function.WithDescription("List the user's Google Tasks task lists. Use task_list_id from results for list_tasks."),
		),
		function.NewFunctionTool(
			c.handleListTasks,
			function.WithName(c.name+"_list_tasks"),
			function.WithDescription("List tasks in a Google Tasks list. Use task_list_id from list_task_lists."),
		),
		function.NewFunctionTool(
			c.handleCreateTask,
			function.WithName(c.name+"_create_task"),
			function.WithDescription("Create a new task in a Google Tasks list."),
		),
		function.NewFunctionTool(
			c.handleUpdateTask,
			function.WithName(c.name+"_update_task"),
			function.WithDescription("Update a Google Task (title, notes, or mark completed)."),
		),
		function.NewFunctionTool(
			c.handleDeleteTask,
			function.WithName(c.name+"_delete_task"),
			function.WithDescription("Delete a Google Task."),
		),
	}
}

func (c *tasksTools) handleListTaskLists(ctx context.Context, req listTaskListsRequest) ([]*TaskListSummary, error) {
	return c.svc.ListTaskLists(ctx, req.MaxResults)
}

func (c *tasksTools) handleListTasks(ctx context.Context, req listTasksRequest) ([]*TaskSummary, error) {
	return c.svc.ListTasks(ctx, req.TaskListID, req.ShowCompleted, req.MaxResults)
}

func (c *tasksTools) handleCreateTask(ctx context.Context, req createTaskRequest) (*TaskDetail, error) {
	return c.svc.CreateTask(ctx, req.TaskListID, req.Title, req.Notes, req.Due)
}

func (c *tasksTools) handleUpdateTask(ctx context.Context, req updateTaskRequest) (string, error) {
	if err := c.svc.UpdateTask(ctx, req.TaskListID, req.TaskID, req.Title, req.Notes, req.Completed); err != nil {
		return "", err
	}
	return "Task updated.", nil
}

func (c *tasksTools) handleDeleteTask(ctx context.Context, req deleteTaskRequest) (string, error) {
	if err := c.svc.DeleteTask(ctx, req.TaskListID, req.TaskID); err != nil {
		return "", err
	}
	return "Task deleted.", nil
}

// AllTools returns all Tasks tools with the given name prefix (e.g. google_tasks).
func AllTools(name string, svc Service) []tool.Tool {
	callables := newTasksTools(name, svc).tools()
	out := make([]tool.Tool, len(callables))
	for i, t := range callables {
		out[i] = t
	}
	return out
}
