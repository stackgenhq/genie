// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/stackgenhq/genie/pkg/httputil"
)

func sendDiscord(ctx context.Context, webhookURL string, notifyReq NotifyRequest) error {
	msg := fmt.Sprintf("🚨 **Agent %s requires assistance**\n**Justification:** %s\n**Message:** %s", notifyReq.AgentName, notifyReq.Justification, notifyReq.Message)
	payload := map[string]string{"content": msg}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewBuffer(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httputil.GetClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord returned status code %d", resp.StatusCode)
	}
	return nil
}
