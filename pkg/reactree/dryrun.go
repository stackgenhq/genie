// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package reactree

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// DryRunResult holds the simulated execution plan summary.
type DryRunResult struct {
	// PlannedSteps is the number of graph nodes that would execute.
	PlannedSteps int
	// ToolsUsed lists the tool names that would be invoked.
	ToolsUsed []string
	// EstimatedCost is a heuristic cost label (low/medium/high).
	EstimatedCost string
	// Summary is a human-readable description of the simulated plan.
	Summary string
}

// DryRunToolWrapper wraps a tool.Tool so that Call() records the invocation
// without executing any real side effects. It returns a mock response.
type DryRunToolWrapper struct {
	tool.Tool
	mu          sync.Mutex
	invocations []string
}

// NewDryRunToolWrapper creates a wrapper that intercepts calls for simulation.
func NewDryRunToolWrapper(t tool.Tool) *DryRunToolWrapper {
	return &DryRunToolWrapper{Tool: t}
}

// Call records the tool invocation and returns a mock success response
// without executing the underlying tool.
func (d *DryRunToolWrapper) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	name := d.Tool.Declaration().Name
	logger.GetLogger(ctx).Info("dry-run: simulated tool call", "tool", name)
	d.mu.Lock()
	d.invocations = append(d.invocations, name)
	d.mu.Unlock()
	return fmt.Sprintf(`{"dry_run": true, "tool": %q, "status": "simulated"}`, name), nil
}

// StreamableCall records the tool invocation and returns nil (no stream in dry run).
func (d *DryRunToolWrapper) StreamableCall(ctx context.Context, jsonArgs []byte) (*tool.StreamReader, error) {
	name := d.Tool.Declaration().Name
	logger.GetLogger(ctx).Info("dry-run: simulated streamable tool call", "tool", name)
	d.mu.Lock()
	d.invocations = append(d.invocations, name)
	d.mu.Unlock()
	return nil, fmt.Errorf("dry-run: streamable calls are not supported in simulation mode")
}

// Invocations returns a copy of the tool names that were "called" during simulation.
func (d *DryRunToolWrapper) Invocations() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, len(d.invocations))
	copy(out, d.invocations)
	return out
}

// WrapToolsForDryRun wraps all tools with DryRunToolWrapper for simulation.
// Returns the wrapped tools and a collector function to gather all invocations.
func WrapToolsForDryRun(tools []tool.Tool) ([]tool.Tool, func() []string) {
	wrappers := make([]*DryRunToolWrapper, len(tools))
	wrapped := make([]tool.Tool, len(tools))
	for i, t := range tools {
		w := NewDryRunToolWrapper(t)
		wrappers[i] = w
		wrapped[i] = w
	}

	collector := func() []string {
		var all []string
		seen := make(map[string]bool)
		for _, w := range wrappers {
			for _, name := range w.Invocations() {
				if !seen[name] {
					seen[name] = true
					all = append(all, name)
				}
			}
		}
		return all
	}

	return wrapped, collector
}

// BuildDryRunSummary creates a human-readable dry run report.
func BuildDryRunSummary(toolsUsed []string, iterationCount int) DryRunResult {
	cost := "low"
	if len(toolsUsed) > 5 || iterationCount > 3 {
		cost = "medium"
	}
	if len(toolsUsed) > 10 || iterationCount > 5 {
		cost = "high"
	}

	var sb strings.Builder
	sb.WriteString("## Dry Run Simulation Report\n\n")
	fmt.Fprintf(&sb, "**Planned Iterations:** %d\n", iterationCount)
	fmt.Fprintf(&sb, "**Estimated Cost:** %s\n", cost)
	fmt.Fprintf(&sb, "**Tools That Would Be Used:** %d\n\n", len(toolsUsed))

	if len(toolsUsed) > 0 {
		sb.WriteString("### Tool Invocations\n")
		for _, t := range toolsUsed {
			fmt.Fprintf(&sb, "- `%s`\n", t)
		}
	}

	return DryRunResult{
		PlannedSteps:  iterationCount,
		ToolsUsed:     toolsUsed,
		EstimatedCost: cost,
		Summary:       sb.String(),
	}
}
