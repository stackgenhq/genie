// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cron_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/cron"
	"github.com/stackgenhq/genie/pkg/cron/cronfakes"
	"github.com/stackgenhq/genie/pkg/db"
)

var _ = Describe("Scheduler", func() {
	var (
		fakeStore *cronfakes.FakeICronStore
		ctx       context.Context
		taskID1   = uuid.MustParse("00000000-0000-0000-0000-000000000001")
		taskID2   = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	)

	BeforeEach(func() {
		fakeStore = &cronfakes.FakeICronStore{}
		ctx = context.Background()
	})

	Describe("Start", func() {
		It("should start even when Enabled config field is false", func() {
			fakeStore.ListTasksReturns(nil, nil)
			dispatcher := func(_ context.Context, _ agui.EventRequest) (string, error) {
				return "", nil
			}
			sched := cron.Config{}.NewScheduler(fakeStore, dispatcher)
			sched.SetTickerIntervalForTest(50 * time.Millisecond)
			err := sched.Start(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer sched.Stop() //nolint:errcheck
		})

		It("should return error when started twice", func() {
			fakeStore.ListTasksReturns(nil, nil)
			dispatcher := func(_ context.Context, _ agui.EventRequest) (string, error) {
				return "", nil
			}
			sched := cron.Config{}.NewScheduler(fakeStore, dispatcher)
			sched.SetTickerIntervalForTest(50 * time.Millisecond)

			err := sched.Start(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer sched.Stop() //nolint:errcheck

			err = sched.Start(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("already running"))
		})

		It("should upsert config-defined tasks on start", func(ctx context.Context) {
			fakeStore.ListTasksReturns(nil, nil)
			fakeStore.CreateTaskReturns(&cron.CronTask{ID: taskID1, Name: "my-task"}, nil)

			dispatcher := func(_ context.Context, _ agui.EventRequest) (string, error) {
				return "", nil
			}
			cfg := cron.Config{
				Tasks: []cron.CronEntry{
					{Name: "my-task", Expression: "*/5 * * * *", Action: "do stuff"},
				},
			}
			sched := cfg.NewScheduler(fakeStore, dispatcher)
			sched.SetTickerIntervalForTest(50 * time.Millisecond)

			err := sched.Start(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer sched.Stop() //nolint:errcheck

			Expect(fakeStore.CreateTaskCallCount()).To(Equal(1))
			_, req := fakeStore.CreateTaskArgsForCall(0)
			Expect(req.Name).To(Equal("my-task"))
			Expect(req.Source).To(Equal("config"))
		})

		It("should skip invalid config entries", func(ctx context.Context) {
			fakeStore.ListTasksReturns(nil, nil)

			dispatcher := func(_ context.Context, _ agui.EventRequest) (string, error) {
				return "", nil
			}
			cfg := cron.Config{

				Tasks: []cron.CronEntry{
					{Name: "", Expression: "", Action: ""}, // all empty — skipped
					{Name: "valid", Expression: "* * * * *", Action: "go"},
				},
			}
			sched := cfg.NewScheduler(fakeStore, dispatcher)
			sched.SetTickerIntervalForTest(50 * time.Millisecond)

			err := sched.Start(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer sched.Stop() //nolint:errcheck

			// Only the valid entry should be upserted.
			Expect(fakeStore.CreateTaskCallCount()).To(Equal(1))
		})

		It("should compute NextRunAt for tasks that need it", func(ctx context.Context) {
			past := time.Now().Add(-10 * time.Minute)
			fakeStore.ListTasksReturns([]cron.CronTask{
				{ID: taskID1, Name: "stale-task", Expression: "* * * * *", NextRunAt: &past},
			}, nil)

			dispatcher := func(_ context.Context, _ agui.EventRequest) (string, error) {
				return "", nil
			}
			sched := cron.Config{}.NewScheduler(fakeStore, dispatcher)
			sched.SetTickerIntervalForTest(50 * time.Millisecond)

			err := sched.Start(ctx)
			Expect(err).NotTo(HaveOccurred())
			defer sched.Stop() //nolint:errcheck

			// SetNextRun should have been called to recompute NextRunAt.
			Expect(fakeStore.SetNextRunCallCount()).To(BeNumerically(">=", 1))
		})

		It("should return error when ListTasks fails during start", func(ctx context.Context) {
			fakeStore.ListTasksReturns(nil, fmt.Errorf("db connection refused"))

			dispatcher := func(_ context.Context, _ agui.EventRequest) (string, error) {
				return "", nil
			}
			sched := cron.Config{}.NewScheduler(fakeStore, dispatcher)
			sched.SetTickerIntervalForTest(50 * time.Millisecond)

			err := sched.Start(ctx)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to load cron tasks"))
		})
	})

	Describe("Tick loop dispatches due tasks", func() {
		It("should dispatch due tasks via the event dispatcher", func(ctx context.Context) {
			// Stub the store to return a due task on the first DueTasks call.
			var dispatchCount atomic.Int32
			dueTask := cron.CronTask{
				ID: taskID1, Name: "every-min", Expression: "* * * * *", Action: "check health",
			}
			fakeStore.DueTasksReturns([]cron.CronTask{dueTask}, nil)
			fakeStore.RecordRunReturns(&cron.CronHistory{
				ID: uuid.New(), TaskID: taskID1, TaskName: "every-min", Status: db.CronStatusRunning,
			}, nil)
			fakeStore.ListTasksReturns(nil, nil)

			dispatcher := func(_ context.Context, req agui.EventRequest) (string, error) {
				dispatchCount.Add(1)
				Expect(req.Source).To(Equal("cron:every-min"))
				return "run-abc", nil
			}

			sched := cron.Config{}.NewScheduler(fakeStore, dispatcher)
			sched.SetTickerIntervalForTest(50 * time.Millisecond)

			err := sched.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Wait for at least one dispatch.
			Eventually(func() int32 {
				return dispatchCount.Load()
			}, 2*time.Second, 10*time.Millisecond).Should(BeNumerically(">=", 1))

			Expect(sched.Stop()).To(Succeed())

			// Verify store interactions.
			Expect(fakeStore.RecordRunCallCount()).To(BeNumerically(">=", 1))
			Expect(fakeStore.UpdateRunCallCount()).To(BeNumerically(">=", 1))
			Expect(fakeStore.MarkTriggeredCallCount()).To(BeNumerically(">=", 1))
			Expect(fakeStore.SetNextRunCallCount()).To(BeNumerically(">=", 1))

			// Verify UpdateRun was called with success status.
			_, updateReq := fakeStore.UpdateRunArgsForCall(0)
			Expect(updateReq.Status).To(Equal(db.CronStatusSuccess))
			Expect(updateReq.RunID).To(Equal("run-abc"))
		})

		It("should record failure when dispatcher errors", func(ctx context.Context) {
			var dispatchCount atomic.Int32
			dueTask := cron.CronTask{
				ID: taskID2, Name: "failing-task", Expression: "* * * * *", Action: "fail",
			}
			fakeStore.DueTasksReturns([]cron.CronTask{dueTask}, nil)
			fakeStore.RecordRunReturns(&cron.CronHistory{
				ID: uuid.New(), TaskID: taskID2, TaskName: "failing-task", Status: db.CronStatusRunning,
			}, nil)
			fakeStore.ListTasksReturns(nil, nil)

			dispatcher := func(_ context.Context, _ agui.EventRequest) (string, error) {
				dispatchCount.Add(1)
				return "", fmt.Errorf("dispatch boom")
			}

			sched := cron.Config{}.NewScheduler(fakeStore, dispatcher)
			sched.SetTickerIntervalForTest(50 * time.Millisecond)

			err := sched.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() int32 {
				return dispatchCount.Load()
			}, 2*time.Second, 10*time.Millisecond).Should(BeNumerically(">=", 1))

			Expect(sched.Stop()).To(Succeed())

			// Verify UpdateRun was called with failed status.
			Expect(fakeStore.UpdateRunCallCount()).To(BeNumerically(">=", 1))
			_, updateReq := fakeStore.UpdateRunArgsForCall(0)
			Expect(updateReq.Status).To(Equal(db.CronStatusFailed))
			Expect(updateReq.Error).To(ContainSubstring("dispatch boom"))
		})

		It("should handle empty due tasks gracefully", func(ctx context.Context) {
			fakeStore.DueTasksReturns(nil, nil) // no due tasks
			fakeStore.ListTasksReturns(nil, nil)

			var dispatchCount atomic.Int32
			dispatcher := func(_ context.Context, _ agui.EventRequest) (string, error) {
				dispatchCount.Add(1)
				return "", nil
			}

			sched := cron.Config{}.NewScheduler(fakeStore, dispatcher)
			sched.SetTickerIntervalForTest(50 * time.Millisecond)

			err := sched.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Let a few ticks pass.
			time.Sleep(200 * time.Millisecond)
			Expect(sched.Stop()).To(Succeed())

			// DueTasks should have been called but no dispatches.
			Expect(fakeStore.DueTasksCallCount()).To(BeNumerically(">=", 1))
			Expect(dispatchCount.Load()).To(Equal(int32(0)))
		})

		It("should survive DueTasks query errors", func(ctx context.Context) {
			// DueTasks always returns error — scheduler should not crash.
			fakeStore.DueTasksReturns(nil, fmt.Errorf("db timeout"))
			fakeStore.ListTasksReturns(nil, nil)

			var dispatchCount atomic.Int32
			dispatcher := func(_ context.Context, _ agui.EventRequest) (string, error) {
				dispatchCount.Add(1)
				return "", nil
			}

			sched := cron.Config{}.NewScheduler(fakeStore, dispatcher)
			sched.SetTickerIntervalForTest(50 * time.Millisecond)

			err := sched.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			time.Sleep(200 * time.Millisecond)
			Expect(sched.Stop()).To(Succeed())

			// No dispatches should have happened.
			Expect(dispatchCount.Load()).To(Equal(int32(0)))
			// DueTasks should still have been called (scheduler kept retrying).
			Expect(fakeStore.DueTasksCallCount()).To(BeNumerically(">=", 1))
		})

		It("should handle RecordRun failure gracefully", func(ctx context.Context) {
			// RecordRun fails — executeAndAdvance should return error, but scheduler should not crash.
			dueTask := cron.CronTask{
				ID: taskID1, Name: "record-fail", Expression: "* * * * *", Action: "test",
			}
			fakeStore.DueTasksReturns([]cron.CronTask{dueTask}, nil)
			fakeStore.RecordRunReturns(nil, fmt.Errorf("db write failed"))
			fakeStore.ListTasksReturns(nil, nil)

			var dispatchCount atomic.Int32
			dispatcher := func(_ context.Context, _ agui.EventRequest) (string, error) {
				dispatchCount.Add(1)
				return "", nil
			}

			sched := cron.Config{}.NewScheduler(fakeStore, dispatcher)
			sched.SetTickerIntervalForTest(50 * time.Millisecond)

			err := sched.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			time.Sleep(200 * time.Millisecond)
			Expect(sched.Stop()).To(Succeed())

			// RecordRun should have been called but dispatch should NOT because
			// executeAndAdvance returns early on RecordRun failure.
			Expect(fakeStore.RecordRunCallCount()).To(BeNumerically(">=", 1))
			Expect(dispatchCount.Load()).To(Equal(int32(0)))
		})

		It("should skip tick when previous dispatch is still in-flight", func(ctx context.Context) {
			// Slow dispatcher that takes longer than the tick interval.
			dueTask := cron.CronTask{
				ID: taskID1, Name: "slow-task", Expression: "* * * * *", Action: "slow",
			}
			fakeStore.DueTasksReturns([]cron.CronTask{dueTask}, nil)
			fakeStore.RecordRunReturns(&cron.CronHistory{
				ID: uuid.New(), TaskID: taskID1, TaskName: "slow-task", Status: db.CronStatusRunning,
			}, nil)
			fakeStore.ListTasksReturns(nil, nil)

			var dispatchCount atomic.Int32
			dispatcher := func(_ context.Context, _ agui.EventRequest) (string, error) {
				dispatchCount.Add(1)
				time.Sleep(300 * time.Millisecond) // Much longer than tick interval
				return "run-slow", nil
			}

			sched := cron.Config{}.NewScheduler(fakeStore, dispatcher)
			sched.SetTickerIntervalForTest(50 * time.Millisecond)

			err := sched.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Wait for multiple ticks — the slow dispatcher blocks additional dispatches.
			time.Sleep(500 * time.Millisecond)
			Expect(sched.Stop()).To(Succeed())

			// Because dispatch takes 300ms and ticks are 50ms apart,
			// multiple ticks should have been skipped.
			// Dispatcher should have been called a small number of times (1-2),
			// NOT once per tick.
			Expect(dispatchCount.Load()).To(BeNumerically("<=", 3))
		})
	})

	Describe("HealthCheck", func() {
		It("should return nil when no failures exist", func() {
			fakeStore.RecentFailuresReturns(nil, nil)
			sched := cron.Config{}.NewScheduler(fakeStore, nil)
			results := sched.HealthCheck(ctx)
			Expect(results).To(BeNil())
		})

		It("should aggregate failures by task", func() {
			fakeStore.RecentFailuresReturns([]cron.CronHistory{
				{TaskID: taskID1, TaskName: "task-1", Error: "err1", Status: db.CronStatusFailed},
				{TaskID: taskID1, TaskName: "task-1", Error: "err2", Status: db.CronStatusFailed},
				{TaskID: taskID2, TaskName: "task-2", Error: "err3", Status: db.CronStatusFailed},
			}, nil)
			sched := cron.Config{}.NewScheduler(fakeStore, nil)
			results := sched.HealthCheck(ctx)
			Expect(results).To(HaveLen(2))

			var t1Result agui.HealthResult
			for _, r := range results {
				if r.Name == "task-1" {
					t1Result = r
				}
			}
			Expect(t1Result.FailureCount).To(Equal(2))
			Expect(t1Result.LastError).To(Equal("err1"))
		})

		It("should return nil when store query fails", func() {
			fakeStore.RecentFailuresReturns(nil, fmt.Errorf("db down"))
			sched := cron.Config{}.NewScheduler(fakeStore, nil)
			results := sched.HealthCheck(ctx)
			Expect(results).To(BeNil())
		})
	})

	Describe("Stop", func() {
		It("should be safe to call when not running", func() {
			sched := cron.Config{}.NewScheduler(fakeStore, nil)
			Expect(sched.Stop()).To(Succeed())
		})

		It("should stop the ticker loop", func(ctx context.Context) {
			fakeStore.DueTasksReturns(nil, nil)
			fakeStore.ListTasksReturns(nil, nil)

			dispatcher := func(_ context.Context, _ agui.EventRequest) (string, error) {
				return "", nil
			}
			sched := cron.Config{}.NewScheduler(fakeStore, dispatcher)
			sched.SetTickerIntervalForTest(50 * time.Millisecond)

			err := sched.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Let a few ticks run.
			time.Sleep(150 * time.Millisecond)
			callsBefore := fakeStore.DueTasksCallCount()

			Expect(sched.Stop()).To(Succeed())

			// Wait and confirm no more calls after stop.
			time.Sleep(200 * time.Millisecond)
			callsAfter := fakeStore.DueTasksCallCount()
			Expect(callsAfter - callsBefore).To(BeNumerically("<=", 1)) // at most 1 in-flight
		})

		It("should be safe to call multiple times", func(ctx context.Context) {
			fakeStore.ListTasksReturns(nil, nil)
			dispatcher := func(_ context.Context, _ agui.EventRequest) (string, error) {
				return "", nil
			}
			sched := cron.Config{}.NewScheduler(fakeStore, dispatcher)
			sched.SetTickerIntervalForTest(50 * time.Millisecond)

			err := sched.Start(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(sched.Stop()).To(Succeed())
			Expect(sched.Stop()).To(Succeed()) // idempotent
		})
	})
})
