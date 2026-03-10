// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package dedup provides a generic type-safe wrapper around
// golang.org/x/sync/singleflight. When multiple goroutines call Do with the
// same key concurrently, only the first caller executes the function —
// subsequent callers block until the first completes and receive the same result.
//
// This is useful for preventing duplicate parallel tool calls from LLMs
// that sometimes emit the same function call multiple times in one response.
package dedup

import "golang.org/x/sync/singleflight"

// Group is a generic singleflight group parameterized by the result type T.
// Zero value is ready to use.
type Group[T any] struct {
	g singleflight.Group
}

// Do executes fn exactly once for a given key, even if called concurrently
// from multiple goroutines.
//
// The first caller with a given key runs fn. All subsequent callers with
// the same key block until fn completes, then receive the same (val, err).
//
// The third return value "shared" is true when the result was produced by
// an earlier call (i.e. this caller was a duplicate).
//
// After fn completes, the key is removed so future calls can execute again.
func (d *Group[T]) Do(key string, fn func() (T, error)) (val T, err error, shared bool) {
	v, err, shared := d.g.Do(key, func() (interface{}, error) {
		return fn()
	})
	if v != nil {
		val = v.(T)
	}
	return val, err, shared
}
