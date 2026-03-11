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
)

var _ = Describe("Episodes (domain type)", func() {
	Describe("HasFailures", func() {
		It("returns false for empty slice", func() {
			Expect(reactreeMemory.Episodes{}.HasFailures()).To(BeFalse())
		})

		It("returns true when a failure episode exists", func() {
			eps := reactreeMemory.Episodes{
				{Status: reactreeMemory.EpisodeSuccess},
				{Status: reactreeMemory.EpisodeFailure},
			}
			Expect(eps.HasFailures()).To(BeTrue())
		})

		It("returns false when no failure episodes exist", func() {
			eps := reactreeMemory.Episodes{
				{Status: reactreeMemory.EpisodeSuccess},
				{Status: reactreeMemory.EpisodePending},
			}
			Expect(eps.HasFailures()).To(BeFalse())
		})
	})

	Describe("HasSuccesses", func() {
		It("returns false for empty slice", func() {
			Expect(reactreeMemory.Episodes{}.HasSuccesses()).To(BeFalse())
		})

		It("returns true for success status", func() {
			eps := reactreeMemory.Episodes{{Status: reactreeMemory.EpisodeSuccess}}
			Expect(eps.HasSuccesses()).To(BeTrue())
		})

		It("returns true for pending status", func() {
			eps := reactreeMemory.Episodes{{Status: reactreeMemory.EpisodePending}}
			Expect(eps.HasSuccesses()).To(BeTrue())
		})

		It("returns false for failure-only", func() {
			eps := reactreeMemory.Episodes{{Status: reactreeMemory.EpisodeFailure}}
			Expect(eps.HasSuccesses()).To(BeFalse())
		})
	})

	Describe("Summarize", func() {
		It("returns empty for empty slice", func() {
			Expect(reactreeMemory.Episodes{}.Summarize()).To(BeEmpty())
		})

		It("formats episodes via String()", func() {
			eps := reactreeMemory.Episodes{
				{Goal: "deploy", Trajectory: "deployed ok", Status: reactreeMemory.EpisodeSuccess},
			}
			result := eps.Summarize()
			Expect(result).To(ContainSubstring("deploy"))
			Expect(result).To(ContainSubstring("deployed ok"))
		})
	})

	Describe("Header", func() {
		It("returns warning for failures", func() {
			eps := reactreeMemory.Episodes{{Status: reactreeMemory.EpisodeFailure}}
			Expect(eps.Header()).To(ContainSubstring("failed before"))
		})

		It("returns success header for successes without failures", func() {
			eps := reactreeMemory.Episodes{{Status: reactreeMemory.EpisodeSuccess}}
			Expect(eps.Header()).To(ContainSubstring("succeeded before"))
		})

		It("returns empty for empty episodes", func() {
			Expect(reactreeMemory.Episodes{}.Header()).To(BeEmpty())
		})

		It("prioritizes failure header when both exist", func() {
			eps := reactreeMemory.Episodes{
				{Status: reactreeMemory.EpisodeSuccess},
				{Status: reactreeMemory.EpisodeFailure},
			}
			Expect(eps.Header()).To(ContainSubstring("failed before"))
		})
	})
})

var _ = Describe("StepAdvisory", func() {
	Describe("Format", func() {
		It("returns empty when no episodes and no wisdom", func() {
			sa := reactreeMemory.StepAdvisory{StepName: "s1"}
			Expect(sa.Format()).To(BeEmpty())
		})

		It("includes episode summary and header", func() {
			sa := reactreeMemory.StepAdvisory{
				StepName: "deploy",
				Episodes: reactreeMemory.Episodes{
					{Goal: "deploy", Trajectory: "success", Status: reactreeMemory.EpisodeSuccess},
				},
			}
			result := sa.Format()
			Expect(result).To(ContainSubstring("Pre-Execution Advisory"))
			Expect(result).To(ContainSubstring("succeeded before"))
			Expect(result).To(ContainSubstring("success"))
		})

		It("includes wisdom section", func() {
			sa := reactreeMemory.StepAdvisory{
				StepName:      "scale",
				WisdomSection: "## Consolidated Lessons\nAlways check HPA.\n",
			}
			result := sa.Format()
			Expect(result).To(ContainSubstring("HPA"))
		})

		It("truncates excessively long advisory", func() {
			long := ""
			for i := 0; i < 200; i++ {
				long += "This is a very long reflection sentence that repeats over and over. "
			}
			sa := reactreeMemory.StepAdvisory{
				StepName: "step",
				Episodes: reactreeMemory.Episodes{
					{Goal: "g", Trajectory: long, Status: reactreeMemory.EpisodeSuccess},
				},
			}
			result := sa.Format()
			Expect(len([]rune(result))).To(BeNumerically("<", 1400))
			Expect(result).To(ContainSubstring("truncated"))
		})
	})
})

