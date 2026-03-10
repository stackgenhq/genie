// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: BUSL-1.1
//
// Use of this source code is governed by the Business Source License 1.1
// included in the LICENSE-BSL file at the root of this repository.
//

package memory_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	reactreeMemory "github.com/stackgenhq/genie/pkg/reactree/memory"
	"github.com/stackgenhq/genie/pkg/reactree/memory/memoryfakes"
	"trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
)

var _ = Describe("WisdomStore", func() {
	Describe("serviceWisdomStore", func() {
		It("should store and retrieve wisdom notes", func(ctx context.Context) {
			svc := inmemory.NewMemoryService()
			defer svc.Close()

			ws := reactreeMemory.WisdomStoreConfig{
				Service: svc,
				AppName: "test-app",
				UserID:  "test-user",
			}.NewWisdomStore()

			ws.StoreWisdom(ctx, reactreeMemory.WisdomNote{
				Summary:      "On 2026-03-10, you learned:\n- Always check health before deploy",
				Period:       "2026-03-10",
				EpisodeCount: 5,
			})

			Eventually(func(g Gomega) {
				notes := ws.RetrieveWisdom(ctx, 10)
				g.Expect(notes).To(HaveLen(1))
				g.Expect(notes[0].Period).To(Equal("2026-03-10"))
				g.Expect(notes[0].EpisodeCount).To(Equal(5))
				g.Expect(notes[0].Summary).To(ContainSubstring("Always check health"))
			}).WithTimeout(1 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())
		})

		It("should limit results", func(ctx context.Context) {
			svc := inmemory.NewMemoryService()
			defer svc.Close()

			ws := reactreeMemory.WisdomStoreConfig{
				Service: svc,
				AppName: "test-app",
				UserID:  "test-user",
			}.NewWisdomStore()

			for i := 0; i < 5; i++ {
				ws.StoreWisdom(ctx, reactreeMemory.WisdomNote{
					Summary: "wisdom",
					Period:  "2026-03-0" + string(rune('1'+i)),
				})
			}

			time.Sleep(10 * time.Millisecond)
			notes := ws.RetrieveWisdom(ctx, 2)
			Expect(len(notes)).To(BeNumerically("<=", 2))
		})

		It("should auto-populate CreatedAt", func(ctx context.Context) {
			svc := inmemory.NewMemoryService()
			defer svc.Close()

			ws := reactreeMemory.WisdomStoreConfig{
				Service: svc,
				AppName: "test-app",
				UserID:  "test-user",
			}.NewWisdomStore()

			ws.StoreWisdom(ctx, reactreeMemory.WisdomNote{
				Summary: "test",
				Period:  "2026-03-10",
			})

			Eventually(func(g Gomega) {
				notes := ws.RetrieveWisdom(ctx, 1)
				g.Expect(notes).To(HaveLen(1))
				g.Expect(notes[0].CreatedAt).NotTo(BeZero())
			}).WithTimeout(1 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())
		})
	})

	Describe("noOpWisdomStore", func() {
		It("should return nil on retrieve", func() {
			ws := reactreeMemory.NewNoOpWisdomStore()
			Expect(ws.RetrieveWisdom(context.Background(), 5)).To(BeNil())
		})

		It("should not panic on store", func() {
			ws := reactreeMemory.NewNoOpWisdomStore()
			Expect(func() {
				ws.StoreWisdom(context.Background(), reactreeMemory.WisdomNote{})
			}).NotTo(Panic())
		})
	})
})

var _ = Describe("FormatWisdomForPrompt", func() {
	It("should return empty for no notes", func() {
		Expect(reactreeMemory.FormatWisdomForPrompt(nil)).To(BeEmpty())
	})

	It("should include all wisdom notes", func() {
		notes := []reactreeMemory.WisdomNote{
			{Summary: "On day 1, you learned:\n- Lesson A"},
			{Summary: "On day 2, you learned:\n- Lesson B"},
		}
		result := reactreeMemory.FormatWisdomForPrompt(notes)
		Expect(result).To(ContainSubstring("Consolidated Lessons"))
		Expect(result).To(ContainSubstring("Lesson A"))
		Expect(result).To(ContainSubstring("Lesson B"))
	})
})

