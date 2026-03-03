package reactree

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"github.com/stackgenhq/genie/pkg/memory/vector/vectorfakes"
	"github.com/stackgenhq/genie/pkg/tools"
	"github.com/stackgenhq/genie/pkg/tools/toolsfakes"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("CreateAgentRequest", func() {

	Describe("timeout", func() {
		DescribeTable("clamps the timeout to [minTimeout, maxTimeout] with a default",
			func(input float64, expected time.Duration) {
				req := CreateAgentRequest{TimeoutSeconds: input}
				Expect(req.timeout()).To(Equal(expected))
			},
			Entry("zero uses default (5 min)", 0.0, defaultTimeout),
			Entry("negative uses default (5 min)", -10.0, defaultTimeout),
			Entry("below floor is clamped to floor", 5.0, minTimeout),
			Entry("exactly at floor stays at floor", minTimeout.Seconds(), minTimeout),
			Entry("within range is unchanged", 180.0, 180*time.Second),
			Entry("exactly at ceiling stays at ceiling", maxTimeout.Seconds(), maxTimeout),
			Entry("above ceiling is clamped to ceiling", 900.0, maxTimeout),
		)
	})

	Describe("clampedMaxToolIterations", func() {
		DescribeTable("clamps to [minToolIterCap, maxToolIterCap]",
			func(input int, expected int) {
				req := CreateAgentRequest{MaxToolIterations: input}
				Expect(req.clampedMaxToolIterations()).To(Equal(expected))
			},
			Entry("zero is raised to floor", 0, minToolIterCap),
			Entry("negative is raised to floor", -1, minToolIterCap),
			Entry("below floor is raised to floor", 3, minToolIterCap),
			Entry("exactly at floor stays", minToolIterCap, minToolIterCap),
			Entry("mid-range is unchanged", 25, 25),
			Entry("exactly at ceiling stays", maxToolIterCap, maxToolIterCap),
			Entry("above ceiling is lowered to ceiling", 100, maxToolIterCap),
		)
	})

	Describe("clampedMaxLLMCalls", func() {
		DescribeTable("clamps to [minLLMCallsCap, maxLLMCallsCap]",
			func(input int, expected int) {
				req := CreateAgentRequest{MaxLLMCalls: input}
				Expect(req.clampedMaxLLMCalls()).To(Equal(expected))
			},
			Entry("zero is raised to floor", 0, minLLMCallsCap),
			Entry("negative is raised to floor", -5, minLLMCallsCap),
			Entry("below floor is raised to floor", 4, minLLMCallsCap),
			Entry("exactly at floor stays", minLLMCallsCap, minLLMCallsCap),
			Entry("mid-range is unchanged", 30, 30),
			Entry("exactly at ceiling stays", maxLLMCallsCap, maxLLMCallsCap),
			Entry("above ceiling is lowered to ceiling", 200, maxLLMCallsCap),
		)
	})

	Describe("flow", func() {
		DescribeTable("maps flow_type string to ControlFlowType",
			func(ctx context.Context, input string, expected ControlFlowType) {
				req := CreateAgentRequest{Flow: input}
				Expect(req.flow(ctx)).To(Equal(expected))
			},
			Entry("parallel", "parallel", ControlFlowParallel),
			Entry("fallback", "fallback", ControlFlowFallback),
			Entry("sequence", "sequence", ControlFlowSequence),
			Entry("empty string defaults to sequence", "", ControlFlowSequence),
			Entry("unknown value defaults to sequence", "round_robin", ControlFlowSequence),
		)
	})

	Describe("resolveStatus", func() {
		const agentName = "test-agent"

		DescribeTable("determines status and output from execution outcome",
			func(timedOut bool, lastErr, result, partialToolResults, expectedStatus string, outputMatcher OmegaMatcher) {
				req := CreateAgentRequest{AgentName: agentName}
				status, output := req.resolveStatus(timedOut, lastErr, result, partialToolResults)
				Expect(status).To(Equal(expectedStatus))
				Expect(output).To(outputMatcher)
			},
			Entry("success with output",
				false, "", "all good", "",
				"success", Equal("all good"),
			),
			Entry("success with empty output and no error",
				false, "", "", "",
				"success", BeEmpty(),
			),
			Entry("timeout with partial output",
				true, "", "partial data", "",
				"partial", And(
					ContainSubstring("[TIME LIMIT REACHED]"),
					ContainSubstring(agentName),
					ContainSubstring("partial data"),
				),
			),
			Entry("timeout with no output",
				true, "", "", "",
				"partial", And(
					ContainSubstring("[TIME LIMIT REACHED]"),
					ContainSubstring("No output was captured"),
				),
			),
			Entry("error with no output",
				false, "connection refused", "", "",
				"error", Equal(fmt.Sprintf("sub-agent error: %s", "connection refused")),
			),
			Entry("error is ignored when output exists",
				false, "some warning", "valid result", "",
				"success", Equal("valid result"),
			),
			Entry("timeout takes precedence over lastErr",
				true, "some error", "", "",
				"partial", ContainSubstring("[TIME LIMIT REACHED]"),
			),
			Entry("error with no output but has partial tool results",
				false, "max LLM calls (10) exceeded", "", "file contents found\n---\nsearch results here",
				"partial", And(
					ContainSubstring("[BUDGET EXCEEDED]"),
					ContainSubstring(agentName),
					ContainSubstring("file contents found"),
					ContainSubstring("search results here"),
					ContainSubstring("max LLM calls (10) exceeded"),
				),
			),
			Entry("timeout with no output but has partial tool results",
				true, "", "", "partial findings from tools",
				"partial", And(
					ContainSubstring("[TIME LIMIT REACHED]"),
					ContainSubstring("partial findings from tools"),
				),
			),
			Entry("error ignored when output exists even with tool results",
				false, "some warning", "valid result", "tool results here",
				"success", Equal("valid result"),
			),
		)
	})
})

