package toolwrap_test

import (
	"context"
	"errors"
	"strings"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/audit/auditfakes"

	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/hitl/hitlfakes"
	"github.com/appcd-dev/genie/pkg/toolwrap"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// --- Fake tools ---

type fakeTool struct {
	name      string
	callCount int
	result    string
}

func (f *fakeTool) Declaration() *tool.Declaration { return &tool.Declaration{Name: f.name} }
func (f *fakeTool) Call(_ context.Context, _ []byte) (any, error) {
	f.callCount++
	return f.result, nil
}

type fakeErrorTool struct {
	name string
	err  error
}

func (f *fakeErrorTool) Declaration() *tool.Declaration { return &tool.Declaration{Name: f.name} }
func (f *fakeErrorTool) Call(_ context.Context, _ []byte) (any, error) {
	return nil, f.err
}

type fakeNonCallableTool struct{ name string }

func (f *fakeNonCallableTool) Declaration() *tool.Declaration { return &tool.Declaration{Name: f.name} }

type ctxCapturingTool struct {
	name        string
	result      string
	capturedCtx context.Context
}

func (f *ctxCapturingTool) Declaration() *tool.Declaration { return &tool.Declaration{Name: f.name} }
func (f *ctxCapturingTool) Call(ctx context.Context, _ []byte) (any, error) {
	f.capturedCtx = ctx
	return f.result, nil
}

type dynamicResultTool struct {
	name     string
	callFunc func() (any, error)
}

func (t *dynamicResultTool) Declaration() *tool.Declaration                { return &tool.Declaration{Name: t.name} }
func (t *dynamicResultTool) Call(_ context.Context, _ []byte) (any, error) { return t.callFunc() }

// --- Tests ---

var _ = Describe("Wrapper", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("Call", func() {
		It("should execute a callable tool and return its result", func() {
			ft := &fakeTool{name: "read_file", result: "content"}
			w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})

			result, err := w.Call(ctx, []byte(`{"file_name":"main.tf"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("content"))
			Expect(ft.callCount).To(Equal(1))
		})

		It("should return error for non-callable tools", func() {
			nc := &fakeNonCallableTool{name: "broken"}
			w := toolwrap.NewWrapper(nc, toolwrap.MiddlewareDeps{})

			result, err := w.Call(ctx, []byte(`{}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not callable"))
			Expect(result).To(BeNil())
		})

		It("should forward errors from the underlying tool", func() {
			ft := &fakeErrorTool{name: "run_shell", err: errors.New("segfault")}
			w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})

			result, err := w.Call(ctx, []byte(`{}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("segfault"))
			Expect(result).To(BeNil())
		})

		It("should work with nil middleware (lazy defaults)", func() {
			ft := &fakeTool{name: "read_file", result: "ok"}
			w := &toolwrap.Wrapper{Tool: ft}

			result, err := w.Call(ctx, []byte(`{}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("ok"))
		})
	})

	Describe("Declaration delegation", func() {
		It("should delegate Declaration to the underlying tool", func() {
			ft := &fakeTool{name: "my_tool"}
			w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})
			Expect(w.Declaration().Name).To(Equal("my_tool"))
		})
	})
})

var _ = Describe("TUI event emission", func() {
	It("should emit AgentToolResponseMsg on the event channel", func() {
		ft := &fakeTool{name: "read_file", result: "content"}
		eventChan := make(chan interface{}, 10)
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{EventChan: eventChan})

		_, err := w.Call(context.Background(), []byte(`{"file_name":"main.tf"}`))
		Expect(err).NotTo(HaveOccurred())

		Eventually(eventChan).Should(Receive(Satisfy(func(msg interface{}) bool {
			toolMsg, ok := msg.(agui.AgentToolResponseMsg)
			return ok && toolMsg.ToolName == "read_file" && toolMsg.Response == "content"
		})))
	})

	It("should NOT panic when EventChan is nil", func() {
		ft := &fakeTool{name: "read_file", result: "content"}
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})

		result, err := w.Call(context.Background(), []byte(`{}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("content"))
	})

	It("should truncate large responses in emitted events", func() {
		longResult := strings.Repeat("a", 90000)
		ft := &fakeTool{name: "execute_code", result: longResult}
		eventChan := make(chan interface{}, 10)
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{EventChan: eventChan})

		_, _ = w.Call(context.Background(), []byte(`{}`))

		Eventually(eventChan).Should(Receive(Satisfy(func(msg interface{}) bool {
			toolMsg, ok := msg.(agui.AgentToolResponseMsg)
			return ok && strings.Contains(toolMsg.Response, "[truncated") && len(toolMsg.Response) < 90000
		})))
	})

	It("should not panic on closed EventChan (recovered by PanicRecovery)", func() {
		ft := &fakeTool{name: "read_file", result: "content"}
		eventChan := make(chan interface{}, 10)
		close(eventChan)
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{EventChan: eventChan})

		Expect(func() {
			_, err := w.Call(context.Background(), []byte(`{}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("internal error"))
		}).NotTo(Panic())
	})
})

