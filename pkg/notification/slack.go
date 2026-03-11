// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package notification

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"

	"github.com/stackgenhq/genie/pkg/httputil"
)

func sendSlack(ctx context.Context, webhookURL string, notifyReq NotifyRequest) error {
	msg := &slack.WebhookMessage{
		Text: fmt.Sprintf("*%s* needs attention.", notifyReq.AgentName),
		Attachments: []slack.Attachment{
			{
				Color: "#36a64f",
				Blocks: slack.Blocks{
					BlockSet: []slack.Block{
						slack.NewSectionBlock(slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("*From:* %s", notifyReq.AgentName), false, false), nil, nil),
						slack.NewSectionBlock(slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("*Reason:* %s", notifyReq.Justification), false, false), nil, nil),
						slack.NewSectionBlock(slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("*Message:* %s", notifyReq.Message), false, false), nil, nil),
					},
				},
			},
		},
	}
	return slack.PostWebhookCustomHTTPContext(ctx, webhookURL, httputil.GetClient(), msg)
}
