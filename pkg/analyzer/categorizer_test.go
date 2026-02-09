package analyzer

import (
	"context"

	"github.com/appcd-dev/cce/pkg/models"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MappedResources", func() {
	Describe("Summarize", func() {
		type summarizeTestCase struct {
			resources           MappedResources
			expectedPromptCount int
			expectedProviders   []string
			expectedSubstrings  map[string][]string // map[provider] -> substrings
		}

		DescribeTable("should generate correct summaries",
			func(tc summarizeTestCase) {
				ctx := context.TODO()
				results := tc.resources.Summarize(ctx)

				if tc.expectedPromptCount == 0 {
					Expect(results).To(BeNil())
					return
				}

				Expect(results).To(HaveLen(tc.expectedPromptCount))

				// Check providers
				var providers []string
				for _, res := range results {
					providers = append(providers, res.CloudProvider)
				}
				Expect(providers).To(ConsistOf(tc.expectedProviders))

				// Check content for each result
				for _, res := range results {
					expectedStrs, ok := tc.expectedSubstrings[res.CloudProvider]
					if ok {
						for _, str := range expectedStrs {
							Expect(res.Prompt).To(ContainSubstring(str))
						}
					}
				}
			},
			Entry("with no resources", summarizeTestCase{
				resources:           MappedResources{},
				expectedPromptCount: 0,
			}),
			Entry("with single provider resources", summarizeTestCase{
				resources: MappedResources{
					{MappedResource: models.MappedResource{Provider: "aws", Resource: "s3_bucket"}},
					{MappedResource: models.MappedResource{Provider: "aws", Resource: "lambda_function"}},
				},
				expectedPromptCount: 1,
				expectedProviders:   []string{"aws"},
				expectedSubstrings: map[string][]string{
					"aws": {
						"Cloud Provider:** aws",
						"Architecture Recommendations Needed",
						"S3 Storage",
						"Lambda Functions",
					},
				},
			}),
			Entry("with multiple provider resources", summarizeTestCase{
				resources: MappedResources{
					{MappedResource: models.MappedResource{Provider: "aws", Resource: "s3_bucket"}},
					{MappedResource: models.MappedResource{Provider: "gcp", Resource: "storage_bucket"}},
				},
				expectedPromptCount: 2,
				expectedProviders:   []string{"aws", "gcp"},
				expectedSubstrings: map[string][]string{
					"aws": {
						"Cloud Provider:** aws",
						"S3 Storage",
					},
					"gcp": {
						"Cloud Provider:** gcp",
						"storage_bucket",
					},
				},
			}),
		)
	})
})
