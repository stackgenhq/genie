package expert_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/audit/auditfakes"
	"github.com/stackgenhq/genie/pkg/expert"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider/modelproviderfakes"
	"github.com/stackgenhq/genie/pkg/hitl/hitlfakes"
	"github.com/stackgenhq/genie/pkg/tools/toolsfakes"
	"github.com/stackgenhq/genie/pkg/toolwrap"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("ExpertBio", func() {
	var (
		fakeAuditor *auditfakes.FakeAuditor
	)
	BeforeEach(func() {
		fakeAuditor = &auditfakes.FakeAuditor{}
	})
	Describe("ToExpert", func() {
		It("should successfully create an expert", func() {
			bio := expert.ExpertBio{
				Name:        "test-expert",
				Description: "A test expert",
				Personality: "Be helpful",
			}

			fakeModelProvider := &modelproviderfakes.FakeModelProvider{}

			exp, err := bio.ToExpert(context.Background(), fakeModelProvider, fakeAuditor, toolwrap.NewService(fakeAuditor, &hitlfakes.FakeApprovalStore{}, nil))

			Expect(err).NotTo(HaveOccurred())
			Expect(exp).NotTo(BeNil())
		})

		It("should create an expert with tools attached", func() {
			readFile := &toolsfakes.FakeCallableTool{}
			readFile.DeclarationReturns(&tool.Declaration{Name: "read_file"})
			readFile.CallReturns("content", nil)

			executeCode := &toolsfakes.FakeCallableTool{}
			executeCode.DeclarationReturns(&tool.Declaration{Name: "execute_code"})
			executeCode.CallReturns("output", nil)

			bio := expert.ExpertBio{
				Name:        "tool-expert",
				Description: "An expert with tools",
				Personality: "Use tools wisely",
				Tools:       []tool.Tool{readFile, executeCode},
			}

			fakeModelProvider := &modelproviderfakes.FakeModelProvider{}

			exp, err := bio.ToExpert(context.Background(), fakeModelProvider, fakeAuditor, toolwrap.NewService(fakeAuditor, &hitlfakes.FakeApprovalStore{}, nil))

			Expect(err).NotTo(HaveOccurred())
			Expect(exp).NotTo(BeNil())
		})

		It("should create an expert with empty bio fields", func() {
			bio := expert.ExpertBio{
				Name: "minimal-expert",
			}

			fakeModelProvider := &modelproviderfakes.FakeModelProvider{}

			exp, err := bio.ToExpert(context.Background(), fakeModelProvider, fakeAuditor, toolwrap.NewService(fakeAuditor, nil, nil))

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
			Expect(cfg.MaxToolIterations).To(Equal(20))
			Expect(cfg.MaxHistoryRuns).To(Equal(0))
			Expect(cfg.DisableParallelTools).To(BeFalse())
		})
	})

	Describe("HighPerformanceConfig", func() {
		It("should have higher limits than default", func() {
			cfg := expert.HighPerformanceConfig()
			defaultCfg := expert.DefaultExpertConfig()
			Expect(cfg.MaxLLMCalls).To(BeNumerically(">", defaultCfg.MaxLLMCalls))
			Expect(cfg.MaxHistoryRuns).To(Equal(0))
		})
	})

	Describe("CostOptimizedConfig", func() {
		It("should have lower limits than default", func() {
			cfg := expert.CostOptimizedConfig()
			defaultCfg := expert.DefaultExpertConfig()
			Expect(cfg.MaxLLMCalls).To(BeNumerically("<", defaultCfg.MaxLLMCalls))
			Expect(cfg.MaxToolIterations).To(BeNumerically("<", defaultCfg.MaxToolIterations))
			Expect(cfg.MaxHistoryRuns).To(Equal(0))
		})
	})
})
