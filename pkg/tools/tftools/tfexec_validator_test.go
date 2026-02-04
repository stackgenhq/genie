package tftools

import (
	"context"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("tfExecValidator", func() {
	var (
		validator *tfExecValidator
		ctx       context.Context
	)

	BeforeEach(func() {
		validator = &tfExecValidator{}
		ctx = context.Background()
	})

	Describe("Validate", func() {
		// Helper to check if terraform or tofu is available
		var binaryAvailable bool

		BeforeEach(func() {
			_, errTofu := exec.LookPath("tofu")
			_, errTf := exec.LookPath("terraform")
			binaryAvailable = errTofu == nil || errTf == nil
		})

		It("should fail if iac_directory is empty", func() {
			_, err := validator.validate(ctx, TFValidatorInput{IACPath: ""})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("iac_directory is required"))
		})

		Context("Integration Tests", func() {
			BeforeEach(func() {
				if !binaryAvailable {
					Skip("terraform/tofu binary not found")
				}
			})

			It("should validate valid terraform code", func() {
				absPath, err := filepath.Abs("testdata/valid")
				Expect(err).ToNot(HaveOccurred())

				output, err := validator.validate(ctx, TFValidatorInput{
					IACPath: absPath,
				})

				// terraform init might fail if testdata/valid has requirements not met or no internet
				// But assuming standard simple tf files it should work or fail explicitly.
				// If error occurs (e.g. init fail), output.Errors will be populated.

				if err != nil {
					// This might happen if init fails totally
					Fail("Validate returned unexpected error: " + err.Error())
				}

				if !output.IsValid {
					// Print errors for debugging if it fails unexpectedly
					GinkgoWriter.Printf("Validation errors: %v\n", output.Errors)
				}

				Expect(output.IsValid).To(BeTrue(), "Expected valid terraform code to pass validation")
				Expect(output.Errors).To(BeEmpty())
			})

			It("should fail invalid terraform code", func() {
				absPath, err := filepath.Abs("testdata/invalid")
				Expect(err).ToNot(HaveOccurred())

				output, err := validator.validate(ctx, TFValidatorInput{
					IACPath: absPath,
				})

				Expect(err).ToNot(HaveOccurred())
				Expect(output.IsValid).To(BeFalse(), "Expected invalid terraform code to fail validation")
				Expect(output.Errors).ToNot(BeEmpty())
			})
		})
	})
})
