package expert_test

import (
	"context"
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/expert"
)

var _ = Describe("HandleExpertError", func() {
	It("should return no error and empty response for nil error", func(ctx context.Context) {
		resp, err := expert.HandleExpertError(ctx, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Choices).To(BeEmpty())
	})

	It("should return a partial response and no error for max tool iterations error", func(ctx context.Context) {
		flowError := fmt.Errorf("flow error: max tool iterations (12) exceeded")
		resp, err := expert.HandleExpertError(ctx, flowError)

		Expect(err).NotTo(HaveOccurred())
		Expect(resp.Choices).To(HaveLen(1))
		Expect(resp.Choices[0].Message.Content).To(ContainSubstring("I have run into my limits (max tool iterations). Do you want me to keep trying? (Reply 'yes' to continue)"))
	})

	It("should return original error for other errors", func(ctx context.Context) {
		otherErr := errors.New("network timeout")
		resp, err := expert.HandleExpertError(ctx, otherErr)

		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("failed to run the expert: network timeout")))
		Expect(resp.Choices).To(BeEmpty())
	})
})
