package secops

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PolicyViolations", func() {
	defaultThresholds := SeverityThresholds{
		High:   0,
		Medium: 42, // Match old magical number
		Low:    -1, // Unlimited
	}

	Describe("isCompliant", func() {
		Context("when there are no violations", func() {
			It("should be compliant", func() {
				violations := PolicyViolations{}
				Expect(violations.isCompliant(defaultThresholds)).To(BeTrue())
			})
		})

		Context("when there are only low severity violations", func() {
			It("should be compliant", func() {
				violations := PolicyViolations{
					{Severity: "low", PolicyName: "Test", Description: "Low issue", FilePath: "/test.tf"},
					{Severity: "low", PolicyName: "Test", Description: "Another low issue", FilePath: "/test.tf"},
				}
				Expect(violations.isCompliant(defaultThresholds)).To(BeTrue())
			})
		})

		Context("when there are exactly 40 medium severity violations", func() {
			It("should be compliant (at threshold)", func() {
				violations := make(PolicyViolations, 40)
				for i := 0; i < 40; i++ {
					violations[i] = PolicyViolation{
						Severity:    "medium",
						PolicyName:  "Test",
						Description: "Medium issue",
						FilePath:    "/test.tf",
					}
				}
				Expect(violations.isCompliant(defaultThresholds)).To(BeTrue())
			})
		})

		Context("when there are 43 medium severity violations", func() {
			It("should be non-compliant (exceeds threshold)", func() {
				violations := make(PolicyViolations, 43)
				for i := 0; i < 43; i++ {
					violations[i] = PolicyViolation{
						Severity:    "medium",
						PolicyName:  "Test",
						Description: "Medium issue",
						FilePath:    "/test.tf",
					}
				}
				Expect(violations.isCompliant(defaultThresholds)).To(BeFalse())
			})
		})

		Context("when there is one high severity violation", func() {
			It("should be non-compliant", func() {
				violations := PolicyViolations{
					{Severity: "high", PolicyName: "Test", Description: "High issue", FilePath: "/test.tf"},
				}
				Expect(violations.isCompliant(defaultThresholds)).To(BeFalse())
			})
		})

		Context("when there is one critical severity violation", func() {
			It("should be non-compliant", func() {
				violations := PolicyViolations{
					{Severity: "critical", PolicyName: "Test", Description: "Critical issue", FilePath: "/test.tf"},
				}
				Expect(violations.isCompliant(defaultThresholds)).To(BeFalse())
			})
		})

		Context("when there are mixed severity violations", func() {
			It("should be non-compliant if any high/critical exists", func() {
				violations := PolicyViolations{
					{Severity: "low", PolicyName: "Test", Description: "Low issue", FilePath: "/test.tf"},
					{Severity: "medium", PolicyName: "Test", Description: "Medium issue", FilePath: "/test.tf"},
					{Severity: "high", PolicyName: "Test", Description: "High issue", FilePath: "/test.tf"},
				}
				Expect(violations.isCompliant(defaultThresholds)).To(BeFalse())
			})

			It("should be compliant if only low and few medium violations", func() {
				violations := PolicyViolations{
					{Severity: "low", PolicyName: "Test", Description: "Low issue 1", FilePath: "/test.tf"},
					{Severity: "low", PolicyName: "Test", Description: "Low issue 2", FilePath: "/test.tf"},
					{Severity: "medium", PolicyName: "Test", Description: "Medium issue 1", FilePath: "/test.tf"},
					{Severity: "medium", PolicyName: "Test", Description: "Medium issue 2", FilePath: "/test.tf"},
				}
				Expect(violations.isCompliant(defaultThresholds)).To(BeTrue())
			})
		})

		Context("when there are 39 medium and 1 high severity violations", func() {
			It("should be non-compliant due to high severity", func() {
				violations := make(PolicyViolations, 40)
				for i := 0; i < 39; i++ {
					violations[i] = PolicyViolation{
						Severity:    "medium",
						PolicyName:  "Test",
						Description: "Medium issue",
						FilePath:    "/test.tf",
					}
				}
				violations[39] = PolicyViolation{
					Severity:    "high",
					PolicyName:  "Test",
					Description: "High issue",
					FilePath:    "/test.tf",
				}
				Expect(violations.isCompliant(defaultThresholds)).To(BeFalse())
			})
		})

		Context("when there are unknown severity levels", func() {
			It("should ignore unknown severities and be compliant", func() {
				violations := PolicyViolations{
					{Severity: "info", PolicyName: "Test", Description: "Info issue", FilePath: "/test.tf"},
					{Severity: "unknown", PolicyName: "Test", Description: "Unknown issue", FilePath: "/test.tf"},
				}
				Expect(violations.isCompliant(defaultThresholds)).To(BeTrue())
			})
		})

		Context("with custom thresholds", func() {
			It("should fail if low severity exceeds limit", func() {
				thresholds := SeverityThresholds{High: -1, Medium: -1, Low: 1}
				violations := PolicyViolations{
					{Severity: "low", PolicyName: "Test", Description: "Low issue 1", FilePath: "/test.tf"},
					{Severity: "low", PolicyName: "Test", Description: "Low issue 2", FilePath: "/test.tf"},
				}
				Expect(violations.isCompliant(thresholds)).To(BeFalse())
			})

			It("should pass high severity if disabled (-1)", func() {
				thresholds := SeverityThresholds{High: -1, Medium: -1, Low: -1}
				violations := PolicyViolations{
					{Severity: "high", PolicyName: "Test", Description: "High issue", FilePath: "/test.tf"},
				}
				Expect(violations.isCompliant(thresholds)).To(BeTrue())
			})

			It("should fail strict medium check", func() {
				thresholds := SeverityThresholds{High: 0, Medium: 0, Low: -1}
				violations := PolicyViolations{
					{Severity: "medium", PolicyName: "Test", Description: "Medium issue", FilePath: "/test.tf"},
				}
				Expect(violations.isCompliant(thresholds)).To(BeFalse())
			})
		})
	})
})
