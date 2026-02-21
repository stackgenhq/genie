package codeowner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/appcd-dev/genie/pkg/agentutils/agentutilsfakes"
	"github.com/appcd-dev/genie/pkg/audit/auditfakes"
	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/expertfakes"
	"github.com/appcd-dev/genie/pkg/memory/vector"
	"github.com/appcd-dev/genie/pkg/memory/vector/vectorfakes"
	"github.com/appcd-dev/genie/pkg/reactree"
	rtmemory "github.com/appcd-dev/genie/pkg/reactree/memory"
	"github.com/appcd-dev/genie/pkg/reactree/reactreefakes"
	"github.com/appcd-dev/genie/pkg/ttlcache"
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
			resume: ttlcache.NewItem(func(_ context.Context) (string, error) {
				return "Kubernetes triage specialist with shell and kubectl tools.", nil
			}, 5*time.Minute),
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

		It("should return OUT_OF_SCOPE when classifier says OUT_OF_SCOPE", func() {
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("OUT_OF_SCOPE"), nil)
			cat, err := co.classifyRequest(ctx, "how to make mango juice")
			Expect(err).NotTo(HaveOccurred())
			Expect(cat).To(Equal(categoryOutOfScope))
		})

		It("should handle case-insensitive OUT_OF_SCOPE", func() {
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("out_of_scope"), nil)
			cat, err := co.classifyRequest(ctx, "recipe for pasta")
			Expect(err).NotTo(HaveOccurred())
			Expect(cat).To(Equal(categoryOutOfScope))
		})

		It("should inject resume into classifier message when resume is available", func() {
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("COMPLEX"), nil)
			_, err := co.classifyRequest(ctx, "check pod status")
			Expect(err).NotTo(HaveOccurred())

			// Verify the message sent to front desk includes the resume
			Expect(fakeFrontDeskExpert.DoCallCount()).To(Equal(1))
			_, req := fakeFrontDeskExpert.DoArgsForCall(0)
			Expect(req.Message).To(ContainSubstring("## Agent Resume"))
			Expect(req.Message).To(ContainSubstring("Kubernetes triage specialist"))
			Expect(req.Message).To(ContainSubstring("check pod status"))
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
			var msg string
			Expect(outputChan).To(Receive(&msg))
			Expect(msg).To(ContainSubstring("no-go zone"))
			// Tree executor should NOT be called
			Expect(fakeTreeExecutor.RunCallCount()).To(Equal(0))
		})

		It("should short-circuit with resume message for OUT_OF_SCOPE category", func() {
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("OUT_OF_SCOPE"), nil)

			outputChan := make(chan string, 10)
			err := co.Chat(ctx, CodeQuestion{
				Question: "how to make mango juice",
			}, outputChan)

			Expect(err).NotTo(HaveOccurred())
			var msg string
			Expect(outputChan).To(Receive(&msg))
			Expect(msg).To(ContainSubstring("Wrong person"))
			Expect(msg).To(ContainSubstring("Kubernetes triage specialist"))
			// Tree executor should NOT be called
			Expect(fakeTreeExecutor.RunCallCount()).To(Equal(0))
		})

		It("should use fallback message when resume is empty for OUT_OF_SCOPE", func() {
			// Override resume to return empty
			co.resume = ttlcache.NewItem(func(_ context.Context) (string, error) {
				return "", nil
			}, 5*time.Minute)
			fakeFrontDeskExpert.DoReturns(fakeExpertResponse("OUT_OF_SCOPE"), nil)

			outputChan := make(chan string, 10)
			err := co.Chat(ctx, CodeQuestion{
				Question: "how to make pasta",
			}, outputChan)

			Expect(err).NotTo(HaveOccurred())
			var msg string
			Expect(outputChan).To(Receive(&msg))
			Expect(msg).To(ContainSubstring("mysterious specialist"))
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

	Describe("recallAccomplishments", func() {
		It("should return empty string when vectorStore is nil", func() {
			co.vectorStore = nil
			result := co.recallAccomplishments(ctx)
			Expect(result).To(BeEmpty())
		})

		It("should return empty string when search returns no results", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.SearchWithFilterReturns([]vector.SearchResult{}, nil)
			co.vectorStore = fakeStore

			result := co.recallAccomplishments(ctx)
			Expect(result).To(BeEmpty())
			Expect(fakeStore.SearchWithFilterCallCount()).To(Equal(1))
			_, query, limit, filter := fakeStore.SearchWithFilterArgsForCall(0)
			Expect(query).To(Equal(rtmemory.AccomplishmentType))
			Expect(limit).To(Equal(50))
			Expect(filter).To(HaveKeyWithValue("type", rtmemory.AccomplishmentType))
		})

		It("should return empty string when search errors", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.SearchWithFilterReturns(nil, errors.New("search failed"))
			co.vectorStore = fakeStore

			result := co.recallAccomplishments(ctx)
			Expect(result).To(BeEmpty())
		})

		It("should format accomplishments as a bulleted list", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.SearchWithFilterReturns([]vector.SearchResult{
				{Content: "Q: deploy app\nA: deployed successfully", Score: 0.9, Metadata: map[string]string{"type": rtmemory.AccomplishmentType}},
				{Content: "Q: fix bug\nA: fixed the null pointer", Score: 0.8, Metadata: map[string]string{"type": rtmemory.AccomplishmentType}},
			}, nil)
			co.vectorStore = fakeStore

			result := co.recallAccomplishments(ctx)
			Expect(result).To(ContainSubstring("- Q: deploy app"))
			Expect(result).To(ContainSubstring("- Q: fix bug"))
		})

		It("should filter by type via metadata (non-accomplishments excluded at vector store level)", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			// SearchWithFilter now receives the type filter, so the vector store
			// only returns accomplishments. We verify the filter is passed correctly.
			fakeStore.SearchWithFilterReturns([]vector.SearchResult{
				{Content: "accomplishment entry", Score: 0.9, Metadata: map[string]string{"type": rtmemory.AccomplishmentType}},
			}, nil)
			co.vectorStore = fakeStore

			result := co.recallAccomplishments(ctx)
			Expect(result).To(ContainSubstring("- accomplishment entry"))
			// Verify filter was passed with type=accomplishment
			Expect(fakeStore.SearchWithFilterCallCount()).To(Equal(1))
			_, _, _, filter := fakeStore.SearchWithFilterArgsForCall(0)
			Expect(filter).To(HaveKeyWithValue("type", rtmemory.AccomplishmentType))
		})

		It("should limit to top 5 accomplishments", func() {
			var results []vector.SearchResult
			for i := 0; i < 8; i++ {
				results = append(results, vector.SearchResult{
					Content:  fmt.Sprintf("task %d", i),
					Score:    float64(8-i) / 10.0,
					Metadata: map[string]string{"type": rtmemory.AccomplishmentType},
				})
			}
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.SearchWithFilterReturns(results, nil)
			co.vectorStore = fakeStore

			result := co.recallAccomplishments(ctx)
			// Should contain first 5 (highest scoring) but not the rest
			for i := 0; i < 5; i++ {
				Expect(result).To(ContainSubstring(fmt.Sprintf("task %d", i)))
			}
			for i := 5; i < 8; i++ {
				Expect(result).NotTo(ContainSubstring(fmt.Sprintf("task %d", i)))
			}
		})

		It("should sort by score descending", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.SearchWithFilterReturns([]vector.SearchResult{
				{Content: "low score", Score: 0.3, Metadata: map[string]string{"type": rtmemory.AccomplishmentType}},
				{Content: "high score", Score: 0.9, Metadata: map[string]string{"type": rtmemory.AccomplishmentType}},
				{Content: "mid score", Score: 0.6, Metadata: map[string]string{"type": rtmemory.AccomplishmentType}},
			}, nil)
			co.vectorStore = fakeStore

			result := co.recallAccomplishments(ctx)
			// high score should appear before mid score, which should appear before low score
			highIdx := strings.Index(result, "high score")
			midIdx := strings.Index(result, "mid score")
			lowIdx := strings.Index(result, "low score")
			Expect(highIdx).To(BeNumerically("<", midIdx))
			Expect(midIdx).To(BeNumerically("<", lowIdx))
		})
	})

	Describe("storeAccomplishment", func() {
		It("should not panic when vectorStore is nil", func() {
			co.vectorStore = nil
			Expect(func() {
				co.storeAccomplishment(ctx, "question", reactree.TreeResult{Output: "answer", Status: reactree.Success})
			}).NotTo(Panic())
		})

		It("should store accomplishment with correct metadata", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			co.vectorStore = fakeStore

			co.storeAccomplishment(ctx, "refactor the database", reactree.TreeResult{Output: "done, refactored 3 files", Status: reactree.Success})

			Expect(fakeStore.AddCallCount()).To(Equal(1))
			_, items := fakeStore.AddArgsForCall(0)
			Expect(items).To(HaveLen(1))
			Expect(items[0].ID).To(HavePrefix(rtmemory.AccomplishmentType + "-"))
			Expect(items[0].Text).To(ContainSubstring("Q: refactor the database"))
			Expect(items[0].Text).To(ContainSubstring("A: done, refactored 3 files"))
			Expect(items[0].Metadata).To(HaveKeyWithValue("type", rtmemory.AccomplishmentType))
		})

		It("should truncate long questions and answers", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			co.vectorStore = fakeStore

			longQuestion := strings.Repeat("q", 500)
			longAnswer := strings.Repeat("a", 1000)
			co.storeAccomplishment(ctx, longQuestion, reactree.TreeResult{
				Output: longAnswer,
				Status: reactree.Success,
			})

			Expect(fakeStore.AddCallCount()).To(Equal(1))
			_, items := fakeStore.AddArgsForCall(0)
			Expect(items).To(HaveLen(1))
			// Should be truncated — total shouldn't exceed Q(200) + A(500) + formatting
			Expect(len(items[0].Text)).To(BeNumerically("<", 800))
		})

		It("should handle Add errors gracefully without panicking", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.AddReturns(errors.New("disk full"))
			co.vectorStore = fakeStore

			Expect(func() {
				co.storeAccomplishment(ctx, "question", reactree.TreeResult{Output: "answer", Status: reactree.Success})
			}).NotTo(Panic())
			Expect(fakeStore.AddCallCount()).To(Equal(1))
		})

		It("should store accomplishments even if output mentions 'error' (status is Success)", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			co.vectorStore = fakeStore

			// These should all be stored because Status is Success — the agent
			// fixed errors, which is a valid accomplishment.
			phrases := []string{
				"Fixed the error handling in auth module",
				"The build failed earlier but I resolved it",
				"Unable to find file — updated path config to fix",
			}

			for _, phrase := range phrases {
				co.storeAccomplishment(ctx, "do something", reactree.TreeResult{Output: phrase, Status: reactree.Success})
			}

			Expect(fakeStore.AddCallCount()).To(Equal(3))
		})

		It("should NOT store accomplishment when status is not Success", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			co.vectorStore = fakeStore

			co.storeAccomplishment(ctx, "do something", reactree.TreeResult{
				Output: "completed the task",
				Status: reactree.Failure,
			})

			Expect(fakeStore.AddCallCount()).To(Equal(0))
		})
	})

	Describe("Resume", func() {
		It("should return cached resume from ttlcache", func() {
			co.resume = ttlcache.NewItem(func(_ context.Context) (string, error) {
				return "I am a helpful coding agent.", nil
			}, 5*time.Minute)

			result := co.Resume(ctx)
			Expect(result).To(Equal("I am a helpful coding agent."))
		})

		It("should return empty string when retriever returns error", func() {
			co.resume = ttlcache.NewItem(func(_ context.Context) (string, error) {
				return "", errors.New("generation failed")
			}, 5*time.Minute)

			result := co.Resume(ctx)
			Expect(result).To(BeEmpty())
		})

		It("should reflect dynamic updates from retriever", func() {
			callCount := 0
			co.resume = ttlcache.NewItem(func(_ context.Context) (string, error) {
				callCount++
				return fmt.Sprintf("version %d", callCount), nil
			}, 0) // TTL of 0 forces refresh on every call

			r1 := co.Resume(ctx)
			Expect(r1).To(Equal("version 1"))
			r2 := co.Resume(ctx)
			Expect(r2).To(Equal("version 2"))
		})
	})

	Describe("createResume", func() {
		It("should generate a resume using the summarizer and accomplishments", func() {
			// Mock findings in vector store
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.SearchWithFilterReturns([]vector.SearchResult{
				{Content: "Built a go app", Score: 1.0, Metadata: map[string]string{"type": rtmemory.AccomplishmentType}},
			}, nil)
			co.vectorStore = fakeStore

			fakeSummarizer := &agentutilsfakes.FakeSummarizer{}
			fakeSummarizer.SummarizeReturns("Generated Resume Content", nil)

			registry := make(reactree.ToolRegistry)

			resume, err := co.createResume(ctx, fakeSummarizer, registry, "SysPrompt")
			Expect(err).NotTo(HaveOccurred())
			Expect(resume).To(Equal("Generated Resume Content"))

			// Verify it tried to recall accomplishments via SearchWithFilter
			Expect(fakeStore.SearchWithFilterCallCount()).To(Equal(1))

			// Verify it called summarizer
			Expect(fakeSummarizer.SummarizeCallCount()).To(Equal(1))
			_, req := fakeSummarizer.SummarizeArgsForCall(0)
			Expect(req.Content).To(ContainSubstring("Built a go app"))
			Expect(req.Content).To(ContainSubstring("SysPrompt"))
		})
	})
})
