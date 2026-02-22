package messenger

import (
	"context"
	"fmt"

	"github.com/appcd-dev/genie/pkg/logger"
)

// LoggingMessenger wraps any Messenger implementation and logs all Send and
// Receive activity. This provides platform-agnostic observability for all
// outgoing and incoming messages without requiring each adapter to duplicate
// logging code.
type LoggingMessenger struct {
	inner Messenger
	ctx   context.Context
}

// WithLogging wraps a Messenger with structured logging for Send/Receive calls.
// Returns the original Messenger unmodified if it is nil.
func WithLogging(ctx context.Context, m Messenger) Messenger {
	if m == nil {
		return nil
	}
	return &LoggingMessenger{inner: m, ctx: ctx}
}

func (lm *LoggingMessenger) Connect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("fn", "messenger.Connect", "platform", string(lm.inner.Platform()))
	log.Info("connecting to messenger")
	err := lm.inner.Connect(ctx)
	if err != nil {
		log.Error("failed to connect to messenger", "error", err)
	} else {
		log.Info("messenger connected")
	}
	return err
}

func (lm *LoggingMessenger) Disconnect(ctx context.Context) error {
	log := logger.GetLogger(ctx).With("fn", "messenger.Disconnect", "platform", string(lm.inner.Platform()))
	log.Info("disconnecting from messenger")
	err := lm.inner.Disconnect(ctx)
	if err != nil {
		log.Error("failed to disconnect from messenger", "error", err)
	} else {
		log.Info("messenger disconnected")
	}
	return err
}

func (lm *LoggingMessenger) Send(ctx context.Context, req SendRequest) (SendResponse, error) {
	log := logger.GetLogger(ctx).With("fn", "messenger.Send", "platform", string(lm.inner.Platform()))

	log.Info("sending outgoing message",
		"channel", req.Channel.ID,
		"threadID", req.ThreadID,
		"textLength", len(req.Content.Text),
	)
	if len(req.Content.Text) == 0 {
		return SendResponse{}, fmt.Errorf("cannot send empty message text")
	}

	resp, err := lm.inner.Send(ctx, req)
	if err != nil {
		log.Error("failed to send message",
			"error", err,
			"channel", req.Channel.ID,
		)
		return resp, err
	}

	log.Info("outgoing message sent",
		"messageID", resp.MessageID,
		"channel", req.Channel.ID,
	)
	return resp, nil
}

func (lm *LoggingMessenger) Receive(ctx context.Context) (<-chan IncomingMessage, error) {
	log := logger.GetLogger(ctx).With("fn", "messenger.Receive", "platform", string(lm.inner.Platform()))

	ch, err := lm.inner.Receive(ctx)
	if err != nil {
		log.Error("failed to open receive channel", "error", err)
		return nil, err
	}

	// Wrap the channel to log each incoming message.
	logged := make(chan IncomingMessage, cap(ch))
	go func() {
		defer close(logged)
		for msg := range ch {
			log.Info("received incoming message",
				"sender", msg.Sender.ID,
				"displayName", msg.Sender.DisplayName,
				"channel", msg.Channel.ID,
				"textLength", len(msg.Content.Text),
			)
			logged <- msg
		}
	}()

	return logged, nil
}

func (lm *LoggingMessenger) Platform() Platform {
	return lm.inner.Platform()
}

func (lm *LoggingMessenger) ConnectionInfo() string {
	return lm.inner.ConnectionInfo()
}

func (lm *LoggingMessenger) FormatApproval(req SendRequest, info ApprovalInfo) SendRequest {
	log := logger.GetLogger(lm.ctx).With("fn", "messenger.FormatApproval", "platform", string(lm.inner.Platform()))
	log.Debug("formatting approval notification",
		"approvalID", info.ID,
		"tool", info.ToolName,
	)
	return lm.inner.FormatApproval(req, info)
}

func (lm *LoggingMessenger) FormatClarification(req SendRequest, info ClarificationInfo) SendRequest {
	log := logger.GetLogger(lm.ctx).With("fn", "messenger.FormatClarification", "platform", string(lm.inner.Platform()))
	log.Debug("formatting clarification request",
		"requestID", info.RequestID,
	)
	return lm.inner.FormatClarification(req, info)
}

// Unwrap returns the underlying Messenger, allowing callers to access
// the concrete messenger type through the logging wrapper. This follows
// the standard Go unwrap convention (similar to errors.Unwrap).
func (lm *LoggingMessenger) Unwrap() Messenger {
	return lm.inner
}