var _ = Describe("loop detection", func() {
	It("should detect a loop after 3 identical consecutive calls", func() {
		ft := &fakeTool{name: "list_issues", result: "data"}
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})
		args := []byte(`{"status":"open"}`)

		_, err := w.Call(context.Background(), args)
		Expect(err).NotTo(HaveOccurred())
		_, err = w.Call(context.Background(), args)
		Expect(err).NotTo(HaveOccurred())
		_, err = w.Call(context.Background(), args)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("loop detected"))
		Expect(ft.callCount).To(Equal(2))
	})

	It("should NOT flag calls with different arguments as a loop", func() {
		ft := &fakeTool{name: "search", result: "results"}
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})

		_, _ = w.Call(context.Background(), []byte(`{"q":"foo"}`))
		_, _ = w.Call(context.Background(), []byte(`{"q":"bar"}`))
		_, _ = w.Call(context.Background(), []byte(`{"q":"baz"}`))
		Expect(ft.callCount).To(Equal(3))
	})

	It("should reset loop tracking when args change mid-sequence", func() {
		ft := &fakeTool{name: "list_issues", result: "data"}
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})

		sameArgs := []byte(`{"status":"open"}`)
		diffArgs := []byte(`{"status":"closed"}`)

		_, _ = w.Call(context.Background(), sameArgs)
		_, _ = w.Call(context.Background(), sameArgs)
		_, _ = w.Call(context.Background(), diffArgs)
		_, _ = w.Call(context.Background(), sameArgs)
		_, _ = w.Call(context.Background(), sameArgs)
		Expect(ft.callCount).To(Equal(5))
	})
})

var _ = Describe("consecutive failure limit", func() {
	It("should block tool after 3 consecutive failures", func() {
		ft := &fakeErrorTool{name: "web_search", err: errors.New("rate limited")}
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})

		for i := 0; i < 3; i++ {
			args := []byte(`{"q":"query-` + strings.Repeat("x", i) + `"}`)
			_, err := w.Call(context.Background(), args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("rate limited"))
		}

		_, err := w.Call(context.Background(), []byte(`{"q":"different"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("consecutively"))
	})

	It("should reset failure count after successful call", func() {
		callNum := 0
		dt := &dynamicResultTool{
			name: "web_search",
			callFunc: func() (any, error) {
				callNum++
				if callNum <= 2 {
					return nil, errors.New("rate limited")
				}
				return "success", nil
			},
		}
		w := toolwrap.NewWrapper(dt, toolwrap.MiddlewareDeps{})

		_, _ = w.Call(context.Background(), []byte(`{"q":"test1"}`))
		_, _ = w.Call(context.Background(), []byte(`{"q":"test2"}`))
		result, err := w.Call(context.Background(), []byte(`{"q":"test3"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("success"))
	})
})

var _ = Describe("semantic cache", func() {
	It("should cache and return results for semantically keyed tools", func() {
		ft := &fakeTool{name: "create_recurring_task", result: "task-created"}
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{
			SemanticKeyFields: map[string][]string{
				"create_recurring_task": {"name"},
			},
		})
		args := []byte(`{"name":"daily_standup","action":"run report"}`)

		r1, err := w.Call(context.Background(), args)
		Expect(err).NotTo(HaveOccurred())
		Expect(r1).To(Equal("task-created"))
		count := ft.callCount

		r2, err := w.Call(context.Background(), args)
		Expect(err).NotTo(HaveOccurred())
		Expect(r2).To(Equal("task-created"))
		Expect(ft.callCount).To(Equal(count)) // NOT called again
	})
})

