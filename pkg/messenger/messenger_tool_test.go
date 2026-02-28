package messenger_test

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/messenger/messengerfakes"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("SendMessageTool", func() {
	var (
		fakeMessenger *messengerfakes.FakeMessenger
		sendTool      tool.CallableTool
	)

	BeforeEach(func() {
		fakeMessenger = &messengerfakes.FakeMessenger{}
		fakeMessenger.PlatformReturns(messenger.PlatformSlack)
		sendTool = messenger.NewSendMessageTool(fakeMessenger).(tool.CallableTool)
	})

	Describe("Declaration", func() {
		It("should have name 'send_message'", func() {
			decl := sendTool.Declaration()
			Expect(decl.Name).To(Equal(messenger.ToolName))
		})

		It("should have a non-empty description", func() {
			decl := sendTool.Declaration()
			Expect(decl.Description).NotTo(BeEmpty())
			Expect(decl.Description).To(ContainSubstring("slack"))
		})

		It("should not require any field unconditionally", func() {
			decl := sendTool.Declaration()
			Expect(decl.InputSchema).NotTo(BeNil())
			Expect(decl.InputSchema.Required).To(BeEmpty())
		})

		It("should define type, channel_id, text, emoji, message_id, and thread_id properties", func() {
			decl := sendTool.Declaration()
			Expect(decl.InputSchema.Properties).To(HaveKey("type"))
			Expect(decl.InputSchema.Properties).To(HaveKey("channel_id"))
			Expect(decl.InputSchema.Properties).To(HaveKey("text"))
			Expect(decl.InputSchema.Properties).To(HaveKey("emoji"))
			Expect(decl.InputSchema.Properties).To(HaveKey("message_id"))
			Expect(decl.InputSchema.Properties).To(HaveKey("thread_id"))
		})
	})

	Describe("Call", func() {
		It("should send a message with valid arguments", func() {
			fakeMessenger.SendReturns(messenger.SendResponse{
				MessageID: "msg-123",
				Timestamp: time.Now(),
			}, nil)

			args, err := json.Marshal(map[string]string{
				"channel_id": "C12345",
				"text":       "Hello from test",
			})
			Expect(err).NotTo(HaveOccurred())

			result, err := sendTool.Call(context.Background(), args)
			Expect(err).NotTo(HaveOccurred())

			resultMap, ok := result.(map[string]string)
			Expect(ok).To(BeTrue())
			Expect(resultMap["message_id"]).To(Equal("msg-123"))
			Expect(resultMap["status"]).To(Equal("sent"))

			// Verify the fake was called with correct args.
			Expect(fakeMessenger.SendCallCount()).To(Equal(1))
			_, req := fakeMessenger.SendArgsForCall(0)
			Expect(req.Channel.ID).To(Equal("C12345"))
			Expect(req.Content.Text).To(Equal("Hello from test"))
			Expect(req.ThreadID).To(BeEmpty())
		})

		It("should pass thread_id when provided", func() {
			fakeMessenger.SendReturns(messenger.SendResponse{
				MessageID: "msg-456",
				Timestamp: time.Now(),
			}, nil)

			args, err := json.Marshal(map[string]string{
				"channel_id": "C12345",
				"text":       "Threaded reply",
				"thread_id":  "1234567890.123456",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = sendTool.Call(context.Background(), args)
			Expect(err).NotTo(HaveOccurred())

			_, req := fakeMessenger.SendArgsForCall(0)
			Expect(req.ThreadID).To(Equal("1234567890.123456"))
		})

		It("should return an error when channel_id is missing and no context", func() {
			args, err := json.Marshal(map[string]string{
				"text": "Hello",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = sendTool.Call(context.Background(), args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("channel_id"))
		})

		It("should use MessageOrigin from context when channel_id is omitted", func() {
			fakeMessenger.SendReturns(messenger.SendResponse{
				MessageID: "msg-ctx",
			}, nil)

			origin := messenger.MessageOrigin{
				Platform: messenger.PlatformSlack,
				Channel:  messenger.Channel{ID: "C-FROM-CTX"},
				ThreadID: "thread-from-ctx",
			}
			ctx := messenger.WithMessageOrigin(context.Background(), origin)

			args, err := json.Marshal(map[string]string{
				"text": "Hello via context",
			})
			Expect(err).NotTo(HaveOccurred())

			result, err := sendTool.Call(ctx, args)
			Expect(err).NotTo(HaveOccurred())

			resultMap := result.(map[string]string)
			Expect(resultMap["status"]).To(Equal("sent"))

			_, req := fakeMessenger.SendArgsForCall(0)
			Expect(req.Channel.ID).To(Equal("C-FROM-CTX"))
			Expect(req.ThreadID).To(Equal("thread-from-ctx"))
		})

		It("should return an error when text is missing", func() {
			args, err := json.Marshal(map[string]string{
				"channel_id": "C12345",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = sendTool.Call(context.Background(), args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("text"))
		})

		It("should return an error for malformed JSON", func() {
			_, err := sendTool.Call(context.Background(), []byte(`{invalid json`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid"))
		})

		It("should propagate errors from the messenger Send", func() {
			fakeMessenger.SendReturns(messenger.SendResponse{}, fmt.Errorf("connection lost"))

			args, err := json.Marshal(map[string]string{
				"channel_id": "C12345",
				"text":       "Hello",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = sendTool.Call(context.Background(), args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("connection lost"))
		})
	})
})
