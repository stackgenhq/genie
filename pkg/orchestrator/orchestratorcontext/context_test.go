// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package orchestratorcontext_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/orchestrator/orchestratorcontext"
)

var _ = Describe("OrchestratorContext", func() {

	Describe("WithAgent", func() {
		It("should store the agent in context", func(ctx context.Context) {
			agent := orchestratorcontext.Agent{Name: "my-bot"}
			ctx = orchestratorcontext.WithAgent(ctx, agent)

			got := orchestratorcontext.AgentFromContext(ctx)
			Expect(got.Name).To(Equal("my-bot"))
		})

		It("should not overwrite an existing non-empty agent", func(ctx context.Context) {
			first := orchestratorcontext.Agent{Name: "first-bot"}
			ctx = orchestratorcontext.WithAgent(ctx, first)

			second := orchestratorcontext.Agent{Name: "second-bot"}
			ctx = orchestratorcontext.WithAgent(ctx, second)

			got := orchestratorcontext.AgentFromContext(ctx)
			Expect(got.Name).To(Equal("first-bot"))
		})

		It("should overwrite an agent with an empty name", func(ctx context.Context) {
			empty := orchestratorcontext.Agent{Name: ""}
			ctx = orchestratorcontext.WithAgent(ctx, empty)

			real := orchestratorcontext.Agent{Name: "real-bot"}
			ctx = orchestratorcontext.WithAgent(ctx, real)

			got := orchestratorcontext.AgentFromContext(ctx)
			Expect(got.Name).To(Equal("real-bot"))
		})
	})

	Describe("AgentFromContext", func() {
		It("should return default agent when context has no agent", func(ctx context.Context) {
			got := orchestratorcontext.AgentFromContext(ctx)
			Expect(got.Name).To(Equal(orchestratorcontext.DefaultAgentName))
		})

		It("should return the stored agent when one exists", func(ctx context.Context) {
			ctx = orchestratorcontext.WithAgent(ctx, orchestratorcontext.Agent{Name: "stored"})

			got := orchestratorcontext.AgentFromContext(ctx)
			Expect(got.Name).To(Equal("stored"))
		})
	})

	Describe("AgentNameFromContext", func() {
		It("should return DefaultAgentName when no agent is set", func(ctx context.Context) {
			name := orchestratorcontext.AgentNameFromContext(ctx)
			Expect(name).To(Equal("Genie"))
		})

		It("should return the configured agent name", func(ctx context.Context) {
			ctx = orchestratorcontext.WithAgent(ctx, orchestratorcontext.Agent{Name: "qa_agent"})

			name := orchestratorcontext.AgentNameFromContext(ctx)
			Expect(name).To(Equal("qa_agent"))
		})

		It("should propagate through child contexts", func(ctx context.Context) {
			ctx = orchestratorcontext.WithAgent(ctx, orchestratorcontext.Agent{Name: "parent-bot"})

			childCtx, cancel := context.WithCancel(ctx)
			defer cancel()

			name := orchestratorcontext.AgentNameFromContext(childCtx)
			Expect(name).To(Equal("parent-bot"))
		})
	})

	Describe("DefaultAgentName", func() {
		It("should be Genie", func() {
			Expect(orchestratorcontext.DefaultAgentName).To(Equal("Genie"))
		})
	})
})
