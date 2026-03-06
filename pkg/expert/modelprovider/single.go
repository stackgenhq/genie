package modelprovider

import (
	"context"

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
func (p *singleModelProvider) GetModel(_ context.Context, _ TaskType) (ModelMap, error) {
	return ModelMap{p.key: p.model}, nil
}

func NewSingleModelProvider(key string, model model.Model) ModelProvider {
	return &singleModelProvider{key: key, model: model}
}
