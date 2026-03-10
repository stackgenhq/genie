// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package expert provides the LLM "expert" abstraction used by Genie's
// orchestrator and ReAcTree execution engine.
//
// It solves the problem of wrapping trpc-agent-go's runner and tool execution
// behind a single interface (Expert) with a named persona (ExpertBio), so that
// different agents (e.g. front-desk classifier, codeowner, report writer) can
// be invoked with the same pattern. Each expert has its own tools, model config,
// and session; the orchestrator routes user requests to the appropriate expert
// and coordinates multi-step workflows (e.g. ReAcTree stages) via Expert.Run.
package expert
