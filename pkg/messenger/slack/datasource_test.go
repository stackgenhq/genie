// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package slack_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	slackapi "github.com/slack-go/slack"

	"github.com/stackgenhq/genie/pkg/datasource"
	"github.com/stackgenhq/genie/pkg/messenger/slack"
)

var _ = Describe("SlackConnector", func() {
	Describe("Name", func() {
		It("returns slack", func() {
			api := slackapi.New("xoxb-test")
			conn := slack.NewSlackConnector(api)
			Expect(conn.Name()).To(Equal("slack"))
		})
	})

	Describe("ListItems", func() {
		It("returns nil when scope has no Slack channel IDs", func(ctx context.Context) {
			api := slackapi.New("xoxb-test")
			conn := slack.NewSlackConnector(api)
			scope := datasource.Scope{}
			items, err := conn.ListItems(ctx, scope)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(BeNil())
		})

		It("returns normalized items from conversation history", func(ctx context.Context) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				Expect(r.URL.Path).To(Equal("/api/conversations.history"))
				w.Header().Set("Content-Type", "application/json")
				body := map[string]any{
					"ok": true,
					"messages": []map[string]any{
						{
							"ts":   "1234567890.123456",
							"text": "Hello from Slack",
							"user": "U123",
						},
					},
				}
				json.NewEncoder(w).Encode(body)
			}))
			defer srv.Close()

			api := slackapi.New("xoxb-test", slackapi.OptionAPIURL(srv.URL+"/api/"))
			conn := slack.NewSlackConnector(api)
			scope := datasource.Scope{SlackChannelIDs: []string{"C1"}}

			items, err := conn.ListItems(ctx, scope)
			Expect(err).NotTo(HaveOccurred())
			Expect(items).To(HaveLen(1))
			Expect(items[0].ID).To(Equal("slack:C1:1234567890.123456"))
			Expect(items[0].Source).To(Equal("slack"))
			Expect(items[0].Content).To(Equal("Hello from Slack"))
			Expect(items[0].Metadata["author"]).To(Equal("U123"))
		})
	})
})