var _ = Describe("HITL approval gate", func() {
	It("should skip approval for allowed tools", func() {
		store := &hitlfakes.FakeApprovalStore{}
		store.IsAllowedStub = func(name string) bool { return name == "read_file" }

		ft := &fakeTool{name: "read_file", result: "content"}
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{
			ApprovalStore: store, ThreadID: "t1", RunID: "r1",
		})

		result, err := w.Call(context.Background(), []byte(`{}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("content"))
		Expect(store.CreateCallCount()).To(Equal(0))
	})

	It("should block until approved then execute", func() {
		store := &hitlfakes.FakeApprovalStore{}
		store.CreateStub = func(_ context.Context, req hitl.CreateRequest) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{ID: "fake-" + req.ToolName, ToolName: req.ToolName, Status: hitl.StatusPending}, nil
		}
		store.WaitForResolutionStub = func(_ context.Context, _ string) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{Status: hitl.StatusApproved}, nil
		}

		ft := &fakeTool{name: "write_file", result: "written"}
		eventChan := make(chan interface{}, 10)
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{
			ApprovalStore: store, EventChan: eventChan, ThreadID: "t1", RunID: "r1",
		})

		result, err := w.Call(context.Background(), []byte(`{"path":"out.txt"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("written"))
		Expect(store.CreateCallCount()).To(Equal(1))
	})

	It("should return error when rejected", func() {
		store := &hitlfakes.FakeApprovalStore{}
		store.CreateStub = func(_ context.Context, req hitl.CreateRequest) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{ID: "rej", ToolName: req.ToolName, Status: hitl.StatusPending}, nil
		}
		store.WaitForResolutionStub = func(_ context.Context, _ string) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{Status: hitl.StatusRejected}, nil
		}

		ft := &fakeTool{name: "execute_code", result: "no"}
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{
			ApprovalStore: store, EventChan: make(chan interface{}, 10), ThreadID: "t1", RunID: "r1",
		})

		_, err := w.Call(context.Background(), []byte(`{}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("rejected"))
		Expect(ft.callCount).To(Equal(0))
	})

	It("should auto-approve second call with same tool+args", func() {
		store := &hitlfakes.FakeApprovalStore{}
		store.CreateStub = func(_ context.Context, req hitl.CreateRequest) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{ID: "c-" + req.ToolName, ToolName: req.ToolName, Status: hitl.StatusPending}, nil
		}
		store.WaitForResolutionStub = func(_ context.Context, _ string) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{Status: hitl.StatusApproved}, nil
		}

		ft := &fakeTool{name: "search_runbook", result: "content"}
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{
			ApprovalStore: store, EventChan: make(chan interface{}, 20), ThreadID: "s1", RunID: "r1",
		})

		args := []byte(`{"query":"cpu usage"}`)
		_, _ = w.Call(context.Background(), args)
		Expect(store.CreateCallCount()).To(Equal(1))

		_, _ = w.Call(context.Background(), args)
		Expect(store.CreateCallCount()).To(Equal(1)) // still 1 — auto-approved
	})
})

var _ = Describe("context enrichment", func() {
	It("should propagate ThreadID and RunID into the tool context", func() {
		ct := &ctxCapturingTool{name: "run_shell", result: "ok"}
		w := toolwrap.NewWrapper(ct, toolwrap.MiddlewareDeps{
			ThreadID: "struct-thread", RunID: "struct-run",
		})

		_, err := w.Call(context.Background(), []byte(`{}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(agui.ThreadIDFromContext(ct.capturedCtx)).To(Equal("struct-thread"))
		Expect(agui.RunIDFromContext(ct.capturedCtx)).To(Equal("struct-run"))
	})

	It("should fall back to context values when deps fields are empty", func() {
		ct := &ctxCapturingTool{name: "run_shell", result: "ok"}
		w := toolwrap.NewWrapper(ct, toolwrap.MiddlewareDeps{})

		parentCtx := agui.WithThreadID(context.Background(), "ctx-thread")
		parentCtx = agui.WithRunID(parentCtx, "ctx-run")

		_, err := w.Call(parentCtx, []byte(`{}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(agui.ThreadIDFromContext(ct.capturedCtx)).To(Equal("ctx-thread"))
		Expect(agui.RunIDFromContext(ct.capturedCtx)).To(Equal("ctx-run"))
	})
})

var _ = Describe("audit integration", func() {
	It("should not panic when Auditor is nil", func() {
		ft := &fakeTool{name: "read_file", result: "content"}
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})

		result, err := w.Call(context.Background(), []byte(`{}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("content"))
	})

	It("should audit tool calls when Auditor is set", func() {
		ft := &fakeTool{name: "read_file", result: "content"}
		auditor := &auditfakes.FakeAuditor{}
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{Auditor: auditor})

		_, err := w.Call(context.Background(), []byte(`{}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(auditor.LogCallCount()).To(Equal(1))
	})
})

var _ = Describe("TruncateForAudit", func() {
	It("should return original string when shorter than maxLen", func() {
		Expect(toolwrap.TruncateForAudit("short", 10)).To(Equal("short"))
	})

	It("should truncate and append ellipsis when longer", func() {
		Expect(toolwrap.TruncateForAudit("this is long", 4)).To(Equal("this…"))
	})

	It("should handle multi-byte characters", func() {
		Expect(toolwrap.TruncateForAudit("こんにちは", 3)).To(Equal("こんに…"))
	})
})

var _ = Describe("redactSensitiveArgs", func() {
	It("should return empty for empty args", func() {
		Expect(toolwrap.RedactSensitiveArgsForTest(nil)).To(Equal(""))
	})

	It("should redact top-level sensitive keys", func() {
		result := toolwrap.RedactSensitiveArgsForTest([]byte(`{"api_key":"sk-secret","query":"hello"}`))
		Expect(result).To(ContainSubstring("[REDACTED]"))
		Expect(result).NotTo(ContainSubstring("sk-secret"))
		Expect(result).To(ContainSubstring("hello"))
	})

	It("should redact nested sensitive keys", func() {
		result := toolwrap.RedactSensitiveArgsForTest([]byte(`{"config":{"password":"mypass","host":"localhost"}}`))
		Expect(result).NotTo(ContainSubstring("mypass"))
		Expect(result).To(ContainSubstring("localhost"))
	})

	It("should truncate oversized args", func() {
		large := `{"data":"` + strings.Repeat("x", 5000) + `"}`
		result := toolwrap.RedactSensitiveArgsForTest([]byte(large))
		Expect(result).To(ContainSubstring("_truncated"))
	})
})

var _ = Describe("semanticKey", func() {
	It("should return false for unknown tool names", func() {
		_, ok := toolwrap.SemanticKeyForTest("read_file", []byte(`{}`))
		Expect(ok).To(BeFalse())
	})

	It("should return key for create_recurring_task", func() {
		key, ok := toolwrap.SemanticKeyForTest("create_recurring_task", []byte(`{"name":"daily_standup"}`))
		Expect(ok).To(BeTrue())
		Expect(key).To(Equal("create_recurring_task:daily_standup"))
	})

	It("should return false when required key field is missing", func() {
		_, ok := toolwrap.SemanticKeyForTest("create_recurring_task", []byte(`{"action":"run"}`))
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("Service.Wrap", func() {
	It("should wrap all tools preserving declarations", func() {
		svc := toolwrap.NewService(nil, nil, nil)
		ft1 := &fakeTool{name: "read_file", result: "c1"}
		ft2 := &fakeTool{name: "write_file", result: "c2"}

		wrapped := svc.Wrap([]tool.Tool{ft1, ft2}, toolwrap.WrapRequest{})
		Expect(wrapped).To(HaveLen(2))
		Expect(wrapped[0].Declaration().Name).To(Equal("read_file"))
		Expect(wrapped[1].Declaration().Name).To(Equal("write_file"))
	})

	It("should return empty slice for empty tools", func() {
		svc := toolwrap.NewService(nil, nil, nil)
		Expect(svc.Wrap(nil, toolwrap.WrapRequest{})).To(BeEmpty())
	})
})

// =============================================================================
// ToolCaller Delegation Tests
// =============================================================================

// fakeToolCaller is a test double for the ToolCaller interface.
type fakeToolCaller struct {
	callCount int
	toolName  string
	args      []byte
	result    any
	err       error
}

func (c *fakeToolCaller) CallTool(_ context.Context, toolName string, args []byte) (any, error) {
	c.callCount++
	c.toolName = toolName
	c.args = args
	return c.result, c.err
}
