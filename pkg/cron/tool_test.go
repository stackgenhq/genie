// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cron_test

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/cron"
	"github.com/stackgenhq/genie/pkg/cron/cronfakes"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("CreateRecurringTaskTool", func() {
	var (
		fakeStore *cronfakes.FakeICronStore
	)

	BeforeEach(func() {
		fakeStore = &cronfakes.FakeICronStore{}
	})

	It("should be created with the correct name", func() {
		t := cron.NewCreateRecurringTaskTool(fakeStore)
		Expect(t.Declaration().Name).To(Equal(cron.ToolName))
	})

	It("should have a description mentioning cron expressions", func() {
		t := cron.NewCreateRecurringTaskTool(fakeStore)
		Expect(t.Declaration().Description).To(ContainSubstring("cron expression"))
	})

	Describe("execute via CallableTool.Call", func() {
		It("should create a task and return its ID", func(ctx context.Context) {
			taskID := uuid.MustParse("00000000-0000-0000-0000-000000000099")
			fakeStore.CreateTaskReturns(&cron.CronTask{
				ID:         taskID,
				Name:       "nightly-backup",
				Expression: "0 2 * * *",
				Action:     "run backup",
			}, nil)

			t := cron.NewCreateRecurringTaskTool(fakeStore)
			callable, ok := t.(tool.CallableTool)
			Expect(ok).To(BeTrue(), "tool should implement CallableTool")

			input, _ := json.Marshal(map[string]string{
				"name":       "nightly-backup",
				"expression": "0 2 * * *",
				"action":     "run backup",
			})

			result, err := callable.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			// Verify CreateTask was called with the raw action (no suffix).
			Expect(fakeStore.CreateTaskCallCount()).To(Equal(1))
			_, req := fakeStore.CreateTaskArgsForCall(0)
			Expect(req.Name).To(Equal("nightly-backup"))
			Expect(req.Expression).To(Equal("0 2 * * *"))
			Expect(req.Action).To(Equal("run backup"))
			Expect(req.Source).To(Equal("tool"))

			// Verify SetNextRun was called.
			Expect(fakeStore.SetNextRunCallCount()).To(Equal(1))
		})

		It("should return error when store fails", func(ctx context.Context) {
			fakeStore.CreateTaskReturns(nil, context.DeadlineExceeded)

			t := cron.NewCreateRecurringTaskTool(fakeStore)
			callable := t.(tool.CallableTool)

			input, _ := json.Marshal(map[string]string{
				"name":       "failing",
				"expression": "* * * * *",
				"action":     "will fail",
			})

			_, err := callable.Call(ctx, input)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to create recurring task"))
		})

		It("should succeed even when SetNextRun fails", func(ctx context.Context) {
			taskID := uuid.MustParse("00000000-0000-0000-0000-000000000088")
			fakeStore.CreateTaskReturns(&cron.CronTask{
				ID:         taskID,
				Name:       "next-run-fail",
				Expression: "0 3 * * *",
				Action:     "test",
			}, nil)
			fakeStore.SetNextRunReturns(fmt.Errorf("db write failed"))

			t := cron.NewCreateRecurringTaskTool(fakeStore)
			callable := t.(tool.CallableTool)

			input, _ := json.Marshal(map[string]string{
				"name":       "next-run-fail",
				"expression": "0 3 * * *",
				"action":     "test",
			})

			result, err := callable.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(fmt.Sprintf("%v", result)).To(ContainSubstring("next-run-fail"))
			Expect(fakeStore.CreateTaskCallCount()).To(Equal(1))
			Expect(fakeStore.SetNextRunCallCount()).To(Equal(1))
		})
	})
})

