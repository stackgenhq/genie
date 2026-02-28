// Package gmail provides a DataSource connector that enumerates Gmail messages
// for given labels for vectorization. It uses the existing Gmail Service to list
// messages (by label) and optionally fetches full body for richer content.
package gmail

import (
	"context"
	"fmt"
	"net/mail"
	"time"

	"github.com/stackgenhq/genie/pkg/datasource"
)

const (
	datasourceNameGmail = "gmail"
	gmailListLimit      = 50
)

// GmailConnector implements datasource.DataSource for Gmail.
// It lists messages for each label in scope.GmailLabelIDs and returns one
// NormalizedItem per message (subject + snippet, or full body when fetched).
type GmailConnector struct {
	svc Service
}

// NewGmailConnector returns a DataSource that lists Gmail messages by label.
// The caller must provide an initialised Gmail Service.
func NewGmailConnector(svc Service) *GmailConnector {
	return &GmailConnector{svc: svc}
}

// Name returns the source identifier for Gmail.
func (c *GmailConnector) Name() string {
	return datasourceNameGmail
}

// ListItems lists messages for each label in scope.GmailLabelIDs (query
// "label:<id>") and returns one NormalizedItem per message with ID
// "gmail:messageId". Content is subject + snippet from the list response only;
// full-body fetch is not performed here to avoid N+1 API calls and rate limits.
// The sync layer can add optional full-body enrichment in a later step if needed.
func (c *GmailConnector) ListItems(ctx context.Context, scope datasource.Scope) ([]datasource.NormalizedItem, error) {
	return c.listItemsWithQuery(ctx, scope, "")
}

// ListItemsSince returns only messages updated (internal date) after the given time.
// It uses Gmail search "after:YYYY/MM/DD" so the API returns only new/updated messages.
func (c *GmailConnector) ListItemsSince(ctx context.Context, scope datasource.Scope, since time.Time) ([]datasource.NormalizedItem, error) {
	if since.IsZero() {
		return c.ListItems(ctx, scope)
	}
	// Gmail search: after:YYYY/MM/DD (internal date)
	after := since.Format("2006/1/2")
	return c.listItemsWithQuery(ctx, scope, " after:"+after)
}

func (c *GmailConnector) listItemsWithQuery(ctx context.Context, scope datasource.Scope, querySuffix string) ([]datasource.NormalizedItem, error) {
	if len(scope.GmailLabelIDs) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{})
	var out []datasource.NormalizedItem
	for _, labelID := range scope.GmailLabelIDs {
		query := "label:" + labelID + querySuffix
		msgs, err := c.svc.ListMessages(ctx, query, gmailListLimit)
		if err != nil {
			return nil, fmt.Errorf("gmail label %s: %w", labelID, err)
		}
		for _, m := range msgs {
			if m == nil {
				continue
			}
			if _, ok := seen[m.ID]; ok {
				continue
			}
			seen[m.ID] = struct{}{}
			updatedAt, _ := parseGmailDate(m.Date)
			content := m.Subject + "\n\n" + m.Snippet
			meta := map[string]string{"subject": m.Subject}
			if m.From != "" {
				meta["from"] = m.From
			}
			out = append(out, datasource.NormalizedItem{
				ID:        "gmail:" + m.ID,
				Source:    datasourceNameGmail,
				SourceRef: &datasource.SourceRef{Type: datasourceNameGmail, RefID: m.ID},
				UpdatedAt: updatedAt,
				Content:   content,
				Metadata:  meta,
			})
		}
	}
	return out, nil
}

func parseGmailDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := mail.ParseDate(s)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// Ensure GmailConnector implements datasource.DataSource and datasource.ListItemsSince.
var (
	_ datasource.DataSource     = (*GmailConnector)(nil)
	_ datasource.ListItemsSince = (*GmailConnector)(nil)
)
