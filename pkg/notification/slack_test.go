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

var _ = Describe("Slack Notification", func() {
	var (
		server *httptest.Server
	)

	BeforeEach(func() {

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("POST"))
			Expect(r.Header.Get("Content-Type")).To(Equal("application/json"))

			var payload map[string]string
			json.NewDecoder(r.Body).Decode(&payload)
			Expect(payload["text"]).To(ContainSubstring("*Justification:* Stuck"))

			w.WriteHeader(http.StatusOK)
		}))
	})

	AfterEach(func() {
		server.Close()
	})

	It("should send notifications successfully using Slack", func(ctx context.Context) {
		cfg := notification.Config{
			Slack: []notification.SlackConfig{
				{WebhookURL: server.URL + "/slack"},
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
