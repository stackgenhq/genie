// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package notification

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/stackgenhq/genie/pkg/httputil"
)

func sendTwilio(ctx context.Context, accountSID, authToken, from, to, baseURL string, notifyReq NotifyRequest) error {
	msg := fmt.Sprintf("Agent %s requires assistance.\nJustification: %s\nMessage: %s", notifyReq.AgentName, notifyReq.Justification, notifyReq.Message)
	host := "https://api.twilio.com"
	if baseURL != "" {
		host = baseURL
	}
	apiURL := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json", host, accountSID)

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

	resp, err := httputil.GetClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("twilio returned status code %d", resp.StatusCode)
	}
	return nil
}
