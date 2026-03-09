package reactree

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/agentutils/agentutilsfakes"
	"github.com/stackgenhq/genie/pkg/audit/auditfakes"
	"github.com/stackgenhq/genie/pkg/expert/expertfakes"
	"github.com/stackgenhq/genie/pkg/hooks"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/reactree/memory/memoryfakes"
	"github.com/stackgenhq/genie/pkg/tools"
	"github.com/stackgenhq/genie/pkg/tools/toolsfakes"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("CreateAgentTool", func() {
	Describe("NewCreateAgentTool", func() {
		It("creates tool with correct fields", func() {
			fakeExpert := &expertfakes.FakeExpert{}
			fakeSummarizer := &agentutilsfakes.FakeSummarizer{}
			fakeEpisodic := &memoryfakes.FakeEpisodicMemory{}
			wm := memory.NewWorkingMemory()
			fakeTool := &toolsfakes.FakeCallableTool{}
			fakeTool.DeclarationReturns(&tool.Declaration{Name: "run_shell"})
			registry := tools.NewRegistry(context.Background(), &testToolProvider{t: []tool.Tool{fakeTool}})

			cat := NewCreateAgentTool(
				nil, fakeExpert, fakeSummarizer, registry, wm, fakeEpisodic, nil, nil, nil,
			)
			Expect(cat).NotTo(BeNil())
			Expect(cat.expert).To(Equal(fakeExpert))
			Expect(cat.description).To(ContainSubstring("run_shell"))
		})

		It("applies options", func() {
			cat := NewCreateAgentTool(nil, nil, nil,
				tools.NewRegistry(context.Background()), nil, nil, nil, nil, nil,
				WithSkipSummarizeMarker(true))
			Expect(cat.skipSummarize).To(BeTrue())
		})

		It("excludes orchestration-only tools from sub-agent registry", func() {
			run := &toolsfakes.FakeCallableTool{}
			run.DeclarationReturns(&tool.Declaration{Name: "run_shell"})
			ca := &toolsfakes.FakeCallableTool{}
			ca.DeclarationReturns(&tool.Declaration{Name: "create_agent"})
			reg := tools.NewRegistry(context.Background(), &testToolProvider{t: []tool.Tool{run, ca}})

			cat := NewCreateAgentTool(nil, nil, nil, reg, nil, nil, nil, nil, nil)
			Expect(cat.subAgentRegistry.ToolNames()).To(ContainElement("run_shell"))
			Expect(cat.subAgentRegistry.ToolNames()).NotTo(ContainElement("create_agent"))
		})
	})

	Describe("GetTool", func() {
		It("returns tool with correct name", func() {
			cat := NewCreateAgentTool(nil, nil, nil,
				tools.NewRegistry(context.Background()), nil, nil, nil, nil, nil)
			Expect(cat.GetTool().Declaration().Name).To(Equal("create_agent"))
		})
	})

	Describe("WithSkipSummarizeMarker", func() {
		DescribeTable("sets skipSummarize",
			func(val bool) {
				t := &createAgentTool{}
				WithSkipSummarizeMarker(val)(t)
				Expect(t.skipSummarize).To(Equal(val))
			},
			Entry("true", true),
			Entry("false", false),
		)
	})

	Describe("SetHalGuardThreshold", func() {
		It("sets the threshold", func() {
			cat := NewCreateAgentTool(nil, nil, nil,
				tools.NewRegistry(context.Background()), nil, nil, nil, nil, nil)
			cat.SetHalGuardThreshold(0.7)
			Expect(cat.halGuardThreshold).To(Equal(0.7))
		})
	})

	Describe("AuditHook.OnPlanExecution", func() {
		It("logs a plan execution event", func() {
			fakeAuditor := &auditfakes.FakeAuditor{}
			hook := NewAuditHook(fakeAuditor)
			hook.OnPlanExecution(context.Background(), hooks.PlanExecutionEvent{
				Flow: "sequence", StepCount: 3,
				StepNames: []string{"s1", "s2", "s3"},
			})
			Expect(fakeAuditor.LogCallCount()).To(Equal(1))
		})
	})
})

