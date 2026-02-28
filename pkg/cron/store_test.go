package cron_test

import (
	"context"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/cron"
	"github.com/stackgenhq/genie/pkg/db"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openTestDB creates a fresh SQLite DB in a temp directory for testing.
func openTestDB() *gorm.DB {
	tmpDir := GinkgoT().TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	Expect(err).NotTo(HaveOccurred())

	err = db.AutoMigrate(&cron.CronTask{}, &cron.CronHistory{})
	Expect(err).NotTo(HaveOccurred())

	return db
}

var _ = Describe("Store", func() {
	var (
		store *cron.Store
	)

	BeforeEach(func() {
		db := openTestDB()
		store = cron.NewStore(db)
	})

	Describe("CreateTask", func() {
		It("should create a task and assign an ID", func(ctx context.Context) {
			task, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name:       "test-task",
				Expression: "*/5 * * * *",
				Action:     "run health check",
				Source:     "config",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(task).NotTo(BeNil())
			Expect(task.ID).NotTo(BeEmpty())
			Expect(task.Name).To(Equal("test-task"))
			Expect(task.Expression).To(Equal("*/5 * * * *"))
			Expect(task.Action).To(Equal("run health check"))
			Expect(task.Enabled).To(BeTrue())
			Expect(task.Source).To(Equal("config"))
		})

		It("should upsert on duplicate task name with updated expression", func(ctx context.Context) {
			_, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name:       "upsert-task",
				Expression: "0 * * * *",
				Action:     "do something",
				Source:     "config",
			})
			Expect(err).NotTo(HaveOccurred())

			// Upsert with a new expression and action.
			updated, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name:       "upsert-task",
				Expression: "*/5 * * * *",
				Action:     "do something else",
				Source:     "tool",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(updated).NotTo(BeNil())

			// Verify only one task exists and it has the updated values.
			tasks, err := store.ListTasks(ctx, cron.ListTasksRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(tasks).To(HaveLen(1))
			Expect(tasks[0].Expression).To(Equal("*/5 * * * *"))
			Expect(tasks[0].Action).To(Equal("do something else"))
			Expect(tasks[0].Source).To(Equal("tool"))
		})
	})

	Describe("ListTasks", func() {
		It("should list all tasks", func(ctx context.Context) {
			_, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name: "task-1", Expression: "* * * * *", Action: "a", Source: "config",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = store.CreateTask(ctx, cron.CreateTaskRequest{
				Name: "task-2", Expression: "* * * * *", Action: "b", Source: "tool",
			})
			Expect(err).NotTo(HaveOccurred())

			tasks, err := store.ListTasks(ctx, cron.ListTasksRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(tasks).To(HaveLen(2))
		})

		It("should filter enabled-only tasks", func(ctx context.Context) {
			_, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name: "enabled-task", Expression: "* * * * *", Action: "a", Source: "config",
			})
			Expect(err).NotTo(HaveOccurred())

			// All tasks are enabled by default
			tasks, err := store.ListTasks(ctx, cron.ListTasksRequest{EnabledOnly: true})
			Expect(err).NotTo(HaveOccurred())
			Expect(tasks).To(HaveLen(1))
		})
	})

	Describe("DeleteTask", func() {
		It("should delete a task by ID", func(ctx context.Context) {
			task, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name: "delete-me", Expression: "* * * * *", Action: "a", Source: "config",
			})
			Expect(err).NotTo(HaveOccurred())

			err = store.DeleteTask(ctx, cron.DeleteTaskRequest{ID: task.ID})
			Expect(err).NotTo(HaveOccurred())

			tasks, err := store.ListTasks(ctx, cron.ListTasksRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(tasks).To(BeEmpty())
		})
	})

	Describe("RecordRun and UpdateRun", func() {
		It("should record a run and update it on completion", func(ctx context.Context) {
			task, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name: "run-task", Expression: "* * * * *", Action: "a", Source: "config",
			})
			Expect(err).NotTo(HaveOccurred())

			history, err := store.RecordRun(ctx, cron.RecordRunRequest{
				TaskID:   task.ID,
				TaskName: task.Name,
				Status:   "running",
				RunID:    "run-123",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(history).NotTo(BeNil())
			Expect(history.Status).To(Equal(db.CronStatusRunning))

			err = store.UpdateRun(ctx, cron.UpdateRunRequest{
				HistoryID: history.ID,
				Status:    "success",
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("RecentFailures", func() {
		It("should return recent failed runs", func(ctx context.Context) {
			task, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name: "failing-task", Expression: "* * * * *", Action: "a", Source: "config",
			})
			Expect(err).NotTo(HaveOccurred())

			// Record a successful run (should not appear).
			_, err = store.RecordRun(ctx, cron.RecordRunRequest{
				TaskID: task.ID, TaskName: task.Name, Status: "success",
			})
			Expect(err).NotTo(HaveOccurred())

			// Record a failed run.
			_, err = store.RecordRun(ctx, cron.RecordRunRequest{
				TaskID: task.ID, TaskName: task.Name, Status: "failed", Error: "timeout",
			})
			Expect(err).NotTo(HaveOccurred())

			failures, err := store.RecentFailures(ctx, cron.RecentFailuresRequest{Limit: 10})
			Expect(err).NotTo(HaveOccurred())
			Expect(failures).To(HaveLen(1))
			Expect(failures[0].Error).To(Equal("timeout"))
		})

		It("should return empty when no failures exist", func(ctx context.Context) {
			failures, err := store.RecentFailures(ctx, cron.RecentFailuresRequest{Limit: 10})
			Expect(err).NotTo(HaveOccurred())
			Expect(failures).To(BeEmpty())
		})
	})

	Describe("DueTasks", func() {
		It("should return tasks whose NextRunAt is in the past", func(ctx context.Context) {
			task, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name: "due-task", Expression: "* * * * *", Action: "a", Source: "config",
			})
			Expect(err).NotTo(HaveOccurred())

			// Set NextRunAt to the past.
			past := time.Now().Add(-1 * time.Minute)
			err = store.SetNextRun(ctx, task.ID, past)
			Expect(err).NotTo(HaveOccurred())

			dueTasks, err := store.DueTasks(ctx, time.Now())
			Expect(err).NotTo(HaveOccurred())
			Expect(dueTasks).To(HaveLen(1))
			Expect(dueTasks[0].Name).To(Equal("due-task"))
		})

		It("should not return tasks whose NextRunAt is in the future", func(ctx context.Context) {
			task, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name: "future-task", Expression: "* * * * *", Action: "a", Source: "config",
			})
			Expect(err).NotTo(HaveOccurred())

			future := time.Now().Add(10 * time.Minute)
			err = store.SetNextRun(ctx, task.ID, future)
			Expect(err).NotTo(HaveOccurred())

			dueTasks, err := store.DueTasks(ctx, time.Now())
			Expect(err).NotTo(HaveOccurred())
			Expect(dueTasks).To(BeEmpty())
		})

		It("should not return tasks with no NextRunAt set", func(ctx context.Context) {
			_, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name: "no-next-run", Expression: "* * * * *", Action: "a", Source: "config",
			})
			Expect(err).NotTo(HaveOccurred())
			// Don't call SetNextRun — NextRunAt is nil.

			dueTasks, err := store.DueTasks(ctx, time.Now())
			Expect(err).NotTo(HaveOccurred())
			Expect(dueTasks).To(BeEmpty())
		})
	})

	Describe("MarkTriggered", func() {
		It("should set LastTriggeredAt on the task", func(ctx context.Context) {
			task, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name: "trigger-task", Expression: "* * * * *", Action: "a", Source: "config",
			})
			Expect(err).NotTo(HaveOccurred())

			now := time.Now()
			err = store.MarkTriggered(ctx, task.ID, now)
			Expect(err).NotTo(HaveOccurred())

			tasks, err := store.ListTasks(ctx, cron.ListTasksRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(tasks[0].LastTriggeredAt).NotTo(BeNil())
		})
	})

	Describe("SetNextRun", func() {
		It("should update NextRunAt on the task", func(ctx context.Context) {
			task, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name: "next-run-task", Expression: "* * * * *", Action: "a", Source: "config",
			})
			Expect(err).NotTo(HaveOccurred())

			nextRun := time.Now().Add(5 * time.Minute)
			err = store.SetNextRun(ctx, task.ID, nextRun)
			Expect(err).NotTo(HaveOccurred())

			tasks, err := store.ListTasks(ctx, cron.ListTasksRequest{})
			Expect(err).NotTo(HaveOccurred())
			Expect(tasks[0].NextRunAt).NotTo(BeNil())
		})
	})

	Describe("CleanupHistory", func() {
		It("should delete history entries older than the given duration", func(ctx context.Context) {
			task, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name: "cleanup-task", Expression: "* * * * *", Action: "a", Source: "config",
			})
			Expect(err).NotTo(HaveOccurred())

			// Record a few runs.
			for i := 0; i < 3; i++ {
				_, err = store.RecordRun(ctx, cron.RecordRunRequest{
					TaskID: task.ID, TaskName: task.Name, Status: db.CronStatusSuccess,
				})
				Expect(err).NotTo(HaveOccurred())
			}

			// Cleanup with 0 duration should delete all entries.
			deleted, err := store.CleanupHistory(ctx, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(deleted).To(Equal(int64(3)))
		})

		It("should not delete recent history entries", func(ctx context.Context) {
			task, err := store.CreateTask(ctx, cron.CreateTaskRequest{
				Name: "keep-task", Expression: "* * * * *", Action: "a", Source: "config",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = store.RecordRun(ctx, cron.RecordRunRequest{
				TaskID: task.ID, TaskName: task.Name, Status: db.CronStatusSuccess,
			})
			Expect(err).NotTo(HaveOccurred())

			// Cleanup with 1 hour — entry is fresh so should be kept.
			deleted, err := store.CleanupHistory(ctx, 1*time.Hour)
			Expect(err).NotTo(HaveOccurred())
			Expect(deleted).To(Equal(int64(0)))
		})
	})
})
