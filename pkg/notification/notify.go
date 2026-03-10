package notification

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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

func (n *notifyTool) Notify(ctx context.Context, req NotifyRequest) (string, error) {
	log := logger.GetLogger(ctx)
	if req.Justification == "" {
		return "", fmt.Errorf("justification is required")
	}
	if req.AgentName == "" {
		return "", fmt.Errorf("agent_name is required")
	}
	if req.Message == "" {
		return "", fmt.Errorf("message is required")
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

func sendSlack(ctx context.Context, webhookURL string, msg string) error {
	payload := map[string]string{"text": msg}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack returned status code %d", resp.StatusCode)
	}
	return nil
}

func sendWebhook(ctx context.Context, u string, headers map[string]string, msg string) error {
	payload := map[string]string{"message": msg}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status code %d", resp.StatusCode)
	}
	return nil
}

func sendTwilio(ctx context.Context, accountSID, authToken, from, to, msg string) error {
	apiURL := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", accountSID)

	data := url.Values{}
	data.Set("To", to)
	data.Set("From", from)
	data.Set("Body", msg)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(accountSID+":"+authToken)))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("twilio returned status code %d", resp.StatusCode)
	}
	return nil
}

func sendDiscord(ctx context.Context, webhookURL string, msg string) error {
	payload := map[string]string{"content": msg}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord returned status code %d", resp.StatusCode)
	}
	return nil
}
