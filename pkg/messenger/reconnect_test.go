// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package messenger_test

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/messenger/messengerfakes"
)

var _ = Describe("ReceiveWithReconnect", func() {
	var fakeMessenger *messengerfakes.FakeMessenger
	BeforeEach(func() {
		fakeMessenger = &messengerfakes.FakeMessenger{}
	})
	It("forwards messages from a healthy connection", func() {
		ch := make(chan messenger.IncomingMessage, 5)
		fakeMessenger.ReceiveStub = func(_ context.Context) (<-chan messenger.IncomingMessage, error) {
			return ch, nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		relay := messenger.ReceiveWithReconnect(ctx, fakeMessenger, 10*time.Millisecond, 50*time.Millisecond)

		// Send a message through the mock channel.
		ch <- messenger.IncomingMessage{
			Platform: "test",
			Content:  messenger.MessageContent{Text: "hello"},
		}

		Eventually(relay).Should(Receive(HaveField("Content.Text", "hello")))
	})

	It("reconnects after the channel closes (disconnect)", func() {
		// First connection: delivers one message then closes.
		ch1 := make(chan messenger.IncomingMessage, 5)
		// Second connection: delivers another message.
		ch2 := make(chan messenger.IncomingMessage, 5)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		fakeMessenger.ReceiveReturnsOnCall(0, ch1, nil)
		fakeMessenger.ReceiveReturnsOnCall(1, ch2, nil)

		relay := messenger.ReceiveWithReconnect(ctx, fakeMessenger, 10*time.Millisecond, 50*time.Millisecond)

		// Send on first connection and verify.
		ch1 <- messenger.IncomingMessage{
			Platform: "test",
			Content:  messenger.MessageContent{Text: "msg1"},
		}
		Eventually(relay).Should(Receive(HaveField("Content.Text", "msg1")))

		// Close first connection to simulate disconnect.
		close(ch1)

		// Send on second connection after reconnect.
		// Wait briefly for the reconnect goroutine to call Receive() again.
		ch2 <- messenger.IncomingMessage{
			Platform: "test",
			Content:  messenger.MessageContent{Text: "msg2"},
		}
		Eventually(relay).Should(Receive(HaveField("Content.Text", "msg2")))
	})

	It("retries with backoff on Receive() error", func() {
		ch := make(chan messenger.IncomingMessage, 5)
		fakeMessenger.ReceiveReturnsOnCall(0, nil, errors.New("connection refused"))
		fakeMessenger.ReceiveReturnsOnCall(1, ch, nil)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		relay := messenger.ReceiveWithReconnect(ctx, fakeMessenger, 10*time.Millisecond, 50*time.Millisecond)

		ch <- messenger.IncomingMessage{
			Platform: "test",
			Content:  messenger.MessageContent{Text: "after-retry"},
		}
		Eventually(relay, 2*time.Second).Should(Receive(HaveField("Content.Text", "after-retry")))
	})

	It("closes relay when context is cancelled", func() {
		ch := make(chan messenger.IncomingMessage)
		fakeMessenger.ReceiveReturnsOnCall(0, ch, nil)
		ctx, cancel := context.WithCancel(context.Background())
		relay := messenger.ReceiveWithReconnect(ctx, fakeMessenger, 10*time.Millisecond, 50*time.Millisecond)

		// Cancel context
		cancel()

		// Relay should close
		Eventually(relay).Should(BeClosed())
	})
})
