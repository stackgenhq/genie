package secops

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/snyk/policy-engine/pkg/models"
)

var _ = Describe("Policy Violation Extraction", func() {
	Describe("NewViolations", func() {
		Context("when there are no results", func() {
			It("should return empty violations", func() {
				results := []models.Result{}
				violations := newViolations(results)
				Expect(violations).To(BeEmpty())
			})
		})

		Context("when there are only passing results", func() {
			It("should return empty violations", func() {
				results := []models.Result{
					{
						RuleResults: []models.RuleResults{
							{
								Id:          "STACK-CC-00001",
								Description: "Test Rule",
								Results: []models.RuleResult{
									{
										Passed:  true,
										Message: "This passed",
									},
								},
							},
						},
					},
				}
				violations := newViolations(results)
				Expect(violations).To(BeEmpty())
			})
		})

		Context("when there are failed results without file paths", func() {
			It("should skip violations without file paths", func() {
				results := []models.Result{
					{
						RuleResults: []models.RuleResults{
							{
								Id:          "STACK-CC-00001",
								Description: "Test Rule",
								Results: []models.RuleResult{
									{
										Passed:            false,
										Message:           "This failed",
										ResourceNamespace: "", // No file path
										Resources:         nil,
									},
								},
							},
						},
					},
				}
				violations := newViolations(results)
				Expect(violations).To(BeEmpty())
			})
		})

		Context("when there are failed results with ResourceNamespace", func() {
			It("should extract violations using ResourceNamespace as file path", func() {
				results := []models.Result{
					{
						RuleResults: []models.RuleResults{
							{
								Id:          "STACK-CC-00507",
								Description: "Managed disk should have encryption enabled",
								Results: []models.RuleResult{
									{
										Passed:            false,
										Message:           "Disk encryption not enabled",
										ResourceNamespace: "/path/to/modules/vmss",
										Resources:         nil,
									},
								},
							},
						},
					},
				}

				violations := newViolations(results)
				Expect(violations).To(HaveLen(1))
				Expect(violations[0].PolicyName).To(Equal("Managed disk should have encryption enabled"))
				Expect(violations[0].Description).To(Equal("Disk encryption not enabled"))
				Expect(violations[0].FilePath).To(Equal("/path/to/modules/vmss"))
			})
		})

		Context("when there are failed results with Resources[].Location[]", func() {
			It("should prefer Resources[].Location[].Filepath over ResourceNamespace", func() {
				results := []models.Result{
					{
						RuleResults: []models.RuleResults{
							{
								Id:          "STACK-CC-00507",
								Description: "Managed disk should have encryption enabled",
								Results: []models.RuleResult{
									{
										Passed:            false,
										Message:           "Disk encryption not enabled",
										ResourceNamespace: "/path/to/modules/vmss",
										Resources: []*models.RuleResultResource{
											{
												Location: []models.SourceLocation{
													{
														Filepath: "/path/to/modules/vmss/main.tf",
														Line:     10,
														Column:   5,
													},
												},
											},
										},
									},
								},
							},
						},
					},
				}

				violations := newViolations(results)
				Expect(violations).To(HaveLen(1))
				Expect(violations[0].FilePath).To(Equal("/path/to/modules/vmss/main.tf"))
			})
		})

		Context("when there are duplicate violations", func() {
			It("should deduplicate violations with same PolicyName, FilePath, and Message", func() {
				results := []models.Result{
					{
						RuleResults: []models.RuleResults{
							{
								Id:          "STACK-CC-00507",
								Description: "Managed disk should have encryption enabled",
								Results: []models.RuleResult{
									{
										Passed:            false,
										Message:           "Disk encryption not enabled",
										ResourceNamespace: "/path/to/modules/vmss/main.tf",
									},
									{
										Passed:            false,
										Message:           "Disk encryption not enabled",
										ResourceNamespace: "/path/to/modules/vmss/main.tf",
									},
								},
							},
						},
					},
				}

				violations := newViolations(results)
				Expect(violations).To(HaveLen(1), "should deduplicate identical violations")
			})

			It("should keep violations with different file paths", func() {
				results := []models.Result{
					{
						RuleResults: []models.RuleResults{
							{
								Id:          "STACK-CC-00507",
								Description: "Managed disk should have encryption enabled",
								Results: []models.RuleResult{
									{
										Passed:            false,
										Message:           "Disk encryption not enabled",
										ResourceNamespace: "/path/to/modules/vmss/main.tf",
									},
									{
										Passed:            false,
										Message:           "Disk encryption not enabled",
										ResourceNamespace: "/path/to/modules/storage/main.tf",
									},
								},
							},
						},
					},
				}

				violations := newViolations(results)
				Expect(violations).To(HaveLen(2), "should keep violations from different files")
			})
		})

		Context("when there are multiple rules with violations", func() {
			It("should extract all unique violations", func() {
				results := []models.Result{
					{
						RuleResults: []models.RuleResults{
							{
								Id:          "STACK-CC-00507",
								Description: "Managed disk should have encryption enabled",
								Results: []models.RuleResult{
									{
										Passed:            false,
										Message:           "Disk encryption not enabled",
										ResourceNamespace: "/path/to/modules/vmss/main.tf",
									},
								},
							},
							{
								Id:          "STACK-CC-00460",
								Description: "Storage account should have logging enabled",
								Results: []models.RuleResult{
									{
										Passed:            false,
										Message:           "Logging not configured",
										ResourceNamespace: "/path/to/modules/storage/main.tf",
									},
								},
							},
						},
					},
				}

				violations := newViolations(results)
				Expect(violations).To(HaveLen(2))

				policyNames := []string{violations[0].PolicyName, violations[1].PolicyName}
				Expect(policyNames).To(ConsistOf(
					"Managed disk should have encryption enabled",
					"Storage account should have logging enabled",
				))
			})
		})

		Context("when there are mixed passing and failing results", func() {
			It("should only extract failing results", func() {
				results := []models.Result{
					{
						RuleResults: []models.RuleResults{
							{
								Id:          "STACK-CC-00507",
								Description: "Managed disk should have encryption enabled",
								Results: []models.RuleResult{
									{
										Passed:            true,
										Message:           "Disk encryption enabled",
										ResourceNamespace: "/path/to/modules/vmss/main.tf",
									},
									{
										Passed:            false,
										Message:           "Disk encryption not enabled",
										ResourceNamespace: "/path/to/modules/storage/main.tf",
									},
								},
							},
						},
					},
				}

				violations := newViolations(results)
				Expect(violations).To(HaveLen(1))
				Expect(violations[0].Description).To(Equal("Disk encryption not enabled"))
			})
		})
	})
})

var _ = Describe("SnykPolicyChecker", func() {
	Describe("Declaration", func() {
		It("should return correct tool declaration", func() {
			checker := snykPolicyChecker{}
			decl := checker.Declaration()
			Expect(decl.Name).To(Equal("check_iac_policy"))
			Expect(decl.Description).To(ContainSubstring("security policies"))
			Expect(decl.InputSchema.Required).To(ContainElement("iac_path"))
		})
	})
})

var _ = Describe("PolicyViolation", func() {
	Describe("String", func() {
		It("should format the violation correctly", func() {
			v := PolicyViolation{
				PolicyName:  "Test Policy",
				Description: "Something is wrong",
			}
			Expect(v.String()).To(Equal("Test Policy: Something is wrong"))
		})
	})
})
