package expert_test

import (
	"context"

	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider/modelproviderfakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("ExpertBio", func() {
	Describe("ToExpert", func() {
		It("should successfully create an expert", func() {
			bio := expert.ExpertBio{
				Name:        "test-expert",
				Description: "A test expert",
				Personality: "Be helpful",
			}

			fakeModelProvider := &modelproviderfakes.FakeModelProvider{}

			exp, err := bio.ToExpert(context.Background(), fakeModelProvider)

			Expect(err).NotTo(HaveOccurred())
			Expect(exp).NotTo(BeNil())
		})

		It("should create an expert with tools attached", func() {
			bio := expert.ExpertBio{
				Name:        "tool-expert",
				Description: "An expert with tools",
				Personality: "Use tools wisely",
				Tools: []tool.Tool{
					&fakeTool{name: "read_file", result: "content"},
					&fakeTool{name: "execute_code", result: "output"},
				},
			}

			fakeModelProvider := &modelproviderfakes.FakeModelProvider{}

			exp, err := bio.ToExpert(context.Background(), fakeModelProvider)

			Expect(err).NotTo(HaveOccurred())
			Expect(exp).NotTo(BeNil())
		})

		It("should create an expert with empty bio fields", func() {
			bio := expert.ExpertBio{
				Name: "minimal-expert",
			}

			fakeModelProvider := &modelproviderfakes.FakeModelProvider{}

			exp, err := bio.ToExpert(context.Background(), fakeModelProvider)

			Expect(err).NotTo(HaveOccurred())
			Expect(exp).NotTo(BeNil())
		})
	})
})

var _ = Describe("ExpertConfig", func() {
	Describe("DefaultExpertConfig", func() {
		It("should return sensible defaults", func() {
			cfg := expert.DefaultExpertConfig()
			Expect(cfg.MaxLLMCalls).To(Equal(15))
			Expect(cfg.MaxToolIterations).To(Equal(30))
			Expect(cfg.MaxHistoryRuns).To(Equal(5))
			Expect(cfg.DisableParallelTools).To(BeFalse())
		})
	})

	Describe("HighPerformanceConfig", func() {
		It("should have higher limits than default", func() {
			cfg := expert.HighPerformanceConfig()
			defaultCfg := expert.DefaultExpertConfig()
			Expect(cfg.MaxLLMCalls).To(BeNumerically(">", defaultCfg.MaxLLMCalls))
			Expect(cfg.MaxHistoryRuns).To(BeNumerically(">", defaultCfg.MaxHistoryRuns))
		})
	})

	Describe("CostOptimizedConfig", func() {
		It("should have lower limits than default", func() {
			cfg := expert.CostOptimizedConfig()
			defaultCfg := expert.DefaultExpertConfig()
			Expect(cfg.MaxLLMCalls).To(BeNumerically("<", defaultCfg.MaxLLMCalls))
			Expect(cfg.MaxToolIterations).To(BeNumerically("<", defaultCfg.MaxToolIterations))
			Expect(cfg.MaxHistoryRuns).To(BeNumerically("<", defaultCfg.MaxHistoryRuns))
		})
	})
})
