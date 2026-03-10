// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	// defaultIMAPTimeout is the fallback deadline for IMAP operations when the
	// caller's context carries no explicit deadline.
	defaultIMAPTimeout = 30 * time.Second

	// maxBodyLen caps the plain-text or HTML body stored per email to prevent
	// oversized payloads from overwhelming the LLM context window.
	maxBodyLen = 2000

	// maxFetchMessages limits the number of messages fetched in a single Read
	// call. Without a cap, an "unread" filter could match thousands of messages
	// and blow past the downstream LLM token limit.
	maxFetchMessages = 10
)

// Service provides capabilities to send and read emails.
type Service interface {
	Send(ctx context.Context, req SendRequest) error
	Read(ctx context.Context, filter string) ([]*Email, error)
	// Validate performs a lightweight health check to detect misconfigurations at startup.
	Validate(ctx context.Context) error
}

type SendRequest struct {
	To      []string
	Subject string
	Body    string
	IsHTML  bool
}

type Email struct {
	From        string
	Subject     string
	Body        string // Plain text body
	Date        string
	Attachments []string // Summaries of attachments
}

// Config holds configuration for Email providers
type Config struct {
	Provider string `json:"provider,omitempty" yaml:"Provider,omitempty" toml:"Provider,omitempty"` // smtp, gmail_api, sendgrid
	Host     string `json:"host" yaml:"Host,omitempty" toml:"Host,omitempty"`                       // SMTP Host
	Port     int    `json:"port" yaml:"Port,omitempty" toml:"Port,omitempty,omitzero"`              // SMTP Port
	Username string `json:"username" yaml:"Username,omitempty" toml:"Username,omitempty"`
	Password string `json:"password" yaml:"Password,omitempty" toml:"Password,omitempty"`

	// IMAP settings for reading emails
	IMAPHost string `json:"imap_host,omitempty" yaml:"IMAPHost,omitempty" toml:"IMAPHost,omitempty"`
	IMAPPort int    `json:"imap_port,omitempty" yaml:"IMAPPort,omitempty" toml:"IMAPPort,omitempty,omitzero"`

	// IMAPTLSConfig is set by the application from security.CryptoConfig.TLSConfig()
	// to enforce NIST 2030 minimums (TLS 1.2+). Not loaded from config file.
	IMAPTLSConfig *tls.Config `json:"-" yaml:"-" toml:"-"`
}

// New creates a new Email Service based on the configuration
func (cfg Config) New() (Service, error) {
	if cfg.Provider == "smtp" || cfg.Provider == "" {
		return &smtpIMAPService{cfg: cfg}, nil
	}
	return nil, fmt.Errorf("unsupported email provider: %s", cfg.Provider)
}

type smtpIMAPService struct {
	cfg Config
}

// Validate checks that required SMTP/IMAP config is present so misconfigurations
// are detected at startup. It does not open connections.
func (s *smtpIMAPService) Validate(ctx context.Context) error {
	if s.cfg.Host == "" || s.cfg.Port == 0 {
		return fmt.Errorf("email not configured: Host and Port are required for SMTP")
	}
	return nil
}

func (s *smtpIMAPService) Send(ctx context.Context, req SendRequest) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)

	msg := []byte(fmt.Sprintf("To: %s\r\n"+
		"Subject: %s\r\n"+
		"\r\n"+
		"%s\r\n", strings.Join(req.To, ","), req.Subject, req.Body))

	// Simple SMTP send (blocking operations, wrapped in goroutine if needed, but here simple)
	// For production, consider using a library like gomail for better MIME handling on send.
	return smtp.SendMail(addr, auth, s.cfg.Username, req.To, msg)
}

