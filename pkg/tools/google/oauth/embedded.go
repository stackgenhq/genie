// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package oauth provides build-time injected OAuth client credentials
// shared by Google API tools (Calendar, Contacts). Both use the same client
// ID and secret so one OAuth consent can grant Calendar + Contacts.
//
// Inject at build time via -X (do not commit to repo):
//
//	go build -ldflags "-X github.com/stackgenhq/genie/pkg/tools/google/oauth.GoogleClientID=ID -X github.com/stackgenhq/genie/pkg/tools/google/oauth.GoogleClientSecret=SECRET" ...
//
// Or set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET in CI (e.g. GitHub Actions).
package oauth

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/stackgenhq/genie/pkg/security/keyring"
)

// GoogleClientID is the OAuth 2.0 client ID for the Genie "installed app"
// client. Injected at build time via -X; empty means tools fall back to
// CredentialsFile from config or GOOGLE_CLIENT_ID env at runtime.
var GoogleClientID = ""

// GoogleClientSecret is the OAuth 2.0 client secret. Injected at build time via -X.
var GoogleClientSecret = ""

// getClientCredentials returns client ID and secret, with runtime env fallback
// when build-time vars are empty (so "genie setup" can open the browser if the
// user sets GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET in the environment).
func getClientCredentials() (clientID, clientSecret string) {
	clientID = strings.TrimSpace(GoogleClientID)
	if clientID == "" {
		clientID = strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID"))
	}
	clientSecret = strings.TrimSpace(GoogleClientSecret)
	if clientSecret == "" {
		clientSecret = strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET"))
	}
	return clientID, clientSecret
}

// EmbeddedCredentialsJSON returns a minimal "installed" credentials JSON
// when both client ID and secret are available (build-time -X or env
// GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET). Returns nil if either is empty.
func EmbeddedCredentialsJSON() []byte {
	clientID, clientSecret := getClientCredentials()
	if clientID == "" || clientSecret == "" {
		return nil
	}
	installed := map[string]interface{}{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"redirect_uris": []string{"http://localhost", "http://localhost:" + RedirectPort},
		"auth_uri":      "https://accounts.google.com/o/oauth2/auth",
		"token_uri":     "https://oauth2.googleapis.com/token",
	}
	out := map[string]interface{}{"installed": installed}
	b, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return b
}

// GetCredentials returns the user-provided credentials JSON (credsEntry), or
// embedded build-time credentials, or credentials stored in the keyring after
// a successful "genie setup" Google sign-in. Returns an error only when no
// credentials are available. ServiceName is used in the error message
// (e.g. "Calendar", "Contacts").
func GetCredentials(credsEntry, serviceName string) ([]byte, error) {
	if credsEntry != "" {
		return []byte(credsEntry), nil
	}
	embedded := EmbeddedCredentialsJSON()
	if len(embedded) > 0 {
		return embedded, nil
	}
	// Use credentials persisted to keyring by RunBrowserFlow so Calendar/Gmail work without env at runtime.
	stored, err := keyring.KeyringGet(keyring.AccountGoogleOAuthCredentials)
	if err == nil && len(stored) > 0 {
		return stored, nil
	}
	return nil, fmt.Errorf(
		"google %s not configured: set CredentialsFile (path or JSON) in your integration, "+
			"or build with -X to inject GoogleClientID and GoogleClientSecret",
		serviceName,
	)
}
