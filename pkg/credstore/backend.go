// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package credstore

import "context"

// Backend is the persistence layer for per-user tokens. Implementations
// store and retrieve tokens keyed by (userID, serviceName).
//
// The package ships with two backends:
//   - MemoryBackend: in-memory (tests, single-process deployments)
//   - KeyringBackend: system keychain (local, single-user Genie)
//
// Custom backends (Redis, Qdrant metadata, Postgres) can be plugged in
// for multi-user remote deployments.
//
//counterfeiter:generate . Backend
type Backend interface {
	// Get returns the stored token for the given user and service.
	// Returns ErrNoToken if no token is stored.
	Get(ctx context.Context, req BackendGetRequest) (*Token, error)

	// Put stores a token for the given user and service.
	Put(ctx context.Context, req BackendPutRequest) error

	// Delete removes the token for the given user and service.
	Delete(ctx context.Context, req BackendDeleteRequest) error
}

// BackendGetRequest is the request for Backend.Get.
type BackendGetRequest struct {
	UserID      string
	ServiceName string
}

// BackendPutRequest is the request for Backend.Put.
type BackendPutRequest struct {
	UserID      string
	ServiceName string
	Token       *Token
}

// BackendDeleteRequest is the request for Backend.Delete.
type BackendDeleteRequest struct {
	UserID      string
	ServiceName string
}