// Read connects to the configured IMAP server, fetches messages matching the
// filter, and returns them as structured Email values. The context deadline is
// propagated to the underlying TCP connection so all IMAP operations (dial,
// login, fetch) are bounded. Without this, a slow or unresponsive IMAP server
// would block the calling sub-agent indefinitely.
func (s *smtpIMAPService) Read(ctx context.Context, filter string) ([]*Email, error) {
	if s.cfg.IMAPHost == "" {
		return nil, fmt.Errorf("IMAP host not configured")
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context already cancelled: %w", err)
	}

	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		deadline = time.Now().Add(defaultIMAPTimeout)
	}

	c, conn, err := s.dialIMAP(ctx, deadline)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = c.Logout()
		_ = conn.Close()
	}()

	if err := c.Login(s.cfg.Username, s.cfg.Password); err != nil {
		return nil, fmt.Errorf("failed to login to IMAP: %w", err)
	}

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		return nil, fmt.Errorf("failed to select INBOX: %w", err)
	}

	seqset, empty, err := s.buildSeqSet(c, mbox, filter)
	if err != nil {
		return nil, err
	}
	if empty {
		return []*Email{}, nil
	}

	return s.fetchMessages(ctx, c, seqset)
}

// dialIMAP establishes a TLS connection to the IMAP server with a deadline
// derived from the caller's context. The returned net.Conn is exposed so the
// caller can close the underlying socket in the defer, even if the IMAP client
// logout hangs. Uses IMAPTLSConfig when set (e.g. NIST 2030–compliant TLS).
func (s *smtpIMAPService) dialIMAP(_ context.Context, deadline time.Time) (*client.Client, net.Conn, error) {
	addr := fmt.Sprintf("%s:%d", s.cfg.IMAPHost, s.cfg.IMAPPort)
	dialer := &net.Dialer{Deadline: deadline}
	tlsCfg := s.cfg.IMAPTLSConfig
	if tlsCfg != nil {
		tlsCfg = tlsCfg.Clone()
	}
	tlsConn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to dial IMAP: %w", err)
	}

	if err := tlsConn.SetDeadline(deadline); err != nil {
		_ = tlsConn.Close()
		return nil, nil, fmt.Errorf("failed to set IMAP deadline: %w", err)
	}

	c, err := client.New(tlsConn)
	if err != nil {
		_ = tlsConn.Close()
		return nil, nil, fmt.Errorf("failed to create IMAP client: %w", err)
	}
	return c, tlsConn, nil
}

// buildSeqSet constructs the IMAP sequence set for the fetch operation.
// When a filter is supplied it maps to IMAP search criteria; otherwise
// it returns the last 5 messages. Results are capped at maxFetchMessages
// to keep the downstream LLM payload within token limits. The boolean
// return indicates an empty mailbox or zero search results so the caller
// can short-circuit.
func (s *smtpIMAPService) buildSeqSet(c *client.Client, mbox *imap.MailboxStatus, filter string) (*imap.SeqSet, bool, error) {
	seqset := new(imap.SeqSet)

	if filter != "" {
		criteria := imap.NewSearchCriteria()
		if strings.Contains(strings.ToLower(filter), "unread") {
			criteria.WithoutFlags = []string{imap.SeenFlag}
		}
		uids, err := c.Search(criteria)
		if err != nil {
			return nil, false, fmt.Errorf("failed to search IMAP: %w", err)
		}
		if len(uids) == 0 {
			return nil, true, nil
		}
		if len(uids) > maxFetchMessages {
			uids = uids[len(uids)-maxFetchMessages:]
		}
		seqset.AddNum(uids...)
		return seqset, false, nil
	}

	if mbox.Messages == 0 {
		return nil, true, nil
	}
	from := uint32(1)
	if mbox.Messages > 5 {
		from = mbox.Messages - 4
	}
	seqset.AddRange(from, mbox.Messages)
	return seqset, false, nil
}

// fetchMessages runs the IMAP FETCH in a goroutine and collects results in
// a context-aware loop so cancellation terminates the read promptly.
func (s *smtpIMAPService) fetchMessages(ctx context.Context, c *client.Client, seqset *imap.SeqSet) ([]*Email, error) {
	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, []imap.FetchItem{imap.FetchEnvelope, imap.FetchBody + "[]", imap.FetchInternalDate}, messages)
	}()

	var emails []*Email
	for {
		select {
		case <-ctx.Done():
			return emails, fmt.Errorf("email fetch interrupted: %w", ctx.Err())
		case msg, ok := <-messages:
			if !ok {
				if err := <-done; err != nil {
					return nil, fmt.Errorf("failed to fetch messages: %w", err)
				}
				return emails, nil
			}
			if parsed := s.parseMessage(msg); parsed != nil {
				emails = append(emails, parsed)
			}
		}
	}
}

