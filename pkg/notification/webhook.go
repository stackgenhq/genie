package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/stackgenhq/genie/pkg/httputil"
)

func sendWebhook(ctx context.Context, u string, headers map[string]string, notifyReq NotifyRequest) error {
	msg := fmt.Sprintf("Agent %s requires assistance.\nJustification: %s\nMessage: %s", notifyReq.AgentName, notifyReq.Justification, notifyReq.Message)
	payload := map[string]string{"message": msg}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := httputil.GetClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status code %d", resp.StatusCode)
	}
	return nil
}
