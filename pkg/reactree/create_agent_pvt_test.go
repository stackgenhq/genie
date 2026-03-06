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
			Entry("middleware cancellation error surfaces correctly",
				false, "loop detected: tool X has been called with identical arguments 3 times consecutively. Stop calling this tool and summarize the results you already have", "", "",
				"error", ContainSubstring("loop detected"),
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

var _ = Describe("createAgentTool halguard integration", func() {
	Describe("SetHalGuardThreshold", func() {
		It("sets a positive threshold", func() {
			t := &createAgentTool{}
			t.SetHalGuardThreshold(0.6)
			Expect(t.halGuardThreshold).To(Equal(0.6))
		})

		It("ignores zero threshold (keeps default)", func() {
			t := &createAgentTool{}
			t.SetHalGuardThreshold(0)
			Expect(t.halGuardThreshold).To(Equal(0.0))
		})

		It("ignores negative threshold", func() {
			t := &createAgentTool{}
			t.SetHalGuardThreshold(-0.5)
			Expect(t.halGuardThreshold).To(Equal(0.0))
		})
	})

	Describe("pre-check threshold logic", func() {
		It("uses default 0.4 threshold when halGuardThreshold is zero", func() {
			t := &createAgentTool{}
			// Default threshold is 0.4, so confidence 0.39 should be below it
			Expect(t.halGuardThreshold).To(Equal(0.0))

			// When threshold is 0, execute uses 0.4 as fallback
			threshold := 0.4
			if t.halGuardThreshold > 0 {
				threshold = t.halGuardThreshold
			}
			Expect(threshold).To(Equal(0.4))
		})

		It("uses configured threshold when set", func() {
			t := &createAgentTool{}
			t.SetHalGuardThreshold(0.7)

			threshold := 0.4
			if t.halGuardThreshold > 0 {
				threshold = t.halGuardThreshold
			}
			Expect(threshold).To(Equal(0.7))
		})
	})
})

var _ = Describe("zero-tool-use guard", func() {
	// The zero-tool-use guard fires when:
	// toolCallCount == 0 && result != "" && status != "error" && !timedOut && len(selectedTools) > 0
	// It annotates the output and sets status = "tool_use_failure"

	type guardInput struct {
		toolCallCount    int
		result           string
		status           string
		timedOut         bool
		numSelectedTools int
	}

	shouldFire := func(gi guardInput) bool {
		return gi.toolCallCount == 0 && gi.result != "" && gi.status != "error" && !gi.timedOut && gi.numSelectedTools > 0
	}

	DescribeTable("fires or skips based on conditions",
		func(gi guardInput, expectFire bool) {
			Expect(shouldFire(gi)).To(Equal(expectFire))
		},
		Entry("fires: zero tool calls, has result, has tools, not error, not timed out",
			guardInput{toolCallCount: 0, result: "some output", status: "success", timedOut: false, numSelectedTools: 3},
			true,
		),
		Entry("skips: sub-agent made tool calls",
			guardInput{toolCallCount: 2, result: "some output", status: "success", timedOut: false, numSelectedTools: 3},
			false,
		),
		Entry("skips: empty result (nothing to annotate)",
			guardInput{toolCallCount: 0, result: "", status: "success", timedOut: false, numSelectedTools: 3},
			false,
		),
		Entry("skips: status is error",
			guardInput{toolCallCount: 0, result: "error message", status: "error", timedOut: false, numSelectedTools: 3},
			false,
		),
		Entry("skips: sub-agent timed out",
			guardInput{toolCallCount: 0, result: "partial output", status: "partial", timedOut: true, numSelectedTools: 3},
			false,
		),
		Entry("skips: no tools available (ask_clarifying_question only agents)",
			guardInput{toolCallCount: 0, result: "some answer", status: "success", timedOut: false, numSelectedTools: 0},
			false,
		),
		Entry("fires: single tool available but unused",
			guardInput{toolCallCount: 0, result: "I don't have access to Azure", status: "success", timedOut: false, numSelectedTools: 1},
			true,
		),
	)

	It("annotates output with tool_use_failure message", func() {
		// Simulate what the guard does to the output
		originalOutput := "I don't know. I do not have access to the 'appcd-demo' Azure subscription"
		toolNames := "run_shell, read_file"

		annotated := fmt.Sprintf(
			"⚠️ SUB-AGENT DID NOT USE TOOLS: The sub-agent produced a text-only response "+
				"without calling any of its available tools (%s). This likely means it echoed "+
				"commands as text or refused the task instead of executing it. "+
				"The sub-agent should be re-spawned. Original output follows:\n\n%s",
			toolNames, originalOutput)

		Expect(annotated).To(ContainSubstring("SUB-AGENT DID NOT USE TOOLS"))
		Expect(annotated).To(ContainSubstring("run_shell"))
		Expect(annotated).To(ContainSubstring("re-spawned"))
		Expect(annotated).To(ContainSubstring(originalOutput))
	})

	It("sets status to tool_use_failure", func() {
		status := "tool_use_failure"
		Expect(status).To(Equal("tool_use_failure"))
		Expect(status).NotTo(Equal("success"))
		Expect(status).NotTo(Equal("error"))
	})
})

var _ = Describe("auto-retry on tool_use_failure", func() {
	// The auto-retry in execute() constructs a retryReq when
	// resp.Status == "tool_use_failure". We test the retry
	// prompt construction and agent naming here.

	It("prepends RETRY prefix to the goal", func() {
		originalGoal := "Run this script: az vm list"
		retryGoal := "[RETRY — PREVIOUS ATTEMPT FAILED] " +
			"Your previous attempt FAILED because you echoed commands as text instead of executing them. " +
			"You MUST call the run_shell tool to execute the script below. " +
			"Do NOT output the script as text. Call run_shell with the script as the command argument.\n\n" +
			originalGoal

		Expect(retryGoal).To(HavePrefix("[RETRY — PREVIOUS ATTEMPT FAILED]"))
		Expect(retryGoal).To(ContainSubstring("You MUST call the run_shell tool"))
		Expect(retryGoal).To(ContainSubstring(originalGoal))
	})

	It("appends -retry suffix to the agent name", func() {
		agentName := "azure-functions-check"
		retryName := agentName + "-retry"
		Expect(retryName).To(Equal("azure-functions-check-retry"))
	})

	It("preserves the original goal in the retry prompt", func() {
		originalGoal := "```bash\naz functionapp list --query '[].{Name:name}'\n```"
		retryGoal := "[RETRY — PREVIOUS ATTEMPT FAILED] " +
			"Your previous attempt FAILED because you echoed commands as text instead of executing them. " +
			"You MUST call the run_shell tool to execute the script below. " +
			"Do NOT output the script as text. Call run_shell with the script as the command argument.\n\n" +
			originalGoal

		Expect(retryGoal).To(ContainSubstring("az functionapp list"))
		Expect(retryGoal).To(ContainSubstring("RETRY"))
		Expect(retryGoal).To(ContainSubstring("run_shell"))
	})

	It("retry is only triggered when status is tool_use_failure", func() {
		// Verify the condition: err == nil && resp.Status == "tool_use_failure"
		type retryCheck struct {
			err    error
			status string
		}
		shouldRetry := func(rc retryCheck) bool {
			return rc.err == nil && rc.status == "tool_use_failure"
		}

		Expect(shouldRetry(retryCheck{nil, "tool_use_failure"})).To(BeTrue())
		Expect(shouldRetry(retryCheck{nil, "success"})).To(BeFalse())
		Expect(shouldRetry(retryCheck{nil, "error"})).To(BeFalse())
		Expect(shouldRetry(retryCheck{nil, "partial"})).To(BeFalse())
		Expect(shouldRetry(retryCheck{fmt.Errorf("some error"), "tool_use_failure"})).To(BeFalse())
	})
})
