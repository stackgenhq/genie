// Package gmail provides Gmail API tools for agents. It enables list, get, and
// send operations using the Gmail API with the shared Google OAuth token
// (Calendar, Contacts, Drive, Gmail). One sign-in can power all Google tools.
//
// Available tools (prefixed with google_gmail_ when registered):
//   - google_gmail_list_messages — list messages with optional query
//   - google_gmail_get_message — get a single message by ID
//   - google_gmail_send — send an email
//
// Authentication: Same as other pkg/tools/google packages — TokenFile,
// Token/Password, or device keyring; CredentialsFile for OAuth client config.
package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/stackgenhq/genie/pkg/security"
	"github.com/stackgenhq/genie/pkg/tools/google/oauth"
	"github.com/stackgenhq/genie/pkg/toolwrap/toolcontext"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const maxListResults = 50

// Gmail API scopes: read + send.
var gmailScopes = []string{
	"https://www.googleapis.com/auth/gmail.readonly",
	"https://www.googleapis.com/auth/gmail.send",
}

// Service provides Gmail operations for tools.
type Service interface {
	ListMessages(ctx context.Context, query string, maxResults int) ([]*MessageSummary, error)
	GetMessage(ctx context.Context, id string) (*MessageDetail, error)
	Send(ctx context.Context, to []string, subject, body string) error
	Validate(ctx context.Context) error
}

// MessageSummary is a lightweight message entry for list results.
type MessageSummary struct {
	ID       string `json:"id"`
	ThreadID string `json:"thread_id"`
	Subject  string `json:"subject,omitempty"`
	From     string `json:"from,omitempty"`
	Date     string `json:"date,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
}

// MessageDetail is a full message for get_message.
type MessageDetail struct {
	ID          string   `json:"id"`
	ThreadID    string   `json:"thread_id"`
	Subject     string   `json:"subject"`
	From        string   `json:"from"`
	To          []string `json:"to"`
	Date        string   `json:"date"`
	BodyPlain   string   `json:"body_plain"`
	BodyHTML    string   `json:"body_html,omitempty"`
	Snippet     string   `json:"snippet"`
	Attachments []string `json:"attachments,omitempty"`
}

type gmailWrapper struct {
	svc *gmail.Service
}

// NewFromSecretProvider creates a Gmail Service using the shared Google OAuth
// token (TokenFile, Token/Password, or device keyring). One sign-in can power
// Calendar, Contacts, Drive, and Gmail.
func NewFromSecretProvider(ctx context.Context, sp security.SecretProvider) (Service, error) {
	credsEntry, _ := sp.GetSecret(ctx, security.GetSecretRequest{
		Name:   "CredentialsFile",
		Reason: fmt.Sprintf("Google Gmail tool: %s", toolcontext.GetJustification(ctx)),
	})
	credsJSON, err := oauth.GetCredentials(credsEntry, "Gmail")
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(credsJSON, &raw); err != nil {
		return nil, fmt.Errorf("gmail: invalid credentials JSON: %w", err)
	}
	if typeField, ok := raw["type"]; ok {
		var t string
		if err := json.Unmarshal(typeField, &t); err == nil && t == "service_account" {
			creds, err := google.CredentialsFromJSON(ctx, credsJSON, gmailScopes...) //nolint:staticcheck
			if err != nil {
				return nil, fmt.Errorf("gmail: invalid service account credentials: %w", err)
			}
			svc, err := gmail.NewService(ctx, option.WithCredentials(creds))
			if err != nil {
				return nil, fmt.Errorf("gmail: failed to create Gmail service: %w", err)
			}
			return &gmailWrapper{svc: svc}, nil
		}
	}
	tokenJSON, save, err := oauth.GetToken(ctx, sp)
	if err != nil {
		return nil, err
	}
	client, err := oauth.HTTPClient(ctx, credsJSON, tokenJSON, save, gmailScopes)
	if err != nil {
		return nil, err
	}
	svc, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("gmail: failed to create Gmail service: %w", err)
	}
	return &gmailWrapper{svc: svc}, nil
}

func (w *gmailWrapper) ListMessages(ctx context.Context, query string, maxResults int) ([]*MessageSummary, error) {
	if maxResults <= 0 || maxResults > maxListResults {
		maxResults = maxListResults
	}
	call := w.svc.Users.Messages.List("me").Context(ctx).MaxResults(int64(maxResults))
	if query != "" {
		call = call.Q(query)
	}
	resp, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("gmail list messages: %w", err)
	}
	if len(resp.Messages) == 0 {
		return nil, nil
	}
	summaries := make([]*MessageSummary, 0, len(resp.Messages))
	for _, m := range resp.Messages {
		meta, err := w.svc.Users.Messages.Get("me", m.Id).Context(ctx).Format("metadata").MetadataHeaders("Subject", "From", "Date").Do()
		if err != nil {
			continue
		}
		s := &MessageSummary{ID: m.Id, ThreadID: m.ThreadId, Snippet: meta.Snippet}
		for _, h := range meta.Payload.Headers {
			switch h.Name {
			case "Subject":
				s.Subject = h.Value
			case "From":
				s.From = h.Value
			case "Date":
				s.Date = h.Value
			}
		}
		summaries = append(summaries, s)
	}
	return summaries, nil
}

func (w *gmailWrapper) GetMessage(ctx context.Context, id string) (detail *MessageDetail, err error) {
	msg, err := w.svc.Users.Messages.Get("me", id).Context(ctx).Format("full").Do()
	if err != nil {
		return nil, fmt.Errorf("gmail get message: %w", err)
	}
	detail = &MessageDetail{ID: msg.Id, ThreadID: msg.ThreadId, Snippet: msg.Snippet}
	for _, h := range msg.Payload.Headers {
		switch h.Name {
		case "Subject":
			detail.Subject = h.Value
		case "From":
			detail.From = h.Value
		case "To":
			detail.To = strings.Split(strings.ReplaceAll(h.Value, " ", ""), ",")
		case "Date":
			detail.Date = h.Value
		}
	}
	if msg.Payload.Body != nil && msg.Payload.Body.Data != "" {
		dec, err := base64.URLEncoding.DecodeString(msg.Payload.Body.Data)
		if err == nil {
			detail.BodyPlain = string(dec)
		}
	}
	if msg.Payload.Parts != nil {
		for _, p := range msg.Payload.Parts {
			if p.Filename != "" {
				detail.Attachments = append(detail.Attachments, p.Filename)
			}
			if p.MimeType == "text/plain" && p.Body != nil && p.Body.Data != "" {
				dec, err := base64.URLEncoding.DecodeString(p.Body.Data)
				if err == nil && detail.BodyPlain == "" {
					detail.BodyPlain = string(dec)
				}
			}
			if p.MimeType == "text/html" && p.Body != nil && p.Body.Data != "" {
				dec, err := base64.URLEncoding.DecodeString(p.Body.Data)
				if err == nil {
					detail.BodyHTML = string(dec)
				}
			}
		}
	}
	return detail, nil
}

func (w *gmailWrapper) Send(ctx context.Context, to []string, subject, body string) error {
	raw := fmt.Sprintf("To: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		strings.Join(to, ", "), subject, body)
	msg := &gmail.Message{
		Raw: base64.URLEncoding.EncodeToString([]byte(raw)),
	}
	_, err := w.svc.Users.Messages.Send("me", msg).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("gmail send: %w", err)
	}
	return nil
}

func (w *gmailWrapper) Validate(ctx context.Context) error {
	_, err := w.svc.Users.GetProfile("me").Context(ctx).Do()
	return err
}

// ─── Tool request structs ────────────────────────────────────────────────

type listMessagesRequest struct {
	Query      string `json:"query,omitempty" jsonschema:"description=Gmail search query (e.g. is:unread, from:user@example.com)."`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"description=Max messages to return (default 50, max 50)."`
}

