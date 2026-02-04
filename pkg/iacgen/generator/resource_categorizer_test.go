package generator

import (
	"fmt"
	"strings"

	"github.com/appcd-dev/cce/pkg/models"
	"github.com/appcd-dev/genie/pkg/analyzer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ResourceCategorizer", func() {
	Describe("getResourceCategorizer", func() {
		It("should return a singleton instance", func() {
			cat1 := getResourceCategorizer()
			cat2 := getResourceCategorizer()
			Expect(cat1).To(Equal(cat2))
		})

		It("should be initialized with lookup maps", func() {
			cat := getResourceCategorizer()
			Expect(cat.keywordLookup).ToNot(BeNil())
			Expect(cat.resourceLookup).ToNot(BeNil())
		})
	})

	Describe("categorizeResources", func() {
		var cat *resourceCategorizer

		BeforeEach(func() {
			cat = getResourceCategorizer()
		})

		It("should correctly categorize mapped resources", func() {
			resources := analyzer.MappedResources{
				{
					MappedResource: models.MappedResource{Resource: "aws_s3_bucket"},
				},
				{
					MappedResource: models.MappedResource{Resource: "aws_lambda_function"},
				},
				{
					MappedResource: models.MappedResource{Resource: "google_storage_bucket"},
				},
			}

			categories := cat.categorizeResources(resources)

			Expect(categories[CategoryStorage]).To(HaveLen(2)) // aws_s3_bucket, google_storage_bucket
			Expect(categories[CategoryCompute]).To(HaveLen(1)) // aws_lambda_function
		})

		It("should handle unknown resources by keyword matching", func() {
			// assuming 'queue' is a keyword for Messaging
			resources := analyzer.MappedResources{
				{
					MappedResource: models.MappedResource{Resource: "custom_queue_service"},
				},
			}

			categories := cat.categorizeResources(resources)
			Expect(categories[CategoryMessaging]).To(HaveLen(1))
		})

		It("should categorize unknown resources as CategoryOther", func() {
			resources := analyzer.MappedResources{
				{
					MappedResource: models.MappedResource{Resource: "unknown_widget"},
				},
			}

			categories := cat.categorizeResources(resources)
			Expect(categories[CategoryOther]).To(HaveLen(1))
		})
	})

	Describe("inferWorkflow", func() {
		It("should detect event-driven pattern", func() {
			categories := ResourceCategories{
				CategoryMessaging: analyzer.MappedResources{{}},
				CategoryCompute:   analyzer.MappedResources{{}},
			}
			workflow := categories.inferWorkflow()
			Expect(workflow).To(ContainSubstring("Event-Driven Architecture Pattern Detected"))
		})

		It("should detect AI/ML pipeline pattern", func() {
			categories := ResourceCategories{
				CategoryAIML:    analyzer.MappedResources{{}},
				CategoryStorage: analyzer.MappedResources{{}},
			}
			workflow := categories.inferWorkflow()
			Expect(workflow).To(ContainSubstring("AI/ML Pipeline Pattern Detected"))
		})

		It("should detect API backend pattern", func() {
			categories := ResourceCategories{
				CategoryNetworking: analyzer.MappedResources{{}},
				CategoryCompute:    analyzer.MappedResources{{}},
				CategoryDatabase:   analyzer.MappedResources{{}},
			}
			workflow := categories.inferWorkflow()
			Expect(workflow).To(ContainSubstring("API Backend Pattern Detected"))
		})

		It("should detect stream processing pattern", func() {
			categories := ResourceCategories{
				CategoryMessaging: analyzer.MappedResources{
					{MappedResource: models.MappedResource{Resource: "aws_kinesis_stream"}},
				},
				CategoryCompute: analyzer.MappedResources{{}},
			}
			workflow := categories.inferWorkflow()
			Expect(workflow).To(ContainSubstring("Stream Processing Pattern Detected"))
		})

		It("should infer data flow for storage-aiml-messaging", func() {
			categories := ResourceCategories{
				CategoryStorage:   analyzer.MappedResources{{}},
				CategoryAIML:      analyzer.MappedResources{{}},
				CategoryMessaging: analyzer.MappedResources{{}},
			}
			workflow := categories.inferWorkflow()
			Expect(workflow).To(ContainSubstring("Likely Data Flow"))
			Expect(workflow).To(ContainSubstring("Data stored in storage layer"))
		})

		It("should infer data flow for networking-compute-db", func() {
			categories := ResourceCategories{
				CategoryNetworking: analyzer.MappedResources{{}},
				CategoryCompute:    analyzer.MappedResources{{}},
				CategoryDatabase:   analyzer.MappedResources{{}},
			}
			workflow := categories.inferWorkflow()
			Expect(workflow).To(ContainSubstring("Likely Data Flow"))
			Expect(workflow).To(ContainSubstring("Requests arrive via API Gateway"))
		})

		It("should fallback to custom pattern if no match", func() {
			categories := ResourceCategories{
				CategoryOther: analyzer.MappedResources{{}},
			}
			workflow := categories.inferWorkflow()
			Expect(workflow).To(ContainSubstring("Custom Architecture Pattern"))
		})
	})

	Describe("buildComponentSummary", func() {
		It("should generate compact summary without file locations by default", func() {
			resources := analyzer.MappedResources{
				{
					MappedResource: models.MappedResource{Resource: "aws_s3_bucket"},
					MethodCall: models.MethodCall{
						FilePath:       "/path/to/main.go",
						Line:           10,
						ParentFunction: "main",
					},
				},
				{
					MappedResource: models.MappedResource{Resource: "aws_s3_bucket"},
					MethodCall: models.MethodCall{
						FilePath: "/path/to/utils.go",
						Line:     20,
					},
				},
			}

			summary := buildComponentSummary(CategoryStorage, resources)

			Expect(summary).To(ContainSubstring("### Storage Components"))
			Expect(summary).To(ContainSubstring("(2 usage(s))"))
			// Should NOT include file locations by default (cost optimization)
			Expect(summary).ToNot(ContainSubstring("main.go"))
			Expect(summary).ToNot(ContainSubstring(":10"))
			Expect(summary).ToNot(ContainSubstring("utils.go"))
		})

		It("should aggregate resources by service family", func() {
			resources := analyzer.MappedResources{
				{MappedResource: models.MappedResource{Resource: "s3"}},
				{MappedResource: models.MappedResource{Resource: "s3control"}},
				{MappedResource: models.MappedResource{Resource: "s3outposts"}},
			}

			summary := buildCompactComponentSummary(CategoryStorage, resources, DefaultPromptOptions())

			Expect(summary).To(ContainSubstring("S3 Storage"))
			Expect(summary).To(ContainSubstring("(3 usage(s))"))
			// Individual s3control/s3outposts should be aggregated
			Expect(summary).ToNot(ContainSubstring("s3control"))
			Expect(summary).ToNot(ContainSubstring("s3outposts"))
		})

		It("should limit to top N resources when configured", func() {
			resources := analyzer.MappedResources{}
			// Create 15 different resources
			for i := 0; i < 15; i++ {
				resources = append(resources, analyzer.MappedResource{
					MappedResource: models.MappedResource{Resource: fmt.Sprintf("resource_%02d", i)},
				})
			}

			opts := PromptOptions{MaxResourcesPerCategory: 5, AggregateByFamily: false}
			summary := buildCompactComponentSummary(CategoryCompute, resources, opts)

			// Should have the "...and X more resources" line
			Expect(summary).To(ContainSubstring("...and 10 more resources"))
			// Count bullet points (should be 6: 5 resources + 1 "more" line)
			Expect(strings.Count(summary, "- ")).To(Equal(6))
		})

		It("should sort resources by usage count descending", func() {
			resources := analyzer.MappedResources{
				{MappedResource: models.MappedResource{Resource: "low_usage"}},
				{MappedResource: models.MappedResource{Resource: "high_usage"}},
				{MappedResource: models.MappedResource{Resource: "high_usage"}},
				{MappedResource: models.MappedResource{Resource: "high_usage"}},
			}

			opts := PromptOptions{AggregateByFamily: false}
			summary := buildCompactComponentSummary(CategoryCompute, resources, opts)

			// high_usage should appear before low_usage
			highIdx := strings.Index(summary, "high_usage")
			lowIdx := strings.Index(summary, "low_usage")
			Expect(highIdx).To(BeNumerically("<", lowIdx))
		})
	})

	Describe("Helper Methods", func() {
		It("should return categories keys", func() {
			categories := ResourceCategories{
				CategoryStorage: analyzer.MappedResources{{}},
				CategoryCompute: analyzer.MappedResources{{}},
			}
			keys := categories.Keys()
			Expect(keys).To(HaveLen(2))
			Expect(keys).To(ContainElement("Storage"))
			Expect(keys).To(ContainElement("Compute"))
		})

		It("should get category resource names", func() {
			resources := analyzer.MappedResources{
				{MappedResource: models.MappedResource{Resource: "res1"}},
				{MappedResource: models.MappedResource{Resource: "res2"}},
			}
			names := getCategoryResourceNames(resources)
			Expect(names).To(Equal("res1, res2"))
		})

		It("should return string representation of category", func() {
			cat := CategoryStorage
			Expect(cat.String()).To(Equal("Storage"))
		})
	})
})
