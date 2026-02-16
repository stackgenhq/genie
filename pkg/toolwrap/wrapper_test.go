package toolwrap_test

import (
	"context"
	"errors"
	"strings"

	"github.com/appcd-dev/genie/pkg/agui"
	"github.com/appcd-dev/genie/pkg/hitl"
	"github.com/appcd-dev/genie/pkg/hitl/hitlfakes"
	rtmemory "github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/genie/pkg/toolwrap"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// fakeTool is a minimal callable tool that counts how many times Call is invoked.
type fakeTool struct {
	name      string
	callCount int
	result    string
}

func (f *fakeTool) Declaration() *tool.Declaration {
	return &tool.Declaration{Name: f.name}
}

func (f *fakeTool) Call(_ context.Context, _ []byte) (any, error) {
	f.callCount++
	return f.result, nil
}

// fakeErrorTool is a callable tool that always returns an error.
type fakeErrorTool struct {
	name string
	err  error
}

func (f *fakeErrorTool) Declaration() *tool.Declaration {
	return &tool.Declaration{Name: f.name}
}

func (f *fakeErrorTool) Call(_ context.Context, _ []byte) (any, error) {
	return nil, f.err
}

// fakeNonCallableTool implements tool.Tool but NOT tool.CallableTool (no Call method).
type fakeNonCallableTool struct {
	name string
}

func (f *fakeNonCallableTool) Declaration() *tool.Declaration {
	return &tool.Declaration{Name: f.name}
}

// ctxCapturingTool is a callable tool that captures the context it receives,
// allowing tests to inspect how the wrapper enriches context for downstream tools.
type ctxCapturingTool struct {
	name        string
	result      string
	capturedCtx context.Context
}

func (f *ctxCapturingTool) Declaration() *tool.Declaration {
	return &tool.Declaration{Name: f.name}
}

func (f *ctxCapturingTool) Call(ctx context.Context, _ []byte) (any, error) {
	f.capturedCtx = ctx
	return f.result, nil
}

var _ = Describe("Wrapper", func() {
	var (
		ctx            context.Context
		wm             *rtmemory.WorkingMemory
		underlyingTool *fakeTool
	)

	BeforeEach(func() {
		ctx = context.Background()
		wm = rtmemory.NewWorkingMemory()
		underlyingTool = &fakeTool{
			name:   "read_file",
			result: "file content here",
		}
	})

	Describe("file-read caching", func() {
		It("should cache read_file results and skip the second call", func() {
			wrapper := &toolwrap.Wrapper{
				Tool:          underlyingTool,
				WorkingMemory: wm,
			}

			args := []byte(`{"file_name":"main.tf"}`)

			// First call → invokes underlying tool
			result1, err := wrapper.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(result1).To(Equal("file content here"))
			Expect(underlyingTool.callCount).To(Equal(1))

			// Second call → should return cached result without calling underlying tool
			result2, err := wrapper.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(result2).To(Equal("file content here"))
			Expect(underlyingTool.callCount).To(Equal(1)) // Still 1 — cache hit
		})

		It("should cache list_file results", func() {
			underlyingTool.name = "list_file"
			underlyingTool.result = "file1.tf\nfile2.tf"
			wrapper := &toolwrap.Wrapper{
				Tool:          underlyingTool,
				WorkingMemory: wm,
			}

			args := []byte(`{"path":"."}`)

			_, err := wrapper.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(underlyingTool.callCount).To(Equal(1))

			_, err = wrapper.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(underlyingTool.callCount).To(Equal(1)) // Cache hit
		})

		It("should cache read_multiple_files results", func() {
			underlyingTool.name = "read_multiple_files"
			underlyingTool.result = "multi file content"
			wrapper := &toolwrap.Wrapper{
				Tool:          underlyingTool,
				WorkingMemory: wm,
			}

			args := []byte(`{"patterns":["*.tf"]}`)

			_, err := wrapper.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(underlyingTool.callCount).To(Equal(1))

			// Same args → cache hit
			_, err = wrapper.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(underlyingTool.callCount).To(Equal(1))

			// Different args → cache miss
			_, err = wrapper.Call(ctx, []byte(`{"patterns":["*.go"]}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(underlyingTool.callCount).To(Equal(2))
		})

		It("should NOT cache non-file tools", func() {
			underlyingTool.name = "execute_code"
			underlyingTool.result = "output"
			wrapper := &toolwrap.Wrapper{
				Tool:          underlyingTool,
				WorkingMemory: wm,
			}

			args := []byte(`{"code":"echo hello"}`)

			_, err := wrapper.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(underlyingTool.callCount).To(Equal(1))

			_, err = wrapper.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(underlyingTool.callCount).To(Equal(2)) // No cache — both calls execute
		})

		It("should work without WorkingMemory (nil does not panic)", func() {
			wrapper := &toolwrap.Wrapper{
				Tool:          underlyingTool,
				WorkingMemory: nil, // No caching
			}

			args := []byte(`{"file_name":"main.tf"}`)

			result, err := wrapper.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("file content here"))
			Expect(underlyingTool.callCount).To(Equal(1))

			// Second call should also invoke the tool since there's no cache
			_, err = wrapper.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(underlyingTool.callCount).To(Equal(2))
		})

		It("should differentiate cache keys for different files", func() {
			wrapper := &toolwrap.Wrapper{
				Tool:          underlyingTool,
				WorkingMemory: wm,
			}

			// Read file A
			_, err := wrapper.Call(ctx, []byte(`{"file_name":"main.tf"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(underlyingTool.callCount).To(Equal(1))

			// Read file B → different key, should invoke tool
			_, err = wrapper.Call(ctx, []byte(`{"file_name":"vars.tf"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(underlyingTool.callCount).To(Equal(2))

			// Re-read file A → cache hit
			_, err = wrapper.Call(ctx, []byte(`{"file_name":"main.tf"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(underlyingTool.callCount).To(Equal(2)) // Still 2
		})

		It("should NOT cache results when the underlying tool returns an error", func() {
			errTool := &fakeErrorTool{
				name: "read_file",
				err:  errors.New("permission denied"),
			}
			wrapper := &toolwrap.Wrapper{
				Tool:          errTool,
				WorkingMemory: wm,
			}

			args := []byte(`{"file_name":"secret.tf"}`)

			_, err := wrapper.Call(ctx, args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("permission denied"))

			// Verify nothing was cached (snapshot should be empty)
			Expect(wm.Snapshot()).To(BeEmpty())
		})

		It("should handle malformed JSON args gracefully (no cache, no panic)", func() {
			wrapper := &toolwrap.Wrapper{
				Tool:          underlyingTool,
				WorkingMemory: wm,
			}

			// Malformed JSON — should not panic and should skip caching
			args := []byte(`not valid json`)

			result, err := wrapper.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("file content here"))
			Expect(underlyingTool.callCount).To(Equal(1))

			// Second call — still no cache because key couldn't be built
			_, err = wrapper.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())
			Expect(underlyingTool.callCount).To(Equal(2))
		})
	})

	Describe("response truncation", func() {
		It("should truncate responses exceeding maxToolResultSize", func() {
			longResult := strings.Repeat("a", 90000) // > 80000 maxToolResultSize
			underlyingTool.name = "execute_code"
			underlyingTool.result = longResult

			eventChan := make(chan interface{}, 10)
			wrapper := &toolwrap.Wrapper{
				Tool:      underlyingTool,
				EventChan: eventChan,
			}

			_, err := wrapper.Call(ctx, []byte(`{}`))
			Expect(err).NotTo(HaveOccurred())

			// The event channel should contain the truncated response
			Eventually(eventChan).Should(Receive(Satisfy(func(msg interface{}) bool {
				toolMsg, ok := msg.(agui.AgentToolResponseMsg)
				if !ok {
					return false
				}
				return strings.Contains(toolMsg.Response, "[truncated") &&
					len(toolMsg.Response) < 90000
			})))
		})

		It("should NOT truncate responses under maxToolResultSize", func() {
			shortResult := strings.Repeat("b", 500)
			underlyingTool.name = "execute_code"
			underlyingTool.result = shortResult

			eventChan := make(chan interface{}, 10)
			wrapper := &toolwrap.Wrapper{
				Tool:      underlyingTool,
				EventChan: eventChan,
			}

			_, err := wrapper.Call(ctx, []byte(`{}`))
			Expect(err).NotTo(HaveOccurred())

			Eventually(eventChan).Should(Receive(Satisfy(func(msg interface{}) bool {
				toolMsg, ok := msg.(agui.AgentToolResponseMsg)
				if !ok {
					return false
				}
				return !strings.Contains(toolMsg.Response, "[truncated") &&
					toolMsg.Response == shortResult
			})))
		})
	})

	Describe("TUI event emission", func() {
		It("should emit AgentToolResponseMsg on the event channel after a call", func() {
			eventChan := make(chan interface{}, 10)
			wrapper := &toolwrap.Wrapper{
				Tool:      underlyingTool,
				EventChan: eventChan,
			}

			_, err := wrapper.Call(ctx, []byte(`{"file_name":"main.tf"}`))
			Expect(err).NotTo(HaveOccurred())

			Eventually(eventChan).Should(Receive(Satisfy(func(msg interface{}) bool {
				toolMsg, ok := msg.(agui.AgentToolResponseMsg)
				if !ok {
					return false
				}
				return toolMsg.ToolName == "read_file" &&
					toolMsg.Response == "file content here" &&
					toolMsg.Error == nil
			})))
		})

		It("should emit AgentToolResponseMsg with error when tool fails", func() {
			errTool := &fakeErrorTool{
				name: "execute_code",
				err:  errors.New("command failed"),
			}
			eventChan := make(chan interface{}, 10)
			wrapper := &toolwrap.Wrapper{
				Tool:      errTool,
				EventChan: eventChan,
			}

			_, err := wrapper.Call(ctx, []byte(`{}`))
			Expect(err).To(HaveOccurred())

			Eventually(eventChan).Should(Receive(Satisfy(func(msg interface{}) bool {
				toolMsg, ok := msg.(agui.AgentToolResponseMsg)
				if !ok {
					return false
				}
				return toolMsg.ToolName == "execute_code" && toolMsg.Error != nil
			})))
		})

		It("should NOT panic when EventChan is nil", func() {
			wrapper := &toolwrap.Wrapper{
				Tool:      underlyingTool,
				EventChan: nil,
			}

			result, err := wrapper.Call(ctx, []byte(`{"file_name":"main.tf"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("file content here"))
		})

		It("should emit event for cache hits", func() {
			eventChan := make(chan interface{}, 10)
			wrapper := &toolwrap.Wrapper{
				Tool:          underlyingTool,
				EventChan:     eventChan,
				WorkingMemory: wm,
			}

			args := []byte(`{"file_name":"main.tf"}`)

			// First call — emits event
			_, err := wrapper.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())
			// Drain the first event
			Eventually(eventChan).Should(Receive())

			// Second call — cache hit should still emit event
			_, err = wrapper.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())
			Eventually(eventChan).Should(Receive(Satisfy(func(msg interface{}) bool {
				toolMsg, ok := msg.(agui.AgentToolResponseMsg)
				if !ok {
					return false
				}
				return toolMsg.ToolName == "read_file"
			})))
		})
	})

	Describe("error handling", func() {
		It("should forward errors from the underlying tool", func() {
			errTool := &fakeErrorTool{
				name: "execute_code",
				err:  errors.New("segfault"),
			}
			wrapper := &toolwrap.Wrapper{
				Tool: errTool,
			}

			result, err := wrapper.Call(ctx, []byte(`{}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("segfault"))
			Expect(result).To(BeNil())
		})

		It("should return error for non-callable tools", func() {
			nonCallable := &fakeNonCallableTool{name: "broken_tool"}
			wrapper := &toolwrap.Wrapper{
				Tool: nonCallable,
			}

			result, err := wrapper.Call(ctx, []byte(`{}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not callable"))
			Expect(result).To(BeNil())
		})
	})

	Describe("closed EventChan resilience", func() {
		It("should not panic when EventChan is closed", func() {
			// Simulate the bug: a Wrapper referencing a closed channel.
			// This happens when the runner is reused across HTTP requests
			// and the previous request's rawEventChan has been closed.
			eventChan := make(chan interface{}, 10)
			close(eventChan) // simulate stale closed channel

			wrapper := &toolwrap.Wrapper{
				Tool:      underlyingTool,
				EventChan: eventChan,
			}

			// Call should NOT panic — the deferred recover in Wrapper.Call
			// should catch the "send on closed channel" and return an error.
			Expect(func() {
				result, err := wrapper.Call(ctx, []byte(`{"file_name":"main.tf"}`))
				// The panic is recovered and turned into an error
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("internal error"))
				Expect(result).To(BeNil())
			}).NotTo(Panic())
		})
	})

	Describe("Declaration delegation", func() {
		It("should delegate Declaration to the underlying tool", func() {
			wrapper := &toolwrap.Wrapper{
				Tool: underlyingTool,
			}

			decl := wrapper.Declaration()
			Expect(decl).NotTo(BeNil())
			Expect(decl.Name).To(Equal("read_file"))
		})
	})
})

// ── HITL Wrapper Approval Gate ───────────────────────────────────────────────

var _ = Describe("Wrapper human approval gate", func() {
	It("should skip approval for readonly tools and execute immediately", func() {
		store := &hitlfakes.FakeApprovalStore{}
		store.IsAllowedStub = func(name string) bool {
			return name == "read_file"
		}
		ft := &fakeTool{name: "read_file", result: "file-content"}
		eventChan := make(chan interface{}, 10)

		wrapper := &toolwrap.Wrapper{
			Tool:          ft,
			EventChan:     eventChan,
			ApprovalStore: store,
			ThreadID:      "t1",
			RunID:         "r1",
		}

		result, err := wrapper.Call(context.Background(), []byte(`{"path":"foo.txt"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("file-content"))
		Expect(ft.callCount).To(Equal(1))
		// No approval should have been created
		Expect(store.CreateCallCount()).To(Equal(0))
	})

	It("should block non-readonly tools until approved then execute", func() {
		store := &hitlfakes.FakeApprovalStore{}
		store.CreateStub = func(_ context.Context, req hitl.CreateRequest) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{
				ID:       "fake-" + req.ToolName,
				ToolName: req.ToolName,
				Status:   hitl.StatusPending,
			}, nil
		}
		store.WaitForResolutionStub = func(_ context.Context, _ string) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{
				Status: hitl.StatusApproved,
			}, nil
		}

		ft := &fakeTool{name: "write_file", result: "written"}
		eventChan := make(chan interface{}, 10)

		wrapper := &toolwrap.Wrapper{
			Tool:          ft,
			EventChan:     eventChan,
			ApprovalStore: store,
			ThreadID:      "t1",
			RunID:         "r1",
		}

		result, err := wrapper.Call(context.Background(), []byte(`{"path":"out.txt","content":"hello"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("written"))
		Expect(ft.callCount).To(Equal(1))

		Expect(store.CreateCallCount()).To(Equal(1))
		Expect(store.WaitForResolutionCallCount()).To(Equal(1))

		// Verify that a TOOL_APPROVAL_REQUEST event was emitted
		var foundApprovalEvent bool
		close(eventChan)
		for e := range eventChan {
			if msg, ok := e.(agui.ToolApprovalRequestMsg); ok {
				foundApprovalEvent = true
				Expect(msg.ToolName).To(Equal("write_file"))
				Expect(msg.ApprovalID).To(Equal("fake-write_file"))
			}
		}
		Expect(foundApprovalEvent).To(BeTrue(), "expected a TOOL_APPROVAL_REQUEST event")
	})

	It("should return an error when a non-readonly tool is rejected", func() {
		store := &hitlfakes.FakeApprovalStore{}
		store.CreateStub = func(_ context.Context, req hitl.CreateRequest) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{
				ID:       "fake-" + req.ToolName,
				ToolName: req.ToolName,
				Status:   hitl.StatusPending,
			}, nil
		}
		store.WaitForResolutionStub = func(_ context.Context, _ string) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{
				Status: hitl.StatusRejected,
			}, nil
		}

		ft := &fakeTool{name: "execute_code", result: "should not run"}
		eventChan := make(chan interface{}, 10)

		wrapper := &toolwrap.Wrapper{
			Tool:          ft,
			EventChan:     eventChan,
			ApprovalStore: store,
			ThreadID:      "t1",
			RunID:         "r1",
		}

		result, err := wrapper.Call(context.Background(), []byte(`{"cmd":"rm -rf /"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("rejected by user"))
		Expect(result).To(BeNil())
		Expect(ft.callCount).To(Equal(0)) // tool should NOT have been called
	})

	It("should execute tools immediately when ApprovalStore is nil (backward compatible)", func() {
		ft := &fakeTool{name: "write_file", result: "written-no-hitl"}
		eventChan := make(chan interface{}, 10)

		wrapper := &toolwrap.Wrapper{
			Tool:          ft,
			EventChan:     eventChan,
			ApprovalStore: nil, // HITL disabled
			ThreadID:      "t1",
			RunID:         "r1",
		}

		result, err := wrapper.Call(context.Background(), []byte(`{"path":"out.txt"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("written-no-hitl"))
		Expect(ft.callCount).To(Equal(1))
	})

	It("should return re-planning error when approved with feedback", func() {
		store := &hitlfakes.FakeApprovalStore{}
		store.CreateStub = func(_ context.Context, req hitl.CreateRequest) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{
				ID:       "fb-" + req.ToolName,
				ToolName: req.ToolName,
				Status:   hitl.StatusPending,
			}, nil
		}
		store.WaitForResolutionStub = func(_ context.Context, _ string) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{
				Status:   hitl.StatusApproved,
				Feedback: "use /tmp/output.txt instead",
			}, nil
		}

		ft := &fakeTool{name: "write_file", result: "should not run"}
		eventChan := make(chan interface{}, 10)

		wrapper := &toolwrap.Wrapper{
			Tool:          ft,
			EventChan:     eventChan,
			ApprovalStore: store,
			ThreadID:      "t1",
			RunID:         "r1",
		}

		result, err := wrapper.Call(context.Background(), []byte(`{"path":"out.txt","content":"hello"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("user requested changes"))
		Expect(err.Error()).To(ContainSubstring("use /tmp/output.txt instead"))
		Expect(result).To(BeNil())
		Expect(ft.callCount).To(Equal(0)) // tool should NOT have been called
	})

	It("should include feedback in rejection error when rejected with feedback", func() {
		store := &hitlfakes.FakeApprovalStore{}
		store.CreateStub = func(_ context.Context, req hitl.CreateRequest) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{
				ID:       "fb-" + req.ToolName,
				ToolName: req.ToolName,
				Status:   hitl.StatusPending,
			}, nil
		}
		store.WaitForResolutionStub = func(_ context.Context, _ string) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{
				Status:   hitl.StatusRejected,
				Feedback: "this operation is too dangerous",
			}, nil
		}

		ft := &fakeTool{name: "run_shell", result: "should not run"}
		eventChan := make(chan interface{}, 10)

		wrapper := &toolwrap.Wrapper{
			Tool:          ft,
			EventChan:     eventChan,
			ApprovalStore: store,
			ThreadID:      "t1",
			RunID:         "r1",
		}

		result, err := wrapper.Call(context.Background(), []byte(`{"cmd":"rm -rf /"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("rejected by user"))
		Expect(err.Error()).To(ContainSubstring("this operation is too dangerous"))
		Expect(result).To(BeNil())
		Expect(ft.callCount).To(Equal(0))
	})

	It("should execute tool normally when approved without feedback", func() {
		store := &hitlfakes.FakeApprovalStore{}
		store.CreateStub = func(_ context.Context, req hitl.CreateRequest) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{
				ID:       "clean-" + req.ToolName,
				ToolName: req.ToolName,
				Status:   hitl.StatusPending,
			}, nil
		}
		store.WaitForResolutionStub = func(_ context.Context, _ string) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{
				Status:   hitl.StatusApproved,
				Feedback: "", // no feedback
			}, nil
		}

		ft := &fakeTool{name: "write_file", result: "written-clean"}
		eventChan := make(chan interface{}, 10)

		wrapper := &toolwrap.Wrapper{
			Tool:          ft,
			EventChan:     eventChan,
			ApprovalStore: store,
			ThreadID:      "t1",
			RunID:         "r1",
		}

		result, err := wrapper.Call(context.Background(), []byte(`{"path":"out.txt","content":"hello"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("written-clean"))
		Expect(ft.callCount).To(Equal(1)) // tool SHOULD have been called
	})
})

// ── Service.Wrap tests ───────────────────────────────────────────────────────

var _ = Describe("Service.Wrap", func() {
	It("should wrap all tools with the service's stable dependencies", func() {
		svc := &toolwrap.Service{}

		ft1 := &fakeTool{name: "read_file", result: "content1"}
		ft2 := &fakeTool{name: "write_file", result: "content2"}

		wrapped := svc.Wrap([]tool.Tool{ft1, ft2}, toolwrap.WrapRequest{})
		Expect(wrapped).To(HaveLen(2))

		// Verify declarations are preserved
		Expect(wrapped[0].Declaration().Name).To(Equal("read_file"))
		Expect(wrapped[1].Declaration().Name).To(Equal("write_file"))
	})

	It("should return an empty slice for empty tools", func() {
		svc := &toolwrap.Service{}
		wrapped := svc.Wrap(nil, toolwrap.WrapRequest{})
		Expect(wrapped).To(BeEmpty())
	})
})

// ── ThreadID / RunID context propagation tests ───────────────────────────────

var _ = Describe("Wrapper threadID/runID context propagation", func() {
	It("should propagate struct-level ThreadID and RunID into the tool context", func() {
		ct := &ctxCapturingTool{name: "run_shell", result: "ok"}
		wrapper := &toolwrap.Wrapper{
			Tool:     ct,
			ThreadID: "struct-thread",
			RunID:    "struct-run",
		}

		_, err := wrapper.Call(context.Background(), []byte(`{}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(agui.ThreadIDFromContext(ct.capturedCtx)).To(Equal("struct-thread"))
		Expect(agui.RunIDFromContext(ct.capturedCtx)).To(Equal("struct-run"))
	})

	It("should fall back to context values when struct fields are empty", func() {
		ct := &ctxCapturingTool{name: "run_shell", result: "ok"}
		wrapper := &toolwrap.Wrapper{
			Tool:     ct,
			ThreadID: "", // empty struct field
			RunID:    "", // empty struct field
		}

		// Simulate the AG-UI handler injecting values into the parent context
		parentCtx := agui.WithThreadID(context.Background(), "ctx-thread")
		parentCtx = agui.WithRunID(parentCtx, "ctx-run")

		_, err := wrapper.Call(parentCtx, []byte(`{}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(agui.ThreadIDFromContext(ct.capturedCtx)).To(Equal("ctx-thread"))
		Expect(agui.RunIDFromContext(ct.capturedCtx)).To(Equal("ctx-run"))
	})

	It("should not overwrite context values that are already set", func() {
		ct := &ctxCapturingTool{name: "run_shell", result: "ok"}
		wrapper := &toolwrap.Wrapper{
			Tool:     ct,
			ThreadID: "struct-thread",
			RunID:    "struct-run",
		}

		// Context already has values — they should be preserved (not overwritten)
		parentCtx := agui.WithThreadID(context.Background(), "ctx-thread")
		parentCtx = agui.WithRunID(parentCtx, "ctx-run")

		_, err := wrapper.Call(parentCtx, []byte(`{}`))
		Expect(err).NotTo(HaveOccurred())
		// Context values already present → wrapper does not overwrite
		Expect(agui.ThreadIDFromContext(ct.capturedCtx)).To(Equal("ctx-thread"))
		Expect(agui.RunIDFromContext(ct.capturedCtx)).To(Equal("ctx-run"))
	})

	It("should leave context empty when neither struct nor context has values", func() {
		ct := &ctxCapturingTool{name: "run_shell", result: "ok"}
		wrapper := &toolwrap.Wrapper{
			Tool:     ct,
			ThreadID: "",
			RunID:    "",
		}

		_, err := wrapper.Call(context.Background(), []byte(`{}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(agui.ThreadIDFromContext(ct.capturedCtx)).To(Equal(""))
		Expect(agui.RunIDFromContext(ct.capturedCtx)).To(Equal(""))
	})
})

// ── Loop Detection Tests ─────────────────────────────────────────────────────

var _ = Describe("Wrapper loop detection", func() {
	It("should detect a loop after 3 identical consecutive calls", func() {
		ft := &fakeTool{name: "list_issues", result: "issue-1, issue-2"}
		wrapper := &toolwrap.Wrapper{
			Tool: ft,
		}

		args := []byte(`{"status":"open"}`)

		// First call — succeeds
		result, err := wrapper.Call(context.Background(), args)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("issue-1, issue-2"))

		// Second call — succeeds
		result, err = wrapper.Call(context.Background(), args)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("issue-1, issue-2"))

		// Third call — loop detected
		result, err = wrapper.Call(context.Background(), args)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("loop detected"))
		Expect(err.Error()).To(ContainSubstring("list_issues"))
		Expect(result).To(BeNil())
		Expect(ft.callCount).To(Equal(2)) // tool was only actually called twice
	})

	It("should NOT flag calls with different arguments as a loop", func() {
		ft := &fakeTool{name: "search", result: "results"}
		wrapper := &toolwrap.Wrapper{
			Tool: ft,
		}

		_, err := wrapper.Call(context.Background(), []byte(`{"q":"foo"}`))
		Expect(err).NotTo(HaveOccurred())

		_, err = wrapper.Call(context.Background(), []byte(`{"q":"bar"}`))
		Expect(err).NotTo(HaveOccurred())

		_, err = wrapper.Call(context.Background(), []byte(`{"q":"baz"}`))
		Expect(err).NotTo(HaveOccurred())

		Expect(ft.callCount).To(Equal(3)) // all 3 executed
	})

	It("should reset loop tracking when args change mid-sequence", func() {
		ft := &fakeTool{name: "list_issues", result: "data"}
		wrapper := &toolwrap.Wrapper{
			Tool: ft,
		}

		sameArgs := []byte(`{"status":"open"}`)
		diffArgs := []byte(`{"status":"closed"}`)

		// 2 identical, then different, then 2 identical = no loop
		_, err := wrapper.Call(context.Background(), sameArgs)
		Expect(err).NotTo(HaveOccurred())

		_, err = wrapper.Call(context.Background(), sameArgs)
		Expect(err).NotTo(HaveOccurred())

		_, err = wrapper.Call(context.Background(), diffArgs) // breaks the streak
		Expect(err).NotTo(HaveOccurred())

		_, err = wrapper.Call(context.Background(), sameArgs)
		Expect(err).NotTo(HaveOccurred())

		_, err = wrapper.Call(context.Background(), sameArgs)
		Expect(err).NotTo(HaveOccurred())

		Expect(ft.callCount).To(Equal(5)) // all 5 executed, no loop
	})

	It("should allow up to 2 identical consecutive calls without error", func() {
		ft := &fakeTool{name: "get_status", result: "ok"}
		wrapper := &toolwrap.Wrapper{
			Tool: ft,
		}

		args := []byte(`{"id":"123"}`)

		_, err := wrapper.Call(context.Background(), args)
		Expect(err).NotTo(HaveOccurred())

		_, err = wrapper.Call(context.Background(), args)
		Expect(err).NotTo(HaveOccurred())

		Expect(ft.callCount).To(Equal(2))
	})
})

// ── Justification Extraction Tests ───────────────────────────────────────────

var _ = Describe("Wrapper justification extraction", func() {
	It("should strip _justification from args and include it in the approval event", func() {
		store := &hitlfakes.FakeApprovalStore{}
		store.CreateStub = func(_ context.Context, req hitl.CreateRequest) (hitl.ApprovalRequest, error) {
			// Verify that _justification was stripped from args
			Expect(req.Args).NotTo(ContainSubstring("_justification"))
			return hitl.ApprovalRequest{
				ID:       "just-" + req.ToolName,
				ToolName: req.ToolName,
				Status:   hitl.StatusPending,
			}, nil
		}
		store.WaitForResolutionStub = func(_ context.Context, _ string) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{Status: hitl.StatusApproved}, nil
		}

		ft := &fakeTool{name: "write_file", result: "written"}
		eventChan := make(chan interface{}, 10)

		wrapper := &toolwrap.Wrapper{
			Tool:          ft,
			EventChan:     eventChan,
			ApprovalStore: store,
			ThreadID:      "t1",
			RunID:         "r1",
		}

		argsWithJustification := []byte(`{"path":"out.txt","content":"hello","_justification":"Writing output for the user's report"}`)
		result, err := wrapper.Call(context.Background(), argsWithJustification)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("written"))

		// Check that the approval event has the justification
		close(eventChan)
		var foundApproval bool
		for e := range eventChan {
			if msg, ok := e.(agui.ToolApprovalRequestMsg); ok {
				foundApproval = true
				Expect(msg.Justification).To(Equal("Writing output for the user's report"))
				Expect(msg.Arguments).NotTo(ContainSubstring("_justification"))
			}
		}
		Expect(foundApproval).To(BeTrue(), "expected a TOOL_APPROVAL_REQUEST event")
	})

	It("should pass args through unchanged when no _justification is present", func() {
		store := &hitlfakes.FakeApprovalStore{}
		store.CreateStub = func(_ context.Context, req hitl.CreateRequest) (hitl.ApprovalRequest, error) {
			Expect(req.Args).To(Equal(`{"path":"out.txt","content":"hello"}`))
			return hitl.ApprovalRequest{
				ID:       "nojust-" + req.ToolName,
				ToolName: req.ToolName,
				Status:   hitl.StatusPending,
			}, nil
		}
		store.WaitForResolutionStub = func(_ context.Context, _ string) (hitl.ApprovalRequest, error) {
			return hitl.ApprovalRequest{Status: hitl.StatusApproved}, nil
		}

		ft := &fakeTool{name: "write_file", result: "written"}
		eventChan := make(chan interface{}, 10)

		wrapper := &toolwrap.Wrapper{
			Tool:          ft,
			EventChan:     eventChan,
			ApprovalStore: store,
			ThreadID:      "t1",
			RunID:         "r1",
		}

		result, err := wrapper.Call(context.Background(), []byte(`{"path":"out.txt","content":"hello"}`))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("written"))

		// Approval event should have empty justification
		close(eventChan)
		for e := range eventChan {
			if msg, ok := e.(agui.ToolApprovalRequestMsg); ok {
				Expect(msg.Justification).To(BeEmpty())
			}
		}
	})

	It("should handle readonly tools (no approval) with _justification gracefully", func() {
		store := &hitlfakes.FakeApprovalStore{}
		store.IsAllowedStub = func(name string) bool {
			return name == "read_file"
		}

		ft := &fakeTool{name: "read_file", result: "content"}
		wrapper := &toolwrap.Wrapper{
			Tool:          ft,
			ApprovalStore: store,
			ThreadID:      "t1",
			RunID:         "r1",
		}

		// Even with _justification, readonly tools should execute immediately
		args := []byte(`{"path":"test.txt","_justification":"Checking if file exists"}`)
		result, err := wrapper.Call(context.Background(), args)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("content"))
		Expect(store.CreateCallCount()).To(Equal(0))
	})
})
