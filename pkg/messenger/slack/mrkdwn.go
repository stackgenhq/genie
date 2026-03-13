// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package slack

import (
	"context"

	"github.com/presmihaylov/md2slack"
	slackapi "github.com/slack-go/slack"
	"github.com/stackgenhq/genie/pkg/logger"
)

// markdownToBlocks converts standard markdown (as produced by LLMs) into
// Slack Block Kit blocks using the md2slack library.
//
// This provides proper AST-based parsing via goldmark, producing native
// Block Kit types (HeaderBlock, DividerBlock, RichTextBlock, etc.) rather
// than raw mrkdwn text. Falls back to a plain-text section block on error.
//
// See: https://github.com/presmihaylov/md2slack
func markdownToBlocks(ctx context.Context, text string) []slackapi.Block {
	if text == "" {
		return nil
	}

	blocks, err := md2slack.Convert(text)
	if err != nil {
		logger.GetLogger(ctx).Warn("failed to convert markdown to blocks", "text", text, "error", err)
		// Fallback: wrap the original text in a simple section block.
		return []slackapi.Block{
			slackapi.NewSectionBlock(
				slackapi.NewTextBlockObject(slackapi.MarkdownType, text, false, false),
				nil, nil,
			),
		}
	}

	if len(blocks) == 0 {
		return nil
	}

	return blocks
}
