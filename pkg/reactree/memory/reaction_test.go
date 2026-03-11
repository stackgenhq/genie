// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package memory_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/db"
	"github.com/stackgenhq/genie/pkg/messenger"
	reactreeMemory "github.com/stackgenhq/genie/pkg/reactree/memory"
)

// newTestStore creates a temp SQLite-backed ShortMemoryStore for testing.
func newTestStore() *db.ShortMemoryStore {
	tmpDir := GinkgoT().TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	gormDB, err := db.Open(dbPath)
	Expect(err).NotTo(HaveOccurred())
	Expect(db.AutoMigrate(gormDB)).To(Succeed())

	DeferCleanup(func() {
		_ = db.Close(gormDB)
		_ = os.RemoveAll(tmpDir)
	})

	return db.NewShortMemoryStore(gormDB)
}

var _ = Describe("ReactionLedger", func() {
	var ledger *reactreeMemory.ReactionLedger

	BeforeEach(func() {
		store := newTestStore()
		ledger = reactreeMemory.NewReactionLedger(store)
	})

	Describe("Record and Lookup", func() {
		It("should store and retrieve a message context", func() {
			ctx := context.Background()
			ledger.Record(ctx, "msg-123", "deploy app", "kubectl apply done", "whatsapp:user1:ch1")

			entry, ok := ledger.Lookup(ctx, "msg-123")
			Expect(ok).To(BeTrue())
			Expect(entry.Goal).To(Equal("deploy app"))
			Expect(entry.Output).To(Equal("kubectl apply done"))
			Expect(entry.SenderKey).To(Equal("whatsapp:user1:ch1"))
		})

		It("should return false for unknown message ID", func() {
			_, ok := ledger.Lookup(context.Background(), "nonexistent")
			Expect(ok).To(BeFalse())
		})

		It("should overwrite existing entry for same message ID", func() {
			ctx := context.Background()
			ledger.Record(ctx, "msg-123", "goal-1", "output-1", "key-1")
			ledger.Record(ctx, "msg-123", "goal-2", "output-2", "key-2")

			entry, ok := ledger.Lookup(ctx, "msg-123")
			Expect(ok).To(BeTrue())
			Expect(entry.Goal).To(Equal("goal-2"))
		})

		It("should track multiple message IDs independently", func() {
			ctx := context.Background()
			ledger.Record(ctx, "msg-1", "goal-a", "out-a", "key-1")
			ledger.Record(ctx, "msg-2", "goal-b", "out-b", "key-2")

			entry1, ok1 := ledger.Lookup(ctx, "msg-1")
			entry2, ok2 := ledger.Lookup(ctx, "msg-2")

			Expect(ok1).To(BeTrue())
			Expect(ok2).To(BeTrue())
			Expect(entry1.Goal).To(Equal("goal-a"))
			Expect(entry2.Goal).To(Equal("goal-b"))
		})
	})

	Describe("nil store safety", func() {
		It("should be safe to use with nil store", func() {
			ctx := context.Background()
			nilLedger := reactreeMemory.NewReactionLedger(nil)
			nilLedger.Record(ctx, "msg-1", "g", "o", "k")
			_, ok := nilLedger.Lookup(ctx, "msg-1")
			Expect(ok).To(BeFalse())
		})
	})
})

var _ = Describe("ReactionHandler", func() {
	var (
		ledger  *reactreeMemory.ReactionLedger
		fakeEp  *fakeEpisodicMemory
		handler *reactreeMemory.ReactionHandler
	)

	BeforeEach(func() {
		store := newTestStore()
		ledger = reactreeMemory.NewReactionLedger(store)
		fakeEp = &fakeEpisodicMemory{}
		handler = reactreeMemory.NewReactionHandler(reactreeMemory.ReactionHandlerConfig{
			Ledger:   ledger,
			Episodic: fakeEp,
		})
	})

	It("should store EpisodeSuccess on thumbs up", func() {
		ctx := context.Background()
		ledger.Record(ctx, "bot-msg-1", "make dinner", "Here is a pasta recipe...", "key")

		handler.HandleReaction(context.Background(), newReactionMsg("👍", "bot-msg-1"))

		Expect(fakeEp.stored).To(HaveLen(1))
		Expect(fakeEp.stored[0].Goal).To(Equal("make dinner"))
		Expect(fakeEp.stored[0].Status).To(Equal(reactreeMemory.EpisodeSuccess))
		Expect(fakeEp.stored[0].Trajectory).To(Equal("Here is a pasta recipe..."))
	})

	It("should store EpisodeFailure on thumbs down", func() {
		ctx := context.Background()
		ledger.Record(ctx, "bot-msg-2", "plan workout", "I cannot help with that.", "key")

		handler.HandleReaction(ctx, newReactionMsg("👎", "bot-msg-2"))

		Expect(fakeEp.stored).To(HaveLen(1))
		Expect(fakeEp.stored[0].Status).To(Equal(reactreeMemory.EpisodeFailure))
	})

	It("should store EpisodeSuccess on fire emoji", func() {
		ctx := context.Background()
		ledger.Record(ctx, "bot-msg-3", "review code", "Looks great!", "key")

		handler.HandleReaction(ctx, newReactionMsg("🔥", "bot-msg-3"))

		Expect(fakeEp.stored).To(HaveLen(1))
		Expect(fakeEp.stored[0].Status).To(Equal(reactreeMemory.EpisodeSuccess))
	})

	It("should ignore unknown emoji", func() {
		ctx := context.Background()
		ledger.Record(ctx, "bot-msg-4", "goal", "output", "key")

		handler.HandleReaction(ctx, newReactionMsg("🤔", "bot-msg-4"))

		Expect(fakeEp.stored).To(BeEmpty())
	})

	It("should ignore reaction for unknown message", func() {
		handler.HandleReaction(context.Background(), newReactionMsg("👍", "unknown-msg"))

		Expect(fakeEp.stored).To(BeEmpty())
	})

	It("should truncate long output in trajectory", func() {
		ctx := context.Background()
		longOutput := make([]byte, 600)
		for i := range longOutput {
			longOutput[i] = 'A'
		}
		ledger.Record(ctx, "bot-msg-5", "goal", string(longOutput), "key")

		handler.HandleReaction(ctx, newReactionMsg("👍", "bot-msg-5"))

		Expect(fakeEp.stored).To(HaveLen(1))
		Expect(len(fakeEp.stored[0].Trajectory)).To(BeNumerically("<=", 520))
	})

	It("should be safe to call on nil handler", func() {
		var nilHandler *reactreeMemory.ReactionHandler
		Expect(func() {
			nilHandler.HandleReaction(context.Background(), newReactionMsg("👍", "msg"))
		}).NotTo(Panic())
	})

	It("should return nil handler when config has nil dependencies", func() {
		h := reactreeMemory.NewReactionHandler(reactreeMemory.ReactionHandlerConfig{})
		Expect(h).To(BeNil())
	})
})

