package messenger

import (
	"context"
	"encoding/json"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// sendMessageArgs is the JSON schema input for the send_message tool.
type sendMessageArgs struct {
	ChannelID string `json:"channel_id"`
	Text      string `json:"text"`
	ThreadID  string `json:"thread_id,omitempty"`
}

// messageTool wraps a Messenger as a tool.CallableTool so the ReAcTree agent
// can send messages to configured platforms.
type messageTool struct {
	messenger Messenger
}

// NewSendMessageTool creates a tool.Tool that wraps a Messenger for sending messages.
// The tool exposes a "send_message" action that the agent can invoke.
func NewSendMessageTool(m Messenger) tool.Tool {
	return &messageTool{messenger: m}
}

// Declaration returns the tool metadata.
func (t *messageTool) Declaration() *tool.Declaration {
	return &tool.Declaration{
		Name:        "send_message",
		Description: fmt.Sprintf("Send a message to a %s channel or conversation. Use this to notify users, post updates, or respond to messages on the configured messaging platform.", t.messenger.Platform()),
		InputSchema: &tool.Schema{
			Type: "object",
			Properties: map[string]*tool.Schema{
				"channel_id": {
					Type:        "string",
					Description: "The channel or conversation ID to send the message to.",
				},
				"text": {
					Type:        "string",
					Description: "The message text to send.",
				},
				"thread_id": {
					Type:        "string",
					Description: "Optional thread ID for threaded replies. Leave empty for top-level messages.",
				},
			},
			Required: []string{"channel_id", "text"},
		},
	}
}

// Call sends a message using the underlying Messenger.
func (t *messageTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var args sendMessageArgs
	if err := json.Unmarshal(jsonArgs, &args); err != nil {
		return nil, fmt.Errorf("invalid send_message arguments: %w", err)
	}

	if args.ChannelID == "" {
		return nil, fmt.Errorf("channel_id is required")
	}
	if args.Text == "" {
		return nil, fmt.Errorf("text is required")
	}

	resp, err := t.messenger.Send(ctx, SendRequest{
		Channel: Channel{
			ID: args.ChannelID,
		},
		Content: MessageContent{
			Text: args.Text,
		},
		ThreadID: args.ThreadID,
	})
	if err != nil {
		return nil, err
	}

	return map[string]string{
		"message_id": resp.MessageID,
		"status":     "sent",
	}, nil
}

// Compile-time interface compliance check.
var _ tool.CallableTool = (*messageTool)(nil)
