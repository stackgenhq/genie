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

func sendSlack(ctx context.Context, webhookURL string, notifyReq NotifyRequest) error {
	payload := map[string]string{
		"message":       notifyReq.Message,
		"agentName":     notifyReq.AgentName,
		"justification": notifyReq.Justification,
	}
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
		return fmt.Errorf("slack returned status code %d", resp.StatusCode)
	}
	return nil
}