var _ = Describe("PlanAdvisoryResult", func() {
	Describe("ForStep", func() {
		It("returns empty for nil advisories", func() {
			r := reactreeMemory.PlanAdvisoryResult{}
			Expect(r.ForStep("step1")).To(BeEmpty())
		})

		It("returns empty for missing step", func() {
			r := reactreeMemory.PlanAdvisoryResult{
				Advisories: map[string]reactreeMemory.StepAdvisory{
					"other": {StepName: "other", Episodes: reactreeMemory.Episodes{
						{Goal: "g", Trajectory: "t", Status: reactreeMemory.EpisodeSuccess},
					}},
				},
			}
			Expect(r.ForStep("step1")).To(BeEmpty())
		})

		It("wraps advisory in delimiters", func() {
			r := reactreeMemory.PlanAdvisoryResult{
				Advisories: map[string]reactreeMemory.StepAdvisory{
					"step1": {StepName: "step1", Episodes: reactreeMemory.Episodes{
						{Goal: "g", Trajectory: "data", Status: reactreeMemory.EpisodeSuccess},
					}},
				},
			}
			result := r.ForStep("step1")
			Expect(result).To(ContainSubstring("Begin Advisory"))
			Expect(result).To(ContainSubstring("End Advisory"))
			Expect(result).To(ContainSubstring("data"))
		})
	})

	Describe("StepsAdvised", func() {
		It("returns 0 for empty result", func() {
			r := reactreeMemory.PlanAdvisoryResult{}
			Expect(r.StepsAdvised()).To(Equal(0))
		})

		It("counts steps with non-empty advisory", func() {
			r := reactreeMemory.PlanAdvisoryResult{
				Advisories: map[string]reactreeMemory.StepAdvisory{
					"s1": {StepName: "s1", Episodes: reactreeMemory.Episodes{
						{Goal: "g", Trajectory: "t", Status: reactreeMemory.EpisodeSuccess},
					}},
					"s2": {StepName: "s2"}, // no episodes, no wisdom → empty Format()
				},
			}
			Expect(r.StepsAdvised()).To(Equal(1))
		})
	})
})

