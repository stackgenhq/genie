// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

/*
Copyright © 2026 StackGen, Inc.
*/

// Package oauth: browser_flow runs the OAuth2 authorization code flow by
// opening the system browser and running a local redirect server to capture
// the code, then exchanging it for a token and storing it in the device keyring.
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"time"

	"github.com/stackgenhq/genie/pkg/httputil"
	"github.com/stackgenhq/genie/pkg/security/keyring"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// RedirectPort is the local port used for the OAuth callback. The Google Cloud
// Console OAuth client must list http://localhost:8765 as an authorized redirect URI.
const RedirectPort = "8765"

// DefaultGenieAGUIPort is the default port for the Genie AG-UI server.
// Must match messenger.DefaultAGUIPort; we cannot import messenger here (messenger/agui imports this package).
// See browser_flow_test.go for an assertion that keeps them in sync.
const DefaultGenieAGUIPort = 9876

// Scopes for Calendar, Contacts, Gmail, Drive, Tasks, Chat, Custom Search, and user profile (one sign-in powers all).
var defaultBrowserFlowScopes = []string{
	"https://www.googleapis.com/auth/calendar",
	"https://www.googleapis.com/auth/chat.messages",
	"https://www.googleapis.com/auth/contacts.readonly",
	"https://www.googleapis.com/auth/drive.readonly",
	"https://www.googleapis.com/auth/gmail.readonly",
	"https://www.googleapis.com/auth/gmail.send",
	"https://www.googleapis.com/auth/tasks",
	"https://www.googleapis.com/auth/cse", // Custom Search JSON API (cse.list)
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
}

// RunBrowserFlow runs the OAuth2 authorization code flow: starts a local server
// on RedirectPort, opens the system browser to the consent URL, waits for the
// redirect with the code, exchanges it for a token, and stores the token in
// the device keyring. credsJSON must be the same format as EmbeddedCredentialsJSON
// or a full credentials file; the redirect URI used is http://localhost:8765.
// Returns an error if credentials are missing, the user denies consent, or the
// keyring cannot be written.
func RunBrowserFlow(ctx context.Context, credsJSON []byte) error {
	if len(credsJSON) == 0 {
		return fmt.Errorf("google OAuth credentials not available: build with GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET, or set CredentialsFile")
	}
	config, err := google.ConfigFromJSON(credsJSON, defaultBrowserFlowScopes...)
	if err != nil {
		return fmt.Errorf("invalid Google OAuth credentials: %w", err)
	}
	config.RedirectURL = "http://localhost:" + RedirectPort

	state := fmt.Sprintf("genie-%d", time.Now().UnixNano())
	authURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)

	fmt.Println("Opening your browser to sign in with Google...")
	doneCh := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "invalid state", http.StatusBadRequest)
			doneCh <- fmt.Errorf("invalid OAuth state")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			doneCh <- fmt.Errorf("authorization was denied or no code returned")
			return
		}
		tok, err := config.Exchange(r.Context(), code)
		if err != nil {
			http.Error(w, "token exchange failed", http.StatusInternalServerError)
			doneCh <- fmt.Errorf("token exchange failed: %w", err)
			return
		}
		tokenJSON, err := json.Marshal(tok)
		if err != nil {
			http.Error(w, "failed to save token", http.StatusInternalServerError)
			doneCh <- fmt.Errorf("marshal token: %w", err)
			return
		}
		if err := keyring.KeyringSet(keyring.AccountGoogleOAuthToken, tokenJSON); err != nil {
			http.Error(w, "failed to save token", http.StatusInternalServerError)
			doneCh <- fmt.Errorf("could not save token to keychain: %w", err)
			return
		}
		// Persist client credentials so Calendar/Gmail work at runtime without GOOGLE_CLIENT_ID/SECRET env.
		if err := keyring.KeyringSet(keyring.AccountGoogleOAuthCredentials, credsJSON); err != nil {
			http.Error(w, "failed to save credentials", http.StatusInternalServerError)
			doneCh <- fmt.Errorf("could not save credentials to keychain: %w", err)
			return
		}
		name, email := fetchUserInfo(tok)
		if name != "" || email != "" {
			userJSON, _ := json.Marshal(map[string]string{"name": name, "email": email})
			_ = keyring.KeyringSet(keyring.AccountGoogleOAuthUser, userJSON)
		}
		salutation := "You're all set"
		if name != "" {
			salutation = "Hello, " + html.EscapeString(name) + "! You're all set."
		}
		// One-click connect: chat.html?url=GENIE_URL auto-connects to that Genie instance (see docs/chat.html).
		genieURL := fmt.Sprintf("http://127.0.0.1:%d", DefaultGenieAGUIPort)
		const chatBase = "https://stackgenhq.github.io/genie"
		chatPageURL := fmt.Sprintf("%s/chat.html?url=%s", chatBase, url.QueryEscape(genieURL))
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<html><body style="font-family: system-ui, sans-serif; max-width: 480px; margin: 2rem auto; padding: 1rem; line-height: 1.5;">
<h2 style="color: #0a0;">✓ %s</h2>
<p>You can close this tab and return to the terminal.</p>
<p><a href="%s" style="color: #06c; font-weight: 600;">Open Genie Chat</a> to start chatting with Genie (connects to your local Genie on port %d).</p>
<p><strong>Your privacy:</strong> Your data stays on your machine. Genie only uses your Google account with your permission, and we always ask before sending anything to a third party.</p>
<p><strong>StackGen does not have access to your data.</strong> When you use Genie, your calendar, contacts, and email are used only by the assistant running on your system—not by StackGen or any remote service.</p>
</body></html>`, salutation, chatPageURL, DefaultGenieAGUIPort)
		doneCh <- nil
	})

	srv := &http.Server{Addr: "localhost:" + RedirectPort, Handler: mux}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("could not start local callback server (is port "+RedirectPort+" in use?): %w", err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		_ = srv.Serve(ln)
	}()
	defer func() { _ = srv.Shutdown(context.Background()) }()

	if err := openBrowser(authURL); err != nil {
		return fmt.Errorf("could not open browser: %w", err)
	}

	select {
	case err := <-doneCh:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		return fmt.Errorf("sign-in cancelled or timed out: %w", ctx.Err())
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("sign-in timed out after 5 minutes")
	}
	return nil
}

// fetchUserInfo calls Google's userinfo endpoint and returns the user's name and email.
// Empty strings are returned on any error or missing fields.
func fetchUserInfo(tok *oauth2.Token) (name, email string) {
	req, err := http.NewRequest(http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := httputil.GetClient().Do(req)
	if err != nil {
		return "", ""
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", ""
	}
	var v struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if json.NewDecoder(resp.Body).Decode(&v) != nil {
		return "", ""
	}
	return v.Name, v.Email
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "linux":
		for _, c := range []string{"xdg-open", "gnome-open", "kde-open"} {
			if err := exec.Command(c, url).Start(); err == nil {
				return nil
			}
		}
		return fmt.Errorf("please open this URL in your browser: %s", url)
	default:
		return fmt.Errorf("please open this URL in your browser: %s", url)
	}
}

// CanRunBrowserFlow reports whether the OAuth browser flow can run (embedded
// credentials available). When false, setup can still record "connect Google"
// and tell the user to run genie grant later.
func CanRunBrowserFlow() bool {
	creds := EmbeddedCredentialsJSON()
	return len(creds) > 0
}