var _ = Describe("ListRecurringTasksTool", func() {
	var (
		fakeStore *cronfakes.FakeICronStore
	)

	BeforeEach(func() {
		fakeStore = &cronfakes.FakeICronStore{}
	})

	It("should return a list of tasks", func(ctx context.Context) {
		t := cron.NewListRecurringTasksTool(fakeStore)
		callable, ok := t.(tool.CallableTool)
		Expect(ok).To(BeTrue(), "tool should implement CallableTool")

		taskID := uuid.MustParse("00000000-0000-0000-0000-000000000099")
		now := time.Now()
		fakeStore.ListTasksReturns([]cron.CronTask{
			{
				ID:         taskID,
				Name:       "task-1",
				Expression: "0 0 * * *",
				Action:     "do something",
				Enabled:    true,
				NextRunAt:  &now,
			},
		}, nil)

		input, _ := json.Marshal(map[string]interface{}{
			"enabled_only": true,
		})

		result, err := callable.Call(ctx, input)
		Expect(err).NotTo(HaveOccurred())

		resStr := fmt.Sprintf("%v", result)
		Expect(resStr).To(ContainSubstring("task-1"))
		Expect(resStr).To(ContainSubstring("0 0 * * *"))
	})

	It("should handle store errors gracefully", func(ctx context.Context) {
		t := cron.NewListRecurringTasksTool(fakeStore)
		callable := t.(tool.CallableTool)

		fakeStore.ListTasksReturns(nil, fmt.Errorf("db error"))

		input, _ := json.Marshal(map[string]interface{}{
			"enabled_only": true,
		})

		_, err := callable.Call(ctx, input)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to list recurring tasks"))
	})
})

var _ = Describe("DeleteRecurringTaskTool", func() {
	var (
		fakeStore *cronfakes.FakeICronStore
	)

	BeforeEach(func() {
		fakeStore = &cronfakes.FakeICronStore{}
	})

	It("should successfully delete a task", func(ctx context.Context) {
		t := cron.NewDeleteRecurringTaskTool(fakeStore)
		callable, ok := t.(tool.CallableTool)
		Expect(ok).To(BeTrue(), "tool should implement CallableTool")

		fakeStore.DeleteTaskReturns(nil)

		input, _ := json.Marshal(map[string]interface{}{
			"task_id": "00000000-0000-0000-0000-000000000099",
		})

		result, err := callable.Call(ctx, input)
		Expect(err).NotTo(HaveOccurred())

		resStr := fmt.Sprintf("%v", result)
		Expect(resStr).To(ContainSubstring("Successfully deleted task"))
	})

	It("should return error for invalid UUID", func(ctx context.Context) {
		t := cron.NewDeleteRecurringTaskTool(fakeStore)
		callable := t.(tool.CallableTool)

		input, _ := json.Marshal(map[string]interface{}{
			"task_id": "invalid-id",
		})

		_, err := callable.Call(ctx, input)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid task_id format"))
	})

	It("should return error when store fails", func(ctx context.Context) {
		t := cron.NewDeleteRecurringTaskTool(fakeStore)
		callable := t.(tool.CallableTool)

		fakeStore.DeleteTaskReturns(fmt.Errorf("db error"))

		input, _ := json.Marshal(map[string]interface{}{
			"task_id": "00000000-0000-0000-0000-000000000099",
		})

		_, err := callable.Call(ctx, input)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to delete task"))
	})
})

var _ = Describe("HistoryRecurringTaskTool", func() {
	var (
		fakeStore *cronfakes.FakeICronStore
	)

	BeforeEach(func() {
		fakeStore = &cronfakes.FakeICronStore{}
	})

	It("should return task history", func(ctx context.Context) {
		t := cron.NewHistoryRecurringTaskTool(fakeStore)
		callable, ok := t.(tool.CallableTool)
		Expect(ok).To(BeTrue(), "tool should implement CallableTool")

		fakeStore.RecentRunsReturns([]cron.CronHistory{
			{
				RunID:     "run-123",
				StartedAt: time.Now(),
				Status:    "success",
			},
		}, nil)

		input, _ := json.Marshal(map[string]interface{}{
			"task_id": "00000000-0000-0000-0000-000000000099",
			"limit":   10,
		})

		result, err := callable.Call(ctx, input)
		Expect(err).NotTo(HaveOccurred())

		resStr := fmt.Sprintf("%v", result)
		Expect(resStr).To(ContainSubstring("run-123"))
		Expect(resStr).To(ContainSubstring("success"))
	})

	It("should adjust limit to default when out of bounds", func(ctx context.Context) {
		t := cron.NewHistoryRecurringTaskTool(fakeStore)
		callable := t.(tool.CallableTool)

		fakeStore.RecentRunsReturns([]cron.CronHistory{}, nil)

		input, _ := json.Marshal(map[string]interface{}{
			"task_id": "00000000-0000-0000-0000-000000000099",
			"limit":   100, // over 50
		})

		_, err := callable.Call(ctx, input)
		Expect(err).NotTo(HaveOccurred())

		Expect(fakeStore.RecentRunsCallCount()).To(Equal(1))
		_, req := fakeStore.RecentRunsArgsForCall(0)
		Expect(req.Limit).To(Equal(5)) // Adjusted to default
	})

	It("should return error for invalid UUID", func(ctx context.Context) {
		t := cron.NewHistoryRecurringTaskTool(fakeStore)
		callable := t.(tool.CallableTool)

		input, _ := json.Marshal(map[string]interface{}{
			"task_id": "invalid-uuid",
			"limit":   10,
		})

		_, err := callable.Call(ctx, input)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid task_id format"))
	})

	It("should handle store errors", func(ctx context.Context) {
		t := cron.NewHistoryRecurringTaskTool(fakeStore)
		callable := t.(tool.CallableTool)

		fakeStore.RecentRunsReturns(nil, fmt.Errorf("db error"))

		input, _ := json.Marshal(map[string]interface{}{
			"task_id": "00000000-0000-0000-0000-000000000099",
			"limit":   10,
		})

		_, err := callable.Call(ctx, input)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get task history"))
	})
})

