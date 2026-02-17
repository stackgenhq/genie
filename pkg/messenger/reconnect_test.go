package messenger_test

import (
	"context"
	"errors"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/messenger"
)

// mockReconnectMessenger is a test double that lets us control reconnection
// behaviour: each call to Receive() pops the next result from the queue.
type mockReconnectMessenger struct {
	mu      sync.Mutex
	calls   []reconnectCall
	callIdx int
}

type reconnectCall struct {
	ch  chan messenger.IncomingMessage
	err error
}

func (m *mockReconnectMessenger) Connect(_ context.Context) error { return nil }

func (m *mockReconnectMessenger) Disconnect(_ context.Context) error { return nil }

func (m *mockReconnectMessenger) Send(_ context.Context, _ messenger.SendRequest) (messenger.SendResponse, error) {
	return messenger.SendResponse{}, nil
}

func (m *mockReconnectMessenger) Receive(_ context.Context) (<-chan messenger.IncomingMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.callIdx >= len(m.calls) {
		// Block forever if no more calls queued — prevents tight-loop spin.
		return make(chan messenger.IncomingMessage), nil
	}
	call := m.calls[m.callIdx]
	m.callIdx++
	if call.err != nil {
		return nil, call.err
	}
	return call.ch, nil
}

func (m *mockReconnectMessenger) Platform() messenger.Platform { return "test" }
func (m *mockReconnectMessenger) FormatApproval(req messenger.SendRequest, _ messenger.ApprovalInfo) messenger.SendRequest {
	return req
}

func (m *mockReconnectMessenger) Close() error { return nil }

var _ = Describe("ReceiveWithReconnect", func() {
	It("forwards messages from a healthy connection", func() {
		ch := make(chan messenger.IncomingMessage, 5)
		mock := &mockReconnectMessenger{
			calls: []reconnectCall{{ch: ch}},
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		relay := messenger.ReceiveWithReconnect(ctx, mock, 10*time.Millisecond, 50*time.Millisecond)

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

		mock := &mockReconnectMessenger{
			calls: []reconnectCall{
				{ch: ch1},
				{ch: ch2},
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		relay := messenger.ReceiveWithReconnect(ctx, mock, 10*time.Millisecond, 50*time.Millisecond)

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
		mock := &mockReconnectMessenger{
			calls: []reconnectCall{
				{err: errors.New("connection refused")}, // first call fails
				{ch: ch},                                // second call succeeds
			},
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		relay := messenger.ReceiveWithReconnect(ctx, mock, 10*time.Millisecond, 50*time.Millisecond)

		ch <- messenger.IncomingMessage{
			Platform: "test",
			Content:  messenger.MessageContent{Text: "after-retry"},
		}
		Eventually(relay, 2*time.Second).Should(Receive(HaveField("Content.Text", "after-retry")))
	})

	It("closes relay when context is cancelled", func() {
		ch := make(chan messenger.IncomingMessage)
		mock := &mockReconnectMessenger{
			calls: []reconnectCall{{ch: ch}},
		}

		ctx, cancel := context.WithCancel(context.Background())
		relay := messenger.ReceiveWithReconnect(ctx, mock, 10*time.Millisecond, 50*time.Millisecond)

		// Cancel context
		cancel()

		// Relay should close
		Eventually(relay).Should(BeClosed())
	})
})
