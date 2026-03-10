// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package llmutil provides stateless utility functions for working with LLM
// response structures (choices, deltas, streaming events).
//
// It exists to centralise common extraction patterns that would otherwise be
// duplicated across orchestrator, reactree, and other call-sites that consume
// expert.Response / model.Choice slices. Without this package, each consumer
// would implement its own scanning logic and diverge over time.
package llmutil

import "trpc.group/trpc-go/trpc-agent-go/model"

// ExtractChoiceContent scans a slice of model.Choice in reverse order and
// returns the first non-empty content string it finds.
//
// In streaming mode, Expert.Do accumulates individual streaming delta events
// as separate choices. The final accumulated message is typically the last
// choice with non-empty Message.Content. Scanning from the end finds it
// efficiently.
//
// Fallback: if no Message.Content is found, Delta.Content is checked for
// streaming deltas.
//
// Returns an empty string when every choice has empty content.
func ExtractChoiceContent(choices []model.Choice) string {
	for i := len(choices) - 1; i >= 0; i-- {
		if choices[i].Message.Content != "" {
			return choices[i].Message.Content
		}
		// Fallback: check Delta.Content for streaming deltas
		if choices[i].Delta.Content != "" {
			return choices[i].Delta.Content
		}
	}
	return ""
}
