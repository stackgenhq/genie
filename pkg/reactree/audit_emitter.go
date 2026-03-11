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
	"time"

	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/hooks"
)

// AuditEventType constants for ReAcTree-specific audit events.
const (
	AuditEventIterationStart  audit.EventType = "reactree_iteration_start"
	AuditEventIterationEnd    audit.EventType = "reactree_iteration_end"
	AuditEventReflection      audit.EventType = "reactree_reflection"
	AuditEventDryRun          audit.EventType = "reactree_dry_run"
	AuditEventPlanExecution   audit.EventType = "reactree_plan_execution"
)

// AuditHook implements hooks.ExecutionHook by writing structured events to
// an audit.Auditor. This is the bridge between the generic hook system and
// the existing audit infrastructure.
type AuditHook struct {
	hooks.NoOpHook // embed no-op defaults
	auditor        audit.Auditor
}

// NewAuditHook creates an ExecutionHook that writes to the given auditor.
// Returns nil if auditor is nil.
func NewAuditHook(auditor audit.Auditor) *AuditHook {
	if auditor == nil {
		return nil
	}
	return &AuditHook{auditor: auditor}
}

// Ensure AuditHook satisfies the interface.
var _ hooks.ExecutionHook = (*AuditHook)(nil)

func (h *AuditHook) OnIterationStart(ctx context.Context, event hooks.IterationStartEvent) {
	h.auditor.Log(ctx, audit.LogRequest{
		EventType: AuditEventIterationStart,
		Actor:     "reactree",
		Action:    fmt.Sprintf("iteration_%d_start", event.Iteration),
		Metadata: map[string]any{
			"goal":           event.Goal,
			"iteration":      event.Iteration,
			"max_iterations": event.MaxIterations,
			"timestamp":      time.Now().UTC().Format(time.RFC3339),
		},
	})
}

func (h *AuditHook) OnIterationEnd(ctx context.Context, event hooks.IterationEndEvent) {
	h.auditor.Log(ctx, audit.LogRequest{
		EventType: AuditEventIterationEnd,
		Actor:     "reactree",
		Action:    fmt.Sprintf("iteration_%d_end", event.Iteration),
		Metadata: map[string]any{
			"iteration":        event.Iteration,
			"status":           event.Status,
			"tool_call_counts": event.ToolCallCounts,
			"task_completed":   event.TaskCompleted,
			"timestamp":        time.Now().UTC().Format(time.RFC3339),
		},
	})
}

func (h *AuditHook) OnReflection(ctx context.Context, event hooks.ReflectionEvent) {
	h.auditor.Log(ctx, audit.LogRequest{
		EventType: AuditEventReflection,
		Actor:     "reactree-reflector",
		Action:    "reflection_completed",
		Metadata: map[string]any{
			"iteration":      event.Iteration,
			"monologue":      event.Monologue,
			"should_proceed": event.ShouldProceed,
			"timestamp":      time.Now().UTC().Format(time.RFC3339),
		},
	})
}

func (h *AuditHook) OnDryRun(ctx context.Context, event hooks.DryRunEvent) {
	h.auditor.Log(ctx, audit.LogRequest{
		EventType: AuditEventDryRun,
		Actor:     "reactree-sentinel",
		Action:    "dry_run_completed",
		Metadata: map[string]any{
			"planned_steps":  event.PlannedSteps,
			"tools_used":     event.ToolsUsed,
			"estimated_cost": event.EstimatedCost,
			"timestamp":      time.Now().UTC().Format(time.RFC3339),
		},
	})
}

func (h *AuditHook) OnPlanExecution(ctx context.Context, event hooks.PlanExecutionEvent) {
	h.auditor.Log(ctx, audit.LogRequest{
		EventType: AuditEventPlanExecution,
		Actor:     "reactree-orchestrator",
		Action:    "plan_execution_start",
		Metadata: map[string]any{
			"flow":       event.Flow,
			"step_count": event.StepCount,
			"step_names": event.StepNames,
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
		},
	})
}
