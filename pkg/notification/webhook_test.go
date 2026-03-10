// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package notification_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/notification"
)

var _ = Describe("Webhook Notification", func() {
	var (
		ctx    context.Context
		server *httptest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer GinkgoRecover()
			Expect(r.Method).To(Equal("POST"))
			Expect(r.Header.Get("Content-Type")).To(Equal("application/json"))
			Expect(r.Header.Get("X-Custom-Auth")).To(Equal("secret"))

			var payload map[string]interface{}
			Expect(json.NewDecoder(r.Body).Decode(&payload)).To(Succeed())
			message, ok := payload["message"].(string)
			Expect(ok).To(BeTrue())
			Expect(message).To(ContainSubstring("Justification: Stuck"))

			w.WriteHeader(http.StatusOK)
		}))
	})

	AfterEach(func() {
		server.Close()
	})

	It("should send notifications successfully via Webhooks", func() {
		cfg := notification.Config{
			Webhooks: []notification.WebhookConfig{
				{
					URL: server.URL + "/webhook",
					Headers: map[string]string{
						"X-Custom-Auth": "secret",
					},
				},
			},
		}

		tool := notification.NewNotifyTool(cfg)
		reqBytes, _ := json.Marshal(map[string]interface{}{
			"justification": "Stuck analyzing code",
			"agent_name":    "Debugger",
			"message":       "Cannot find syntax error",
		})
		res, err := tool.Call(ctx, reqBytes)

		Expect(err).NotTo(HaveOccurred())
		resStr := res.(string)
		Expect(resStr).To(ContainSubstring("Successfully sent notification through 1 endpoint(s)."))
	})
})
