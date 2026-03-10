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

var _ = Describe("Twilio Notification", func() {
	var (
		server *httptest.Server
	)

	BeforeEach(func() {

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal("POST"))
			Expect(r.Header.Get("Content-Type")).To(Equal("application/x-www-form-urlencoded"))

			w.WriteHeader(http.StatusOK)
		}))
	})

	AfterEach(func() {
		server.Close()
	})

	It("sends to the configured test server successfully", func(ctx context.Context) {
		cfg := notification.Config{
			Twilio: []notification.TwilioConfig{
				{
					AccountSID: "AC12300000000",
					AuthToken:  "token",
					From:       "+123",
					To:         "+456",
					BaseURL:    server.URL,
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
		Expect(res.(string)).To(ContainSubstring("Successfully sent notification"))
	})

})
