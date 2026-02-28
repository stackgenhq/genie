// Package oauth: token retrieval (file, inline, or device keychain) and
// OAuth2 HTTP client construction with token refresh and persist.
package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"os"

	"github.com/stackgenhq/genie/pkg/httputil"
	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/security/keyring"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GetToken returns the Google OAuth token JSON and a save callback. Token is
// resolved in order: TokenFile (path from secret provider), Token/Password
// (inline from secret provider), then device keychain. Save should be called
// after token refresh so the new token is persisted (to file or keyring).
func GetToken(ctx context.Context, sp security.SecretProvider) (tokenJSON []byte, save func([]byte) error, err error) {
	tokenFile, _ := sp.GetSecret(ctx, "TokenFile")
	if tokenFile != "" {
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return nil, nil, err
		}
		save := func(b []byte) error {
			return os.WriteFile(tokenFile, b, 0600)
		}
		return data, save, nil
	}

	tokenInline, _ := sp.GetSecret(ctx, "Token")
	if tokenInline == "" {
		tokenInline, _ = sp.GetSecret(ctx, "Password")
	}
	if tokenInline != "" {
		data := []byte(tokenInline)
		save := func(b []byte) error {
			return keyring.KeyringSet(keyring.AccountGoogleOAuthToken, b)
		}
		return data, save, nil
	}

	data, err := keyring.KeyringGet(keyring.AccountGoogleOAuthToken)
	if err == nil && len(data) > 0 {
		save := func(b []byte) error {
			return keyring.KeyringSet(keyring.AccountGoogleOAuthToken, b)
		}
		return data, save, nil
	}

	return nil, nil, errNoToken
}

// GetStoredUserInfo returns the Google user name and email stored after the OAuth
// browser flow (e.g. for salutation and /health). Empty strings are returned if
// no user info is in the keyring.
func GetStoredUserInfo() (name, email string) {
	data, err := keyring.KeyringGet(keyring.AccountGoogleOAuthUser)
	if err != nil || len(data) == 0 {
		return "", ""
	}
	var v struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if json.Unmarshal(data, &v) != nil {
		return "", ""
	}
	return v.Name, v.Email
}

// errNoToken is returned when no token is available (file, inline, or keyring).
var errNoToken = &noTokenError{}

type noTokenError struct{}

func (e *noTokenError) Error() string {
	return "Google OAuth token not found: set TokenFile or Token/Password in your integration, " +
		"or complete the OAuth flow and store the token (e.g. in device keychain)"
}

// HTTPClient builds an OAuth2-authenticated *http.Client for the given
// credentials JSON, token JSON, and scopes. SaveToken is called whenever the
// token is refreshed so it can be persisted (file or keyring).
func HTTPClient(ctx context.Context, credsJSON, tokenJSON []byte, saveToken func([]byte) error, scopes []string) (*http.Client, error) {
	config, err := google.ConfigFromJSON(credsJSON, scopes...)
	if err != nil {
		return nil, err
	}

	var tok oauth2.Token
	if err := json.Unmarshal(tokenJSON, &tok); err != nil {
		return nil, err
	}

	baseTS := config.TokenSource(ctx, &tok)
	savingTS := &savingTokenSource{base: baseTS, save: saveToken}
	// Use oauth2.Transport with Source: savingTS so the client gets refreshed tokens
	// and Base: httputil's round tripper for consistent TLS (NIST 2030).
	baseTransport := httputil.NewRoundTripper()
	client := &http.Client{Transport: &oauth2.Transport{Source: savingTS, Base: baseTransport}}
	return client, nil
}

// savingTokenSource wraps a TokenSource and persists the token only when it changes (e.g. after refresh).
type savingTokenSource struct {
	base       oauth2.TokenSource
	save       func([]byte) error
	lastAccess string
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := s.base.Token()
	if err != nil {
		return nil, err
	}
	if tok.AccessToken != s.lastAccess {
		s.lastAccess = tok.AccessToken
		if data, err := json.MarshalIndent(tok, "", "  "); err == nil {
			_ = s.save(data)
		}
	}
	return tok, nil
}
