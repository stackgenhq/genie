package codeowner

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/appcd-dev/genie/pkg/audit/auditfakes"
	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/expertfakes"
	"github.com/appcd-dev/genie/pkg/reactree"
	"github.com/appcd-dev/genie/pkg/reactree/reactreefakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type mockTool struct{}

func (m *mockTool) Declaration() *tool.Declaration {
	return &tool.Declaration{Name: "mock"}
}
func (m *mockTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	return nil, nil
}

type namedMockTool struct {
	name string
}

func (m *namedMockTool) Declaration() *tool.Declaration {
	return &tool.Declaration{Name: m.name}
}
func (m *namedMockTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	return nil, nil
}

// fakeExpertResponse builds a fake expert response with the given text content.
func fakeExpertResponse(text string) expert.Response {
	return expert.Response{
		Choices: []model.Choice{
			{Message: model.Message{Content: text}},
		},
	}
}

var _ = Describe("CodeOwner", func() {
	var (
		fakeExpert          *expertfakes.FakeExpert
		fakeFrontDeskExpert *expertfakes.FakeExpert
		fakeTreeExecutor    *reactreefakes.FakeTreeExecutor
		co                  *codeOwner
		ctx                 context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		fakeExpert = &expertfakes.FakeExpert{}
		fakeFrontDeskExpert = &expertfakes.FakeExpert{}
		fakeTreeExecutor = &reactreefakes.FakeTreeExecutor{}
		co = &codeOwner{
			expert:          fakeExpert,
			frontDeskExpert: fakeFrontDeskExpert,
			treeExecutor:    fakeTreeExecutor,
			memorySvc:       inmemory.NewMemoryService(),
			memoryUserKey: memory.UserKey{
				AppName: "test",
				UserID:  "test",
			},
			auditor: &auditfakes.FakeAuditor{},
		}
	})

	Describe("classifyRequest", func() {
		It("should return REFUSE when classifier says REFUSE", func() {
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("REFUSE"), nil)
			cat, err := co.classifyRequest(ctx, "how do I hack a system?")
			Expect(err).NotTo(HaveOccurred())
			Expect(cat).To(Equal(categoryRefuse))
		})

		It("should return SALUTATION when classifier says SALUTATION", func() {
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("SALUTATION"), nil)
			cat, err := co.classifyRequest(ctx, "hello there!")
			Expect(err).NotTo(HaveOccurred())
			Expect(cat).To(Equal(categorySalutation))
		})

		It("should return COMPLEX when classifier says COMPLEX", func() {
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("COMPLEX"), nil)
			cat, err := co.classifyRequest(ctx, "refactor the database layer")
			Expect(err).NotTo(HaveOccurred())
			Expect(cat).To(Equal(categoryComplex))
		})

		It("should handle case-insensitive classifier output", func() {
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("refuse"), nil)
			cat, err := co.classifyRequest(ctx, "dangerous request")
			Expect(err).NotTo(HaveOccurred())
			Expect(cat).To(Equal(categoryRefuse))
		})

		It("should handle classifier output with extra whitespace", func() {
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("  SALUTATION  \n"), nil)
			cat, err := co.classifyRequest(ctx, "hi")
			Expect(err).NotTo(HaveOccurred())
			Expect(cat).To(Equal(categorySalutation))
		})

		It("should default to COMPLEX on unexpected response (fail-open)", func() {
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("I don't understand"), nil)
			cat, err := co.classifyRequest(ctx, "some question")
			Expect(err).NotTo(HaveOccurred())
			Expect(cat).To(Equal(categoryComplex))
		})

		It("should return error but default to COMPLEX when expert fails", func() {
			fakeFrontDeskExpert.DoReturns(expert.Response{}, errors.New("model unreachable"))
			cat, err := co.classifyRequest(ctx, "hello")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("classification call failed"))
			Expect(cat).To(Equal(categoryComplex))
		})
	})

	Describe("Chat", func() {
		It("should short-circuit with refusal for REFUSE category", func() {
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("REFUSE"), nil)

			outputChan := make(chan string, 10)
			err := co.Chat(ctx, CodeQuestion{
				Question: "hack something",
			}, outputChan)

			Expect(err).NotTo(HaveOccurred())
			Expect(outputChan).To(Receive(Equal("I'm sorry, I can't help with that request.")))
			// Tree executor should NOT be called
			Expect(fakeTreeExecutor.RunCallCount()).To(Equal(0))
		})

		It("should use main expert for SALUTATION category", func() {
			// Classification (frontDeskExpert) returns SALUTATION
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("SALUTATION"), nil)
			// Salutation response uses the main expert (not frontDeskExpert)
			fakeExpert.DoReturns(fakeExpertResponse("Hello! How can I help you today?"), nil)

			outputChan := make(chan string, 10)
			err := co.Chat(ctx, CodeQuestion{
				Question: "hi there",
			}, outputChan)

			Expect(err).NotTo(HaveOccurred())
			Expect(outputChan).To(Receive(Equal("Hello! How can I help you today?")))
			// Tree executor should NOT be called
			Expect(fakeTreeExecutor.RunCallCount()).To(Equal(0))
			// Front desk used once (classify), main expert used once (respond)
			Expect(fakeFrontDeskExpert.DoCallCount()).To(Equal(1))
			Expect(fakeExpert.DoCallCount()).To(Equal(1))
		})

		It("should proceed to tree executor for COMPLEX category", func() {
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("COMPLEX"), nil)
			fakeTreeExecutor.RunReturns(reactree.TreeResult{
				Status: reactree.Success,
				Output: "Here is the refactored code...",
			}, nil)

			mTool := &mockTool{}
			co.tools = reactree.ToolRegistry{mTool.Declaration().Name: mTool}

			outputChan := make(chan string, 10)
			err := co.Chat(ctx, CodeQuestion{
				Question: "refactor the database layer",
			}, outputChan)

			Expect(err).NotTo(HaveOccurred())
			// Verify Tree Executor was called
			Expect(fakeTreeExecutor.RunCallCount()).To(Equal(1))
			_, req := fakeTreeExecutor.RunArgsForCall(0)
			Expect(req.Goal).To(ContainSubstring("refactor the database layer"))
			Expect(req.Tools).To(ContainElement(mTool))
			// Verify output
			Expect(outputChan).To(Receive(Equal("Here is the refactored code...")))
		})

		It("should fall through to COMPLEX when classification fails", func() {
			fakeFrontDeskExpert.DoReturns(expert.Response{}, errors.New("network error"))
			fakeTreeExecutor.RunReturns(reactree.TreeResult{
				Status: reactree.Success,
				Output: "Hello World",
			}, nil)

			outputChan := make(chan string, 10)
			err := co.Chat(ctx, CodeQuestion{
				Question: "Hi",
			}, outputChan)

			Expect(err).NotTo(HaveOccurred())
			// Should fall through to tree executor
			Expect(fakeTreeExecutor.RunCallCount()).To(Equal(1))
			Expect(outputChan).To(Receive(Equal("Hello World")))
		})

		It("should return error if tree executor fails", func() {
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("COMPLEX"), nil)
			fakeTreeExecutor.RunReturns(reactree.TreeResult{}, errors.New("tree execution failed"))

			outputChan := make(chan string, 10)
			err := co.Chat(ctx, CodeQuestion{
				Question: "Hi",
			}, outputChan)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("tree execution failed"))
		})
	})

	Describe("extractTextFromChoices", func() {
		It("should extract text from single choice", func() {
			choices := []model.Choice{
				{Message: model.Message{Content: "hello"}},
			}
			Expect(extractTextFromChoices(choices)).To(Equal("hello"))
		})

		It("should concatenate text from multiple choices", func() {
			choices := []model.Choice{
				{Message: model.Message{Content: "hello "}},
				{Message: model.Message{Content: "world"}},
			}
			Expect(extractTextFromChoices(choices)).To(Equal("hello world"))
		})

		It("should skip choices with empty content", func() {
			choices := []model.Choice{
				{Message: model.Message{Content: ""}},
				{Message: model.Message{Content: "only this"}},
			}
			Expect(extractTextFromChoices(choices)).To(Equal("only this"))
		})

		It("should return empty string for nil/empty choices", func() {
			Expect(extractTextFromChoices(nil)).To(Equal(""))
			Expect(extractTextFromChoices([]model.Choice{})).To(Equal(""))
		})
	})

	Describe("loadAgentsGuide", func() {
		It("should return contents when Agents.md exists", func() {
			tmpDir := GinkgoT().TempDir()
			content := "# Coding Standards\n\nFollow these rules."
			err := os.WriteFile(filepath.Join(tmpDir, "Agents.md"), []byte(content), 0644)
			Expect(err).NotTo(HaveOccurred())

			result := loadAgentsGuide(tmpDir)
			Expect(result).To(Equal(content))
		})

		It("should return empty string when Agents.md does not exist", func() {
			tmpDir := GinkgoT().TempDir()
			result := loadAgentsGuide(tmpDir)
			Expect(result).To(BeEmpty())
		})

		It("should return empty string when directory is empty", func() {
			result := loadAgentsGuide("")
			Expect(result).To(BeEmpty())
		})
	})

	Describe("filterTools", func() {
		It("should preserve all tools when no exclusions given", func() {
			webSearch := &namedMockTool{name: "web_search"}
			listFile := &namedMockTool{name: "list_file"}
			runShell := &namedMockTool{name: "run_shell"}
			tools := []tool.Tool{webSearch, listFile, runShell}

			result := filterTools(tools, nil)
			Expect(result).To(HaveLen(3))
			Expect(result).To(ContainElement(webSearch))
			Expect(result).To(ContainElement(listFile))
			Expect(result).To(ContainElement(runShell))
		})

		It("should remove excluded tools but keep others", func() {
			webSearch := &namedMockTool{name: "web_search"}
			listFile := &namedMockTool{name: "list_file"}
			sendMsg := &namedMockTool{name: "send_message"}
			tools := []tool.Tool{webSearch, listFile, sendMsg}

			result := filterTools(tools, []string{"send_message"})
			Expect(result).To(HaveLen(2))
			Expect(result).To(ContainElement(webSearch))
			Expect(result).To(ContainElement(listFile))
			Expect(result).NotTo(ContainElement(sendMsg))
		})

		It("should not mutate the original tools slice", func() {
			webSearch := &namedMockTool{name: "web_search"}
			listFile := &namedMockTool{name: "list_file"}
			tools := []tool.Tool{webSearch, listFile}

			_ = filterTools(tools, []string{"list_file"})
			Expect(tools).To(HaveLen(2)) // original unmodified
		})
	})
})
