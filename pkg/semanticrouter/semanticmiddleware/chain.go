// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package semanticmiddleware

import "context"

// ClassifyContext carries accumulated routing context through the middleware chain.
// Each middleware may read and enrich this context before deciding or passing downstream.
type ClassifyContext struct {
	// Input fields (set by the caller).

	// Question is the user's input message to classify.
	Question string

	// Resume is the agent's capability summary. May be empty on first call.
	Resume string

	// Enrichment fields (set by middlewares for downstream consumption).

	// ClosestRoute is the best L1 route match, even if below threshold.
	// Downstream middlewares (e.g. L2 LLM) can use this signal to bias
	// their decision when the score is close but not conclusive.
	ClosestRoute string

	// RouteScore is the similarity score of the ClosestRoute match.
	RouteScore float64

	// IsFollowUp is true when L0 regex detected a conversational follow-up
	// pattern (e.g. "try again", "but I wanted"). Downstream middlewares
	// can use this to fast-track COMPLEX classification.
	IsFollowUp bool
}

// ClassifyFunc is the signature for the next handler in the chain.
type ClassifyFunc func(ctx context.Context, cc *ClassifyContext) (ClassifyResult, error)

// Middleware processes a classification request. It receives the
// accumulated context and a next function to call the next middleware.
// If the middleware makes a final decision, it returns directly.
// Otherwise it enriches cc and calls next(ctx, cc) to continue the chain.
type Middleware func(ctx context.Context, cc *ClassifyContext, next ClassifyFunc) (ClassifyResult, error)

// ClassifyResult carries the classification decision made by a middleware.
type ClassifyResult struct {
	// Category is the classification decision (REFUSE, SALUTATION, OUT_OF_SCOPE, COMPLEX).
	Category string

	// Reason is non-empty only for OUT_OF_SCOPE.
	Reason string

	// BypassedLLM is true if classification was decided without an LLM call.
	BypassedLLM bool

	// Level indicates which middleware tier or internal handler made the decision
	// (e.g. "L0", "L1", "L2", "terminal", "follow_up_bypass").
	Level string
}

// BuildChain composes a list of middlewares into a single ClassifyFunc.
// The last middleware in the chain receives a terminal next that returns
// COMPLEX (the safest default when no middleware has decided).
func BuildChain(middlewares ...Middleware) ClassifyFunc {
	terminal := func(_ context.Context, _ *ClassifyContext) (ClassifyResult, error) {
		return ClassifyResult{Category: "COMPLEX", Level: "terminal"}, nil
	}

	// Build from right to left so the first middleware in the list runs first.
	chain := terminal
	for i := len(middlewares) - 1; i >= 0; i-- {
		mw := middlewares[i]
		next := chain
		chain = func(ctx context.Context, cc *ClassifyContext) (ClassifyResult, error) {
			return mw(ctx, cc, next)
		}
	}
	return chain
}
