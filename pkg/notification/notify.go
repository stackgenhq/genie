package notification

import (
	"context"
	"fmt"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

// NotifyRequest is the input schema for the notify tool.
type NotifyRequest struct {
	Justification string `json:"justification" jsonschema:"description=The justification for sending this notification"`
	AgentName     string `json:"agent_name" jsonschema:"description=The name of the agent encountering the issue"`
	Message       string `json:"message" jsonschema:"description=A clear message on which the agent is stuck"`
}

type notifyTool struct {
	cfg Config
}

// NewNotifyTool creates a new notify tool based on Config.
func NewNotifyTool(cfg Config) tool.CallableTool {
	n := &notifyTool{cfg: cfg}
	return function.NewFunctionTool(
		n.Notify,
		function.WithName("notify"),
		function.WithDescription("Notify users when the subagent or orchestrator runs into issues they are not able to solve. Every notification must contain the justification, agent name, and a clear message on which the agent is stuck."),
	)
}

func (req NotifyRequest) validate() error {
	missingFields := make([]string, 3)
	if req.Justification == "" {
		missingFields = append(missingFields, "justification")
	}
	if req.AgentName == "" {
		missingFields = append(missingFields, "agent_name")
	}
	if req.Message == "" {
		missingFields = append(missingFields, "message")
	}
	if len(missingFields) > 0 {
		return fmt.Errorf("missing fields: %s", strings.Join(missingFields, ", "))
	}
	return nil
}

func (n *notifyTool) Notify(ctx context.Context, req NotifyRequest) (string, error) {
	log := logger.GetLogger(ctx)
	if err := req.validate(); err != nil {
		return "", err
	}

	var errs []string
	notifiedCount := 0

	// Pre-format the broadcast message.
	textMessage := fmt.Errorf("Agent %s requires assistance.\nJustification: %s\nMessage: %s", req.AgentName, req.Justification, req.Message).Error()

	// 1. Slack
	for _, slk := range n.cfg.Slack {
		if slk.WebhookURL == "" {
			continue
		}
		if err := sendSlack(ctx, slk.WebhookURL, textMessage); err != nil {
			errs = append(errs, fmt.Sprintf("slack error: %v", err))
		} else {
			notifiedCount++
		}
	}

	// 2. Webhooks
	for _, wh := range n.cfg.Webhooks {
		if wh.URL == "" {
			continue
		}
		if err := sendWebhook(ctx, wh.URL, wh.Headers, textMessage); err != nil {
			errs = append(errs, fmt.Sprintf("webhook error: %v", err))
		} else {
			notifiedCount++
		}
	}

	// 3. Twilio
	for _, twi := range n.cfg.Twilio {
		if twi.AccountSID == "" || twi.AuthToken == "" || twi.From == "" || twi.To == "" {
			continue
		}
		if err := sendTwilio(ctx, twi.AccountSID, twi.AuthToken, twi.From, twi.To, textMessage); err != nil {
			errs = append(errs, fmt.Sprintf("twilio error: %v", err))
		} else {
			notifiedCount++
		}
	}

	// 4. Discord
	for _, dsc := range n.cfg.Discord {
		if dsc.WebhookURL == "" {
			continue
		}
		if err := sendDiscord(ctx, dsc.WebhookURL, textMessage); err != nil {
			errs = append(errs, fmt.Sprintf("discord error: %v", err))
		} else {
			notifiedCount++
		}
	}

	if len(errs) > 0 {
		log.Warn("One or more notification endpoints failed", "errors", errs)
	}

	if notifiedCount == 0 && len(errs) == 0 {
		return "No notifications configured. The issue was not reported to any endpoints.", nil
	}

	if notifiedCount == 0 && len(errs) > 0 {
		return "", fmt.Errorf("Failed to send notification to all configured endpoints. Errors: %s", strings.Join(errs, "; "))
	}

	return fmt.Sprintf("Successfully sent notification through %d endpoint(s).", notifiedCount), nil
}
