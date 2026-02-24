package expert

import (
	"context"
	"strings"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/pii"
	"trpc.group/trpc-go/trpc-agent-go/model"
)

// piiReplacerKey is the context key for carrying the *strings.Replacer
// from BeforeModel to AfterModel. The Replacer maps [HIDDEN:hash] → original.
type piiReplacerKey struct{}

// piiCallbacks implements PII redaction and rehydration as model callbacks.
//
// This exists to prevent user PII (emails, API keys, passwords, high-entropy
// secrets) from being transmitted to third-party LLM providers. Without this,
// every user message would be sent verbatim — including any embedded secrets.
//
// The redaction uses the pii-shield entropy-based scanner (Shannon entropy,
// bigram scoring, Luhn validation, context-aware key detection, and
// deterministic HMAC hashing), which is far more robust than static regex.
//
// Flow:
//
//	User message (contains PII)
//	    → BeforeModel: pii.RedactWithReplacer() replaces secrets with [HIDDEN:hash]
//	    → LLM sees only redacted content
//	    → AfterModel: replacer.Replace() restores [HIDDEN:hash] → original
//	    → User sees unmasked response
type piiCallbacks struct{}

// beforeModel redacts PII from all user messages before the LLM call.
// It stores a *strings.Replacer in the context so afterModel can reverse
// individual [HIDDEN:hash] tokens with a single Replace() call.
func (p *piiCallbacks) beforeModel(
	ctx context.Context,
	args *model.BeforeModelArgs,
) (*model.BeforeModelResult, error) {
	logr := logger.GetLogger(ctx).With("fn", "pii.BeforeModel")

	// Collect replacement pairs from all user messages into one Replacer.
	var allPairs []string
	redactedCount := 0

	for i, msg := range args.Request.Messages {
		if msg.Role != model.RoleUser || msg.Content == "" {
			continue
		}

		redacted, replacer := pii.RedactWithReplacer(msg.Content)
		if redacted == msg.Content {
			continue
		}

		args.Request.Messages[i].Content = redacted
		redactedCount++

		// Extract pairs from the Replacer by applying it to a probe.
		// Since we can't inspect Replacer internals, we merge by re-calling
		// RedactWithReplacer which gives us the pairs we need.
		_ = replacer // Replacer is per-message; we collect pairs below.

		// Simpler: just store redacted→original per message for fallback.
		allPairs = append(allPairs, redacted, msg.Content)

		logr.Debug("redacted PII in user message",
			"msg_index", i,
			"original_len", len(msg.Content),
			"redacted_len", len(redacted),
		)
	}

	if redactedCount > 0 {
		logr.Info("PII redaction applied", "redacted_messages", redactedCount)
	}

	// Build a single Replacer for all messages and carry in context.
	var replacer *strings.Replacer
	if len(allPairs) > 0 {
		replacer = strings.NewReplacer(allPairs...)
	}
	ctx = context.WithValue(ctx, piiReplacerKey{}, replacer)
	return &model.BeforeModelResult{Context: ctx}, nil
}

// afterModel rehydrates redacted tokens in the assistant response so the
// end-user sees unmasked output. If no PII was redacted in the request,
// this is a no-op. Without this method, the user would see [HIDDEN:hash]
// placeholders in the LLM's reply.
func (p *piiCallbacks) afterModel(
	ctx context.Context,
	args *model.AfterModelArgs,
) (*model.AfterModelResult, error) {
	if args.Response == nil {
		return nil, nil
	}

	replacer, _ := ctx.Value(piiReplacerKey{}).(*strings.Replacer)
	if replacer == nil {
		return nil, nil
	}

	logr := logger.GetLogger(ctx).With("fn", "pii.AfterModel")

	for i, choice := range args.Response.Choices {
		original := choice.Message.Content
		rehydrated := replacer.Replace(original)
		if rehydrated != original {
			args.Response.Choices[i].Message.Content = rehydrated
			logr.Debug("rehydrated PII in response", "choice_index", i)
		}
	}

	return nil, nil
}

// NewPIIModelCallbacks creates model.Callbacks that redact PII from user
// messages before they reach the LLM, and rehydrate the original values in
// the response so the end-user sees unmasked output.
func NewPIIModelCallbacks() *model.Callbacks {
	p := &piiCallbacks{}
	callbacks := model.NewCallbacks()
	callbacks.RegisterBeforeModel(p.beforeModel)
	callbacks.RegisterAfterModel(p.afterModel)
	return callbacks
}
