// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package slack

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Slack Internal", func() {
	Describe("splitMessageID", func() {
		It("splits valid format", func() {
			res := splitMessageID("ch:ts")
			Expect(res).To(Equal([]string{"ch", "ts"}))
		})
		It("returns nil for invalid format", func() {
			res := splitMessageID("chts")
			Expect(res).To(BeNil())
		})
	})
	Describe("parseSlackMessageID", func() {
		It("parses correctly", func() {
			ch, ts, ok := parseSlackMessageID("ch:ts")
			Expect(ok).To(BeTrue())
			Expect(ch).To(Equal("ch"))
			Expect(ts).To(Equal("ts"))
		})
		It("fails on missing colon", func() {
			_, _, ok := parseSlackMessageID("chts")
			Expect(ok).To(BeFalse())
		})
		It("fails on colon at start", func() {
			_, _, ok := parseSlackMessageID(":ts")
			Expect(ok).To(BeFalse())
		})
		It("fails on colon at end", func() {
			_, _, ok := parseSlackMessageID("ch:")
			Expect(ok).To(BeFalse())
		})
	})
	Describe("slackEmojiName", func() {
		It("returns correctly", func() {
			Expect(slackEmojiName("✅")).To(Equal("white_check_mark"))
			Expect(slackEmojiName("thumbsup")).To(Equal("")) // invalid without actual emoji
			Expect(slackEmojiName("👍")).To(Equal("thumbsup"))
			Expect(slackEmojiName("👎")).To(Equal("thumbsdown"))
			Expect(slackEmojiName("other")).To(Equal(""))
		})
	})
	Describe("useHTTPEventsAPI", func() {
		It("returns true if secret set", func() {
			m := &Messenger{cfg: Config{SigningSecret: "secret"}}
			Expect(m.useHTTPEventsAPI()).To(BeTrue())
		})
		It("returns false if secret not set", func() {
			m := &Messenger{cfg: Config{SigningSecret: ""}}
			Expect(m.useHTTPEventsAPI()).To(BeFalse())
		})
	})
})
