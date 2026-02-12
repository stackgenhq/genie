package codeowner

import (
	"context"
	"errors"

	"github.com/appcd-dev/genie/pkg/expert/expertfakes"
	"github.com/appcd-dev/genie/pkg/reactree"
	"github.com/appcd-dev/genie/pkg/reactree/reactreefakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type mockTool struct{}

func (m *mockTool) Declaration() *tool.Declaration {
	return &tool.Declaration{Name: "mock"}
}
func (m *mockTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	return nil, nil
}

var _ = Describe("CodeOwner", func() {
	var (
		fakeExpert       *expertfakes.FakeExpert
		fakeTreeExecutor *reactreefakes.FakeTreeExecutor
		co               *codeOwner
		ctx              context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeExpert = &expertfakes.FakeExpert{}
		fakeTreeExecutor = &reactreefakes.FakeTreeExecutor{}
		co = &codeOwner{
			expert:       fakeExpert,
			treeExecutor: fakeTreeExecutor,
			memorySvc:    inmemory.NewMemoryService(),
			memoryUserKey: memory.UserKey{
				AppName: "test",
				UserID:  "test",
			},
		}
	})

	Describe("Chat", func() {
		It("should return no error when tree executor succeeds", func() {
			fakeTreeExecutor.RunReturns(reactree.TreeResult{
				Status: reactree.Success,
				Output: "Hello World",
			}, nil)

			// Inject a tool to verify it's passed
			mTool := &mockTool{}
			co.tools = []tool.Tool{mTool}

			outputChan := make(chan string, 10)
			err := co.Chat(ctx, CodeQuestion{
				Question: "Hi",
			}, outputChan)

			Expect(err).NotTo(HaveOccurred())

			// Verify Tree Executor was called correctly
			_, req := fakeTreeExecutor.RunArgsForCall(0)
			Expect(req.Goal).To(ContainSubstring("Hi"))
			Expect(req.Tools).To(ContainElement(mTool))

			// Verify output
			Expect(outputChan).To(Receive(Equal("Hello World")))
		})

		It("should return error if tree executor fails", func() {
			fakeTreeExecutor.RunReturns(reactree.TreeResult{}, errors.New("tree execution failed"))

			outputChan := make(chan string, 10)
			err := co.Chat(ctx, CodeQuestion{
				Question: "Hi",
			}, outputChan)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("tree execution failed"))
		})
	})
})
