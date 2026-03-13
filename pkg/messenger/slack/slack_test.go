// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package slack_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/messenger"
	slackmsg "github.com/stackgenhq/genie/pkg/messenger/slack"
)

var _ = Describe("Slack Messenger", func() {
	Describe("New", func() {
		It("should create a messenger with empty config (no validation on constructor)", func() {
			m := slackmsg.New(slackmsg.Config{}, "", nil)
			Expect(m).NotTo(BeNil())
		})

		It("should create a messenger with valid config", func() {
			m := slackmsg.New(slackmsg.Config{
				AppToken: "xapp-1-test-token",
				BotToken: "xoxb-test-token",
			}, "", nil)
			Expect(m).NotTo(BeNil())
		})

		It("should accept functional options", func() {
			m := slackmsg.New(slackmsg.Config{
				AppToken: "xapp-1-test-token",
				BotToken: "xoxb-test-token",
			}, "", nil, messenger.WithMessageBuffer(500))
			Expect(m).NotTo(BeNil())
		})
	})

	Describe("Connect", func() {
		var (
			mockServer *httptest.Server
			m          *slackmsg.Messenger
		)

		BeforeEach(func() {
			// Mock Slack API server
			mockServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "auth.test") {
					// Simulate auth.test failure
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(`{"ok": false, "error": "invalid_auth"}`))
					return
				}

				// Standard success response for other endpoints
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"ok": true}`))
			}))
		})

		AfterEach(func() {
			mockServer.Close()
			if m != nil {
				_ = m.Disconnect(context.Background())
			}
		})

		It("should fall back to respondTo=all when auth.test fails", func() {
			// Initialize with mentions mode
			m = slackmsg.New(slackmsg.Config{
				AppToken: "xapp-1-test",
				BotToken: "xoxb-1-test",
				APIURL:   mockServer.URL + "/",
			}, "mentions", nil)

			// Should connect successfully despite auth.test failure
			handler, err := m.Connect(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(handler).To(BeNil())

			// We cannot directly inspect `respondTo` since it's private and we don't have
			// an exported getter, but we know it won't crash and we have hit the branch.
			// The internal test suite directly checks the shouldProcess method behavior.
		})
	})

	Describe("Platform", func() {
		It("should return PlatformSlack", func() {
			m := slackmsg.New(slackmsg.Config{}, "", nil)
			Expect(m.Platform()).To(Equal(messenger.PlatformSlack))
		})
	})

	Describe("Connection state guards", func() {
		var m *slackmsg.Messenger

		BeforeEach(func() {
			m = slackmsg.New(slackmsg.Config{
				AppToken: "xapp-1-test-token",
				BotToken: "xoxb-test-token",
			}, "", nil)
		})

		It("should return ErrNotConnected when Send is called before Connect", func() {
			_, err := m.Send(context.Background(), messenger.SendRequest{
				Channel: messenger.Channel{ID: "C12345"},
				Content: messenger.MessageContent{Text: "test"},
			})
			Expect(err).To(MatchError(messenger.ErrNotConnected))
		})

		It("should return ErrNotConnected when Receive is called before Connect", func() {
			ch, err := m.Receive(context.Background())
			Expect(err).To(MatchError(messenger.ErrNotConnected))
			Expect(ch).To(BeNil())
		})

		It("should return ErrNotConnected when Disconnect is called before Connect", func() {
			err := m.Disconnect(context.Background())
			Expect(err).To(MatchError(messenger.ErrNotConnected))
		})
	})

	Describe("Interface compliance", func() {
		It("should satisfy the messenger.Messenger interface", func() {
			var _ messenger.Messenger = slackmsg.New(slackmsg.Config{}, "", nil)
		})
	})
})
