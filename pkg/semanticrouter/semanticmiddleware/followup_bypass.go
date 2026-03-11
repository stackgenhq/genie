// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package semanticmiddleware

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// FollowUpBypassConfig configures the follow-up bypass middleware.
type FollowUpBypassConfig struct {
	// Disabled turns off follow-up bypass. When disabled, messages flagged
	// as follow-ups by L0 will still proceed to L2 for classification.
	Disabled bool `yaml:"disabled,omitempty" toml:"disabled,omitempty"`
}

// NewFollowUpBypass returns a middleware that short-circuits to COMPLEX
// when the L0 layer has already flagged the message as a follow-up.
// This ensures the follow-up signal from regex matching is always respected
// even if L1 didn't match (e.g. when using dummy embedder), avoiding a
// wasteful LLM call for messages like "try again".
func NewFollowUpBypass(cfg FollowUpBypassConfig) Middleware {
	if cfg.Disabled {
		return passthrough
	}

	return func(ctx context.Context, cc *ClassifyContext, next ClassifyFunc) (ClassifyResult, error) {
		if !cc.IsFollowUp {
			return next(ctx, cc)
		}

		span := oteltrace.SpanFromContext(ctx)
		span.SetAttributes(
			attribute.String("semanticrouter.level", "follow_up_bypass"),
			attribute.String("semanticrouter.category", "COMPLEX"),
			attribute.Bool("semanticrouter.bypassed_llm", true),
			attribute.Bool("semanticrouter.is_follow_up", true),
		)
		return ClassifyResult{
			Category:    "COMPLEX",
			BypassedLLM: true,
			Level:       "follow_up_bypass",
		}, nil
	}
}
