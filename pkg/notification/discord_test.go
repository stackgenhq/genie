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

var _ = Describe("Discord Notification", func() {
	var (
		ctx    context.Context
		server *httptest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("POST"))
			Expect(r.Header.Get("Content-Type")).To(Equal("application/json"))

			var payload map[string]string
			json.NewDecoder(r.Body).Decode(&payload)
			Expect(payload["content"]).To(ContainSubstring("**Justification:** Stuck"))

			w.WriteHeader(http.StatusOK)
		}))
	})

	AfterEach(func() {
		server.Close()
	})

	It("should send notifications successfully via Discord", func() {
		cfg := notification.Config{
			Discord: []notification.DiscordConfig{
				{WebhookURL: server.URL + "/discord"},
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
