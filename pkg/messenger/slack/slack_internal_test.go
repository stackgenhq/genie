// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"

	slack "github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/messenger"
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

	Describe("isDirectedAtBot", func() {
		It("defaults to mention-only mode (drops non-mention messages)", func() {
			m := &Messenger{botUserID: "U_BOT"}
			Expect(m.isDirectedAtBot("C123", "hello world", "")).To(BeFalse())
		})

		It("processes messages with bot @mention in default mode", func() {
			m := &Messenger{botUserID: "U_BOT"}
			Expect(m.isDirectedAtBot("C123", "hey <@U_BOT> help me", "")).To(BeTrue())
		})

		It("processes all messages when respondTo is 'all'", func() {
			m := &Messenger{respondTo: "all", botUserID: "U_BOT"}
			Expect(m.isDirectedAtBot("C123", "generic message", "")).To(BeTrue())
		})

		It("always processes DMs regardless of mode", func() {
			m := &Messenger{botUserID: "U_BOT"} // default = mentions
			Expect(m.isDirectedAtBot("D123ABC", "hello", "")).To(BeTrue())
		})

		It("drops thread messages even if bot was previously mentioned (no thread tracking)", func() {
			m := &Messenger{botUserID: "U_BOT"}
			// First mention in thread
			Expect(m.isDirectedAtBot("C123", "<@U_BOT> help", "thread_ts_1")).To(BeTrue())
			// Subsequent message in same thread WITHOUT mention should be dropped
			Expect(m.isDirectedAtBot("C123", "more context", "thread_ts_1")).To(BeFalse())
		})
	})

	Describe("isUserAllowed", func() {
		It("returns false when allowlist is empty", func() {
			Expect(isUserAllowed("U_ANYONE", nil)).To(BeFalse())
			Expect(isUserAllowed("U_ANYONE", []string{})).To(BeFalse())
		})

		It("returns true for exact match", func() {
			Expect(isUserAllowed("U_ADMIN", []string{"U_ADMIN"})).To(BeTrue())
		})

		It("returns false for non-matching user", func() {
			Expect(isUserAllowed("U_USER", []string{"U_ADMIN"})).To(BeFalse())
		})

		It("returns true for wildcard match", func() {
			Expect(isUserAllowed("U_ADMIN_123", []string{"U_ADMIN*"})).To(BeTrue())
		})

		It("returns false for non-matching wildcard", func() {
			Expect(isUserAllowed("U_USER_123", []string{"U_ADMIN*"})).To(BeFalse())
		})

		It("supports multiple allowed users including wildcards", func() {
			allowed := []string{"U_EXACT", "U_ADMIN*"}
			Expect(isUserAllowed("U_EXACT", allowed)).To(BeTrue())
			Expect(isUserAllowed("U_ADMIN_NEW", allowed)).To(BeTrue())
			Expect(isUserAllowed("U_USER", allowed)).To(BeFalse())
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

	Describe("resolveUserInfo", func() {
		It("returns userID for both fields when api is nil", func() {
			m := &Messenger{}
			email, displayName := m.resolveUserInfo(context.Background(), "U_USER")
			Expect(email).To(Equal("U_USER"))
			Expect(displayName).To(Equal("U_USER"))
		})

		It("returns cached result on second call", func() {
			m := &Messenger{}
			m.userInfoCache.Store("U_CACHED", cachedUserInfo{
				email:       "cached@example.com",
				displayName: "Cached User",
			})
			email, displayName := m.resolveUserInfo(context.Background(), "U_CACHED")
			Expect(email).To(Equal("cached@example.com"))
			Expect(displayName).To(Equal("Cached User"))
		})

		It("resolves user info from API and caches the result", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "users.info") {
					w.Write([]byte(`{"ok":true,"user":{"id":"U_TEST","name":"testuser","real_name":"Test User","profile":{"email":"test@example.com","display_name":"TestDisplay"}}}`))
					return
				}
				w.Write([]byte(`{"ok":true}`))
			}))
			defer srv.Close()

			m := &Messenger{
				api: slack.New("xoxb-test", slack.OptionAPIURL(srv.URL+"/")),
			}
			email, displayName := m.resolveUserInfo(context.Background(), "U_TEST")
			Expect(email).To(Equal("test@example.com"))
			Expect(displayName).To(Equal("TestDisplay"))

			// Second call should use cache.
			email2, displayName2 := m.resolveUserInfo(context.Background(), "U_TEST")
			Expect(email2).To(Equal("test@example.com"))
			Expect(displayName2).To(Equal("TestDisplay"))
		})

		It("falls back to real_name when display_name is empty", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "users.info") {
					w.Write([]byte(`{"ok":true,"user":{"id":"U_TEST","name":"testuser","real_name":"Real Name","profile":{"email":"test@example.com","display_name":""}}}`))
					return
				}
				w.Write([]byte(`{"ok":true}`))
			}))
			defer srv.Close()

			m := &Messenger{
				api: slack.New("xoxb-test", slack.OptionAPIURL(srv.URL+"/")),
			}
			email, displayName := m.resolveUserInfo(context.Background(), "U_TEST")
			Expect(email).To(Equal("test@example.com"))
			Expect(displayName).To(Equal("Real Name"))
		})

		It("falls back to userID on API error", func() {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "users.info") {
					w.Write([]byte(`{"ok":false,"error":"user_not_found"}`))
					return
				}
				w.Write([]byte(`{"ok":true}`))
			}))
			defer srv.Close()

			m := &Messenger{
				api: slack.New("xoxb-test", slack.OptionAPIURL(srv.URL+"/")),
			}
			email, displayName := m.resolveUserInfo(context.Background(), "U_UNKNOWN")
			Expect(email).To(Equal("U_UNKNOWN"))
			Expect(displayName).To(Equal("U_UNKNOWN"))
		})
	})

	Describe("handleEventsAPI", func() {
		It("skips bot messages", func() {
			incoming := make(chan messenger.IncomingMessage, 10)
			m := &Messenger{
				incoming:  incoming,
				respondTo: respondToAll,
			}

			event := slackevents.EventsAPIEvent{
				Type: slackevents.CallbackEvent,
				InnerEvent: slackevents.EventsAPIInnerEvent{
					Data: &slackevents.MessageEvent{
						BotID:     "B_BOT",
						Channel:   "C1",
						User:      "U1",
						Text:      "echo",
						TimeStamp: "1.1",
					},
				},
			}
			m.handleEventsAPI(context.Background(), event)
			Expect(incoming).To(BeEmpty())
		})

		It("enqueues valid user messages", func() {
			incoming := make(chan messenger.IncomingMessage, 10)
			m := &Messenger{
				incoming:  incoming,
				respondTo: respondToAll,
			}

			event := slackevents.EventsAPIEvent{
				Type: slackevents.CallbackEvent,
				InnerEvent: slackevents.EventsAPIInnerEvent{
					Data: &slackevents.MessageEvent{
						Channel:   "C1",
						User:      "U1",
						Text:      "hello",
						TimeStamp: "2.2",
					},
				},
			}
			m.handleEventsAPI(context.Background(), event)
			Expect(incoming).To(HaveLen(1))
			msg := <-incoming
			Expect(msg.Content.Text).To(Equal("hello"))
			Expect(msg.Channel.ID).To(Equal("C1"))
			Expect(msg.ThreadID).To(Equal("2.2"))
		})

		It("rejects unauthorized users", func() {
			incoming := make(chan messenger.IncomingMessage, 10)
			m := &Messenger{
				incoming:     incoming,
				respondTo:    respondToAll,
				allowedUsers: []string{"U_ALLOWED"},
			}

			event := slackevents.EventsAPIEvent{
				Type: slackevents.CallbackEvent,
				InnerEvent: slackevents.EventsAPIInnerEvent{
					Data: &slackevents.MessageEvent{
						Channel:   "C1",
						User:      "U_INTRUDER",
						Text:      "sneaky",
						TimeStamp: "3.3",
					},
				},
			}
			m.handleEventsAPI(context.Background(), event)
			Expect(incoming).To(BeEmpty())
		})

		It("ignores non-callback event types", func() {
			incoming := make(chan messenger.IncomingMessage, 10)
			m := &Messenger{incoming: incoming}

			event := slackevents.EventsAPIEvent{
				Type: "url_verification",
			}
			m.handleEventsAPI(context.Background(), event)
			Expect(incoming).To(BeEmpty())
		})
	})
})
