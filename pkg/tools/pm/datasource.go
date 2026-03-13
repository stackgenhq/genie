// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package pm provides a DataSource connector for issue-tracking systems.
// The Linear (and optionally other) backend can list issues and return them as
// NormalizedItems for vectorization. Scope uses LinearTeamIDs; when the
// underlying ListIssues does not support team filter yet, all listed issues
// are returned.
package pm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/datasource"
)

const datasourceNameLinear = "linear"

// LinearConnector implements datasource.DataSource for Linear (and any
// pm.Service that supports ListIssues). It lists issues and returns one
// NormalizedItem per issue for the sync pipeline to vectorize.
type LinearConnector struct {
	svc Service
}

// NewLinearConnector returns a DataSource that lists issues from the given
// pm.Service. When the config provider is Linear, scope.LinearTeamIDs is
// intended to filter by team; the current Linear API in this package lists
// all issues (team filter can be added later to the GraphQL query).
func NewLinearConnector(svc Service) *LinearConnector {
	return &LinearConnector{svc: svc}
}

// Name returns the source identifier for Linear.
func (c *LinearConnector) Name() string {
	return datasourceNameLinear
}

// ListItems lists issues via the pm.Service (e.g. Linear ListIssues) and
// returns one NormalizedItem per issue with ID "linear:identifier". Content
// is title + description; metadata includes status and assignee.
func (c *LinearConnector) ListItems(ctx context.Context, scope datasource.Scope) ([]datasource.NormalizedItem, error) {
	// Linear ListIssues currently does not filter by team; we list open issues.
	// When scope includes linear team IDs, a future enhancement can pass team filter.
	_ = scope.Get("linear")
	issues, err := c.svc.ListIssues(ctx, IssueFilter{})
	if err != nil {
		return nil, fmt.Errorf("linear list issues: %w", err)
	}
	out := make([]datasource.NormalizedItem, 0, len(issues))
	for _, issue := range issues {
		if issue == nil {
			continue
		}
		content := issue.Title
		if issue.Description != "" {
			content = issue.Title + "\n\n" + issue.Description
		}
		// Fetch and append issue comments for richer context.
		comments, err := c.svc.ListComments(ctx, issue.ID)
		if err == nil && len(comments) > 0 {
			content += "\n\n--- Comments ---"
			for _, cmt := range comments {
				if cmt == nil || cmt.Body == "" {
					continue
				}
				header := "\n\n"
				if cmt.Author != "" {
					header += cmt.Author + ": "
				}
				content += header + cmt.Body
			}
		}
		meta := map[string]string{"title": issue.Title}
		if issue.Status != "" {
			meta["status"] = issue.Status
		}
		if issue.Assignee != "" {
			meta["assignee"] = issue.Assignee
		}
		if len(issue.Labels) > 0 {
			meta["labels"] = strings.Join(issue.Labels, ",")
		}
		if comments != nil {
			meta["comment_count"] = fmt.Sprintf("%d", len(comments))
		}
		out = append(out, datasource.NormalizedItem{
			ID:        "linear:" + issue.ID,
			Source:    datasourceNameLinear,
			UpdatedAt: time.Now(), // Linear issue list does not expose updated_at in current type
			Content:   content,
			Metadata:  meta,
		})
	}
	return out, nil
}

// Ensure LinearConnector implements datasource.DataSource at compile time.
var _ datasource.DataSource = (*LinearConnector)(nil)
