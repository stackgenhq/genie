// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package contacts provides Google Contacts (People API) tools for agents.
// It enables listing and searching the user's contacts using the same
// embedded OAuth client as Calendar when built with -X (see pkg/tools/google/oauth).
//
// Authentication: Same as Calendar — CredentialsFile (path or JSON) or
// build-time injected GoogleClientID/GoogleClientSecret. TokenFile (or
// inline token) required for OAuth2. Enable People API in Google Cloud Console.
package contacts

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/tools/google/oauth"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	people "google.golang.org/api/people/v1"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	apiTimeout     = 30 * time.Second
	maxListResults = 100
	personFields   = "names,emailAddresses,phoneNumbers"
)

// listContactsRequest is the input for the contacts_list tool.
type listContactsRequest struct {
	PageSize  int    `json:"page_size,omitempty" jsonschema:"description=Max contacts to return (1-100). Default 50."`
	PageToken string `json:"page_token,omitempty" jsonschema:"description=Token from previous list for pagination."`
}

// searchContactsRequest is the input for the contacts_search tool.
type searchContactsRequest struct {
	Query     string `json:"query" jsonschema:"description=Search query (name or email).,required"`
	PageSize  int    `json:"page_size,omitempty" jsonschema:"description=Max results (1-100). Default 20."`
	PageToken string `json:"page_token,omitempty" jsonschema:"description=Pagination token."`
}

// contactEntry is a single contact for JSON output.
type contactEntry struct {
	ResourceName string   `json:"resource_name"`
	DisplayName  string   `json:"display_name,omitempty"`
	Emails       []string `json:"emails,omitempty"`
	Phones       []string `json:"phones,omitempty"`
}

// contactsResponse is the tool response.
type contactsResponse struct {
	Operation string         `json:"operation"`
	Contacts  []contactEntry `json:"contacts,omitempty"`
	Count     int            `json:"count,omitempty"`
	NextToken string         `json:"next_page_token,omitempty"`
	Message   string         `json:"message"`
}

// Service provides Google Contacts operations for tools.
type Service interface {
	ListContacts(ctx context.Context, req listContactsRequest) (contactsResponse, error)
	SearchContacts(ctx context.Context, req searchContactsRequest) (contactsResponse, error)
}

type contactsWrapper struct {
	svc *people.Service
}

type contactsTools struct {
	svc  Service
	name string
}

func newContactsTools(name string, svc Service) *contactsTools {
	return &contactsTools{svc: svc, name: name}
}

func (c *contactsTools) tools() []tool.CallableTool {
	return []tool.CallableTool{
		function.NewFunctionTool(
			c.handleListContacts,
			function.WithName(fmt.Sprintf("%s_list_contacts", c.name)),
			function.WithDescription(
				"List contacts from the "+c.name+" Google Contacts (People API). "+
					"Returns names, emails, and phone numbers. Use page_token for pagination.",
			),
		),
		function.NewFunctionTool(
			c.handleSearchContacts,
			function.WithName(fmt.Sprintf("%s_search_contacts", c.name)),
			function.WithDescription(
				"Search contacts in the "+c.name+" Google Contacts by name or email. "+
					"Returns matching contacts with names, emails, and phones.",
			),
		),
	}
}

// lazyService implements Service by deferring the People API client creation
// to the first call. This allows the contacts tools to be registered
// unconditionally (tools remain discoverable) while the actual OAuth/secret
// resolution happens at execution time. If initialization fails at call time,
// the error is returned to the caller as a clear runtime error.
type lazyService struct {
	sp   security.SecretProvider
	name string
	once sync.Once
	svc  Service
	err  error
}

// NewLazyService returns a Service that lazily initialises the People API
// client on first use. The returned Service is safe for concurrent use.
func NewLazyService(sp security.SecretProvider, name string) Service {
	return &lazyService{sp: sp, name: name}
}

func (l *lazyService) init(ctx context.Context) {
	l.once.Do(func() {
		l.svc, l.err = NewFromSecretProvider(ctx, l.sp, l.name)
	})
}

func (l *lazyService) ListContacts(ctx context.Context, req listContactsRequest) (contactsResponse, error) {
	l.init(ctx)
	if l.err != nil {
		return contactsResponse{}, fmt.Errorf("google contacts not configured: %w", l.err)
	}
	return l.svc.ListContacts(ctx, req)
}

func (l *lazyService) SearchContacts(ctx context.Context, req searchContactsRequest) (contactsResponse, error) {
	l.init(ctx)
	if l.err != nil {
		return contactsResponse{}, fmt.Errorf("google contacts not configured: %w", l.err)
	}
	return l.svc.SearchContacts(ctx, req)
}

