package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"github.com/stackgenhq/genie/pkg/audit"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/halguard"
	"github.com/stackgenhq/genie/pkg/toolwrap"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// singleModelProvider adapts a single model.Model into the
// modelprovider.ModelProvider interface so that expert.ExpertBio.ToExpert can
// use a specific, pre-selected model rather than task-type-based routing.
// This is necessary because halguard selects diverse models for cross-model
// verification and needs to call each one individually.
type singleModelProvider struct {
	key   string
	model model.Model
}

// GetModel always returns the single wrapped model regardless of task type.
func (p *singleModelProvider) GetModel(_ context.Context, _ modelprovider.TaskType) (modelprovider.ModelMap, error) {
	return modelprovider.ModelMap{p.key: p.model}, nil
}

// newHalguardTextGenerator creates a halguard.TextGeneratorFunc that routes
// LLM calls through expert.Expert → trpc-agent-go runner → TraceChat.
// This gives proper Langfuse tracing (gen_ai.input.messages, gen_ai.output.messages,
// token usage) for free, eliminating the "N/A" input problem.
func newHalguardTextGenerator(auditor audit.Auditor, toolwrapSvc *toolwrap.Service) halguard.TextGeneratorFunc {
	return func(ctx context.Context, m model.Model, prompt string) (string, error) {
		modelName := ""
		if info := m.Info(); info.Name != "" {
			modelName = info.Name
		}

		mp := &singleModelProvider{key: modelName, model: m}

		bio := expert.ExpertBio{
			Name:        "halguard-" + modelName,
			Description: "Halguard verification model",
		}

		exp, err := bio.ToExpert(ctx, mp, auditor, toolwrapSvc)
		if err != nil {
			return "", fmt.Errorf("create halguard expert: %w", err)
		}

		resp, err := exp.Do(ctx, expert.Request{
			Message:  prompt,
			TaskType: modelprovider.TaskEfficiency,
			Mode: expert.ExpertConfig{
				MaxLLMCalls:       1,
				MaxToolIterations: 0,
			},
		})
		if err != nil {
			return "", fmt.Errorf("halguard generate: %w", err)
		}

		var sb strings.Builder
		for _, choice := range resp.Choices {
			if choice.Message.Content != "" {
				sb.WriteString(choice.Message.Content)
			}
		}
		return sb.String(), nil
	}
}
