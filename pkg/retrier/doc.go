// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package retrier provides configurable retry logic with backoff for operations
// that may transiently fail (e.g. network calls, MCP connections).
//
// It solves the problem of centralizing retry behavior: callers pass a function
// and options (attempts, backoff duration, retry-if predicate, on-retry hook).
// Do runs the function and retries on error according to the options. Without
// this package, each integration would implement its own retry loops and
// backoff logic inconsistently.
package retrier