var _ = Describe("createAgentTool guard helpers", func() {
	// stubTool creates a minimal tool.Tool with a given name using counterfeiter.
	stubTool := func(name string) tool.Tool {
		fake := &toolsfakes.FakeCallableTool{}
		fake.DeclarationReturns(&tool.Declaration{Name: name})
		return fake
	}

	// makeRegistry builds a tools.Registry from tool names.
	makeRegistry := func(names ...string) *tools.Registry {
		var tt tools.Tools
		for _, n := range names {
			tt = append(tt, stubTool(n))
		}
		return tools.NewRegistry(context.Background(), tt)
	}

	Describe("isRetrievalOnly", func() {
		cat := &createAgentTool{}

		It("returns true when all tools are retrieval-only", func() {
			reg := makeRegistry("memory_search", "graph_query")
			Expect(cat.isRetrievalOnly(reg)).To(BeTrue())
		})

		It("returns true for a single retrieval tool", func() {
			reg := makeRegistry("memory_search")
			Expect(cat.isRetrievalOnly(reg)).To(BeTrue())
		})

		It("returns false when any non-retrieval tool is present", func() {
			reg := makeRegistry("memory_search", "run_shell")
			Expect(cat.isRetrievalOnly(reg)).To(BeFalse())
		})

		It("returns false for an empty registry", func() {
			reg := makeRegistry()
			Expect(cat.isRetrievalOnly(reg)).To(BeFalse())
		})

		It("returns true for all four retrieval tools", func() {
			reg := makeRegistry("memory_search", "graph_query", "graph_get_entity", "graph_shortest_path")
			Expect(cat.isRetrievalOnly(reg)).To(BeTrue())
		})
	})

	Describe("hasVectorBackedTools", func() {
		cat := &createAgentTool{}

		It("returns true when memory_search is present", func() {
			reg := makeRegistry("memory_search", "graph_query")
			Expect(cat.hasVectorBackedTools(reg)).To(BeTrue())
		})

		It("returns false for graph-only tools", func() {
			reg := makeRegistry("graph_query", "graph_get_entity")
			Expect(cat.hasVectorBackedTools(reg)).To(BeFalse())
		})

		It("returns false for empty registry", func() {
			reg := makeRegistry()
			Expect(cat.hasVectorBackedTools(reg)).To(BeFalse())
		})
	})

	Describe("isMemoryEmpty", func() {
		It("returns true when vectorStore is nil", func() {
			cat := &createAgentTool{vectorStore: nil}
			Expect(cat.isMemoryEmpty(context.Background())).To(BeTrue())
		})

		It("returns true when Search returns no results", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.SearchReturns(nil, nil)
			cat := &createAgentTool{vectorStore: fakeStore}
			Expect(cat.isMemoryEmpty(context.Background())).To(BeTrue())
		})

		It("returns false when Search returns results", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.SearchReturns([]vector.SearchResult{{ID: "1", Content: "x"}}, nil)
			cat := &createAgentTool{vectorStore: fakeStore}
			Expect(cat.isMemoryEmpty(context.Background())).To(BeFalse())
		})

		It("returns false when Search errors (safe fallback)", func() {
			fakeStore := &vectorfakes.FakeIStore{}
			fakeStore.SearchReturns(nil, fmt.Errorf("connection refused"))
			cat := &createAgentTool{vectorStore: fakeStore}
			Expect(cat.isMemoryEmpty(context.Background())).To(BeFalse())
		})
	})
})