type getMessageRequest struct {
	MessageID string `json:"message_id" jsonschema:"description=Gmail message ID from list_messages.,required"`
}

type sendMessageRequest struct {
	To      []string `json:"to" jsonschema:"description=Recipient email addresses.,required"`
	Subject string   `json:"subject" jsonschema:"description=Email subject.,required"`
	Body    string   `json:"body" jsonschema:"description=Plain text body.,required"`
}

// ─── Tool provider (name-prefixed) ──────────────────────────────────────────

type gmailTools struct {
	name string
	svc  Service
}

func newGmailTools(name string, svc Service) *gmailTools {
	return &gmailTools{name: name, svc: svc}
}

func (c *gmailTools) tools() []tool.CallableTool {
	return []tool.CallableTool{
		function.NewFunctionTool(
			c.handleListMessages,
			function.WithName(c.name+"_list_messages"),
			function.WithDescription("List Gmail messages with optional search query (e.g. is:unread, from:user)."),
		),
		function.NewFunctionTool(
			c.handleGetMessage,
			function.WithName(c.name+"_get_message"),
			function.WithDescription("Get a single Gmail message by ID (from list_messages)."),
		),
		function.NewFunctionTool(
			c.handleSend,
			function.WithName(c.name+"_send"),
			function.WithDescription("Send an email via Gmail."),
		),
	}
}

func (c *gmailTools) handleListMessages(ctx context.Context, req listMessagesRequest) ([]*MessageSummary, error) {
	return c.svc.ListMessages(ctx, req.Query, req.MaxResults)
}

func (c *gmailTools) handleGetMessage(ctx context.Context, req getMessageRequest) (*MessageDetail, error) {
	return c.svc.GetMessage(ctx, req.MessageID)
}

func (c *gmailTools) handleSend(ctx context.Context, req sendMessageRequest) (string, error) {
	if len(req.To) == 0 || req.Subject == "" || req.Body == "" {
		return "", fmt.Errorf("to, subject, and body are required")
	}
	if err := c.svc.Send(ctx, req.To, req.Subject, req.Body); err != nil {
		return "", err
	}
	return fmt.Sprintf("Sent email to %s with subject %q.", strings.Join(req.To, ", "), req.Subject), nil
}

// AllTools returns all Gmail tools with the given name prefix (e.g. google_gmail).
func AllTools(name string, svc Service) []tool.Tool {
	callables := newGmailTools(name, svc).tools()
	out := make([]tool.Tool, len(callables))
	for i, t := range callables {
		out[i] = t
	}
	return out
}
