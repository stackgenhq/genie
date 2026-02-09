package expert_test

import (
	"errors"
	"fmt"

	"github.com/appcd-dev/genie/pkg/expert"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HandleExpertError", func() {
	It("should return no error and empty response for nil error", func() {
		resp, err := expert.HandleExpertError(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Choices).To(BeEmpty())
	})

	It("should return a partial response and no error for max tool iterations error", func() {
		flowError := fmt.Errorf("flow error: max tool iterations (12) exceeded")
		resp, err := expert.HandleExpertError(flowError)

		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Choices).To(HaveLen(1))
		Expect(resp.Choices[0].Message.Content).To(ContainSubstring("I stopped because I reached the maximum number of tool iterations"))
	})

	It("should return original error for other errors", func() {
		otherErr := errors.New("network timeout")
		resp, err := expert.HandleExpertError(otherErr)

		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("failed to run the expert: network timeout")))
		Expect(resp.Choices).To(BeEmpty())
	})
})
