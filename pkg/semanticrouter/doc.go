// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//
// Change Date: 2029-03-10
// Change License: Apache License, Version 2.0

// Package semanticrouter provides fast, embedding-based intent routing,
// jailbreak detection, and response semantic caching for Genie.
//
// By sitting in front of the main orchestration flow, the semantic router
// acts as a lightweight gatekeeper. It quickly embeds user requests
// using a pre-configured vector embedding model (e.g. OpenAI, Gemini)
// and matches the resulting vector against a set of predefined routes
// (e.g. "jailbreak", "salutation").
//
// If a route match is found (i.e. above the configured Threshold),
// the router immediately returns a ClassificationResult (e.g. REFUSE
// or SALUTATION) without invoking the more expensive, higher-latency
// front-desk LLM.
//
// Further, it manages Semantic Caching which allows repeated or highly
// similar queries to immediately bypass generation entirely, fetching
// previously generated results from the Vector Store.
package semanticrouter
