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

			// Note: Because Twilio's actual URL is hardcoded in the implementation,
			// this won't be easily mocked without passing the base URL as config.
			// However for unit testing we want to make sure it doesn't crash.
			// If we wanted to point this to a test server, we should make the URL configurable in TwilioConfig.
			// Since it's hardcoded to api.twilio.com, we can't easily assert on the server URL here
			// without sending real traffic.

			w.WriteHeader(http.StatusOK)
		}))
	})

	AfterEach(func() {
		server.Close()
	})

	It("fails to send if external Twilio URL cannot validate unconfigured tokens (simulated integration test behavior)", func(ctx context.Context) {
		cfg := notification.Config{
			Twilio: []notification.TwilioConfig{
				{
					AccountSID: "AC12300000000",
					AuthToken:  "token",
					From:       "+123",
					To:         "+456",
				},
			},
		}

		tool := notification.NewNotifyTool(cfg)
		reqBytes, _ := json.Marshal(map[string]interface{}{
			"justification": "Stuck analyzing code",
			"agent_name":    "Debugger",
			"message":       "Cannot find syntax error",
		})
		_, err := tool.Call(ctx, reqBytes)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("status code 401")) // Twilio will fail 401 Unauthorized for fake token
	})
})