// parseMessage converts a raw IMAP message into an Email struct, guarding
// against nil envelopes and truncating oversized bodies. It uses
// github.com/emersion/go-message/mail for MIME parsing (RFC 5322 / MIME).
func (s *smtpIMAPService) parseMessage(msg *imap.Message) *Email {
	if msg == nil || msg.Envelope == nil {
		return nil
	}

	var section imap.BodySectionName
	r := msg.GetBody(&section)
	if r == nil {
		return nil
	}

	mr, err := mail.CreateReader(r)
	if err != nil {
		return nil
	}
	defer func() { _ = mr.Close() }()

	var textParts, htmlParts []string
	var attachments []string

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil
		}

		mediaType, _, _ := parseContentType(p.Header.Get("Content-Type"))

		switch h := p.Header.(type) {
		case *mail.AttachmentHeader:
			filename, _ := h.Filename()
			if filename == "" {
				filename = "attachment"
			}
			attachments = append(attachments, fmt.Sprintf("%s (%s)", filename, p.Header.Get("Content-Type")))
			_, _ = io.Copy(io.Discard, p.Body)
		case *mail.InlineHeader:
			slurp, _ := io.ReadAll(p.Body)
			s := string(slurp)
			if strings.HasPrefix(mediaType, "text/plain") {
				textParts = append(textParts, s)
			} else if strings.HasPrefix(mediaType, "text/html") {
				htmlParts = append(htmlParts, s)
			}
		default:
			_, _ = io.Copy(io.Discard, p.Body)
		}
	}

	textBody := strings.Join(textParts, "\n")
	htmlBody := strings.Join(htmlParts, "\n")
	body := textBody
	if body == "" {
		body = htmlBody
	}
	if len(body) > maxBodyLen {
		body = body[:maxBodyLen] + "\n...(truncated)"
	}

	from := "unknown"
	if len(msg.Envelope.From) > 0 {
		from = msg.Envelope.From[0].Address()
	}

	return &Email{
		From:        from,
		Subject:     msg.Envelope.Subject,
		Body:        body,
		Date:        msg.Envelope.Date.Format(time.RFC3339),
		Attachments: attachments,
	}
}

// parseContentType returns the media type and params from a Content-Type header.
// For an empty header it returns ("text/plain", nil, nil). Otherwise it returns
// mime.ParseMediaType(v); the returned error indicates parse failure.
func parseContentType(v string) (mediaType string, params map[string]string, err error) {
	if v == "" {
		return "text/plain", nil, nil
	}
	return mime.ParseMediaType(v)
}

// ── Tool Definitions ────────────────────────────────────────────────────

type SendEmailRequest struct {
	To      []string `json:"to" jsonschema:"description=Recipient email addresses,required"`
	Subject string   `json:"subject" jsonschema:"description=Email subject,required"`
	Body    string   `json:"body" jsonschema:"description=Email body content,required"`
}

type ReadEmailRequest struct {
	Filter string `json:"filter" jsonschema:"description=Filter instructions (e.g. 'unread', 'security alert')"`
}

// ── Tool Constructors ───────────────────────────────────────────────────

type toolSet struct {
	s Service
}

func NewSendEmailTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.sendEmail,
		function.WithName("email_send"),
		function.WithDescription("Send an email to one or more recipients."),
	)
}

func NewReadEmailTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.readEmail,
		function.WithName("email_read"),
		function.WithDescription("Read emails matching a filter."),
	)
}

func AllTools(s Service) []tool.Tool {
	return []tool.Tool{
		NewSendEmailTool(s),
		NewReadEmailTool(s),
	}
}

// ── Tool Implementations ────────────────────────────────────────────────

func (ts *toolSet) sendEmail(ctx context.Context, req SendEmailRequest) (string, error) {
	err := ts.s.Send(ctx, SendRequest{
		To:      req.To,
		Subject: req.Subject,
		Body:    req.Body,
		IsHTML:  false,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"DONE. Email successfully sent to %s with subject %q. Do NOT call email_send again for this message.",
		strings.Join(req.To, ", "), req.Subject,
	), nil
}

func (ts *toolSet) readEmail(ctx context.Context, req ReadEmailRequest) ([]*Email, error) {
	return ts.s.Read(ctx, req.Filter)
}
