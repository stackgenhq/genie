package toolwrap_test

import (
	"context"
	"errors"
	"strings"

	"github.com/stackgenhq/genie/pkg/audit/auditfakes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/tools/toolsfakes"
	"github.com/stackgenhq/genie/pkg/toolwrap"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// --- Fake tool helpers using counterfeiter-generated fakes ---

// newFakeTool creates a FakeCallableTool pre-configured with a name and result.
func newFakeTool(name, result string) *toolsfakes.FakeCallableTool {
	ft := &toolsfakes.FakeCallableTool{}
	ft.DeclarationReturns(&tool.Declaration{Name: name})
	ft.CallReturns(result, nil)
	return ft
}

// newFakeErrorTool creates a FakeCallableTool that always returns an error.
func newFakeErrorTool(name string, err error) *toolsfakes.FakeCallableTool {
	ft := &toolsfakes.FakeCallableTool{}
	ft.DeclarationReturns(&tool.Declaration{Name: name})
	ft.CallReturns(nil, err)
	return ft
}

// nonCallableTool is a minimal tool.Tool that does NOT implement Call().
// Kept as hand-rolled because there is no counterfeiter FakeTool for tool.Tool.
type nonCallableTool struct{ name string }

func (f *nonCallableTool) Declaration() *tool.Declaration { return &tool.Declaration{Name: f.name} }

func newFakeNonCallableTool(name string) *nonCallableTool {
	return &nonCallableTool{name: name}
}

// ctxCapturingTool captures the context passed to Call for assertion.
// Kept as a hand-rolled type because FakeCallableTool cannot expose capturedCtx.
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

// dynamicResultTool uses a callback to determine its result.
// Kept as a hand-rolled type because FakeCallableTool.CallStub would work
// but loses the struct literal convenience used in tests.
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
			ft := newFakeTool("read_file", "content")
			w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})

			result, err := w.Call(ctx, []byte(`{"file_name":"main.tf"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("content"))
			Expect(ft.CallCallCount()).To(Equal(1))
		})

		It("should return error for non-callable tools", func() {
			nc := newFakeNonCallableTool("broken")
			w := toolwrap.NewWrapper(nc, toolwrap.MiddlewareDeps{})

			result, err := w.Call(ctx, []byte(`{}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not callable"))
			Expect(result).To(BeNil())
		})

		It("should forward errors from the underlying tool", func() {
			ft := newFakeErrorTool("run_shell", errors.New("segfault"))
			w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})

			result, err := w.Call(ctx, []byte(`{}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("segfault"))
			Expect(result).To(BeNil())
		})

		It("should work with nil middleware (lazy defaults)", func() {
			ft := newFakeTool("read_file", "ok")
			w := &toolwrap.Wrapper{Tool: ft}

			result, err := w.Call(ctx, []byte(`{}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("ok"))
		})
	})

	Describe("Declaration delegation", func() {
		It("should delegate Declaration to the underlying tool", func() {
			ft := newFakeTool("my_tool", "")
			w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})
			Expect(w.Declaration().Name).To(Equal("my_tool"))
		})
	})
})

var _ = Describe("TUI event emission", func() {
	It("should NOT panic when EventChan is nil", func() {
		ft := newFakeTool("read_file", "content")
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})

		result, err := w.Call(context.Background(), []byte(`{}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("content"))
	})

	PIt("should truncate large responses in emitted events", func() {
		// TODO: implement TUI emitter middleware that sends AgentToolResponseMsg
		// to the EventChan on each tool call completion.
	})

	PIt("should not panic on closed EventChan (recovered by PanicRecovery)", func() {
		// TODO: implement TUI emitter middleware; then test that a closed
		// channel is handled gracefully by PanicRecovery.
	})
})

var _ = Describe("loop detection", func() {
	It("should detect a loop after 3 identical consecutive calls", func() {
		ft := newFakeTool("list_issues", "data")
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})
		args := []byte(`{"status":"open"}`)

		_, err := w.Call(context.Background(), args)
		Expect(err).NotTo(HaveOccurred())
		_, err = w.Call(context.Background(), args)
		Expect(err).NotTo(HaveOccurred())
		_, err = w.Call(context.Background(), args)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("loop detected"))
		Expect(ft.CallCallCount()).To(Equal(2))
	})

	It("should NOT flag calls with different arguments as a loop", func() {
		ft := newFakeTool("search", "results")
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})

		_, _ = w.Call(context.Background(), []byte(`{"q":"foo"}`))
		_, _ = w.Call(context.Background(), []byte(`{"q":"bar"}`))
		_, _ = w.Call(context.Background(), []byte(`{"q":"baz"}`))
		Expect(ft.CallCallCount()).To(Equal(3))
	})

	It("should reset loop tracking when args change mid-sequence", func() {
		ft := newFakeTool("list_issues", "data")
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})

		sameArgs := []byte(`{"status":"open"}`)
		diffArgs := []byte(`{"status":"closed"}`)

		_, _ = w.Call(context.Background(), sameArgs)
		_, _ = w.Call(context.Background(), sameArgs)
		_, _ = w.Call(context.Background(), diffArgs)
		_, _ = w.Call(context.Background(), sameArgs)
		_, _ = w.Call(context.Background(), sameArgs)
		Expect(ft.CallCallCount()).To(Equal(5))
	})
})

var _ = Describe("consecutive failure limit", func() {
	It("should block tool after 3 consecutive failures", func() {
		ft := newFakeErrorTool("web_search", errors.New("rate limited"))
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
		ft := newFakeTool("create_recurring_task", "task-created")
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{
			SemanticKeyFields: map[string][]string{
				"create_recurring_task": {"name"},
			},
		})
		args := []byte(`{"name":"daily_standup","action":"run report"}`)

		r1, err := w.Call(context.Background(), args)
		Expect(err).NotTo(HaveOccurred())
		Expect(r1).To(Equal("task-created"))
		count := ft.CallCallCount()

		r2, err := w.Call(context.Background(), args)
		Expect(err).NotTo(HaveOccurred())
		Expect(r2).To(Equal("task-created"))
		Expect(ft.CallCallCount()).To(Equal(count)) // NOT called again
	})
})

var _ = Describe("audit integration", func() {
	It("should not panic when Auditor is nil", func() {
		ft := newFakeTool("read_file", "content")
		w := toolwrap.NewWrapper(ft, toolwrap.MiddlewareDeps{})

		result, err := w.Call(context.Background(), []byte(`{}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("content"))
	})

	It("should audit tool calls when Auditor is set", func() {
		ft := newFakeTool("read_file", "content")
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
	It("should wrap all tools preserving declarations", func(ctx context.Context) {
		svc := toolwrap.NewService(nil, nil, nil)
		ft1 := newFakeTool("read_file", "c1")
		ft2 := newFakeTool("write_file", "c2")

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
