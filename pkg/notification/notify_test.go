package notification_test

import (
	"context"
	"encoding/json"
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
		cfg    notification.Config
		server *httptest.Server
	)

	BeforeEach(func() {
		ctx = context.Background()

		// Start a mock server for all webhook needs
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body []byte
			_ = r.ParseForm()
			if r.ContentLength > 0 {
				buf := make([]byte, r.ContentLength)
				r.Body.Read(buf)
				body = buf
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
			Expect(toolInfo.Declaration().Description).To(ContainSubstring("Notify users"))
		})
	})

	Describe("Execution", func() {
		Context("with invalid inputs", func() {
			It("should fail if justification is missing", func() {
				tool := notification.NewNotifyTool(notification.Config{})
				reqBytes, _ := json.Marshal(map[string]interface{}{
					"agent_name": "TestAgent",
					"message":    "Help!",
				})
				_, err := tool.Call(ctx, reqBytes)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("justification is required"))
			})

			It("should fail if agent_name is missing", func() {
				tool := notification.NewNotifyTool(notification.Config{})
				reqBytes, _ := json.Marshal(map[string]interface{}{
					"justification": "Needs help",
					"message":       "Help!",
				})
				_, err := tool.Call(ctx, reqBytes)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("agent_name is required"))
			})

			It("should fail if message is missing", func() {
				tool := notification.NewNotifyTool(notification.Config{})
				reqBytes, _ := json.Marshal(map[string]interface{}{
					"justification": "Needs help",
					"agent_name":    "TestAgent",
				})
				_, err := tool.Call(ctx, reqBytes)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("message is required"))
			})
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

		Context("with various configured providers", func() {
			BeforeEach(func() {
				cfg = notification.Config{
					Slack: []notification.SlackConfig{
						{WebhookURL: server.URL + "/slack"},
					},
					Webhooks: []notification.WebhookConfig{
						{
							URL: server.URL + "/webhook",
							Headers: map[string]string{
								"X-Custom-Auth": "secret",
							},
						},
					},
					Twilio: []notification.TwilioConfig{
						{
							AccountSID: "AC123",
							AuthToken:  "token",
							From:       "+123",
							To:         "+456",
						}, // Note: Twilio URL is hardcoded in implementation, so this will fail the real call if we don't mock it, but we can override it by having an interface or we can accept it errors out for Twilio while Slack/Webhook succeed. Let's just avoid Twilio in this passing test or mock http default client. For now let's just leave Twilio out of this HTTP mock test since it hits external URL. Let's omit or test error handling.
					},
					Discord: []notification.DiscordConfig{
						{WebhookURL: server.URL + "/discord"},
					},
				}
				// override twilio to be empty for this success test to avoid external call
				cfg.Twilio = nil
			})

			It("should send notifications successfully", func() {
				tool := notification.NewNotifyTool(cfg)
				reqBytes, _ := json.Marshal(map[string]interface{}{
					"justification": "Stuck analyzing code",
					"agent_name":    "Debugger",
					"message":       "Cannot find syntax error",
				})
				res, err := tool.Call(ctx, reqBytes)
				Expect(err).NotTo(HaveOccurred())
				resStr := res.(string)
				Expect(resStr).To(ContainSubstring("Successfully sent notification"))
				Expect(resStr).To(ContainSubstring("3")) // slack, webhook, discord
			})
		})
	})
})