var _ = Describe("ToggleRecurringTaskTool", func() {
	var (
		fakeStore *cronfakes.FakeICronStore
	)

	BeforeEach(func() {
		fakeStore = &cronfakes.FakeICronStore{}
	})

	It("should be created with the correct name", func() {
		t := cron.NewToggleRecurringTaskTool(fakeStore)
		Expect(t.Declaration().Name).To(Equal(cron.ToggleToolName))
	})

	DescribeTable("toggle scenarios",
		func(ctx context.Context, enabled bool, expectAction string) {
			fakeStore.SetEnabledReturns(nil)

			t := cron.NewToggleRecurringTaskTool(fakeStore)
			callable := t.(tool.CallableTool)

			input, _ := json.Marshal(map[string]interface{}{
				"task_id": "00000000-0000-0000-0000-000000000099",
				"enabled": enabled,
			})

			result, err := callable.Call(ctx, input)
			Expect(err).NotTo(HaveOccurred())

			resStr := fmt.Sprintf("%v", result)
			Expect(resStr).To(ContainSubstring(expectAction))

			Expect(fakeStore.SetEnabledCallCount()).To(Equal(1))
			_, id, passedEnabled := fakeStore.SetEnabledArgsForCall(0)
			Expect(id.String()).To(Equal("00000000-0000-0000-0000-000000000099"))
			Expect(passedEnabled).To(Equal(enabled))
		},
		Entry("pause a task", false, "paused"),
		Entry("resume a task", true, "resumed"),
	)

	It("should return error for invalid UUID", func(ctx context.Context) {
		t := cron.NewToggleRecurringTaskTool(fakeStore)
		callable := t.(tool.CallableTool)

		input, _ := json.Marshal(map[string]interface{}{
			"task_id": "not-a-uuid",
			"enabled": false,
		})

		_, err := callable.Call(ctx, input)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid task_id format"))
	})

	It("should return error when store fails", func(ctx context.Context) {
		fakeStore.SetEnabledReturns(fmt.Errorf("db error"))

		t := cron.NewToggleRecurringTaskTool(fakeStore)
		callable := t.(tool.CallableTool)

		input, _ := json.Marshal(map[string]interface{}{
			"task_id": "00000000-0000-0000-0000-000000000099",
			"enabled": true,
		})

		_, err := callable.Call(ctx, input)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to toggle task"))
	})
})

