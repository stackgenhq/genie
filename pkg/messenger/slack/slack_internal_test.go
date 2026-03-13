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

	Describe("shouldProcess", func() {
		It("defaults to mention-only mode (drops non-mention messages)", func() {
			m := &Messenger{botUserID: "U_BOT"}
			Expect(m.shouldProcess("C123", "U1", "hello world", "")).To(BeFalse())
		})

		It("processes messages with bot @mention in default mode", func() {
			m := &Messenger{botUserID: "U_BOT"}
			Expect(m.shouldProcess("C123", "U1", "hey <@U_BOT> help me", "")).To(BeTrue())
		})

		It("processes all messages when respondTo is 'all'", func() {
			m := &Messenger{respondTo: "all", botUserID: "U_BOT"}
			Expect(m.shouldProcess("C123", "U1", "generic message", "")).To(BeTrue())
		})

		It("always processes DMs regardless of mode", func() {
			m := &Messenger{botUserID: "U_BOT"} // default = mentions
			Expect(m.shouldProcess("D123ABC", "U1", "hello", "")).To(BeTrue())
		})

		It("processes thread messages where bot was previously mentioned", func() {
			m := &Messenger{botUserID: "U_BOT"}
			// First mention in thread
			Expect(m.shouldProcess("C123", "U1", "<@U_BOT> help", "thread_ts_1")).To(BeTrue())
			// Subsequent message in same thread (no mention)
			Expect(m.shouldProcess("C123", "U1", "more context", "thread_ts_1")).To(BeTrue())
		})

		It("drops thread messages in untracked threads", func() {
			m := &Messenger{botUserID: "U_BOT"}
			Expect(m.shouldProcess("C123", "U1", "some reply", "thread_ts_2")).To(BeFalse())
		})

		It("drops messages from non-allowed senders", func() {
			m := &Messenger{
				botUserID:    "U_BOT",
				respondTo:    "all",
				allowedUsers: []string{"U_ADMIN"},
			}
			Expect(m.shouldProcess("C123", "U_OTHER", "<@U_BOT> do stuff", "")).To(BeFalse())
		})

		It("allows messages from allowed senders", func() {
			m := &Messenger{
				botUserID:    "U_BOT",
				allowedUsers: []string{"U_ADMIN"},
			}
			Expect(m.shouldProcess("C123", "U_ADMIN", "<@U_BOT> deploy", "")).To(BeTrue())
		})

		It("allows all senders when allowedUsers is empty", func() {
			m := &Messenger{botUserID: "U_BOT"}
			Expect(m.shouldProcess("C123", "ANYONE", "<@U_BOT> hi", "")).To(BeTrue())
		})
	})

	Describe("containsBotMention", func() {
		It("returns true when text contains bot mention", func() {
			m := &Messenger{botUserID: "U12345"}
			Expect(m.containsBotMention("hello <@U12345> world")).To(BeTrue())
		})

		It("returns false when text does not contain mention", func() {
			m := &Messenger{botUserID: "U12345"}
			Expect(m.containsBotMention("hello world")).To(BeFalse())
		})

		It("returns false when botUserID is empty", func() {
			m := &Messenger{}
			Expect(m.containsBotMention("hello <@U12345> world")).To(BeFalse())
		})

		It("returns false for partial mention", func() {
			m := &Messenger{botUserID: "U12345"}
			Expect(m.containsBotMention("hello <@U1234> world")).To(BeFalse())
		})
	})

	Describe("stripBotMention", func() {
		It("removes bot mention from text", func() {
			m := &Messenger{botUserID: "U_BOT"}
			Expect(m.stripBotMention("<@U_BOT> help me")).To(Equal("help me"))
		})

		It("removes bot mention with colon suffix", func() {
			m := &Messenger{botUserID: "U_BOT"}
			Expect(m.stripBotMention("<@U_BOT>: good day")).To(Equal("good day"))
		})

		It("removes bot mention mid-text", func() {
			m := &Messenger{botUserID: "U_BOT"}
			Expect(m.stripBotMention("hey <@U_BOT> deploy this")).To(Equal("hey  deploy this"))
		})

		It("returns text unchanged when botUserID is empty", func() {
			m := &Messenger{}
			Expect(m.stripBotMention("<@U_BOT> hello")).To(Equal("<@U_BOT> hello"))
		})

		It("returns text unchanged when no mention present", func() {
			m := &Messenger{botUserID: "U_BOT"}
			Expect(m.stripBotMention("just a regular message")).To(Equal("just a regular message"))
		})
	})
})
