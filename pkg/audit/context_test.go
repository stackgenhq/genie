package audit_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/audit"
)

var _ = Describe("Context helpers", func() {

	Describe("WithAgentName / AgentNameFromContext", func() {
		It("round-trips the agent name through context", func(ctx context.Context) {
			ctx = audit.WithAgentName(ctx, "pirate-bot")
			Expect(audit.AgentNameFromContext(ctx)).To(Equal("pirate-bot"))
		})

		It("returns empty string when no agent name is set", func(ctx context.Context) {
			Expect(audit.AgentNameFromContext(ctx)).To(BeEmpty())
		})

		It("stores an empty string without error", func(ctx context.Context) {
			ctx = audit.WithAgentName(ctx, "")
			// Empty string *is* set, so AgentNameFromContext should return it.
			Expect(audit.AgentNameFromContext(ctx)).To(BeEmpty())
		})

		It("preserves the first agent name (first-write-wins)", func(ctx context.Context) {
			ctx = audit.WithAgentName(ctx, "first-bot")
			ctx = audit.WithAgentName(ctx, "second-bot")
			Expect(audit.AgentNameFromContext(ctx)).To(Equal("first-bot"))
		})

		It("allows setting after an empty-string agent name", func(ctx context.Context) {
			// An empty string doesn't count as "existing" for the guard,
			// so a subsequent non-empty call should succeed.
			ctx = audit.WithAgentName(ctx, "")
			ctx = audit.WithAgentName(ctx, "real-bot")
			Expect(audit.AgentNameFromContext(ctx)).To(Equal("real-bot"))
		})

		It("does not leak between independent context branches", func(ctx context.Context) {
			branch1 := audit.WithAgentName(ctx, "agent-a")
			branch2 := audit.WithAgentName(ctx, "agent-b")

			Expect(audit.AgentNameFromContext(branch1)).To(Equal("agent-a"))
			Expect(audit.AgentNameFromContext(branch2)).To(Equal("agent-b"))
			Expect(audit.AgentNameFromContext(ctx)).To(BeEmpty())
		})
	})
})
