// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

// Package semanticmiddleware provides composable classification middleware
// for the semantic router. Each middleware processes a ClassifyContext and
// either makes a final classification decision or enriches the context and
// passes it to the next middleware in the chain.
//
// The chain is built once at router startup and executed per-request:
//
//	L0 (regex) → L1 (vector) → follow-up bypass → L2 (LLM)
//
// Each middleware can read and enrich the shared ClassifyContext, so
// downstream middlewares benefit from upstream signals (e.g. L1's
// near-miss route score is available to L2 as a routing hint).
package semanticmiddleware
