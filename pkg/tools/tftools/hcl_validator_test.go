package tftools

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Validator", func() {
	var (
		validator hclValidator
		ctx       context.Context
	)

	BeforeEach(func() {
		validator = NewValidator()
		ctx = context.Background()
	})

	Describe("validate", func() {
		Context("when given an empty path", func() {
			It("should return an error", func() {
				_, err := validator.validate(ctx, TFValidatorInput{
					IACPath: "",
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("IACPath is required"))
			})
		})

		Context("when given a non-existent path", func() {
			It("should return an error", func() {
				_, err := validator.validate(ctx, TFValidatorInput{
					IACPath: "/non/existent/path",
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("failed to access path"))
			})
		})

		Context("when given a non-.tf file", func() {
			It("should return an error", func() {
				_, err := validator.validate(ctx, TFValidatorInput{
					IACPath: "testdata/nested/README.md",
				})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("is not a .tf file"))
			})
		})

		Context("when validating a single valid .tf file", func() {
			It("should return success", func() {
				result, err := validator.validate(ctx, TFValidatorInput{
					IACPath: "testdata/valid/main.tf",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.IsValid).To(BeTrue())
				Expect(result.FilesChecked).To(Equal(1))
			})
		})

		Context("when validating a directory with valid .tf files", func() {
			It("should return success and count all files", func() {
				result, err := validator.validate(ctx, TFValidatorInput{
					IACPath: "testdata/valid",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.IsValid).To(BeTrue())
				Expect(result.FilesChecked).To(Equal(2)) // main.tf and ec2.tf
			})
		})

		Context("when validating a single invalid .tf file", func() {
			It("should return failure with error details", func() {
				result, err := validator.validate(ctx, TFValidatorInput{
					IACPath: "testdata/invalid/missing_brace.tf",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.IsValid).To(BeFalse())
				Expect(result.FilesChecked).To(Equal(1))
				Expect(result.Notes).ToNot(BeEmpty())
				// Should contain error message about the file
				Expect(result.Notes[0]).To(ContainSubstring("Validation failed"))
			})
		})

		Context("when validating a directory with invalid .tf files", func() {
			It("should return failure with error details for each file", func() {
				result, err := validator.validate(ctx, TFValidatorInput{
					IACPath: "testdata/invalid",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.IsValid).To(BeFalse())
				Expect(result.FilesChecked).To(Equal(2)) // missing_brace.tf and incomplete_attribute.tf
				Expect(result.Notes).ToNot(BeEmpty())
				// Should have summary message plus individual errors
				Expect(len(result.Notes)).To(BeNumerically(">", 2))
			})
		})

		Context("when validating a nested directory structure", func() {
			It("should recursively find and validate all .tf files", func() {
				result, err := validator.validate(ctx, TFValidatorInput{
					IACPath: "testdata/nested",
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.IsValid).To(BeTrue())
				// Should find vpc.tf and subdir/subnet.tf, but not README.md
				Expect(result.FilesChecked).To(Equal(2))
			})
		})

		Context("when validating the entire testdata directory", func() {
			It("should find all .tf files recursively", func() {
				result, err := validator.validate(ctx, TFValidatorInput{
					IACPath: "testdata",
				})
				Expect(err).ToNot(HaveOccurred())
				// Total: 2 valid + 2 invalid + 2 nested = 6 files
				Expect(result.FilesChecked).To(Equal(6))
				// Should fail because invalid directory has errors
				Expect(result.IsValid).To(BeFalse())
			})
		})

		Context("when validating with absolute paths", func() {
			It("should work with absolute paths", func() {
				absPath, err := filepath.Abs("testdata/valid/main.tf")
				Expect(err).ToNot(HaveOccurred())

				result, err := validator.validate(ctx, TFValidatorInput{
					IACPath: absPath,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(result.IsValid).To(BeTrue())
				Expect(result.FilesChecked).To(Equal(1))
			})
		})
	})
})
