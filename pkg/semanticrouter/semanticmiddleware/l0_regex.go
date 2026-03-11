// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package semanticmiddleware

import (
	"context"
	"regexp"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// L0RegexConfig configures the L0 regex pre-filter middleware.
type L0RegexConfig struct {
	// Disabled turns off the regex pre-filter entirely.
	Disabled bool `yaml:"disabled,omitempty" toml:"disabled,omitempty"`

	// ExtraPatterns allows adding custom regex patterns that should be
	// treated as follow-up messages. Each string is compiled as a regexp.
	ExtraPatterns []string `yaml:"extra_patterns,omitempty" toml:"extra_patterns,omitempty"`
}

// defaultL0Patterns are compiled patterns for ultra-fast detection
// of conversational follow-ups and corrections. When matched, the middleware
// flags cc.IsFollowUp and passes downstream — it does NOT short-circuit.
// The follow-up bypass middleware downstream handles the actual decision.
var defaultL0Patterns = []*regexp.Regexp{
	// Retry / repeat requests
	regexp.MustCompile(`(?i)^(pls\s+|please\s+)?(try|do\s+it|retry)\s+(again|once\s+more)`),
	regexp.MustCompile(`(?i)^(try|retry)\s+again`),
	regexp.MustCompile(`(?i)^(pls|please)\s+(retry|redo|repeat)`),

	// Corrections / clarifications
	regexp.MustCompile(`(?i)^(but\s+)?(I\s+)?(wanted|meant|asked\s+for)`),
	regexp.MustCompile(`(?i)^that'?s\s+not\s+what\s+I`),
	regexp.MustCompile(`(?i)^no,?\s+(I|that|it)\s+`),
	regexp.MustCompile(`(?i)^you\s+(already|just|can)\s+`),

	// Contextual meta-commands
	regexp.MustCompile(`(?i)^(do|run|execute)\s+(that|this|it)\s+(again|now|please)`),
	regexp.MustCompile(`(?i)^same\s+(thing|request|query)`),
}

// NewL0Regex returns a middleware that checks the question against compiled
// regex patterns for common conversational follow-ups. If matched, it
// enriches cc.IsFollowUp=true and passes to the next middleware. The actual
// short-circuit to COMPLEX is handled by the follow-up bypass downstream.
func NewL0Regex(cfg L0RegexConfig) Middleware {
	if cfg.Disabled {
		return passthrough
	}

	// Merge default + extra patterns.
	patterns := make([]*regexp.Regexp, len(defaultL0Patterns))
	copy(patterns, defaultL0Patterns)
	for _, p := range cfg.ExtraPatterns {
		compiled, err := regexp.Compile(p)
		if err != nil {
			logger.GetLogger(context.Background()).Warn(
				"ignoring invalid L0 extra regex pattern",
				"pattern", p,
				"error", err,
			)
			continue
		}
		patterns = append(patterns, compiled)
	}

	return func(ctx context.Context, cc *ClassifyContext, next ClassifyFunc) (ClassifyResult, error) {
		q := strings.TrimSpace(cc.Question)
		for _, pat := range patterns {
			if pat.MatchString(q) {
				logger.GetLogger(ctx).Info("L0 regex flagged follow-up pattern",
					"pattern", pat.String())
				cc.IsFollowUp = true

				span := oteltrace.SpanFromContext(ctx)
				span.SetAttributes(
					attribute.String("semanticrouter.l0_match", pat.String()),
					attribute.Bool("semanticrouter.is_follow_up", true),
				)
				break
			}
		}
		return next(ctx, cc)
	}
}

// passthrough is a no-op middleware that just calls next.
func passthrough(ctx context.Context, cc *ClassifyContext, next ClassifyFunc) (ClassifyResult, error) {
	return next(ctx, cc)
}
