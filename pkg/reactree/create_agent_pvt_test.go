package reactree

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
			func(timedOut bool, lastErr, result, expectedStatus string, outputMatcher OmegaMatcher) {
				req := CreateAgentRequest{AgentName: agentName}
				status, output := req.resolveStatus(timedOut, lastErr, result)
				Expect(status).To(Equal(expectedStatus))
				Expect(output).To(outputMatcher)
			},
			Entry("success with output",
				false, "", "all good",
				"success", Equal("all good"),
			),
			Entry("success with empty output and no error",
				false, "", "",
				"success", BeEmpty(),
			),
			Entry("timeout with partial output",
				true, "", "partial data",
				"partial", And(
					ContainSubstring("[TIME LIMIT REACHED]"),
					ContainSubstring(agentName),
					ContainSubstring("partial data"),
				),
			),
			Entry("timeout with no output",
				true, "", "",
				"partial", And(
					ContainSubstring("[TIME LIMIT REACHED]"),
					ContainSubstring("No output was captured"),
				),
			),
			Entry("error with no output",
				false, "connection refused", "",
				"error", Equal(fmt.Sprintf("sub-agent error: %s", "connection refused")),
			),
			Entry("error is ignored when output exists",
				false, "some warning", "valid result",
				"success", Equal("valid result"),
			),
			Entry("timeout takes precedence over lastErr",
				true, "some error", "",
				"partial", ContainSubstring("[TIME LIMIT REACHED]"),
			),
		)
	})
})
