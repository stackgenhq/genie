package memory

import (
	"context"

	"github.com/appcd-dev/genie/pkg/logger"
	"github.com/appcd-dev/genie/pkg/messenger"
)

// positiveReactions maps emoji strings to positive feedback intent.
var positiveReactions = map[string]bool{
	"👍":  true,
	"❤️": true,
	"🔥":  true,
	"👏":  true,
	"🎉":  true,
	"💯":  true,
}

// negativeReactions maps emoji strings to negative feedback intent.
var negativeReactions = map[string]bool{
	"👎": true,
}

// ReactionHandler processes incoming emoji reactions and converts them
// into episodic memory entries. This is the core of the "giving the LLM
// a cookie" mechanism: positive reactions store the episode as validated
// success, negative reactions store it as failure.
//
// Without this handler, the system would rely solely on heuristic-based
// episode storage (looksLikeError), which is susceptible to memory
// poisoning from graceful LLM failures.
type ReactionHandler struct {
	ledger   *ReactionLedger
	episodic EpisodicMemory
}

// ReactionHandlerConfig holds the dependencies needed to create a ReactionHandler.
type ReactionHandlerConfig struct {
	Ledger   *ReactionLedger
	Episodic EpisodicMemory
}

// NewReactionHandler creates a handler that processes incoming reactions
// against the ledger and stores/updates episodes accordingly.
// Returns nil if either dependency is nil (safe to call HandleReaction on nil).
func NewReactionHandler(cfg ReactionHandlerConfig) *ReactionHandler {
	if cfg.Ledger == nil || cfg.Episodic == nil {
		return nil
	}
	return &ReactionHandler{
		ledger:   cfg.Ledger,
		episodic: cfg.Episodic,
	}
}

// HandleReaction processes an incoming reaction message. It looks up the
// reacted message in the ledger, maps the emoji to an episode status,
// and stores the validated episode in episodic memory.
//
// It is safe to call on a nil receiver (no-op).
func (h *ReactionHandler) HandleReaction(ctx context.Context, msg messenger.IncomingMessage) {
	if h == nil {
		return
	}

	logr := logger.GetLogger(ctx).With("fn", "ReactionHandler.HandleReaction")

	// Look up the reacted message in the ledger.
	entry, ok := h.ledger.Lookup(ctx, msg.ReactedMessageID)
	if !ok {
		logr.Debug("reaction for unknown message, ignoring",
			"reacted_msg_id", msg.ReactedMessageID,
			"emoji", msg.ReactionEmoji,
		)
		return
	}

	// Map emoji to episode status.
	var status EpisodeStatus
	switch {
	case positiveReactions[msg.ReactionEmoji]:
		status = EpisodeSuccess
	case negativeReactions[msg.ReactionEmoji]:
		status = EpisodeFailure
	default:
		logr.Debug("reaction with unrecognized emoji, ignoring",
			"emoji", msg.ReactionEmoji,
			"reacted_msg_id", msg.ReactedMessageID,
		)
		return
	}

	// Cap trajectory to prevent large outputs from bloating future prompts.
	trajectory := entry.Output
	const maxTrajectoryRunes = 500
	runes := []rune(trajectory)
	if len(runes) > maxTrajectoryRunes {
		trajectory = string(runes[:maxTrajectoryRunes]) + "... (truncated)"
	}

	// Store the human-validated episode.
	h.episodic.Store(ctx, Episode{
		Goal:       entry.Goal,
		Trajectory: trajectory,
		Status:     status,
	})

	logr.Info("episode stored with human validation",
		"goal", entry.Goal,
		"status", status,
		"emoji", msg.ReactionEmoji,
		"reacted_msg_id", msg.ReactedMessageID,
	)
}
