package messenger

import (
	"context"
	"time"

	"github.com/stackgenhq/genie/pkg/logger"
)

// ReceiveWithReconnect wraps Messenger.Receive() in a reconnection loop with
// exponential backoff. When the platform connection drops (channel closes),
// it retries Receive() automatically. The returned relay channel stays open
// until ctx is cancelled.
//
// Parameters:
//   - initialBackoff: starting delay between reconnection attempts (e.g. 1s)
//   - maxBackoff: maximum delay cap (e.g. 30s)
func ReceiveWithReconnect(ctx context.Context, msgr Messenger, initialBackoff, maxBackoff time.Duration) <-chan IncomingMessage {
	log := logger.GetLogger(ctx).With("fn", "ReceiveWithReconnect")
	relay := make(chan IncomingMessage, 100)

	go func() {
		defer close(relay)

		const multiplier = 2
		backoff := initialBackoff

		for {
			ch, err := msgr.Receive(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return // context cancelled
				}
				log.Warn("messenger Receive failed, retrying",
					"error", err, "backoff", backoff)

				timer := time.NewTimer(backoff)
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					return
				}
				backoff = min(backoff*multiplier, maxBackoff)
				continue
			}

			// Reset backoff on successful connection.
			backoff = initialBackoff
			log.Info("messenger connected, listening for messages")

			// Forward messages until the channel is closed (disconnect).
			for {
				select {
				case msg, ok := <-ch:
					if !ok {
						log.Warn("messenger disconnected, attempting reconnection")
						goto reconnect
					}
					select {
					case relay <- msg:
					case <-ctx.Done():
						return
					}
				case <-ctx.Done():
					return
				}
			}
		reconnect:
		}
	}()

	return relay
}
