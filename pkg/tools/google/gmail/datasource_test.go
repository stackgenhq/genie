// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package gmail_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/tools/google/gmail"
)

// mockGmailService is a minimal implementation of gmail.Service for tests.
type mockGmailService struct {
	listMessages func(ctx context.Context, query string, maxResults int) ([]*gmail.MessageSummary, error)
	getMessage   func(ctx context.Context, id string) (*gmail.MessageDetail, error)
}

func (m *mockGmailService) ListMessages(ctx context.Context, query string, maxResults int) ([]*gmail.MessageSummary, error) {
	if m.listMessages != nil {
		return m.listMessages(ctx, query, maxResults)
	}
	return nil, nil
}

func (m *mockGmailService) GetMessage(ctx context.Context, id string) (*gmail.MessageDetail, error) {
	if m.getMessage != nil {
		return m.getMessage(ctx, id)
	}
	return nil, nil
}

func (m *mockGmailService) Send(ctx context.Context, to []string, subject, body string) error {
	return nil
}
func (m *mockGmailService) Validate(ctx context.Context) error { return nil }

var _ = Describe("GmailConnector", func() {
	Describe("Name", func() {
		It("returns gmail", func() {
			conn := gmail.NewGmailConnector(&mockGmailService{})
			Expect(conn.Name()).To(Equal("gmail"))
		})
	})

	Describe("ListItems", func() {
		It("returns nil when scope has no Gmail label IDs", func(ctx context.Context) {
			conn := gmail.NewGmailConnector(&mockGmailService{})
			items, err := conn.ListItems(ctx, datasource.Scope{})
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeNil())
		})

		It("returns normalized items for messages in scope labels", func(ctx context.Context) {
			mock := &mockGmailService{
				listMessages: func(_ context.Context, query string, _ int) ([]*gmail.MessageSummary, error) {
					Expect(query).To(Equal("label:INBOX"))
					return []*gmail.MessageSummary{
						{ID: "msg1", Subject: "Test", Snippet: "Snippet text", From: "a@b.com", Date: "Mon, 1 Jan 2025 12:00:00 +0000"},
					}, nil
				},
			}
			conn := gmail.NewGmailConnector(mock)
			scope := datasource.Scope{GmailLabelIDs: []string{"INBOX"}}

			items, err := conn.ListItems(ctx, scope)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].ID).To(Equal("gmail:msg1"))
			Expect(items[0].Source).To(Equal("gmail"))
			Expect(items[0].Content).To(ContainSubstring("Test"))
			Expect(items[0].Content).To(ContainSubstring("Snippet text"))
			Expect(items[0].Metadata["subject"]).To(Equal("Test"))
		})
	})
})
