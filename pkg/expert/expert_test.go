package expert_test

import (
	"context"

	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider/modelproviderfakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
	})
})

// Note: Testing expert.Do logic would require mocking internal llmagent/runner
// which is harder due to trpc-agent-go dependencies.
// For now, we verified the basic factory method.
