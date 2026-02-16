package email

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/jhillyerd/enmime"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// Service provides capabilities to send and read emails.
type Service interface {
	Send(ctx context.Context, req SendRequest) error
	Read(ctx context.Context, filter string) ([]*Email, error)
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
	Provider string // smtp, gmail_api, sendgrid
	Host     string // SMTP Host
	Port     int    // SMTP Port
	Username string
	Password string

	// IMAP settings for reading emails
	IMAPHost string
	IMAPPort int
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

func (s *smtpIMAPService) Read(ctx context.Context, filter string) ([]*Email, error) {
	addr := fmt.Sprintf("%s:%d", s.cfg.IMAPHost, s.cfg.IMAPPort)
	if s.cfg.IMAPHost == "" {
		// Default fallback if not set: try to guess or error?
		// For now, let's assume if reading is requested, IMAPHost must be set.
		return nil, fmt.Errorf("IMAP host not configured")
	}

	c, err := client.DialTLS(addr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to dial IMAP: %w", err)
	}
	defer func() {
		_ = c.Logout()
	}()

	if err := c.Login(s.cfg.Username, s.cfg.Password); err != nil {
		return nil, fmt.Errorf("failed to login to IMAP: %w", err)
	}

	mbox, err := c.Select("INBOX", false)
	if err != nil {
		return nil, fmt.Errorf("failed to select INBOX: %w", err)
	}

	// Search criteria
	criteria := imap.NewSearchCriteria()
	if filter != "" {
		// Very basic filtering mapping
		if strings.Contains(strings.ToLower(filter), "unread") {
			criteria.WithoutFlags = []string{imap.SeenFlag}
		}
		// Add more advanced text search if needed:
		// criteria.Text = []string{filter}
	}

	seqset := new(imap.SeqSet)
	if filter != "" {
		uids, err := c.Search(criteria)
		if err != nil {
			return nil, fmt.Errorf("failed to search IMAP: %w", err)
		}
		if len(uids) == 0 {
			return []*Email{}, nil
		}
		seqset.AddNum(uids...)
	} else {
		// If no filter, get last 5 messages
		if mbox.Messages == 0 {
			return []*Email{}, nil
		}
		from := uint32(1)
		if mbox.Messages > 5 {
			from = mbox.Messages - 4
		}
		seqset.AddRange(from, mbox.Messages)
	}

	messages := make(chan *imap.Message, 10)
	done := make(chan error, 1)
	go func() {
		done <- c.Fetch(seqset, []imap.FetchItem{imap.FetchEnvelope, imap.FetchBody + "[]", imap.FetchInternalDate}, messages)
	}()

	var emails []*Email
	for msg := range messages {
		// Parse with enmime
		var section imap.BodySectionName
		r := msg.GetBody(&section)
		if r == nil {
			continue
		}

		env, err := enmime.ReadEnvelope(r)
		if err != nil {
			// Log error but continue?
			continue
		}

		body := env.Text
		if body == "" {
			body = env.HTML
		}

		var attachments []string
		for _, att := range env.Attachments {
			attachments = append(attachments, fmt.Sprintf("%s (%s)", att.FileName, att.ContentType))
		}

		emails = append(emails, &Email{
			From:        msg.Envelope.From[0].Address(),
			Subject:     msg.Envelope.Subject,
			Body:        body,
			Date:        msg.Envelope.Date.Format(time.RFC3339),
			Attachments: attachments,
		})
	}

	if err := <-done; err != nil {
		return nil, fmt.Errorf("failed to fetch messages: %w", err)
	}

	return emails, nil
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
	return "email sent successfully", nil
}

func (ts *toolSet) readEmail(ctx context.Context, req ReadEmailRequest) ([]*Email, error) {
	return ts.s.Read(ctx, req.Filter)
}
