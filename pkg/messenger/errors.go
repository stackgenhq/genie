// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package messenger

import "errors"

var (
	// ErrNotConnected is returned when Send or Receive is called before Connect.
	ErrNotConnected = errors.New("messenger: not connected")

	// ErrAlreadyConnected is returned when Connect is called on an already-connected messenger.
	ErrAlreadyConnected = errors.New("messenger: already connected")

	// ErrChannelNotFound is returned when the target channel does not exist or is inaccessible.
	ErrChannelNotFound = errors.New("messenger: channel not found")

	// ErrSendFailed is returned when a message could not be delivered.
	ErrSendFailed = errors.New("messenger: send failed")

	// ErrRateLimited is returned when the platform's rate limit has been exceeded.
	ErrRateLimited = errors.New("messenger: rate limited")
)
