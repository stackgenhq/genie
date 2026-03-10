// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package agui

import "context"

// HealthResult reports the health status of a background worker or subsystem.
// Used by cron scheduler health checks and heartbeat diagnostics.
type HealthResult struct {
	Healthy      bool   `json:"healthy"`
	LastError    string `json:"last_error,omitempty"`
	FailureCount int    `json:"failure_count"`
	Name         string `json:"name"`
}

//go:generate go tool counterfeiter -generate

// BGWorker is an interface for background workers that can be started and
// stopped. Implementations include the cron scheduler (pkg/cron).
//
//counterfeiter:generate . BGWorker
type BGWorker interface {
	Start(ctx context.Context) error
	HealthCheck(ctx context.Context) []HealthResult
	Stop() error
}