var _ = Describe("CreateAgentRequest", func() {
	Describe("flow", func() {
		DescribeTable("maps flow string to ControlFlowType",
			func(input string, expected ControlFlowType) {
				Expect(CreateAgentRequest{Flow: input}.flow(context.Background())).To(Equal(expected))
			},
			Entry("parallel", "parallel", ControlFlowParallel),
			Entry("fallback", "fallback", ControlFlowFallback),
			Entry("sequence", "sequence", ControlFlowSequence),
			Entry("empty → sequence", "", ControlFlowSequence),
			Entry("unknown → sequence", "weird", ControlFlowSequence),
		)
	})

	Describe("clampedMaxToolIterations", func() {
		DescribeTable("clamps value",
			func(input int, min, max int) {
				v := CreateAgentRequest{MaxToolIterations: input}.clampedMaxToolIterations()
				Expect(v).To(BeNumerically(">=", min))
				Expect(v).To(BeNumerically("<=", max))
			},
			Entry("low", 1, minToolIterCap, maxToolIterCap),
			Entry("high", 9999, minToolIterCap, maxToolIterCap),
			Entry("in range", 20, 20, 20),
		)
	})

	Describe("clampedMaxLLMCalls", func() {
		DescribeTable("clamps value",
			func(input int, min, max int) {
				v := CreateAgentRequest{MaxLLMCalls: input}.clampedMaxLLMCalls()
				Expect(v).To(BeNumerically(">=", min))
				Expect(v).To(BeNumerically("<=", max))
			},
			Entry("low", 1, minLLMCallsCap, maxLLMCallsCap),
			Entry("high", 9999, minLLMCallsCap, maxLLMCallsCap),
		)
	})

	Describe("timeout", func() {
		DescribeTable("returns correct timeout",
			func(input float64, expected string) {
				t := CreateAgentRequest{TimeoutSeconds: input}.timeout()
				if expected == "default" {
					Expect(t).To(Equal(defaultTimeout))
				} else {
					Expect(t).To(BeNumerically("<=", maxTimeout))
				}
			},
			Entry("zero → default", 0.0, "default"),
			Entry("negative → default", -10.0, "default"),
			Entry("huge → clamped", 99999.0, "max"),
		)
	})

	Describe("resolveStatus", func() {
		DescribeTable("resolves status correctly",
			func(timedOut bool, errMsg, output, partial, expectedStatus string) {
				req := CreateAgentRequest{AgentName: "test"}
				status, _ := req.resolveStatus(timedOut, errMsg, output, partial)
				Expect(status).To(Equal(expectedStatus))
			},
			Entry("success with output", false, "", "result", "", "success"),
			Entry("partial on timeout", true, "", "some", "", "partial"),
			Entry("partial timeout no output", true, "", "", "partial", "partial"),
			Entry("partial timeout nothing", true, "", "", "", "partial"),
			Entry("error no output", false, "model failed", "", "", "error"),
			Entry("partial error+partial", false, "budget", "", "found 3", "partial"),
			Entry("success despite error", false, "err", "real output", "", "success"),
		)
	})

	Describe("guards", func() {
		It("isRetrievalOnly returns false for empty registry", func() {
			cat := &createAgentTool{}
			Expect(cat.isRetrievalOnly(tools.NewRegistry(context.Background()))).To(BeFalse())
		})

		It("hasVectorBackedTools returns true for memory_search", func() {
			cat := &createAgentTool{}
			ft := &toolsfakes.FakeCallableTool{}
			ft.DeclarationReturns(&tool.Declaration{Name: "memory_search"})
			reg := tools.NewRegistry(context.Background(), &testToolProvider{t: []tool.Tool{ft}})
			Expect(cat.hasVectorBackedTools(reg)).To(BeTrue())
		})

		It("isMemoryEmpty returns true when vectorStore is nil", func() {
			Expect((&createAgentTool{}).isMemoryEmpty(context.Background())).To(BeTrue())
		})
	})
})
