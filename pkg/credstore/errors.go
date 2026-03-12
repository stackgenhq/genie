// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package credstore

import (
	"errors"
	"fmt"
)

// ErrNoToken is returned when no token is available and no OAuth flow
// is configured to obtain one.
var ErrNoToken = errors.New("no token available")

// AuthRequiredError signals that the user must complete an OAuth flow
// to obtain a token. The AuthURL should be sent to the user via chat
// so they can click it and authenticate.
type AuthRequiredError struct {
	// AuthURL is the authorization URL the user should visit.
	AuthURL string
	// ServiceName is the name of the service that requires auth.
	ServiceName string
}

// Error implements the error interface.
func (e *AuthRequiredError) Error() string {
	return fmt.Sprintf("authentication required for %s: please sign in at %s", e.ServiceName, e.AuthURL)
}

// IsAuthRequiredError checks if an error is an *AuthRequiredError.
func IsAuthRequiredError(err error) bool {
	var target *AuthRequiredError
	return errors.As(err, &target)
}

// GetAuthURL extracts the AuthURL from an AuthRequiredError, or returns
// an empty string if the error is not an AuthRequiredError.
func GetAuthURL(err error) string {
	var target *AuthRequiredError
	if errors.As(err, &target) {
		return target.AuthURL
	}
	return ""
}
