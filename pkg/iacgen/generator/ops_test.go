package generator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/appcd-dev/genie/pkg/expert"
	"github.com/appcd-dev/genie/pkg/expert/expertfakes"
	"github.com/appcd-dev/genie/pkg/expert/modelprovider/modelproviderfakes"
	"github.com/appcd-dev/genie/pkg/tools/secops"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomodel "trpc.group/trpc-go/trpc-agent-go/model"
)

var _ = Describe("IACWriter", func() {
	var (
		ctx               context.Context
		fakeModelProvider *modelproviderfakes.FakeModelProvider
		fakeExpert        *expertfakes.FakeExpert
		tempDir           string
		iacWriter         IACWriter
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tempDir, err = os.MkdirTemp("", "iacwriter-test-*")
		Expect(err).ToNot(HaveOccurred())

		fakeModelProvider = &modelproviderfakes.FakeModelProvider{}
		fakeExpert = &expertfakes.FakeExpert{}

		// Inject fake expert into the struct directly for method testing
		iacWriter = &llmBasedIACWriter{
			expert:       fakeExpert,
			outputFolder: tempDir,
		}
	})

	AfterEach(func() {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
	})

	Describe("NewLLMBasedIACWriter", func() {
		It("should initialize successfully", func() {
			fakeModelProvider.GetModelReturns(nil, nil)
			writer, err := NewLLMBasedIACWriter(ctx, fakeModelProvider, OpsConfig{}, secops.SecOpsConfig{})
			Expect(err).ToNot(HaveOccurred())
			Expect(writer).ToNot(BeNil())
		})
	})

	Describe("CreateIAC", func() {
		It("should return error if architecture requirement is empty", func() {
			req := IACRequest{
				ArchitectureRequirement: []string{},
				OutputFolder:            tempDir,
			}
			_, err := iacWriter.CreateIAC(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("architecture requirement cannot be empty"))
		})

		It("should return error if output folder is empty", func() {
			req := IACRequest{
				ArchitectureRequirement: []string{"req"},
				OutputFolder:            "",
			}
			_, err := iacWriter.CreateIAC(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("output folder cannot be empty"))
		})

		It("should generate terraform code successfully", func() {
			req := IACRequest{
				ArchitectureRequirement: []string{"req"},
				OutputFolder:            tempDir,
			}

			fakeExpert.DoReturns(expert.Response{
				Choices: []gomodel.Choice{
					{Message: gomodel.Message{Content: "Generated Code"}},
				},
			}, nil)

			resp, err := iacWriter.CreateIAC(ctx, req)
			Expect(err).ToNot(HaveOccurred())
			Expect(resp.IACCodePath).To(Equal(tempDir))
			// Notes contain static messages about the generation approach and output path
			Expect(resp.Notes).To(ContainElement(ContainSubstring("Terraform code generated using module-first approach")))
			Expect(resp.Notes).To(ContainElement(ContainSubstring("Files written to:")))
		})

		It("should return error if expert fails", func() {
			req := IACRequest{
				ArchitectureRequirement: []string{"req"},
				OutputFolder:            tempDir,
			}

			fakeExpert.DoReturns(expert.Response{}, fmt.Errorf("expert failure"))

			_, err := iacWriter.CreateIAC(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expert failure"))
		})

		It("should return error if file toolset creation fails", func() {
			// Providing a non-existent path that is not createable might trigger toolset failure?
			// file.NewToolSet verifies base path exists or creates it?
			// If we pass a file as base dir it should fail.
			filePath := filepath.Join(tempDir, "file")
			err := os.WriteFile(filePath, []byte("content"), 0644)
			Expect(err).ToNot(HaveOccurred())

			req := IACRequest{
				ArchitectureRequirement: []string{"req"},
				OutputFolder:            filePath, // invalid as directory
			}

			_, err = iacWriter.CreateIAC(ctx, req)
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("Cost Optimization", func() {
	Describe("preApprovedAWSModules", func() {
		It("should contain common AWS modules", func() {
			Expect(preApprovedAWSModules).To(HaveKey("vpc"))
			Expect(preApprovedAWSModules).To(HaveKey("s3-bucket"))
			Expect(preApprovedAWSModules).To(HaveKey("sqs"))
			Expect(preApprovedAWSModules).To(HaveKey("autoscaling"))
			Expect(preApprovedAWSModules).To(HaveKey("kms"))
			Expect(preApprovedAWSModules).To(HaveKey("eventbridge"))
			Expect(preApprovedAWSModules).To(HaveKey("ec2-instance"))
			Expect(preApprovedAWSModules).To(HaveKey("iam"))
			Expect(preApprovedAWSModules).To(HaveKey("rds"))
			Expect(preApprovedAWSModules).To(HaveKey("lambda"))
		})

		It("should have valid module source format", func() {
			for name, source := range preApprovedAWSModules {
				Expect(source).To(ContainSubstring("terraform-aws-modules/"),
					"Module %s should have terraform-aws-modules source", name)
				Expect(source).To(ContainSubstring("/aws"),
					"Module %s should specify aws provider", name)
				Expect(source).To(MatchRegexp(`v\d+\.\d+\.\d+`),
					"Module %s should have a version", name)
			}
		})
	})

	Describe("getPreApprovedModulesSection", func() {
		It("should return formatted module list", func() {
			section := getPreApprovedModulesSection()

			// Check format
			Expect(section).To(ContainSubstring("- **"))
			Expect(section).To(ContainSubstring("**: `"))

			// Check all modules are included
			for name := range preApprovedAWSModules {
				Expect(section).To(ContainSubstring(name))
			}
		})
	})

	Describe("buildModuleFirstPrompt", func() {
		var req IACRequest

		BeforeEach(func() {
			req = IACRequest{
				ArchitectureRequirement: []string{"Build a VPC with S3 bucket"},
				OutputFolder:            "/tmp/terraform",
			}
		})

		It("should contain pre-approved modules section", func() {
			prompt := buildModuleFirstPrompt(req, OpsConfig{EnableVerification: true})

			Expect(prompt).To(ContainSubstring("PRE-APPROVED MODULES"))
			Expect(prompt).To(ContainSubstring("DO NOT SEARCH OR FETCH DETAILS"))
			Expect(prompt).To(ContainSubstring("terraform-aws-modules"))
		})

		It("should contain strict tool constraints", func() {
			prompt := buildModuleFirstPrompt(req, OpsConfig{EnableVerification: true})

			Expect(prompt).To(ContainSubstring("STRICT CONSTRAINTS"))
			Expect(prompt).To(ContainSubstring("5 tool calls or fewer"))
			Expect(prompt).To(ContainSubstring("NEVER"))
			Expect(prompt).To(ContainSubstring("get_module_details"))
		})

		It("should include output folder in prompt", func() {
			prompt := buildModuleFirstPrompt(req, OpsConfig{EnableVerification: true})

			Expect(prompt).To(ContainSubstring("/tmp/terraform"))
			Expect(prompt).To(ContainSubstring("Output Folder"))
		})

		It("should include architecture requirements", func() {
			prompt := buildModuleFirstPrompt(req, OpsConfig{EnableVerification: true})

			Expect(prompt).To(ContainSubstring("Build a VPC with S3 bucket"))
		})

		It("should contain validation section when enabled", func() {
			prompt := buildModuleFirstPrompt(req, OpsConfig{EnableVerification: true})

			Expect(prompt).To(ContainSubstring("VALIDATION"))
			Expect(prompt).To(ContainSubstring("iac-validator"))
			Expect(prompt).To(ContainSubstring("terraform-validate"))
			Expect(prompt).To(ContainSubstring("check_iac_policy"))
			Expect(prompt).To(ContainSubstring("Validate with all three tools"))
		})

		It("should NOT contain validation section when disabled", func() {
			prompt := buildModuleFirstPrompt(req, OpsConfig{EnableVerification: false})

			Expect(prompt).ToNot(ContainSubstring("iac-validator"))
			Expect(prompt).ToNot(ContainSubstring("terraform-validate"))
			Expect(prompt).ToNot(ContainSubstring("check_iac_policy"))
			Expect(prompt).ToNot(ContainSubstring("Validate with all three tools"))
		})

		It("should handle empty output folder gracefully", func() {
			req.OutputFolder = ""
			prompt := buildModuleFirstPrompt(req, OpsConfig{EnableVerification: true})

			// Should not panic and still have core content
			Expect(prompt).To(ContainSubstring("OBJECTIVE"))
			Expect(prompt).To(ContainSubstring("PRE-APPROVED MODULES"))
		})

		It("should handle multiple architecture requirements", func() {
			req.ArchitectureRequirement = []string{
				"Build a VPC",
				"Add S3 bucket",
				"Configure SQS queue",
			}
			prompt := buildModuleFirstPrompt(req, OpsConfig{EnableVerification: true})

			Expect(prompt).To(ContainSubstring("Build a VPC"))
			Expect(prompt).To(ContainSubstring("Add S3 bucket"))
			Expect(prompt).To(ContainSubstring("Configure SQS queue"))
		})
	})
})
