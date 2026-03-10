// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package logger provides structured logging (slog) and context-aware logger
// retrieval for Genie.
//
// It solves the problem of having a single place to configure log level, output
// format (JSON or text), and to attach loggers to context so that downstream
// code can use backend.Logger.FromContext(ctx) without passing a logger
// explicitly. GetLogger(ctx) returns the request-scoped logger when available,
// falling back to the default. Without this package, logging would be ad-hoc
// and inconsistent across components.
package logger
