package tasks

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"google.golang.org/api/tasks/v1"
)

type mockService struct {
	ListTaskListsFunc func(ctx context.Context, maxResults int) ([]*TaskListSummary, error)
	ListTasksFunc     func(ctx context.Context, taskListID string, showCompleted bool, maxResults int) ([]*TaskSummary, error)
	CreateTaskFunc    func(ctx context.Context, taskListID, title, notes, due string) (*TaskDetail, error)
	UpdateTaskFunc    func(ctx context.Context, taskListID, taskID, title, notes string, completed bool) error
	DeleteTaskFunc    func(ctx context.Context, taskListID, taskID string) error
	ValidateFunc      func(ctx context.Context) error
}

func (m *mockService) ListTaskLists(ctx context.Context, maxResults int) ([]*TaskListSummary, error) {
	if m.ListTaskListsFunc != nil {
		return m.ListTaskListsFunc(ctx, maxResults)
	}
	return []*TaskListSummary{}, nil
}

func (m *mockService) ListTasks(ctx context.Context, taskListID string, showCompleted bool, maxResults int) ([]*TaskSummary, error) {
	if m.ListTasksFunc != nil {
		return m.ListTasksFunc(ctx, taskListID, showCompleted, maxResults)
	}
	return []*TaskSummary{}, nil
}

func (m *mockService) CreateTask(ctx context.Context, taskListID, title, notes, due string) (*TaskDetail, error) {
	if m.CreateTaskFunc != nil {
		return m.CreateTaskFunc(ctx, taskListID, title, notes, due)
	}
	return &TaskDetail{}, nil
}

func (m *mockService) UpdateTask(ctx context.Context, taskListID, taskID, title, notes string, completed bool) error {
	if m.UpdateTaskFunc != nil {
		return m.UpdateTaskFunc(ctx, taskListID, taskID, title, notes, completed)
	}
	return nil
}

func (m *mockService) DeleteTask(ctx context.Context, taskListID, taskID string) error {
	if m.DeleteTaskFunc != nil {
		return m.DeleteTaskFunc(ctx, taskListID, taskID)
	}
	return nil
}

func (m *mockService) Validate(ctx context.Context) error {
	if m.ValidateFunc != nil {
		return m.ValidateFunc(ctx)
	}
	return nil
}

var _ = Describe("Google Tasks Tools", func() {
	var (
		svc *mockService
		ctx context.Context
		tt  *tasksTools
	)

	BeforeEach(func() {
		ctx = context.Background()
		svc = &mockService{}
		tt = newTasksTools("test_tasks", svc)
	})

	Describe("handleListTaskLists", func() {
		It("calls svc.ListTaskLists", func() {
			svc.ListTaskListsFunc = func(ctx context.Context, maxResults int) ([]*TaskListSummary, error) {
				return []*TaskListSummary{{ID: "1", Title: "list1"}}, nil
			}
			req := listTaskListsRequest{MaxResults: 10}
			res, err := tt.handleListTaskLists(ctx, req)
			Expect(err).To(Not(HaveOccurred()))
			Expect(res).To(HaveLen(1))
			Expect(res[0].ID).To(Equal("1"))
		})
	})

	Describe("handleListTasks", func() {
		It("calls svc.ListTasks", func() {
			svc.ListTasksFunc = func(ctx context.Context, taskListID string, showCompleted bool, maxResults int) ([]*TaskSummary, error) {
				return []*TaskSummary{{ID: "t1", Title: "task1"}}, nil
			}
			req := listTasksRequest{TaskListID: "l1", ShowCompleted: true, MaxResults: 10}
			res, err := tt.handleListTasks(ctx, req)
			Expect(err).To(Not(HaveOccurred()))
			Expect(res).To(HaveLen(1))
			Expect(res[0].ID).To(Equal("t1"))
		})
	})

	Describe("handleCreateTask", func() {
		It("calls svc.CreateTask", func() {
			svc.CreateTaskFunc = func(ctx context.Context, taskListID, title, notes, due string) (*TaskDetail, error) {
				return &TaskDetail{ID: "tnew", Title: title}, nil
			}
			req := createTaskRequest{TaskListID: "l1", Title: "newtask", Notes: "a note", Due: "2024-01-01"}
			res, err := tt.handleCreateTask(ctx, req)
			Expect(err).To(Not(HaveOccurred()))
			Expect(res).To(Not(BeNil()))
			Expect(res.Title).To(Equal("newtask"))
		})
	})

	Describe("handleUpdateTask", func() {
		It("calls svc.UpdateTask successfully", func() {
			svc.UpdateTaskFunc = func(ctx context.Context, taskListID, taskID, title, notes string, completed bool) error {
				return nil
			}
			req := updateTaskRequest{TaskListID: "l1", TaskID: "t1", Title: "updatedtask", Notes: "updated notes", Completed: true}
			res, err := tt.handleUpdateTask(ctx, req)
			Expect(err).To(Not(HaveOccurred()))
			Expect(res).To(Equal("Task updated."))
		})

		It("returns error when UpdateTask fails", func() {
			svc.UpdateTaskFunc = func(ctx context.Context, taskListID, taskID, title, notes string, completed bool) error {
				return errors.New("update err")
			}
			req := updateTaskRequest{TaskListID: "l1", TaskID: "t1", Title: "updatedtask"}
			_, err := tt.handleUpdateTask(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("update err"))
		})
	})

	Describe("handleDeleteTask", func() {
		It("calls svc.DeleteTask successfully", func() {
			svc.DeleteTaskFunc = func(ctx context.Context, taskListID, taskID string) error {
				return nil
			}
			req := deleteTaskRequest{TaskListID: "l1", TaskID: "t1"}
			res, err := tt.handleDeleteTask(ctx, req)
			Expect(err).To(Not(HaveOccurred()))
			Expect(res).To(Equal("Task deleted."))
		})

		It("returns error when DeleteTask fails", func() {
			svc.DeleteTaskFunc = func(ctx context.Context, taskListID, taskID string) error {
				return errors.New("delete err")
			}
			req := deleteTaskRequest{TaskListID: "l1", TaskID: "t1"}
			_, err := tt.handleDeleteTask(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("delete err"))
		})
	})

	Describe("AllTools", func() {
		It("returns the correct number of tools", func() {
			tools := AllTools("test", svc)
			Expect(tools).To(HaveLen(5))
		})
	})
	
	Describe("taskDetailFromAPI", func() {
		It("maps fields correctly", func() {
			t := "completed"
			apiTask := &tasks.Task{
				Id:        "123",
				Title:     "Title",
				Notes:     "Notes",
				Status:    "completed",
				Due:       "2023-12-01",
				Updated:   "2023-11-01",
				Completed: &t,
			}
			detail := taskDetailFromAPI(apiTask)
			Expect(detail.ID).To(Equal("123"))
			Expect(detail.Title).To(Equal("Title"))
			Expect(detail.Notes).To(Equal("Notes"))
			Expect(detail.Status).To(Equal("completed"))
			Expect(detail.Due).To(Equal("2023-12-01"))
			Expect(detail.Updated).To(Equal("2023-11-01"))
			Expect(detail.Completed).To(Equal("completed"))
		})
	})
})
