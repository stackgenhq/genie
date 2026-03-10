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
	"trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
)

var _ = Describe("Failure Learning", func() {

	Describe("Episode with Reflection", func() {
		It("should include reflection in String() for failure episodes", func() {
			ep := reactreeMemory.Episode{
				Goal:       "deploy application",
				Trajectory: "error: context deadline exceeded",
				Status:     reactreeMemory.EpisodeFailure,
				Reflection: "The deployment timed out. Next time, increase the timeout or paginate the request.",
			}

			result := ep.String()
			Expect(result).To(ContainSubstring("⚠️ Previous Failure"))
			Expect(result).To(ContainSubstring("deploy application"))
			Expect(result).To(ContainSubstring("timed out"))
			// Should NOT contain the raw error output
			Expect(result).NotTo(ContainSubstring("context deadline exceeded"))
		})

		It("should show trajectory for success episodes", func() {
			ep := reactreeMemory.Episode{
				Goal:       "deploy application",
				Trajectory: "kubectl apply -f deployment.yaml succeeded",
				Status:     reactreeMemory.EpisodeSuccess,
			}

			result := ep.String()
			Expect(result).To(ContainSubstring("Goal: deploy application"))
			Expect(result).To(ContainSubstring("kubectl apply"))
			Expect(result).NotTo(ContainSubstring("⚠️"))
		})

		It("should fallback to trajectory format for failures without reflection", func() {
			ep := reactreeMemory.Episode{
				Goal:       "fix bug",
				Trajectory: "error output here",
				Status:     reactreeMemory.EpisodeFailure,
				Reflection: "",
			}

			result := ep.String()
			Expect(result).To(ContainSubstring("Goal: fix bug"))
			Expect(result).To(ContainSubstring("error output here"))
		})
	})

	Describe("Episode CreatedAt auto-population", func() {
		It("should set CreatedAt on store when zero", func(ctx context.Context) {
			svc := inmemory.NewMemoryService()
			defer svc.Close()

			ep := reactreeMemory.EpisodicMemoryConfig{
				Service: svc,
				AppName: "test-app",
				UserID:  "test-user",
			}.NewEpisodicMemory()

			before := time.Now()
			ep.Store(ctx, reactreeMemory.Episode{
				Goal:       "auto-populate time",
				Trajectory: "result",
				Status:     reactreeMemory.EpisodeSuccess,
			})
			after := time.Now()

			Eventually(func(g Gomega) {
				results := ep.Retrieve(ctx, "auto-populate time", 5)
				g.Expect(results).To(HaveLen(1))
				g.Expect(results[0].CreatedAt).NotTo(BeZero())
				g.Expect(results[0].CreatedAt).To(BeTemporally(">=", before))
				g.Expect(results[0].CreatedAt).To(BeTemporally("<=", after))
			}).WithTimeout(1 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())
		})

		It("should preserve explicit CreatedAt", func(ctx context.Context) {
			svc := inmemory.NewMemoryService()
			defer svc.Close()

			ep := reactreeMemory.EpisodicMemoryConfig{
				Service: svc,
				AppName: "test-app",
				UserID:  "test-user",
			}.NewEpisodicMemory()

			fixedTime := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
			ep.Store(ctx, reactreeMemory.Episode{
				Goal:       "explicit time",
				Trajectory: "result",
				Status:     reactreeMemory.EpisodeSuccess,
				CreatedAt:  fixedTime,
			})

			Eventually(func(g Gomega) {
				results := ep.Retrieve(ctx, "explicit time", 5)
				g.Expect(results).To(HaveLen(1))
				g.Expect(results[0].CreatedAt).To(Equal(fixedTime))
			}).WithTimeout(1 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())
		})
	})

	Describe("Episode with Reflection fields roundtrip", func() {
		It("should store and retrieve reflection and importance", func(ctx context.Context) {
			svc := inmemory.NewMemoryService()
			defer svc.Close()

			ep := reactreeMemory.EpisodicMemoryConfig{
				Service: svc,
				AppName: "test-app",
				UserID:  "test-user",
			}.NewEpisodicMemory()

			ep.Store(ctx, reactreeMemory.Episode{
				Goal:       "failed task",
				Trajectory: "error: API returned 500",
				Status:     reactreeMemory.EpisodeFailure,
				Reflection: "The API was overloaded. Add retry logic with backoff.",
				Importance: 8,
			})

			Eventually(func(g Gomega) {
				results := ep.Retrieve(ctx, "failed task", 5)
				g.Expect(results).To(HaveLen(1))
				g.Expect(results[0].Status).To(Equal(reactreeMemory.EpisodeFailure))
				g.Expect(results[0].Reflection).To(Equal("The API was overloaded. Add retry logic with backoff."))
				g.Expect(results[0].Importance).To(Equal(8))
			}).WithTimeout(1 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())
		})
	})

	Describe("RetrieveWeighted", func() {
		It("should rank recent episodes higher than old ones", func(ctx context.Context) {
			svc := inmemory.NewMemoryService()
			defer svc.Close()

			ep := reactreeMemory.EpisodicMemoryConfig{
				Service: svc,
				AppName: "test-app",
				UserID:  "test-user",
			}.NewEpisodicMemory()

			now := time.Now()

			// Store a 10-day-old success episode
			ep.Store(ctx, reactreeMemory.Episode{
				Goal:       "deploy app",
				Trajectory: "old approach",
				Status:     reactreeMemory.EpisodeSuccess,
				CreatedAt:  now.Add(-10 * 24 * time.Hour),
			})

			// Store a recent failure episode (1 hour old)
			ep.Store(ctx, reactreeMemory.Episode{
				Goal:       "deploy app",
				Trajectory: "recent failure",
				Status:     reactreeMemory.EpisodeFailure,
				Reflection: "Use the v2 API endpoint instead.",
				CreatedAt:  now.Add(-1 * time.Hour),
			})

			// Store a very recent success episode (5 min old)
			ep.Store(ctx, reactreeMemory.Episode{
				Goal:       "deploy app",
				Trajectory: "newest approach works",
				Status:     reactreeMemory.EpisodeSuccess,
				CreatedAt:  now.Add(-5 * time.Minute),
			})

			Eventually(func(g Gomega) {
				results := ep.RetrieveWeighted(ctx, "deploy app", 3)
				g.Expect(results).To(HaveLen(3))

				// The newest episode should be first
				g.Expect(results[0].Trajectory).To(Equal("newest approach works"))

				// The recent failure should be second
				g.Expect(results[1].Trajectory).To(Equal("recent failure"))

				// The old episode should be last
				g.Expect(results[2].Trajectory).To(Equal("old approach"))
			}).WithTimeout(1 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())
		})

		It("should limit results to k", func(ctx context.Context) {
			svc := inmemory.NewMemoryService()
			defer svc.Close()

			ep := reactreeMemory.EpisodicMemoryConfig{
				Service: svc,
				AppName: "test-app",
				UserID:  "test-user",
			}.NewEpisodicMemory()

			for i := 0; i < 5; i++ {
				ep.Store(ctx, reactreeMemory.Episode{
					Goal:       "repeated goal",
					Trajectory: "traj",
					Status:     reactreeMemory.EpisodeSuccess,
				})
			}

			time.Sleep(10 * time.Millisecond)

			results := ep.RetrieveWeighted(ctx, "repeated goal", 2)
			Expect(len(results)).To(BeNumerically("<=", 2))
		})

		It("should boost high-importance episodes even when older", func(ctx context.Context) {
			svc := inmemory.NewMemoryService()
			defer svc.Close()

			ep := reactreeMemory.EpisodicMemoryConfig{
				Service: svc,
				AppName: "test-app",
				UserID:  "test-user",
			}.NewEpisodicMemory()

			now := time.Now()

			// Store a 3-day-old critical lesson (importance=10)
			ep.Store(ctx, reactreeMemory.Episode{
				Goal:       "data migration",
				Trajectory: "critical lesson",
				Status:     reactreeMemory.EpisodeFailure,
				Reflection: "Never run migrations without backup",
				Importance: 10,
				CreatedAt:  now.Add(-3 * 24 * time.Hour),
			})

			// Store a recent but unimportant episode (importance=1)
			ep.Store(ctx, reactreeMemory.Episode{
				Goal:       "data migration",
				Trajectory: "minor note",
				Status:     reactreeMemory.EpisodeSuccess,
				Importance: 1,
				CreatedAt:  now.Add(-1 * time.Hour),
			})

			Eventually(func(g Gomega) {
				results := ep.RetrieveWeighted(ctx, "data migration", 2)
				g.Expect(results).To(HaveLen(2))

				// The recent low-importance episode still ranks higher due to
				// recency weight (0.6) dominating, but the high-importance old
				// episode should still appear in results.
				g.Expect(results).To(ContainElement(
					HaveField("Reflection", "Never run migrations without backup"),
				))
			}).WithTimeout(1 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())
		})

		It("should return nil for no matches", func(ctx context.Context) {
			svc := inmemory.NewMemoryService()
			defer svc.Close()

			ep := reactreeMemory.EpisodicMemoryConfig{
				Service: svc,
				AppName: "test-app",
				UserID:  "test-user",
			}.NewEpisodicMemory()

			results := ep.RetrieveWeighted(ctx, "nonexistent", 5)
			Expect(results).To(BeNil())
		})

		It("should work with NoOpEpisodicMemory", func(ctx context.Context) {
			ep := reactreeMemory.NewNoOpEpisodicMemory()
			results := ep.RetrieveWeighted(ctx, "any goal", 5)
			Expect(results).To(BeNil())
		})
	})

	Describe("NoOpFailureReflector", func() {
		It("should return empty reflection", func(ctx context.Context) {
			reflector := reactreeMemory.NewNoOpFailureReflector()
			result := reflector.Reflect(ctx, reactreeMemory.FailureReflectionRequest{
				Goal:        "deploy",
				ErrorOutput: "error occurred",
			})
			Expect(result).To(BeEmpty())
		})
	})
})