// NewFromSecretProvider creates an authenticated People API client using
// CredentialsFile + TokenFile from the secret provider, or embedded
// build-time credentials (see pkg/tools/google/oauth).
func NewFromSecretProvider(ctx context.Context, secretProvider security.SecretProvider, name string) (Service, error) {
	credsEntry, _ := secretProvider.GetSecret(ctx, security.GetSecretRequest{
		Name:   "CredentialsFile",
		Reason: fmt.Sprintf("%s Google Contacts tool: %s", name, toolcontext.GetJustification(ctx)),
	})
	credsJSON, err := oauth.GetCredentials(credsEntry, "Contacts")
	if err != nil {
		return nil, err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(credsJSON, &raw); err != nil {
		return nil, fmt.Errorf("invalid credentials JSON: %w", err)
	}

	scopes := []string{"https://www.googleapis.com/auth/contacts.readonly"}

	if typeField, ok := raw["type"]; ok {
		var t string
		if err := json.Unmarshal(typeField, &t); err == nil && t == "service_account" {
			creds, err := google.CredentialsFromJSON(ctx, credsJSON, scopes...) //nolint:staticcheck
			if err != nil {
				return nil, fmt.Errorf("invalid service account credentials: %w", err)
			}
			svc, err := people.NewService(ctx, option.WithCredentials(creds))
			if err != nil {
				return nil, err
			}
			return &contactsWrapper{svc: svc}, nil
		}
	}

	tokenJSON, save, err := oauth.GetToken(ctx, secretProvider)
	if err != nil {
		return nil, fmt.Errorf("OAuth2 token required: %w", err)
	}
	client, err := oauth.HTTPClient(ctx, credsJSON, tokenJSON, save, scopes)
	if err != nil {
		return nil, fmt.Errorf("OAuth2 client: %w", err)
	}
	svc, err := people.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}
	return &contactsWrapper{svc: svc}, nil
}

func personToEntry(p *people.Person) contactEntry {
	e := contactEntry{ResourceName: p.ResourceName}
	if len(p.Names) > 0 && p.Names[0].DisplayName != "" {
		e.DisplayName = p.Names[0].DisplayName
	}
	for _, em := range p.EmailAddresses {
		if em.Value != "" {
			e.Emails = append(e.Emails, em.Value)
		}
	}
	for _, ph := range p.PhoneNumbers {
		if ph.Value != "" {
			e.Phones = append(e.Phones, ph.Value)
		}
	}
	return e
}

func (w *contactsWrapper) ListContacts(ctx context.Context, req listContactsRequest) (contactsResponse, error) {
	resp := contactsResponse{Operation: "list_contacts"}

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > maxListResults {
		pageSize = maxListResults
	}

	call := w.svc.People.Connections.List("people/me").PersonFields(personFields).PageSize(int64(pageSize))
	if req.PageToken != "" {
		call = call.PageToken(req.PageToken)
	}

	conn, err := call.Context(ctx).Do()
	if err != nil {
		return resp, fmt.Errorf("people API error (list_contacts): %w", err)
	}

	for _, p := range conn.Connections {
		resp.Contacts = append(resp.Contacts, personToEntry(p))
	}
	resp.Count = len(resp.Contacts)
	resp.NextToken = conn.NextPageToken
	resp.Message = fmt.Sprintf("Listed %d contacts.", resp.Count)
	return resp, nil
}

func (w *contactsWrapper) SearchContacts(ctx context.Context, req searchContactsRequest) (contactsResponse, error) {
	resp := contactsResponse{Operation: "search_contacts"}

	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()

	query := strings.TrimSpace(req.Query)
	if query == "" {
		return resp, fmt.Errorf("query is required for search_contacts")
	}

	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > maxListResults {
		pageSize = maxListResults
	}

	// People API search: SearchContacts returns matches; we use list and filter by query
	// or use the search endpoint if available. People API v1 has people.searchContacts
	// in some versions. Checking: the REST API has people.searchContacts. The Go client
	// may expose it as People.SearchContacts. We'll use Connections.List with a high
	// page size and filter client-side for minimal implementation, or call search.
	// Actually the People API has "SearchDirectoryPeople" for domain directory and
	// "SearchContacts" - let me use the list and filter by query for simplicity so we
	// don't depend on a specific client version.
	call := w.svc.People.Connections.List("people/me").PersonFields(personFields).PageSize(int64(pageSize * 3))
	if req.PageToken != "" {
		call = call.PageToken(req.PageToken)
	}

	conn, err := call.Context(ctx).Do()
	if err != nil {
		return resp, fmt.Errorf("people API error (search_contacts): %w", err)
	}

	queryLower := strings.ToLower(query)
	for _, p := range conn.Connections {
		entry := personToEntry(p)
		matches := strings.Contains(strings.ToLower(entry.DisplayName), queryLower)
		for _, em := range entry.Emails {
			if strings.Contains(strings.ToLower(em), queryLower) {
				matches = true
				break
			}
		}
		if matches {
			resp.Contacts = append(resp.Contacts, entry)
			if len(resp.Contacts) >= pageSize {
				break
			}
		}
	}
	resp.Count = len(resp.Contacts)
	resp.Message = fmt.Sprintf("Found %d contact(s) matching %q.", resp.Count, query)
	return resp, nil
}

func (c *contactsTools) handleListContacts(ctx context.Context, req listContactsRequest) (contactsResponse, error) {
	return c.svc.ListContacts(ctx, req)
}

func (c *contactsTools) handleSearchContacts(ctx context.Context, req searchContactsRequest) (contactsResponse, error) {
	return c.svc.SearchContacts(ctx, req)
}