var _ = Describe("TriggerRecurringTaskTool", func() {
	var (
		fakeStore  *cronfakes.FakeICronStore
		dispatcher cron.EventDispatcher
	)

	BeforeEach(func() {
		fakeStore = &cronfakes.FakeICronStore{}
		dispatcher = func(_ context.Context, req agui.EventRequest) (string, error) {
			return "manual-run-001", nil
		}
	})

	It("should be created with the correct name", func() {
		t := cron.NewTriggerRecurringTaskTool(fakeStore, dispatcher)
		Expect(t.Declaration().Name).To(Equal(cron.TriggerToolName))
	})

	It("should successfully trigger a task", func(ctx context.Context) {
		taskID := uuid.MustParse("00000000-0000-0000-0000-000000000099")
		fakeStore.GetTaskReturns(&cron.CronTask{
			ID:         taskID,
			Name:       "backup-task",
			Action:     "run backup",
			Expression: "0 2 * * *",
		}, nil)
		fakeStore.RecordRunReturns(&cron.CronHistory{ID: uuid.New(), TaskID: taskID}, nil)

		t := cron.NewTriggerRecurringTaskTool(fakeStore, dispatcher)
		callable := t.(tool.CallableTool)

		input, _ := json.Marshal(map[string]interface{}{
			"task_id": taskID.String(),
		})

		result, err := callable.Call(ctx, input)
		Expect(err).NotTo(HaveOccurred())

		resStr := fmt.Sprintf("%v", result)
		Expect(resStr).To(ContainSubstring("Successfully triggered"))
		Expect(resStr).To(ContainSubstring("manual-run-001"))

		Expect(fakeStore.GetTaskCallCount()).To(Equal(1))
		Expect(fakeStore.RecordRunCallCount()).To(Equal(1))
		Expect(fakeStore.UpdateRunCallCount()).To(Equal(1))
	})

	It("should return error for invalid UUID", func(ctx context.Context) {
		t := cron.NewTriggerRecurringTaskTool(fakeStore, dispatcher)
		callable := t.(tool.CallableTool)

		input, _ := json.Marshal(map[string]interface{}{
			"task_id": "not-valid",
		})

		result, err := callable.Call(ctx, input)
		Expect(err).NotTo(HaveOccurred())
		resStr := fmt.Sprintf("%v", result)
		Expect(resStr).To(ContainSubstring("invalid task_id format"))
	})

	It("should return error when task not found", func(ctx context.Context) {
		fakeStore.GetTaskReturns(nil, fmt.Errorf("record not found"))

		t := cron.NewTriggerRecurringTaskTool(fakeStore, dispatcher)
		callable := t.(tool.CallableTool)

		input, _ := json.Marshal(map[string]interface{}{
			"task_id": "00000000-0000-0000-0000-000000000099",
		})

		result, err := callable.Call(ctx, input)
		Expect(err).NotTo(HaveOccurred())
		resStr := fmt.Sprintf("%v", result)
		Expect(resStr).To(ContainSubstring("task not found"))
	})

	It("should return error when dispatcher is nil", func(ctx context.Context) {
		taskID := uuid.MustParse("00000000-0000-0000-0000-000000000099")
		fakeStore.GetTaskReturns(&cron.CronTask{
			ID:   taskID,
			Name: "task",
		}, nil)

		t := cron.NewTriggerRecurringTaskTool(fakeStore, nil)
		callable := t.(tool.CallableTool)

		input, _ := json.Marshal(map[string]interface{}{
			"task_id": taskID.String(),
		})

		result, err := callable.Call(ctx, input)
		Expect(err).NotTo(HaveOccurred())
		resStr := fmt.Sprintf("%v", result)
		Expect(resStr).To(ContainSubstring("dispatcher not available"))
	})

	It("should handle dispatch failure", func(ctx context.Context) {
		taskID := uuid.MustParse("00000000-0000-0000-0000-000000000099")
		fakeStore.GetTaskReturns(&cron.CronTask{
			ID:         taskID,
			Name:       "failing-task",
			Action:     "do thing",
			Expression: "* * * * *",
		}, nil)
		fakeStore.RecordRunReturns(&cron.CronHistory{ID: uuid.New(), TaskID: taskID}, nil)

		failDispatcher := func(_ context.Context, _ agui.EventRequest) (string, error) {
			return "", fmt.Errorf("dispatch failed")
		}

		t := cron.NewTriggerRecurringTaskTool(fakeStore, failDispatcher)
		callable := t.(tool.CallableTool)

		input, _ := json.Marshal(map[string]interface{}{
			"task_id": taskID.String(),
		})

		result, err := callable.Call(ctx, input)
		Expect(err).NotTo(HaveOccurred())
		resStr := fmt.Sprintf("%v", result)
		Expect(resStr).To(ContainSubstring("dispatch failed"))

		// Should still have updated the history with failure status.
		Expect(fakeStore.UpdateRunCallCount()).To(Equal(1))
	})
})

var _ = Describe("ToolProvider", func() {
	var (
		fakeStore *cronfakes.FakeICronStore
	)

	BeforeEach(func() {
		fakeStore = &cronfakes.FakeICronStore{}
	})

	It("should provide 5 tools without dispatcher", func() {
		p := cron.NewToolProvider(fakeStore)
		tools := p.GetTools(context.Background())
		Expect(len(tools)).To(Equal(5))
	})

	It("should provide 6 tools with dispatcher", func() {
		p := cron.NewToolProvider(fakeStore)
		p.SetDispatcher(func(_ context.Context, _ agui.EventRequest) (string, error) {
			return "", nil
		})
		tools := p.GetTools(context.Background())
		Expect(len(tools)).To(Equal(6))
	})
})
