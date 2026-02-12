package expert_test

import (
	"context"
	"errors"
	"strings"

	"github.com/appcd-dev/genie/pkg/expert"
	rtmemory "github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/genie/pkg/tui"
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

var _ = Describe("ToolWrapper", func() {
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
			wrapper := &expert.ToolWrapper{
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
			wrapper := &expert.ToolWrapper{
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
			wrapper := &expert.ToolWrapper{
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
			wrapper := &expert.ToolWrapper{
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
			wrapper := &expert.ToolWrapper{
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
			wrapper := &expert.ToolWrapper{
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
			wrapper := &expert.ToolWrapper{
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
			wrapper := &expert.ToolWrapper{
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
		It("should truncate responses exceeding 2000 characters", func() {
			longResult := strings.Repeat("a", 3000)
			underlyingTool.name = "execute_code"
			underlyingTool.result = longResult

			eventChan := make(chan interface{}, 10)
			wrapper := &expert.ToolWrapper{
				Tool:      underlyingTool,
				EventChan: eventChan,
			}

			_, err := wrapper.Call(ctx, []byte(`{}`))
			Expect(err).NotTo(HaveOccurred())

			// The event channel should contain the truncated response
			Eventually(eventChan).Should(Receive(Satisfy(func(msg interface{}) bool {
				toolMsg, ok := msg.(tui.AgentToolResponseMsg)
				if !ok {
					return false
				}
				return strings.Contains(toolMsg.Response, "[truncated") &&
					len(toolMsg.Response) < 3000
			})))
		})

		It("should NOT truncate responses under 2000 characters", func() {
			shortResult := strings.Repeat("b", 500)
			underlyingTool.name = "execute_code"
			underlyingTool.result = shortResult

			eventChan := make(chan interface{}, 10)
			wrapper := &expert.ToolWrapper{
				Tool:      underlyingTool,
				EventChan: eventChan,
			}

			_, err := wrapper.Call(ctx, []byte(`{}`))
			Expect(err).NotTo(HaveOccurred())

			Eventually(eventChan).Should(Receive(Satisfy(func(msg interface{}) bool {
				toolMsg, ok := msg.(tui.AgentToolResponseMsg)
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
			wrapper := &expert.ToolWrapper{
				Tool:      underlyingTool,
				EventChan: eventChan,
			}

			_, err := wrapper.Call(ctx, []byte(`{"file_name":"main.tf"}`))
			Expect(err).NotTo(HaveOccurred())

			Eventually(eventChan).Should(Receive(Satisfy(func(msg interface{}) bool {
				toolMsg, ok := msg.(tui.AgentToolResponseMsg)
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
			wrapper := &expert.ToolWrapper{
				Tool:      errTool,
				EventChan: eventChan,
			}

			_, err := wrapper.Call(ctx, []byte(`{}`))
			Expect(err).To(HaveOccurred())

			Eventually(eventChan).Should(Receive(Satisfy(func(msg interface{}) bool {
				toolMsg, ok := msg.(tui.AgentToolResponseMsg)
				if !ok {
					return false
				}
				return toolMsg.ToolName == "execute_code" && toolMsg.Error != nil
			})))
		})

		It("should NOT panic when EventChan is nil", func() {
			wrapper := &expert.ToolWrapper{
				Tool:      underlyingTool,
				EventChan: nil,
			}

			result, err := wrapper.Call(ctx, []byte(`{"file_name":"main.tf"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal("file content here"))
		})

		It("should emit event for cache hits", func() {
			eventChan := make(chan interface{}, 10)
			wrapper := &expert.ToolWrapper{
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
				toolMsg, ok := msg.(tui.AgentToolResponseMsg)
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
			wrapper := &expert.ToolWrapper{
				Tool: errTool,
			}

			result, err := wrapper.Call(ctx, []byte(`{}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("segfault"))
			Expect(result).To(BeNil())
		})

		It("should return error for non-callable tools", func() {
			nonCallable := &fakeNonCallableTool{name: "broken_tool"}
			wrapper := &expert.ToolWrapper{
				Tool: nonCallable,
			}

			result, err := wrapper.Call(ctx, []byte(`{}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not callable"))
			Expect(result).To(BeNil())
		})
	})

	Describe("Declaration delegation", func() {
		It("should delegate Declaration to the underlying tool", func() {
			wrapper := &expert.ToolWrapper{
				Tool: underlyingTool,
			}

			decl := wrapper.Declaration()
			Expect(decl).NotTo(BeNil())
			Expect(decl.Name).To(Equal("read_file"))
		})
	})
})
