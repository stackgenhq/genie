package slacktools_test

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/tools/slacktools"
	"github.com/appcd-dev/genie/pkg/tools/slacktools/slacktoolsfakes"
)

var _ = Describe("Slack Tools", func() {
	var fake *slacktoolsfakes.FakeService

	BeforeEach(func() {
		fake = new(slacktoolsfakes.FakeService)
	})

	Describe("slack_search_messages", func() {
		It("should return matching messages", func(ctx context.Context) {
			fake.SearchMessagesReturns([]slacktools.MessageResult{
				{Channel: "general", User: "alice", Text: "deploy done", Timestamp: "1234567890.000001"},
			}, nil)

			tool := slacktools.NewSearchMessagesTool(fake)
			reqJSON, _ := json.Marshal(struct {
				Query string `json:"query"`
			}{Query: "deploy"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			msgs, ok := resp.([]slacktools.MessageResult)
			Expect(ok).To(BeTrue())
			Expect(msgs).To(HaveLen(1))
			Expect(msgs[0].Text).To(ContainSubstring("deploy"))
		})
	})

	Describe("slack_list_channels", func() {
		It("should return channels", func(ctx context.Context) {
			fake.ListChannelsReturns([]slacktools.ChannelInfo{
				{ID: "C01", Name: "general", MemberCount: 50},
				{ID: "C02", Name: "engineering", MemberCount: 25},
			}, nil)

			tool := slacktools.NewListChannelsTool(fake)

			resp, err := tool.Call(ctx, []byte(`{}`))
			Expect(err).NotTo(HaveOccurred())

			channels, ok := resp.([]slacktools.ChannelInfo)
			Expect(ok).To(BeTrue())
			Expect(channels).To(HaveLen(2))
		})
	})

	Describe("slack_read_channel_history", func() {
		It("should return messages from channel", func(ctx context.Context) {
			fake.ReadChannelHistoryReturns([]slacktools.Message{
				{User: "bob", Text: "hello", Timestamp: "1234567890.000001"},
			}, nil)

			tool := slacktools.NewReadChannelHistoryTool(fake)
			reqJSON, _ := json.Marshal(struct {
				ChannelID string `json:"channel_id"`
			}{ChannelID: "C01"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			msgs, ok := resp.([]slacktools.Message)
			Expect(ok).To(BeTrue())
			Expect(msgs[0].User).To(Equal("bob"))
		})
	})

	Describe("slack_post_message", func() {
		It("should return success", func(ctx context.Context) {
			fake.PostMessageReturns(nil)

			tool := slacktools.NewPostMessageTool(fake)
			reqJSON, _ := json.Marshal(struct {
				ChannelID string `json:"channel_id"`
				Text      string `json:"text"`
			}{ChannelID: "C01", Text: "Hello from Genie"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).To(ContainSubstring("C01"))

			_, chID, text := fake.PostMessageArgsForCall(0)
			Expect(chID).To(Equal("C01"))
			Expect(text).To(Equal("Hello from Genie"))
		})
	})
})

var _ = Describe("AllTools", func() {
	It("should return 6 tools", func() {
		fake := new(slacktoolsfakes.FakeService)
		tools := slacktools.AllTools(fake)
		Expect(tools).To(HaveLen(6))
	})
})

var _ = Describe("New", func() {
	It("should return error when token is missing", func() {
		_, err := slacktools.New(slacktools.Config{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("token"))
	})
})

var _ = Describe("Slack Error Paths", func() {
	var fake *slacktoolsfakes.FakeService

	BeforeEach(func() {
		fake = new(slacktoolsfakes.FakeService)
	})

	It("should propagate SearchMessages error", func(ctx context.Context) {
		fake.SearchMessagesReturns(nil, fmt.Errorf("auth failed"))
		tool := slacktools.NewSearchMessagesTool(fake)
		_, err := tool.Call(ctx, []byte(`{"query":"test"}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate ListChannels error", func(ctx context.Context) {
		fake.ListChannelsReturns(nil, fmt.Errorf("rate limited"))
		tool := slacktools.NewListChannelsTool(fake)
		_, err := tool.Call(ctx, []byte(`{}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate PostMessage error", func(ctx context.Context) {
		fake.PostMessageReturns(fmt.Errorf("channel not found"))
		tool := slacktools.NewPostMessageTool(fake)
		_, err := tool.Call(ctx, []byte(`{"channel_id":"BAD","text":"hi"}`))
		Expect(err).To(HaveOccurred())
	})
})