var _ = Describe("PlanAdvisor", func() {
	Describe("NewPlanAdvisor", func() {
		It("returns no-op when episodic is nil", func() {
			advisor := reactreeMemory.NewPlanAdvisor(reactreeMemory.PlanAdvisorConfig{})
			result := advisor.Advise(context.Background(), reactreeMemory.PlanAdvisoryRequest{
				StepGoals: map[string]string{"step1": "do something"},
			})
			Expect(result.StepsAdvised()).To(Equal(0))
		})
	})

	Describe("Advise", func() {
		var (
			fakeEp *memoryfakes.FakeEpisodicMemory
			fakeWs *memoryfakes.FakeWisdomStore
			ctx    context.Context
		)

		BeforeEach(func() {
			fakeEp = &memoryfakes.FakeEpisodicMemory{}
			fakeWs = &memoryfakes.FakeWisdomStore{}
			ctx = context.Background()
		})

		It("returns empty result when no episodes or wisdom match", func() {
			fakeEp.RetrieveWeightedReturns(nil)
			fakeWs.RetrieveWisdomReturns(nil)

			advisor := reactreeMemory.NewPlanAdvisor(reactreeMemory.PlanAdvisorConfig{
				Episodic: fakeEp,
				Wisdom:   fakeWs,
			})

			result := advisor.Advise(ctx, reactreeMemory.PlanAdvisoryRequest{
				OverallGoal: "deploy app",
				StepGoals:   map[string]string{"step1": "check cluster"},
			})

			Expect(result.StepsAdvised()).To(Equal(0))
			Expect(fakeEp.RetrieveWeightedCallCount()).To(Equal(1))
			_, goal, k := fakeEp.RetrieveWeightedArgsForCall(0)
			Expect(goal).To(Equal("check cluster"))
			Expect(k).To(Equal(2))
		})

		It("includes failure episodes with warning prefix", func() {
			fakeEp.RetrieveWeightedReturns([]reactreeMemory.Episode{
				{
					Goal:       "check cluster",
					Trajectory: "timeout connecting to k8s API",
					Status:     reactreeMemory.EpisodeFailure,
					Reflection: "The kubeconfig was stale. Refresh credentials before connecting.",
					CreatedAt:  time.Now().Add(-1 * time.Hour),
				},
			})
			fakeWs.RetrieveWisdomReturns(nil)

			advisor := reactreeMemory.NewPlanAdvisor(reactreeMemory.PlanAdvisorConfig{
				Episodic: fakeEp,
				Wisdom:   fakeWs,
			})

			result := advisor.Advise(ctx, reactreeMemory.PlanAdvisoryRequest{
				OverallGoal: "deploy app",
				StepGoals:   map[string]string{"check_cluster": "check cluster"},
			})

			Expect(result.StepsAdvised()).To(Equal(1))
			advisory := result.ForStep("check_cluster")
			Expect(advisory).To(ContainSubstring("failed before"))
			Expect(advisory).To(ContainSubstring("kubeconfig was stale"))
		})

		It("includes success episodes with positive prefix", func() {
			fakeEp.RetrieveWeightedReturns([]reactreeMemory.Episode{
				{
					Goal:       "deploy service",
					Trajectory: "deployed successfully using helm chart v3",
					Status:     reactreeMemory.EpisodeSuccess,
					CreatedAt:  time.Now().Add(-2 * time.Hour),
				},
			})
			fakeWs.RetrieveWisdomReturns(nil)

			advisor := reactreeMemory.NewPlanAdvisor(reactreeMemory.PlanAdvisorConfig{
				Episodic: fakeEp,
				Wisdom:   fakeWs,
			})

			result := advisor.Advise(ctx, reactreeMemory.PlanAdvisoryRequest{
				OverallGoal: "deploy service",
				StepGoals:   map[string]string{"deploy": "deploy service"},
			})

			advisory := result.ForStep("deploy")
			Expect(advisory).To(ContainSubstring("succeeded before"))
			Expect(advisory).To(ContainSubstring("helm chart v3"))
		})

		It("includes wisdom notes alongside episodes", func() {
			fakeEp.RetrieveWeightedReturns([]reactreeMemory.Episode{
				{
					Goal:       "scale pods",
					Trajectory: "scaled to 5 replicas",
					Status:     reactreeMemory.EpisodeSuccess,
					CreatedAt:  time.Now().Add(-3 * time.Hour),
				},
			})
			fakeWs.RetrieveWisdomReturns([]reactreeMemory.WisdomNote{
				{
					Summary:      "Always check HPA limits before manual scaling",
					Period:       "2026-03-09",
					EpisodeCount: 3,
				},
			})

			advisor := reactreeMemory.NewPlanAdvisor(reactreeMemory.PlanAdvisorConfig{
				Episodic: fakeEp,
				Wisdom:   fakeWs,
			})

			result := advisor.Advise(ctx, reactreeMemory.PlanAdvisoryRequest{
				OverallGoal: "scale application",
				StepGoals:   map[string]string{"scale_pods": "scale pods"},
			})

			advisory := result.ForStep("scale_pods")
			Expect(advisory).To(ContainSubstring("scaled to 5 replicas"))
			Expect(advisory).To(ContainSubstring("HPA limits"))
		})

		It("advises multiple steps independently", func() {
			fakeEp.RetrieveWeightedCalls(func(_ context.Context, goal string, _ int) []reactreeMemory.Episode {
				if goal == "build the project" {
					return []reactreeMemory.Episode{
						{Goal: "build", Trajectory: "build succeeded", Status: reactreeMemory.EpisodeSuccess, CreatedAt: time.Now()},
					}
				}
				return nil
			})
			fakeWs.RetrieveWisdomReturns(nil)

			advisor := reactreeMemory.NewPlanAdvisor(reactreeMemory.PlanAdvisorConfig{
				Episodic: fakeEp,
				Wisdom:   fakeWs,
			})

			result := advisor.Advise(ctx, reactreeMemory.PlanAdvisoryRequest{
				OverallGoal: "CI pipeline",
				StepGoals: map[string]string{
					"build": "build the project",
					"test":  "run unit tests",
				},
			})

			Expect(result.ForStep("build")).NotTo(BeEmpty())
			Expect(result.ForStep("test")).To(BeEmpty())
		})

		It("works without wisdom store (nil)", func() {
			fakeEp.RetrieveWeightedReturns([]reactreeMemory.Episode{
				{Goal: "check", Trajectory: "ok", Status: reactreeMemory.EpisodeSuccess, CreatedAt: time.Now()},
			})

			advisor := reactreeMemory.NewPlanAdvisor(reactreeMemory.PlanAdvisorConfig{
				Episodic: fakeEp,
			})

			result := advisor.Advise(ctx, reactreeMemory.PlanAdvisoryRequest{
				StepGoals: map[string]string{"s1": "check"},
			})

			Expect(result.ForStep("s1")).NotTo(BeEmpty())
		})
	})

	Describe("NoOpPlanAdvisor", func() {
		It("returns empty result from Advise", func() {
			advisor := reactreeMemory.NewNoOpPlanAdvisor()
			result := advisor.Advise(context.Background(), reactreeMemory.PlanAdvisoryRequest{
				StepGoals: map[string]string{"s": "goal"},
			})
			Expect(result.StepsAdvised()).To(Equal(0))
		})
	})
})
