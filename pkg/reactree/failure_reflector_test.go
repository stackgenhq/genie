// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/expertfakes"
	"github.com/stackgenhq/genie/pkg/reactree"
	"github.com/stackgenhq/genie/pkg/reactree/memory"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

var _ = Describe("ExpertFailureReflector", func() {
	var (
		fakeExpert *expertfakes.FakeExpert
		reflector  *reactree.ExpertFailureReflector
	)

	BeforeEach(func() {
		fakeExpert = &expertfakes.FakeExpert{}
		reflector = reactree.NewExpertFailureReflector(fakeExpert)
	})

	It("should generate a reflection using the expert", func() {
		fakeExpert.DoReturns(expert.Response{
			Choices: []model.Choice{
				{Message: model.Message{Content: "The API timed out due to large payload. Use pagination next time."}},
			},
		}, nil)

		reflection := reflector.Reflect(context.Background(), memory.FailureReflectionRequest{
			Goal:        "fetch all records",
			ErrorOutput: "context deadline exceeded",
		})

		Expect(reflection).To(ContainSubstring("timed out"))
		Expect(fakeExpert.DoCallCount()).To(Equal(1))

		// Verify the prompt sent to the expert contains the goal and error
		_, req := fakeExpert.DoArgsForCall(0)
		Expect(req.Message).To(ContainSubstring("fetch all records"))
		Expect(req.Message).To(ContainSubstring("context deadline exceeded"))
	})

	It("should return empty string on expert error", func() {
		fakeExpert.DoReturns(expert.Response{}, fmt.Errorf("model unavailable"))

		reflection := reflector.Reflect(context.Background(), memory.FailureReflectionRequest{
			Goal:        "fetch data",
			ErrorOutput: "error occurred",
		})

		Expect(reflection).To(BeEmpty())
	})

	It("should truncate long reflections to 300 runes", func() {
		longContent := make([]byte, 1000)
		for i := range longContent {
			longContent[i] = 'A'
		}

		fakeExpert.DoReturns(expert.Response{
			Choices: []model.Choice{
				{Message: model.Message{Content: string(longContent)}},
			},
		}, nil)

		reflection := reflector.Reflect(context.Background(), memory.FailureReflectionRequest{
			Goal:        "test",
			ErrorOutput: "error",
		})

		// 300 runes + "..."
		Expect(len([]rune(reflection))).To(BeNumerically("<=", 303))
		Expect(reflection).To(HaveSuffix("..."))
	})

	It("should truncate long error output before sending to expert", func() {
		longError := make([]byte, 1000)
		for i := range longError {
			longError[i] = 'E'
		}

		fakeExpert.DoReturns(expert.Response{
			Choices: []model.Choice{
				{Message: model.Message{Content: "reflection text"}},
			},
		}, nil)

		_ = reflector.Reflect(context.Background(), memory.FailureReflectionRequest{
			Goal:        "test goal",
			ErrorOutput: string(longError),
		})

		// Verify the prompt sent to the expert has truncated error
		_, req := fakeExpert.DoArgsForCall(0)
		Expect(req.Message).To(ContainSubstring("(truncated)"))
	})

	It("should handle empty choices gracefully", func() {
		fakeExpert.DoReturns(expert.Response{
			Choices: []model.Choice{},
		}, nil)

		reflection := reflector.Reflect(context.Background(), memory.FailureReflectionRequest{
			Goal:        "test",
			ErrorOutput: "error",
		})

		Expect(reflection).To(BeEmpty())
	})

	It("should use TaskEfficiency for cheap model calls", func() {
		fakeExpert.DoReturns(expert.Response{
			Choices: []model.Choice{
				{Message: model.Message{Content: "reflection"}},
			},
		}, nil)

		_ = reflector.Reflect(context.Background(), memory.FailureReflectionRequest{
			Goal:        "test",
			ErrorOutput: "err",
		})

		_, req := fakeExpert.DoArgsForCall(0)
		Expect(string(req.TaskType)).To(Equal("efficiency"))
		Expect(req.Mode.MaxLLMCalls).To(Equal(1))
		Expect(req.Mode.MaxToolIterations).To(Equal(0))
		Expect(req.Mode.Silent).To(BeTrue())
	})
})
