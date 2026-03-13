// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/slack-go/slack/slackevents"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/messenger"
)

var _ = Describe("Events HTTP Handler", func() {
	Describe("eventsHTTPHandler.isDirectedAtBot", func() {
		It("returns true when respondTo is all", func() {
			h := &eventsHTTPHandler{respondTo: "all"}
			Expect(h.isDirectedAtBot("C123", "hello", "")).To(BeTrue())
		})

		It("returns true for DM channels", func() {
			h := &eventsHTTPHandler{botUserID: "U_BOT"}
			Expect(h.isDirectedAtBot("D123ABC", "hello", "")).To(BeTrue())
		})

		It("returns true when text contains bot mention", func() {
			h := &eventsHTTPHandler{botUserID: "U_BOT"}
			Expect(h.isDirectedAtBot("C123", "hey <@U_BOT> help", "")).To(BeTrue())
		})

		It("returns false for channel message without mention", func() {
			h := &eventsHTTPHandler{botUserID: "U_BOT"}
			Expect(h.isDirectedAtBot("C123", "hello world", "")).To(BeFalse())
		})

		It("returns false when botUserID is empty", func() {
			h := &eventsHTTPHandler{}
			Expect(h.isDirectedAtBot("C123", "<@U_BOT> help", "")).To(BeFalse())
		})
	})

	Describe("eventsHTTPHandler.stripBotMention", func() {
		It("removes bot mention", func() {
			h := &eventsHTTPHandler{botUserID: "U_BOT"}
			Expect(h.stripBotMention("<@U_BOT> help me")).To(Equal("help me"))
		})

		It("removes bot mention with colon", func() {
			h := &eventsHTTPHandler{botUserID: "U_BOT"}
			Expect(h.stripBotMention("<@U_BOT>: do something")).To(Equal("do something"))
		})

		It("returns text unchanged when botUserID is empty", func() {
			h := &eventsHTTPHandler{}
			Expect(h.stripBotMention("<@U_BOT> hello")).To(Equal("<@U_BOT> hello"))
		})
	})

	Describe("resolveThreadID", func() {
		It("returns threadTS when non-empty", func() {
			Expect(resolveThreadID("1234.5678", "9999.0000")).To(Equal("1234.5678"))
		})

		It("returns messageTS when threadTS is empty", func() {
			Expect(resolveThreadID("", "9999.0000")).To(Equal("9999.0000"))
		})
	})

	Describe("buildIncomingMessage", func() {
		It("builds a message with correct fields", func() {
			h := &eventsHTTPHandler{}
			ev := &slackevents.MessageEvent{
				TimeStamp:       "1234.5678",
				Channel:         "C_CHAN",
				User:            "U_USER",
				Text:            "hello bot",
				ThreadTimeStamp: "",
			}
			msg := h.buildIncomingMessage(context.Background(), messageParams{
				event:       ev,
				cleanText:   "hello bot",
				threadID:    "1234.5678",
				senderID:    "user@example.com",
				displayName: "Test User",
			})

			Expect(msg.ID).To(Equal("1234.5678"))
			Expect(msg.Platform).To(Equal(messenger.PlatformSlack))
			Expect(msg.Channel.ID).To(Equal("C_CHAN"))
			Expect(msg.Sender.ID).To(Equal("user@example.com"))
			Expect(msg.Sender.DisplayName).To(Equal("Test User"))
			Expect(msg.Content.Text).To(Equal("hello bot"))
			Expect(msg.ThreadID).To(Equal("1234.5678"))
			Expect(msg.Metadata).To(BeNil())
		})

		It("sets quoted_message_id metadata for thread replies", func() {
			h := &eventsHTTPHandler{}
			ev := &slackevents.MessageEvent{
				TimeStamp:       "1111.2222",
				Channel:         "C_CHAN",
				User:            "U_USER",
				ThreadTimeStamp: "0000.1111",
			}
			msg := h.buildIncomingMessage(context.Background(), messageParams{
				event:       ev,
				cleanText:   "reply",
				threadID:    "0000.1111",
				senderID:    "U_USER",
				displayName: "U_USER",
			})

			Expect(msg.Metadata).To(HaveKeyWithValue(
				messenger.QuotedMessageID, "C_CHAN:0000.1111",
			))
		})

		It("calls resolveUser when set", func() {
			h := &eventsHTTPHandler{
				resolveUser: func(_ context.Context, _ string) (string, string) {
					return "resolved@email.com", "Resolved Name"
				},
			}
			ev := &slackevents.MessageEvent{
				TimeStamp: "1234.5678",
				Channel:   "C_CHAN",
				User:      "U_USER",
			}
			msg := h.buildIncomingMessage(context.Background(), messageParams{
				event:       ev,
				cleanText:   "text",
				threadID:    "1234.5678",
				senderID:    "U_USER",
				displayName: "U_USER",
			})

			Expect(msg.Sender.ID).To(Equal("resolved@email.com"))
			Expect(msg.Sender.DisplayName).To(Equal("Resolved Name"))
		})
	})

	Describe("ServeHTTP", func() {
		It("rejects non-POST requests", func() {
			h := &eventsHTTPHandler{}
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			Expect(w.Code).To(Equal(http.StatusMethodNotAllowed))
		})

		It("responds to URL verification challenge", func() {
			challenge := map[string]string{
				"type":      "url_verification",
				"token":     "test-token",
				"challenge": "test-challenge-value",
			}
			body, _ := json.Marshal(challenge)
			h := &eventsHTTPHandler{}
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Body.String()).To(Equal("test-challenge-value"))
		})

		It("rejects requests with invalid signing secret", func() {
			h := &eventsHTTPHandler{signingSecret: "secret123"}
			body := []byte(`{"type":"event_callback"}`)
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusUnauthorized))
		})

		It("returns 200 for valid callback event with bot message (skipped)", func() {
			incoming := make(chan messenger.IncomingMessage, 10)
			h := &eventsHTTPHandler{
				incoming:  incoming,
				respondTo: "all",
			}

			// Use raw JSON matching Slack's actual Events API format.
			body := []byte(`{
				"type": "event_callback",
				"event": {
					"type": "message",
					"bot_id": "B_BOT",
					"channel": "C_CHAN",
					"user": "U_USER",
					"text": "bot message",
					"ts": "1234.5678"
				}
			}`)
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			// Bot messages are skipped — channel should be empty.
			Expect(incoming).To(BeEmpty())
		})

		It("processes a valid user message and enqueues it", func() {
			incoming := make(chan messenger.IncomingMessage, 10)
			h := &eventsHTTPHandler{
				incoming:  incoming,
				respondTo: "all",
			}

			body := []byte(`{
				"type": "event_callback",
				"event": {
					"type": "message",
					"channel": "C_TEST",
					"user": "U_SENDER",
					"text": "hello genie",
					"ts": "1111.2222"
				}
			}`)
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(incoming).To(HaveLen(1))
			msg := <-incoming
			Expect(msg.Content.Text).To(Equal("hello genie"))
			Expect(msg.Channel.ID).To(Equal("C_TEST"))
			Expect(msg.Sender.Username).To(Equal("U_SENDER"))
			Expect(msg.ThreadID).To(Equal("1111.2222"))
		})

		It("rejects unauthorized users when allowlist is set", func() {
			incoming := make(chan messenger.IncomingMessage, 10)
			h := &eventsHTTPHandler{
				incoming:     incoming,
				respondTo:    "all",
				allowedUsers: []string{"U_ALLOWED"},
			}

			body := []byte(`{
				"type": "event_callback",
				"event": {
					"type": "message",
					"channel": "C_TEST",
					"user": "U_INTRUDER",
					"text": "sneaky",
					"ts": "3333.4444"
				}
			}`)
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(incoming).To(BeEmpty())
		})

		It("skips non-directed messages in mention-only mode", func() {
			incoming := make(chan messenger.IncomingMessage, 10)
			h := &eventsHTTPHandler{
				incoming:  incoming,
				botUserID: "U_BOT",
			}

			body := []byte(`{
				"type": "event_callback",
				"event": {
					"type": "message",
					"channel": "C_TEST",
					"user": "U_USER",
					"text": "general chat",
					"ts": "5555.6666"
				}
			}`)
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(incoming).To(BeEmpty())
		})

		It("routes interactive payloads", func() {
			incoming := make(chan messenger.IncomingMessage, 10)
			h := &eventsHTTPHandler{incoming: incoming}

			payload := `{"type":"block_actions","user":{"id":"U1","name":"user1"},"channel":{"id":"C1","name":"general"},"message":{"ts":"1234.5678"},"actions":[{"action_id":"approve_1","block_id":"blk_1","value":"val1","type":"button"}],"response_url":"https://hooks.slack.com/actions/T00/B00/XXX"}`
			body := []byte("payload=" + payload)

			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(incoming).To(HaveLen(1))
			msg := <-incoming
			Expect(msg.Type).To(Equal(messenger.MessageTypeInteraction))
			Expect(msg.Interaction.ActionID).To(Equal("approve_1"))
			Expect(msg.Interaction.ActionValue).To(Equal("val1"))
		})
	})

	Describe("ConnectionInfo", func() {
		It("returns HTTP Events API info when signing secret set", func() {
			m := &Messenger{cfg: Config{SigningSecret: "secret"}}
			Expect(m.ConnectionInfo()).To(ContainSubstring("HTTP Events API"))
		})

		It("returns Socket Mode info when no signing secret", func() {
			m := &Messenger{cfg: Config{}}
			Expect(m.ConnectionInfo()).To(ContainSubstring("Socket Mode"))
		})
	})

	Describe("urlDecode", func() {
		It("decodes valid url", func() {
			res, err := urlDecode("a+b%20c")
			Expect(err).To(Not(HaveOccurred()))
			Expect(res).To(Equal("a b c"))
		})
		It("fails on incomplete encoding", func() {
			_, err := urlDecode("a%")
			Expect(err).To(HaveOccurred())
		})
		It("fails on invalid hex", func() {
			_, err := urlDecode("a%2z")
			Expect(err).To(HaveOccurred())
		})
		It("returns empty string for empty input", func() {
			res, err := urlDecode("")
			Expect(err).To(Not(HaveOccurred()))
			Expect(res).To(Equal(""))
		})
		It("decodes special characters", func() {
			res, err := urlDecode("%7B%22key%22%3A%22value%22%7D")
			Expect(err).To(Not(HaveOccurred()))
			Expect(res).To(Equal(`{"key":"value"}`))
		})
	})

	Describe("unhex", func() {
		It("returns correctly for digits", func() {
			val, ok := unhex('0')
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal(byte(0)))
		})
		It("returns correctly for lowercase hex", func() {
			val, ok := unhex('a')
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal(byte(10)))
		})
		It("returns correctly for uppercase hex", func() {
			val, ok := unhex('F')
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal(byte(15)))
		})
		It("fails for non-hex characters", func() {
			_, ok := unhex('z')
			Expect(ok).To(BeFalse())
		})
	})

	Describe("verifySignature", func() {
		It("returns true for valid signature", func() {
			h := &eventsHTTPHandler{signingSecret: "8f742231b10e8888abcd99yyyzzz85a5"}
			req, _ := http.NewRequest("POST", "/", bytes.NewReader([]byte("foobar")))
			req.Header.Set("X-Slack-Request-Timestamp", "1531420618")
			req.Header.Set("X-Slack-Signature", "v0=0f634490448c74fb103758753b814d0857d227fe078460d71076ea744b8013d6")

			Expect(h.verifySignature(req, []byte("foobar"))).To(BeTrue())
		})
		It("returns false on missing headers", func() {
			h := &eventsHTTPHandler{signingSecret: "secret"}
			req, _ := http.NewRequest("POST", "/", bytes.NewReader([]byte("foobar")))
			Expect(h.verifySignature(req, []byte("foobar"))).To(BeFalse())
		})
		It("returns false on wrong signature", func() {
			h := &eventsHTTPHandler{signingSecret: "secret"}
			req, _ := http.NewRequest("POST", "/", bytes.NewReader([]byte("foobar")))
			req.Header.Set("X-Slack-Request-Timestamp", "1531420618")
			req.Header.Set("X-Slack-Signature", "v0=invalid")
			Expect(h.verifySignature(req, []byte("foobar"))).To(BeFalse())
		})
	})

	Describe("handleInteractiveHTTP edge cases", func() {
		It("ignores payloads without payload= prefix", func() {
			incoming := make(chan messenger.IncomingMessage, 10)
			h := &eventsHTTPHandler{incoming: incoming}
			h.handleInteractiveHTTP(context.Background(), []byte("notapayload"))
			Expect(incoming).To(BeEmpty())
		})

		It("ignores payloads with invalid JSON", func() {
			incoming := make(chan messenger.IncomingMessage, 10)
			h := &eventsHTTPHandler{incoming: incoming}
			h.handleInteractiveHTTP(context.Background(), []byte("payload={invalid"))
			Expect(incoming).To(BeEmpty())
		})

		It("ignores payloads with no actions", func() {
			incoming := make(chan messenger.IncomingMessage, 10)
			h := &eventsHTTPHandler{incoming: incoming}
			payload := `payload={"type":"block_actions","user":{"id":"U1","name":"user1"},"channel":{"id":"C1"},"message":{"ts":"1234.5678"},"actions":[]}`
			h.handleInteractiveHTTP(context.Background(), []byte(payload))
			Expect(incoming).To(BeEmpty())
		})
	})
})
