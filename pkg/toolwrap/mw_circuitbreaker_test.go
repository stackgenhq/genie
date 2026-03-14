// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package toolwrap_test

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/toolwrap"
)

var _ = Describe("CircuitBreakerMiddleware", func() {
	It("should open circuit after threshold failures", func() {
		mw := toolwrap.CircuitBreakerMiddleware(toolwrap.CircuitBreakerConfig{
			FailureThreshold: 2,
			OpenDuration:     1 * time.Second,
		})
		handler := mw.Wrap(failing(errors.New("down")))

		for i := 0; i < 2; i++ {
			tc := &toolwrap.ToolCallContext{ToolName: "api", Args: []byte(`{"i":` + string(rune('0'+i)) + `}`)}
			_, err := handler(context.Background(), tc)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("down"))
		}

		_, err := handler(context.Background(), tc("api"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("circuit"))
	})

	It("should allow calls when circuit is closed", func() {
		mw := toolwrap.CircuitBreakerMiddleware(toolwrap.CircuitBreakerConfig{
			FailureThreshold: 5,
		})
		handler := mw.Wrap(passthrough("ok"))

		result, err := handler(context.Background(), tc("api"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})

	It("should isolate breakers per tool", func() {
		mw := toolwrap.CircuitBreakerMiddleware(toolwrap.CircuitBreakerConfig{
			FailureThreshold: 1,
			OpenDuration:     1 * time.Second,
		})
		handler := mw.Wrap(failing(errors.New("down")))

		_, _ = handler(context.Background(), tc("tool_a"))
		_, err := handler(context.Background(), tc("tool_a"))
		Expect(err.Error()).To(ContainSubstring("circuit"))

		_, err = handler(context.Background(), tc("tool_b"))
		Expect(err.Error()).To(ContainSubstring("down"))
	})

	It("should isolate scoped breakers per agent", func() {
		mw := toolwrap.CircuitBreakerMiddleware(toolwrap.CircuitBreakerConfig{
			FailureThreshold: 1,
			OpenDuration:     5 * time.Second,
		})
		scopedA := mw.WithScope("agent_a")
		scopedB := mw.WithScope("agent_b")

		handlerA := scopedA.Wrap(failing(errors.New("policy denied")))
		handlerB := scopedB.Wrap(passthrough("ok"))

		// Trip the circuit for agent_a's "math" tool.
		_, _ = handlerA(context.Background(), tc("math"))
		_, err := handlerA(context.Background(), tc("math"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("circuit"))

		// agent_b's "math" tool should still work — independent circuit.
		result, err := handlerB(context.Background(), tc("math"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("ok"))
	})

	It("should return scoped open tools correctly", func() {
		mw := toolwrap.CircuitBreakerMiddleware(toolwrap.CircuitBreakerConfig{
			FailureThreshold: 1,
			OpenDuration:     5 * time.Second,
		})
		scopedA := mw.WithScope("agent_a")
		scopedB := mw.WithScope("agent_b")

		handlerA := scopedA.Wrap(failing(errors.New("down")))
		handlerB := scopedB.Wrap(passthrough("ok"))

		// Trip agent_a's "math" breaker.
		_, _ = handlerA(context.Background(), tc("math"))

		// agent_b's "math" should succeed (not tripped).
		_, _ = handlerB(context.Background(), tc("math"))

		// OpenTools for agent_a should include "math".
		Expect(scopedA.OpenTools()).To(ContainElement("math"))

		// OpenTools for agent_b should NOT include "math".
		Expect(scopedB.OpenTools()).NotTo(ContainElement("math"))
	})

	It("should return 'this' when WithScope is called with empty string", func() {
		mw := toolwrap.CircuitBreakerMiddleware(toolwrap.CircuitBreakerConfig{
			FailureThreshold: 3,
		})
		Expect(mw.WithScope("")).To(BeIdenticalTo(mw))
	})

	It("should bypass circuit breaker when tool has exact match in ExemptTools", func(ctx context.Context) {
		mw := toolwrap.CircuitBreakerMiddleware(toolwrap.CircuitBreakerConfig{
			FailureThreshold: 1,
			ExemptTools:      []string{"exempt_tool"},
		})
		handler := mw.Wrap(failing(errors.New("down")))

		// Normally, the second call fails with "circuit" due to threshold = 1
		_, err := handler(ctx, tc("exempt_tool"))
		Expect(err.Error()).To(ContainSubstring("down"))

		// Since it's exempt, we expect it to STILL hit the failing handler
		// instead of being blocked by the circuit breaker.
		_, err = handler(ctx, tc("exempt_tool"))
		Expect(err.Error()).To(ContainSubstring("down"))
	})

	It("should bypass circuit breaker when tool matches prefix pattern in ExemptTools", func() {
		mw := toolwrap.CircuitBreakerMiddleware(toolwrap.CircuitBreakerConfig{
			FailureThreshold: 1,
			ExemptTools:      []string{"google_*"},
		})

		handler := mw.Wrap(failing(errors.New("down")))

		// "google_drive" matches "google_*"
		_, err := handler(context.Background(), tc("google_drive"))
		Expect(err.Error()).To(ContainSubstring("down"))

		_, err = handler(context.Background(), tc("google_drive"))
		Expect(err.Error()).To(ContainSubstring("down"))

		// "other_tool" does NOT match
		_, err = handler(context.Background(), tc("other_tool"))
		Expect(err.Error()).To(ContainSubstring("down"))

		// Second call for "other_tool" trips circuit breaker
		_, err = handler(context.Background(), tc("other_tool"))
		Expect(err.Error()).To(ContainSubstring("circuit"))
	})
})