var _ = Describe("EpisodeConsolidator", func() {
	It("should return nil when dependencies are missing", func() {
		c := reactreeMemory.NewEpisodeConsolidator(reactreeMemory.EpisodeConsolidatorConfig{})
		Expect(c).To(BeNil())
		// Should be safe to call Consolidate on nil.
		Expect(c.Consolidate(context.Background())).To(Equal(0))
	})

	It("should consolidate recent episodes into a wisdom note", func(ctx context.Context) {
		fakeEp := &memoryfakes.FakeEpisodicMemory{}
		fakeWs := &memoryfakes.FakeWisdomStore{}
		fakeSummarizer := &memoryfakes.FakeEpisodeSummarizer{}

		// Return no existing wisdom notes (no duplicate period).
		fakeWs.RetrieveWisdomReturns(nil)

		// Return some recent episodes.
		now := time.Now()
		fakeEp.RetrieveReturns([]reactreeMemory.Episode{
			{Goal: "deploy", Trajectory: "deployed via kubectl", Status: reactreeMemory.EpisodeSuccess, CreatedAt: now.Add(-1 * time.Hour)},
			{Goal: "fix bug", Trajectory: "root cause was nil pointer", Status: reactreeMemory.EpisodeFailure, Reflection: "Check for nil before accessing", CreatedAt: now.Add(-2 * time.Hour)},
		})

		// Return a summary.
		fakeSummarizer.SummarizeReturns("- Always check nil before dereferencing\n- Use kubectl rollout status to verify deployments")

		c := reactreeMemory.NewEpisodeConsolidator(reactreeMemory.EpisodeConsolidatorConfig{
			Episodic:   fakeEp,
			Wisdom:     fakeWs,
			Summarizer: fakeSummarizer,
		})

		count := c.Consolidate(ctx)
		Expect(count).To(Equal(2))

		// Verify summarizer was called with the episodes.
		Expect(fakeSummarizer.SummarizeCallCount()).To(Equal(1))
		_, passedEpisodes := fakeSummarizer.SummarizeArgsForCall(0)
		Expect(passedEpisodes).To(HaveLen(2))

		// Verify wisdom was stored.
		Expect(fakeWs.StoreWisdomCallCount()).To(Equal(1))
		_, note := fakeWs.StoreWisdomArgsForCall(0)
		Expect(note.EpisodeCount).To(Equal(2))
		Expect(note.Summary).To(ContainSubstring("you learned"))
		Expect(note.Summary).To(ContainSubstring("nil"))
	})

	It("should skip when wisdom already exists for the period", func(ctx context.Context) {
		fakeEp := &memoryfakes.FakeEpisodicMemory{}
		fakeWs := &memoryfakes.FakeWisdomStore{}
		fakeSummarizer := &memoryfakes.FakeEpisodeSummarizer{}

		// Existing wisdom note for today.
		today := time.Now().UTC().Format("2006-01-02")
		fakeWs.RetrieveWisdomReturns([]reactreeMemory.WisdomNote{
			{Period: today, Summary: "existing"},
		})

		c := reactreeMemory.NewEpisodeConsolidator(reactreeMemory.EpisodeConsolidatorConfig{
			Episodic:   fakeEp,
			Wisdom:     fakeWs,
			Summarizer: fakeSummarizer,
		})

		count := c.Consolidate(ctx)
		Expect(count).To(Equal(0))

		// Summarizer should NOT be called.
		Expect(fakeSummarizer.SummarizeCallCount()).To(Equal(0))
	})

	It("should skip when no episodes exist", func(ctx context.Context) {
		fakeEp := &memoryfakes.FakeEpisodicMemory{}
		fakeWs := &memoryfakes.FakeWisdomStore{}
		fakeSummarizer := &memoryfakes.FakeEpisodeSummarizer{}

		fakeEp.RetrieveReturns(nil)
		fakeWs.RetrieveWisdomReturns(nil)

		c := reactreeMemory.NewEpisodeConsolidator(reactreeMemory.EpisodeConsolidatorConfig{
			Episodic:   fakeEp,
			Wisdom:     fakeWs,
			Summarizer: fakeSummarizer,
		})

		count := c.Consolidate(ctx)
		Expect(count).To(Equal(0))
	})

	It("should skip old episodes outside lookback window", func(ctx context.Context) {
		fakeEp := &memoryfakes.FakeEpisodicMemory{}
		fakeWs := &memoryfakes.FakeWisdomStore{}
		fakeSummarizer := &memoryfakes.FakeEpisodeSummarizer{}

		fakeWs.RetrieveWisdomReturns(nil)

		// Return only old episodes (>24h ago).
		fakeEp.RetrieveReturns([]reactreeMemory.Episode{
			{Goal: "old task", Trajectory: "result", Status: reactreeMemory.EpisodeSuccess,
				CreatedAt: time.Now().Add(-48 * time.Hour)},
		})

		c := reactreeMemory.NewEpisodeConsolidator(reactreeMemory.EpisodeConsolidatorConfig{
			Episodic:   fakeEp,
			Wisdom:     fakeWs,
			Summarizer: fakeSummarizer,
		})

		count := c.Consolidate(ctx)
		Expect(count).To(Equal(0))
		Expect(fakeSummarizer.SummarizeCallCount()).To(Equal(0))
	})

	It("should handle summarizer returning empty", func(ctx context.Context) {
		fakeEp := &memoryfakes.FakeEpisodicMemory{}
		fakeWs := &memoryfakes.FakeWisdomStore{}
		fakeSummarizer := &memoryfakes.FakeEpisodeSummarizer{}

		fakeWs.RetrieveWisdomReturns(nil)
		fakeEp.RetrieveReturns([]reactreeMemory.Episode{
			{Goal: "task", Trajectory: "out", Status: reactreeMemory.EpisodeSuccess,
				CreatedAt: time.Now().Add(-1 * time.Hour)},
		})
		fakeSummarizer.SummarizeReturns("")

		c := reactreeMemory.NewEpisodeConsolidator(reactreeMemory.EpisodeConsolidatorConfig{
			Episodic:   fakeEp,
			Wisdom:     fakeWs,
			Summarizer: fakeSummarizer,
		})

		count := c.Consolidate(ctx)
		Expect(count).To(Equal(0))
		Expect(fakeWs.StoreWisdomCallCount()).To(Equal(0))
	})
})

var _ = Describe("ImportanceScorer", func() {
	Describe("NoOpImportanceScorer", func() {
		It("should return 0", func() {
			scorer := reactreeMemory.NewNoOpImportanceScorer()
			score := scorer.Score(context.Background(), reactreeMemory.ImportanceScoringRequest{
				Goal:   "test",
				Output: "result",
				Status: reactreeMemory.EpisodeSuccess,
			})
			Expect(score).To(Equal(0))
		})
	})
})
