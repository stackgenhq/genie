// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package semanticmiddleware

import (
	"context"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/memory/vector"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// L1VectorConfig configures the L1 vector-based semantic routing middleware.
type L1VectorConfig struct {
	// Disabled turns off L1 vector routing.
	Disabled bool `yaml:"disabled,omitempty" toml:"disabled,omitempty"`

	// Threshold for semantic similarity matches (0.0 to 1.0).
	// Matches above this threshold trigger a short-circuit decision.
	Threshold float64 `yaml:"threshold,omitempty" toml:"threshold,omitempty"`

	// EnrichBelowThreshold controls whether near-miss matches (above 0.5
	// but below Threshold) are added to the ClassifyContext for downstream
	// middlewares to use as routing hints. Default: true.
	EnrichBelowThreshold bool `yaml:"enrich_below_threshold,omitempty" toml:"enrich_below_threshold,omitempty"`
}

// defaultL1EnrichmentFloor is the minimum score needed for a near-miss to
// be worth passing downstream as a hint.
const defaultL1EnrichmentFloor = 0.5

// Route name constants shared with l0 and the parent router package.
const (
	RouteJailbreak  = "jailbreak"
	RouteSalutation = "salutation"
	RouteFollowUp   = "follow_up"
)

// NewL1Vector returns a middleware that checks the question against
// a route vector store. If a route exceeds the threshold, it decides immediately.
// If the score is below threshold but above the enrichment floor, it enriches
// cc with the closest route and score so downstream middlewares can use the signal.
func NewL1Vector(cfg L1VectorConfig, routeStore vector.IStore) Middleware {
	if cfg.Disabled || routeStore == nil {
		return passthrough
	}

	return func(ctx context.Context, cc *ClassifyContext, next ClassifyFunc) (ClassifyResult, error) {
		results, err := routeStore.Search(ctx, cc.Question, 1)
		if err != nil {
			logger.GetLogger(ctx).Warn("L1 semantic route search failed", "error", err)
			return next(ctx, cc)
		}
		if len(results) == 0 {
			return next(ctx, cc)
		}

		bestRoute := results[0].Metadata["route"]
		bestScore := results[0].Score

		// Enrich context for downstream middlewares (even below threshold).
		if cfg.EnrichBelowThreshold && bestScore >= defaultL1EnrichmentFloor {
			cc.ClosestRoute = bestRoute
			cc.RouteScore = bestScore
		}

		if bestScore < cfg.Threshold {
			return next(ctx, cc)
		}

		// Decisive match — short-circuit.
		logger.GetLogger(ctx).Info("L1 semantic route matched, bypassing LLM",
			"route", bestRoute, "score", bestScore)

		category := routeToCategory(bestRoute)
		span := oteltrace.SpanFromContext(ctx)
		span.SetAttributes(
			attribute.String("semanticrouter.level", "L1"),
			attribute.String("semanticrouter.route", bestRoute),
			attribute.Float64("semanticrouter.route_score", bestScore),
			attribute.String("semanticrouter.category", category),
			attribute.Bool("semanticrouter.bypassed_llm", true),
		)
		return ClassifyResult{
			Category:    category,
			BypassedLLM: true,
			Level:       "L1",
		}, nil
	}
}

// routeToCategory maps a route name to a classification category.
func routeToCategory(route string) string {
	switch route {
	case RouteJailbreak:
		return "REFUSE"
	case RouteSalutation:
		return "SALUTATION"
	case RouteFollowUp:
		return "COMPLEX"
	default:
		return "COMPLEX"
	}
}
