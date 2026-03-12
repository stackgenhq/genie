// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package mcp_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	aguitypes "github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/credstore"
	"github.com/stackgenhq/genie/pkg/credstore/credstorefakes"
	"github.com/stackgenhq/genie/pkg/mcp"
	"github.com/stackgenhq/genie/pkg/messenger"
)

var _ = Describe("DummyAuthTool", func() {
	var (
		fakeStore *credstorefakes.FakeStore
		ctx       context.Context
	)

	BeforeEach(func() {
		fakeStore = &credstorefakes.FakeStore{}
		ctx = context.Background()
	})

	Describe("NewDummyAuthTool", func() {
		It("returns a tool with the correct name", func() {
			t := mcp.NewDummyAuthTool("StackGen", fakeStore)
			Expect(t.Declaration().Name).To(Equal("stackgen_connect"))
		})

		It("returns a tool with the correct description", func() {
			t := mcp.NewDummyAuthTool("StackGen", fakeStore)
			Expect(t.Declaration().Description).To(ContainSubstring("StackGen"))
			Expect(t.Declaration().Description).To(ContainSubstring("sign in"))
		})

		It("returns a valid declaration", func() {
			t := mcp.NewDummyAuthTool("GitHub", fakeStore)
			decl := t.Declaration()
			Expect(decl).NotTo(BeNil())
			Expect(decl.Name).To(Equal("github_connect"))
			Expect(decl.InputSchema).NotTo(BeNil())
			Expect(decl.InputSchema.Type).To(Equal("object"))
		})
	})

	Describe("Call", func() {
		// callTool is a helper that casts to *DummyAuthTool and calls Call.
		callTool := func(t interface {
			Call(context.Context, []byte) (any, error)
		}, callerCtx context.Context) (any, error) {
			return t.Call(callerCtx, nil)
		}

		Context("when token is already available", func() {
			It("returns success message", func() {
				fakeStore.GetTokenReturns(&credstore.Token{AccessToken: "abc"}, nil)
				t := mcp.NewDummyAuthTool("StackGen", fakeStore).(*mcp.DummyAuthTool)
				result, err := callTool(t, ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(ContainSubstring("Successfully authenticated"))
				Expect(result).To(ContainSubstring("StackGen"))
			})
		})

		Context("when authentication is required", func() {
			BeforeEach(func() {
				fakeStore.GetTokenReturns(nil, &credstore.AuthRequiredError{
					AuthURL:     "https://example.com/auth?code=xyz",
					ServiceName: "stackgen",
				})
			})

			It("returns the sign-in message on first call", func() {
				t := mcp.NewDummyAuthTool("StackGen", fakeStore).(*mcp.DummyAuthTool)
				result, err := callTool(t, ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(ContainSubstring("Authentication required"))
				Expect(result).To(ContainSubstring("StackGen"))
			})

			It("returns an error on second call to prevent looping", func() {
				t := mcp.NewDummyAuthTool("StackGen", fakeStore).(*mcp.DummyAuthTool)

				// First call — succeeds with auth URL
				_, err := callTool(t, ctx)
				Expect(err).NotTo(HaveOccurred())

				// Second call — should error
				_, err = callTool(t, ctx)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("already in progress"))
			})

			It("resets the called flag after successful authentication", func() {
				t := mcp.NewDummyAuthTool("StackGen", fakeStore).(*mcp.DummyAuthTool)

				// First call — auth required
				_, err := callTool(t, ctx)
				Expect(err).NotTo(HaveOccurred())

				// Simulate successful token retrieval
				fakeStore.GetTokenReturns(&credstore.Token{AccessToken: "new-token"}, nil)

				// Third call — should succeed again
				result, err := callTool(t, ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(result).To(ContainSubstring("Successfully authenticated"))
			})

			It("emits UserActionRequiredMsg via agui.Emit", func() {
				// Register a channel on the global event bus so we can
				// capture events emitted by the tool.
				eventChan := make(chan interface{}, 10)
				origin := messenger.MessageOrigin{
					Platform: "test",
					Channel: messenger.Channel{
						ID: "test-channel-dummy",
					},
					Sender: messenger.Sender{
						ID: "test-sender",
					},
				}
				aguitypes.Register(origin, eventChan)
				defer aguitypes.Deregister(origin)

				ctxWithOrigin := messenger.WithMessageOrigin(ctx, origin)

				t := mcp.NewDummyAuthTool("StackGen", fakeStore).(*mcp.DummyAuthTool)
				_, err := callTool(t, ctxWithOrigin)
				Expect(err).NotTo(HaveOccurred())

				// Should have emitted exactly one UserActionRequiredMsg.
				Eventually(eventChan).Should(Receive(BeAssignableToTypeOf(aguitypes.UserActionRequiredMsg{})))

				// Drain and re-create to test fresh
				t2 := mcp.NewDummyAuthTool("GitHub", fakeStore).(*mcp.DummyAuthTool)
				fakeStore.GetTokenReturns(nil, &credstore.AuthRequiredError{
					AuthURL:     "https://github.com/login/oauth",
					ServiceName: "github",
				})
				_, err = callTool(t2, ctxWithOrigin)
				Expect(err).NotTo(HaveOccurred())

				var emittedEvent aguitypes.UserActionRequiredMsg
				Eventually(eventChan).Should(Receive(&emittedEvent))
				Expect(emittedEvent.Action).To(Equal("oauth_login"))
				Expect(emittedEvent.Service).To(Equal("GitHub"))
				Expect(emittedEvent.URL).To(Equal("https://github.com/login/oauth"))
				Expect(emittedEvent.Message).To(ContainSubstring("GitHub"))
			})

			It("does not emit event on second call (loop prevention)", func() {
				eventChan := make(chan interface{}, 10)
				origin := messenger.MessageOrigin{
					Platform: "test",
					Channel: messenger.Channel{
						ID: "test-channel-dummy-2",
					},
					Sender: messenger.Sender{
						ID: "test-sender",
					},
				}
				aguitypes.Register(origin, eventChan)
				defer aguitypes.Deregister(origin)

				ctxWithOrigin := messenger.WithMessageOrigin(ctx, origin)

				t := mcp.NewDummyAuthTool("StackGen", fakeStore).(*mcp.DummyAuthTool)

				// First call — emits event
				_, _ = callTool(t, ctxWithOrigin)
				Eventually(eventChan).Should(Receive())

				// Second call — should NOT emit event (returns error instead)
				_, err := callTool(t, ctxWithOrigin)
				Expect(err).To(HaveOccurred())
				Consistently(eventChan).ShouldNot(Receive())
			})
		})

		Context("when a non-auth error occurs", func() {
			It("returns the error directly", func() {
				fakeStore.GetTokenReturns(nil, credstore.ErrNoToken)
				t := mcp.NewDummyAuthTool("StackGen", fakeStore).(*mcp.DummyAuthTool)
				_, err := callTool(t, ctx)
				Expect(err).To(HaveOccurred())
				Expect(err).To(Equal(credstore.ErrNoToken))
			})
		})
	})

	Describe("Name formatting", func() {
		It("lowercases the server name", func() {
			t := mcp.NewDummyAuthTool("MyServer", fakeStore)
			Expect(t.Declaration().Name).To(Equal("myserver_connect"))
		})

		It("handles already lowercase names", func() {
			t := mcp.NewDummyAuthTool("github", fakeStore)
			Expect(t.Declaration().Name).To(Equal("github_connect"))
		})
	})
})
