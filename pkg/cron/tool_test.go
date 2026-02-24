package cron_test

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
		Expect(t.Declaration().Name).To(Equal("create_recurring_task"))
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

			// Verify CreateTask was called with correct params.
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
			// SetNextRun failure is now a warning — the tool should succeed
			// and the scheduler's Start() will compute NextRunAt later.
			Expect(err).NotTo(HaveOccurred())
			Expect(fmt.Sprintf("%v", result)).To(ContainSubstring("next-run-fail"))
			Expect(fakeStore.CreateTaskCallCount()).To(Equal(1))
			Expect(fakeStore.SetNextRunCallCount()).To(Equal(1)) // Should have been attempted.
		})
	})
})
