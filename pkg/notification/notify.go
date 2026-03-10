package notification

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/stackgenhq/genie/pkg/logger"
	"golang.org/x/sync/errgroup"
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
		function.WithDescription("Send notifications, alerts, or messages to configured providers including Slack, Discord, Twilio, and Webhooks. Use this to notify users when stuck, to report status updates, or when explicitly requested. Every notification must contain the justification, agent name, and a clear message. Note: Do not ask the user for channel or recipient details, as the tool sends to globally configured default destinations. No clarification or confirmation is required to call this tool—use the available context to construct the message and send it immediately."),
	)
}

func (req NotifyRequest) validate() error {
	missingFields := make([]string, 0, 3)
	if req.Justification == "" {
		missingFields = append(missingFields, "justification")
	}
	if req.AgentName == "" {
		missingFields = append(missingFields, "agent_name")
	}
	if req.Message == "" {
		missingFields = append(missingFields, "message")
	}
	if len(missingFields) != 0 {
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
	var errsMu sync.Mutex
	notifiedCount := atomic.Int32{}
	errGroup, ctx := errgroup.WithContext(ctx)

	// 1. Slack
	for _, slk := range n.cfg.Slack {
		if slk.WebhookURL == "" {
			continue
		}
		errGroup.Go(func() error {
			if err := sendSlack(ctx, slk.WebhookURL, req); err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Sprintf("slack error: %v", err))
				errsMu.Unlock()
			} else {
				notifiedCount.Add(1)
			}
			return nil
		})
	}

	// 2. Webhooks
	for _, wh := range n.cfg.Webhooks {
		if wh.URL == "" {
			continue
		}
		errGroup.Go(func() error {
			if err := sendWebhook(ctx, wh.URL, wh.Headers, req); err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Sprintf("webhook error: %v", err))
				errsMu.Unlock()
			} else {
				notifiedCount.Add(1)
			}
			return nil
		})
	}

	// 3. Twilio
	for _, twi := range n.cfg.Twilio {
		if twi.AccountSID == "" || twi.AuthToken == "" || twi.From == "" || twi.To == "" {
			continue
		}
		errGroup.Go(func() error {
			if err := sendTwilio(ctx, twi.AccountSID, twi.AuthToken, twi.From, twi.To, twi.BaseURL, req); err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Sprintf("twilio error: %v", err))
				errsMu.Unlock()
			} else {
				notifiedCount.Add(1)
			}
			return nil
		})
	}

	// 4. Discord
	for _, dsc := range n.cfg.Discord {
		if dsc.WebhookURL == "" {
			continue
		}
		errGroup.Go(func() error {
			if err := sendDiscord(ctx, dsc.WebhookURL, req); err != nil {
				errsMu.Lock()
				errs = append(errs, fmt.Sprintf("discord error: %v", err))
				errsMu.Unlock()
			} else {
				notifiedCount.Add(1)
			}
			return nil
		})
	}

	_ = errGroup.Wait()

	if len(errs) > 0 {
		log.Warn("One or more notification endpoints failed", "errors", errs)
	}

	if notifiedCount.Load() == 0 && len(errs) == 0 {
		return "No notifications configured. The issue was not reported to any endpoints.", nil
	}

	if notifiedCount.Load() == 0 && len(errs) > 0 {
		return "", fmt.Errorf("Failed to send notification to all configured endpoints. Errors: %s", strings.Join(errs, "; "))
	}

	if len(errs) > 0 {
		return fmt.Sprintf(
			"Successfully sent notification through %d endpoint(s), but %d endpoint(s) failed: %s",
			notifiedCount.Load(),
			len(errs),
			strings.Join(errs, "; "),
		), nil
	}

	return fmt.Sprintf("Successfully sent notification through %d endpoint(s).", notifiedCount.Load()), nil
}
