// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	aguitypes "github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/credstore"
	"github.com/stackgenhq/genie/pkg/pii"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// DummyAuthTool is a synthetic tool registered when an MCP server fails to connect
// due to authentication errors (e.g. 401/404 during DCR or discovery).
// When the agent calls this tool, it invokes the credstore to trigger the OAuth
// flow, returning an AuthRequiredError to the agent, which presents the link
// to the user.
//
// The tool tracks whether it has already presented the auth URL in the current
// session. On the first call it returns the sign-in link as a successful result;
// on any subsequent call it returns an error so the LLM stops retrying.
type DummyAuthTool struct {
	serverName string
	store      credstore.Store
	called     atomic.Bool // true after the first auth-URL response
}

// NewDummyAuthTool creates a new dummy authentication tool for the given server.
func NewDummyAuthTool(serverName string, store credstore.Store) tool.CallableTool {
	return &DummyAuthTool{
		serverName: serverName,
		store:      store,
	}
}

// Name returns the namespaced tool name.
func (t *DummyAuthTool) Name() string {
	return strings.ToLower(t.serverName) + "_connect"
}

// Description returns the tool description.
func (t *DummyAuthTool) Description() string {
	return fmt.Sprintf("Connects and authenticates to the %s MCP server. You MUST call this tool to sign in before you can use any other %s tools.", t.serverName, t.serverName)
}

// Declaration returns the tool declaration.
func (t *DummyAuthTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        t.Name(),
		Description: t.Description(),
		InputSchema: &tool.Schema{
			Type:       "object",
			Properties: map[string]*tool.Schema{},
		},
	}
}

// Call triggers the OAuth flow by calling GetToken on the credstore.
//
// On the first call that requires authentication, a structured
// UserActionRequiredMsg is emitted to the AG-UI chat UI (rendered as a native
// sign-in card), and the sign-in URL is also returned as text so the LLM can
// relay it. Any subsequent call (before the user completes sign-in) returns
// an error to prevent the LLM from looping.
func (t *DummyAuthTool) Call(ctx context.Context, _ []byte) (any, error) {
	_, err := t.store.GetToken(ctx)
	if err != nil {
		authURL := credstore.GetAuthURL(err)
		if authURL != "" {
			// If we already showed the auth URL, return an error so the LLM
			// stops retrying. The loop detection middleware will then cancel
			// the sub-agent context.
			if !t.called.CompareAndSwap(false, true) {
				return nil, fmt.Errorf(
					"authentication is already in progress for %s. "+
						"The sign-in link has already been provided to the user. "+
						"Do NOT call this tool again. "+
						"Relay the sign-in message you already received and stop",
					t.serverName,
				)
			}

			// Emit a structured event so the chat UI renders a native
			// sign-in card with a button (instead of an embedded markdown link).
			aguitypes.Emit(ctx, aguitypes.UserActionRequiredMsg{
				Action:  "oauth_login",
				Service: t.serverName,
				URL:     authURL,
				Message: fmt.Sprintf("Sign in to %s to access its tools and services.", t.serverName),
			})

			return fmt.Sprintf(
				"%s🔐 **Authentication required for %s**\n\n"+
					"A sign-in card has been shown to the user in the chat UI.\n\n"+
					"IMPORTANT: Do NOT call this tool again. "+
					"The user must complete sign-in in their browser first. "+
					"Tell the user to click the Sign In button and stop.",
				pii.SkipRedactionMarker, t.serverName,
			), nil
		}
		// For non-auth errors, return as error.
		return nil, err
	}
	// If GetToken succeeds without error, the user is already authenticated.
	// Reset the called flag so it can be used again if needed.
	t.called.Store(false)
	return fmt.Sprintf("Successfully authenticated to %s. The tools should now be available.", t.serverName), nil
}
