// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package notification_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/tool"

	"github.com/stackgenhq/genie/pkg/notification"
)

var _ = Describe("Notify Tool", func() {
	var (
		ctx    context.Context
		server *httptest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Start a mock server for all webhook needs
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer GinkgoRecover()
			_ = r.ParseForm()
			if r.ContentLength > 0 {
				body, err := io.ReadAll(r.Body)
				Expect(err).NotTo(HaveOccurred())
				if r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
					_ = r.Form.Encode()
				} else {
					_ = string(body)
				}
			}
			w.WriteHeader(http.StatusOK)
		}))
	})

	AfterEach(func() {
		server.Close()
	})

	Describe("Initialization", func() {
		It("should create a callable tool", func() {
			callable := notification.NewNotifyTool(notification.Config{})
			Expect(callable).NotTo(BeNil())
			toolInfo := callable.(tool.Tool)
			Expect(toolInfo.Declaration().Name).To(Equal("notify"))
			Expect(toolInfo.Declaration().Description).To(ContainSubstring("Send notifications, alerts, or messages"))
		})
	})

	Describe("Execution", func() {
		Context("with invalid inputs", func() {
			DescribeTable("missing fields validation",
				func(req map[string]interface{}, expectedErr string) {
					toolInstance := notification.NewNotifyTool(notification.Config{})
					reqBytes, _ := json.Marshal(req)
					_, err := toolInstance.Call(ctx, reqBytes)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(expectedErr))
				},
				Entry("justification is missing", map[string]interface{}{
					"agent_name": "TestAgent",
					"message":    "Help!",
				}, "missing fields: justification"),
				Entry("agent_name is missing", map[string]interface{}{
					"justification": "Needs help",
					"message":       "Help!",
				}, "missing fields: agent_name"),
				Entry("message is missing", map[string]interface{}{
					"justification": "Needs help",
					"agent_name":    "TestAgent",
				}, "missing fields: message"),
				Entry("all fields missing", map[string]interface{}{}, "missing fields: justification, agent_name, message"),
			)
		})

		Context("with valid inputs and no config", func() {
			It("should return unconfigured message", func() {
				tool := notification.NewNotifyTool(notification.Config{})
				reqBytes, _ := json.Marshal(map[string]interface{}{
					"justification": "Need help",
					"agent_name":    "Agent007",
					"message":       "Stuck compiling",
				})
				res, err := tool.Call(ctx, reqBytes)
				Expect(err).NotTo(HaveOccurred())
				Expect(res).To(ContainSubstring("No notifications configured"))
			})
		})
	})
	Describe("Config", func() {
		DescribeTable("IsEmpty",
			func(cfg notification.Config, expected bool) {
				Expect(cfg.IsEmpty()).To(Equal(expected))
			},
			Entry("fully empty config", notification.Config{}, true),
			Entry("with slack configured", notification.Config{
				Slack: []notification.SlackConfig{{WebhookURL: "https://hooks.slack.com/test"}},
			}, false),
			Entry("with webhook configured", notification.Config{
				Webhooks: []notification.WebhookConfig{{URL: "https://example.com/hook"}},
			}, false),
			Entry("with twilio configured", notification.Config{
				Twilio: []notification.TwilioConfig{{AccountSID: "AC123", AuthToken: "tok", From: "+1", To: "+2"}},
			}, false),
			Entry("with discord configured", notification.Config{
				Discord: []notification.DiscordConfig{{WebhookURL: "https://discord.com/api/webhooks/test"}},
			}, false),
		)
	})
})