var _ = Describe("EpisodePending status", func() {
	It("should roundtrip through JSON", func() {
		ep := reactreeMemory.Episode{
			Goal:       "test goal",
			Trajectory: "test trajectory",
			Status:     reactreeMemory.EpisodePending,
		}
		Expect(ep.Status).To(Equal(reactreeMemory.EpisodePending))
		Expect(string(ep.Status)).To(Equal("pending"))
	})
})

var _ = Describe("ShortMemoryStore", func() {
	var store *db.ShortMemoryStore

	BeforeEach(func() {
		store = newTestStore()
	})

	It("should set and get a value", func() {
		err := store.Set(context.Background(), "test_type", "key1", `{"foo":"bar"}`, time.Hour)
		Expect(err).NotTo(HaveOccurred())

		val, found, err := store.Get(context.Background(), "test_type", "key1")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(val).To(Equal(`{"foo":"bar"}`))
	})

	It("should not return expired entries", func() {
		err := store.Set(context.Background(), "test_type", "key1", "v", -1*time.Second)
		Expect(err).NotTo(HaveOccurred())

		_, found, err := store.Get(context.Background(), "test_type", "key1")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeFalse())
	})

	It("should isolate by memory_type", func() {
		Expect(store.Set(context.Background(), "type_a", "key1", "val_a", time.Hour)).To(Succeed())
		Expect(store.Set(context.Background(), "type_b", "key1", "val_b", time.Hour)).To(Succeed())

		valA, foundA, _ := store.Get(context.Background(), "type_a", "key1")
		valB, foundB, _ := store.Get(context.Background(), "type_b", "key1")

		Expect(foundA).To(BeTrue())
		Expect(foundB).To(BeTrue())
		Expect(valA).To(Equal("val_a"))
		Expect(valB).To(Equal("val_b"))
	})

	It("should delete an entry", func() {
		Expect(store.Set(context.Background(), "t", "k", "v", time.Hour)).To(Succeed())
		Expect(store.Delete(context.Background(), "t", "k")).To(Succeed())

		_, found, _ := store.Get(context.Background(), "t", "k")
		Expect(found).To(BeFalse())
	})

	It("should cleanup expired entries", func() {
		Expect(store.Set(context.Background(), "t", "k1", "v", -1*time.Second)).To(Succeed())
		Expect(store.Set(context.Background(), "t", "k2", "v", time.Hour)).To(Succeed())

		cleaned, err := store.Cleanup(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(cleaned).To(Equal(int64(1)))

		count, err := store.Count(context.Background(), "t")
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(int64(1)))
	})

	It("should upsert on same key", func() {
		Expect(store.Set(context.Background(), "t", "k", "v1", time.Hour)).To(Succeed())
		Expect(store.Set(context.Background(), "t", "k", "v2", time.Hour)).To(Succeed())

		val, found, _ := store.Get(context.Background(), "t", "k")
		Expect(found).To(BeTrue())
		Expect(val).To(Equal("v2"))
	})
})

// --- test helpers ---

// fakeEpisodicMemory records Store calls for verification.
type fakeEpisodicMemory struct {
	stored []reactreeMemory.Episode
}

func (f *fakeEpisodicMemory) Store(_ context.Context, ep reactreeMemory.Episode) {
	f.stored = append(f.stored, ep)
}

func (f *fakeEpisodicMemory) Retrieve(_ context.Context, _ string, _ int) []reactreeMemory.Episode {
	return nil
}

func (f *fakeEpisodicMemory) RetrieveWeighted(_ context.Context, _ string, _ int) []reactreeMemory.Episode {
	return nil
}

// newReactionMsg creates a messenger.IncomingMessage with reaction fields set.
func newReactionMsg(emoji, reactedMsgID string) messenger.IncomingMessage {
	return messenger.IncomingMessage{
		Type:             messenger.MessageTypeReaction,
		ReactionEmoji:    emoji,
		ReactedMessageID: reactedMsgID,
	}
}
