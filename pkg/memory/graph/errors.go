// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package graph

import "errors"

// ErrInvalidInput is returned when a tool request is missing required fields.
var ErrInvalidInput = errors.New("invalid input: required fields missing")
