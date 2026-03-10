// Copyright (C) StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//
// Change Date: 2029-03-10
// Change License: Apache License, Version 2.0

package memory

import (
	"context"
	"encoding/json"
	"time"

	"github.com/stackgenhq/genie/pkg/db"
)

// ReactionContext captures the agent's goal and output when a message was
// sent, so that a later emoji reaction can be correlated back to the
// episode. Without this correlation, reactions would be meaningless
// because we wouldn't know which goal/output the user is approving.
type ReactionContext struct {
	// Goal is the agent's goal when the message was sent.
	Goal string `json:"goal"`
	// Output is the agent's output text (truncated for storage).
	Output string `json:"output"`
	// SenderKey identifies the user/channel ("platform:senderID:channelID").
	SenderKey string `json:"sender_key"`
}

// ledgerMemoryType is the memory_type discriminator for the short_memories table.
const ledgerMemoryType = "reaction_ledger"

// defaultLedgerTTL is the duration after which ledger entries expire.
// Users typically react within minutes; 1 hour is generous.
const defaultLedgerTTL = 1 * time.Hour

// ReactionLedger is a DB-backed ledger that maps sent message IDs to the
// agent context that produced them. The send_message tool records entries;
// the reaction handler looks them up.
//
// Without this ledger, incoming reactions (which only carry the reacted
// message ID) cannot be correlated to the agent's goal and output.
//
// Backed by the generic short_memories table (memory_type = "reaction_ledger").
type ReactionLedger struct {
	store *db.ShortMemoryStore
	ttl   time.Duration
}

// NewReactionLedger creates a new DB-backed ledger.
// If store is nil, the ledger operates as a no-op (lookups always return false).
func NewReactionLedger(store *db.ShortMemoryStore) *ReactionLedger {
	return &ReactionLedger{
		store: store,
		ttl:   defaultLedgerTTL,
	}
}

// Record associates a sent message ID with the agent context that produced it.
func (l *ReactionLedger) Record(ctx context.Context, messageID string, goal, output, senderKey string) {
	if l.store == nil {
		return
	}

	value, err := json.Marshal(ReactionContext{
		Goal:      goal,
		Output:    output,
		SenderKey: senderKey,
	})
	if err != nil {
		return // best-effort; don't fail the send for a ledger write error
	}

	_ = l.store.Set(ctx, ledgerMemoryType, messageID, string(value), l.ttl)
}

// Lookup retrieves the agent context for a sent message ID.
// Returns the context and true if found and not expired, or zero value
// and false otherwise.
func (l *ReactionLedger) Lookup(ctx context.Context, messageID string) (ReactionContext, bool) {
	if l.store == nil {
		return ReactionContext{}, false
	}

	raw, found, err := l.store.Get(ctx, ledgerMemoryType, messageID)
	if err != nil || !found {
		return ReactionContext{}, false
	}

	var entry ReactionContext
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return ReactionContext{}, false
	}

	return entry, true
}
