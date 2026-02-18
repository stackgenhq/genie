package agui

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/appcd-dev/genie/pkg/logger"
)

// significantWords extracts lowercased words of length ≥ 4 from text,
// returning them as a set. Short words (articles, prepositions) are
// excluded to focus on meaningful content that distinguishes messages.
func significantWords(text string) map[string]struct{} {
	words := make(map[string]struct{})
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(w) >= 4 {
			words[w] = struct{}{}
		}
	}
	return words
}

// jaccardSimilarity computes |A ∩ B| / |A ∪ B| for two word sets.
// Returns 0 if both sets are empty.
func jaccardSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	intersection := 0
	for w := range a {
		if _, ok := b[w]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// NewChatHandlerFromCodeOwner creates a ChatHandler that bridges the AG-UI
// server to the existing codeOwner.Chat() pipeline.
//
// The expert.Do() method converts raw *event.Event objects to TUI messages
// (AgentStreamChunkMsg, TextMessageStartMsg, etc.) via its own EventAdapter
// before writing them to the event channel. The ReAcTree runs multiple stages,
// each producing the same text content — causing duplicated output.
//
// This function inserts a dedup filter between the agent and the SSE stream:
// after the first TEXT_MESSAGE_END, all subsequent text message events are
// suppressed while tool calls and stage progress events pass through.
func NewChatHandlerFromCodeOwner(
	resumeFunc func(ctx context.Context) string,
	chatFunc func(ctx context.Context, message string, agentsMessage chan<- interface{}) error,
) Expert {
	return expert{
		resumeFunc: resumeFunc,
		chatFunc:   chatFunc,
	}
}

type expert struct {
	resumeFunc func(ctx context.Context) string
	chatFunc   func(ctx context.Context, message string, agentsMessage chan<- interface{}) error
}

func (e expert) Resume(ctx context.Context) string {
	if e.resumeFunc == nil {
		return ""
	}
	return e.resumeFunc(ctx)
}

func (e expert) Handle(ctx context.Context, req ChatRequest) {
	// Emit RUN_STARTED
	req.EventChan <- AgentThinkingMsg{
		Type:      EventRunStarted,
		AgentName: "Genie",
		Message:   "Processing your request...",
	}

	// Create a raw event channel for the agent.
	// expert.Do() writes TUI messages here (not raw *event.Event).
	rawEventChan := make(chan interface{}, 100)

	// Dedup filter: reads TUI messages from rawEventChan, suppresses
	// multi-stage text replays, and forwards everything else.
	//
	// Duplicate text arises from two sources:
	//  1. Within a single messageID: the trpc-agent library replays the
	//     accumulated text as a single chunk after streaming deltas.
	//  2. Across messageIDs: each expert.Do() call creates a new EventAdapter
	//     (and thus new messageIDs), but the parent agent, sub-agent, and
	//     adaptive-loop iterations all echo similar task-result summaries.
	//
	// We handle (1) via per-messageID prefix checking, and (2) via
	// word-overlap similarity: when a text message completes, we record its
	// significant words. If a subsequent message's word set overlaps ≥50%
	// with any previously-completed message (Jaccard similarity), the
	// entire lifecycle (START → chunks → END) is suppressed. This catches
	// LLM rephrasings of the same tool result across pipeline stages.
	converterDone := make(chan struct{})
	go func() {
		defer close(converterDone)

		sentStart := make(map[string]bool)     // messageId → true after first START
		sentContent := make(map[string]string) // messageId → all content sent so far
		suppressed := make(map[string]bool)    // messageId → true if this msg is being suppressed

		// Global: word sets from completed messages, used for cross-message dedup.
		type completedMsg struct {
			words map[string]struct{}
		}
		var completedMsgs []completedMsg

		for raw := range rawEventChan {
			select {
			case <-ctx.Done():
				return
			default:
			}

			switch evt := raw.(type) {
			// ── Text message lifecycle: cumulative dedup ──
			case TextMessageStartMsg:
				if sentStart[evt.MessageID] {
					continue // suppress duplicate START for same messageId
				}
				sentStart[evt.MessageID] = true
				// Don't emit yet — buffer until we see some content and can
				// decide if this message is a cross-message duplicate.
				// We'll emit it on the first non-suppressed chunk.

			case AgentStreamChunkMsg:
				// Per-messageID replay suppression (source 1).
				prior := sentContent[evt.MessageID]
				if len(evt.Content) > 0 && strings.HasPrefix(prior, evt.Content) {
					continue // already sent within this message — suppress replay
				}
				sentContent[evt.MessageID] = prior + evt.Content

				// Cross-message dedup (source 2): once we've accumulated
				// enough content (>120 chars to avoid false positives on
				// short common prefixes), check if this message's word
				// set overlaps significantly with a previously-completed
				// message.
				accumulated := sentContent[evt.MessageID]
				if !suppressed[evt.MessageID] && len(accumulated) > 120 {
					newWords := significantWords(accumulated)
					for _, prev := range completedMsgs {
						if jaccardSimilarity(newWords, prev.words) >= 0.50 {
							suppressed[evt.MessageID] = true
							logger.GetLogger(ctx).Debug("agui: suppressing similar cross-message duplicate",
								"messageID", evt.MessageID,
								"matchLen", len(accumulated))
							break
						}
					}
				}
				if suppressed[evt.MessageID] {
					continue
				}

				// Lazily emit the START event on first non-suppressed chunk.
				if prior == "" {
					req.EventChan <- TextMessageStartMsg{
						Type:      EventTextMessageStart,
						MessageID: evt.MessageID,
					}
				}
				req.EventChan <- evt

			case TextMessageEndMsg:
				content := sentContent[evt.MessageID]
				if !suppressed[evt.MessageID] && content != "" {
					// Only emit END if we actually sent content chunks.
					// If START was buffered but no chunks arrived, suppress
					// the entire lifecycle to avoid blank UI bubbles.
					req.EventChan <- evt
				}
				// Record completed content for cross-message dedup,
				// but only if it has meaningful length.
				if len(content) > 80 {
					completedMsgs = append(completedMsgs, completedMsg{
						words: significantWords(content),
					})
				}
				// Clean up per-message tracking.
				delete(sentContent, evt.MessageID)
				delete(suppressed, evt.MessageID)
				delete(sentStart, evt.MessageID)

			case AgentReasoningMsg:
				req.EventChan <- evt

			// ── Tool events: always pass through ──
			case AgentToolCallMsg:
				req.EventChan <- evt
			case ToolCallArgsMsg:
				req.EventChan <- evt
			case ToolCallEndMsg:
				req.EventChan <- evt
			case AgentToolResponseMsg:
				req.EventChan <- evt

			// ── Lifecycle/progress events: always pass through ──
			case StageProgressMsg:
				req.EventChan <- evt
			case AgentThinkingMsg:
				req.EventChan <- evt
			case AgentErrorMsg:
				req.EventChan <- evt
			case AgentCompleteMsg:
				req.EventChan <- evt
			case AgentChatMessage:
				req.EventChan <- evt
			case LogMsg:
				req.EventChan <- evt

			// ── HITL approval events: always pass through ──
			case ToolApprovalRequestMsg:
				req.EventChan <- evt

			default:
				logger.GetLogger(ctx).Warn("agui: skipping unknown raw event", "type", fmt.Sprintf("%T", raw))
			}
		}
	}()

	// Run the agent — it writes TUI messages to rawEventChan.
	// Inject ThreadID/RunID into context so the toolwrap.Wrapper can access them
	// for HITL approval requests without threading through every intermediate struct.
	agentCtx := context.WithValue(ctx, ctxKeyThreadID, req.ThreadID)
	agentCtx = context.WithValue(agentCtx, ctxKeyRunID, req.RunID)
	// Store rawEventChan in context so sub-agent tool wrappers can emit
	// HITL approval events back to the UI even when they don't have a
	// direct reference to the event channel.
	agentCtx = WithEventChan(agentCtx, rawEventChan)
	logger.GetLogger(ctx).Info("agui: invoking chatFunc",
		"threadID", req.ThreadID, "runID", req.RunID,
		"messageLen", len(req.Message))
	if err := e.chatFunc(agentCtx, req.Message, rawEventChan); err != nil {
		req.EventChan <- AgentErrorMsg{
			Type:    EventRunError,
			Error:   err,
			Context: "while processing chat message",
		}
	}

	// Close raw channel to signal converter to finish, wait for it to drain.
	close(rawEventChan)
	<-converterDone

	// Emit RUN_FINISHED
	req.EventChan <- AgentCompleteMsg{
		Type:    EventRunFinished,
		Success: true,
		Message: "Request completed",
	}
}
