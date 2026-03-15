// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package learning

import "context"

//counterfeiter:generate . ILearner

// ILearner is the interface for the skill distillation pipeline.
// The orchestrator depends on this interface so background learning
// can be faked in unit tests.
type ILearner interface {
	Learn(ctx context.Context, req LearnRequest) error
}
