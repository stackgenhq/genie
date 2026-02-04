package tftools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("TFValidator", func() {
	Describe("Declaration", func() {
		It("should return the correct tool declaration", func() {
			validator := TFValidator{}
			decl := validator.Declaration()

			Expect(decl).To(Equal(&tool.Declaration{
				Name:        "validate_iac",
				Description: "Validates Terraform/OpenTofu configurations",
				InputSchema: &tool.Schema{
					Type: "object",
					Properties: map[string]*tool.Schema{
						"iac_path": {
							Type:        "string",
							Description: "Absolute path to the directory containing Terraform/OpenTofu .tf files",
						},
					},
					Required: []string{"iac_path"},
				},
			}))
		})
	})

	Describe("Call", func() {
		var (
			ctx       context.Context
			validator TFValidator
		)

		BeforeEach(func() {
			ctx = context.Background()
			validator = TFValidator{}
		})

		Context("when given invalid JSON arguments", func() {
			It("should return an error", func() {
				_, err := validator.Call(ctx, []byte(`{invalid-json`))
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("invalid character"))
			})
		})

		Context("when IACPath is empty", func() {
			It("should return an error", func() {
				jsonArgs := []byte(`{"iac_path": ""}`)
				_, err := validator.Call(ctx, jsonArgs)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("iac_path cannot be empty"))
			})
		})

		Context("Integration Tests", func() {
			var binaryAvailable bool

			BeforeEach(func() {
				path, err := exec.LookPath("terraform")
				if err != nil {
					path, err = exec.LookPath("tofu")
				}
				binaryAvailable = err == nil && path != ""

				if !binaryAvailable {
					Skip("terraform/tofu binary not found")
				}
			})

			It("should validate valid terraform code", func() {
				absPath, err := filepath.Abs("testdata/valid")
				Expect(err).NotTo(HaveOccurred())

				jsonArgs := []byte(fmt.Sprintf(`{"iac_path": "%s"}`, absPath))
				result, err := validator.Call(ctx, jsonArgs)

				Expect(err).NotTo(HaveOccurred())

				res, ok := result.(tfValidationResult)
				Expect(ok).To(BeTrue())
				Expect(res.TFExecResult).NotTo(BeNil())
			})
		})
	})
})

var _ = Describe("TFValidatorInput", func() {
	Describe("filesOfInterest", func() {
		var (
			tempDir string
			subdir  string
			err     error
		)

		BeforeEach(func() {
			// Create a temporary directory for testing
			tempDir, err = os.MkdirTemp("", "tftools_test")
			Expect(err).NotTo(HaveOccurred())

			// Create file structure:
			// tempDir/
			//   main.tf
			//   readme.md
			//   subdir/
			//     variables.tf
			//     image.png

			err = os.WriteFile(filepath.Join(tempDir, "main.tf"), []byte(""), 0644)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(filepath.Join(tempDir, "readme.md"), []byte(""), 0644)
			Expect(err).NotTo(HaveOccurred())

			subdir = filepath.Join(tempDir, "subdir")
			Expect(os.Mkdir(subdir, 0755)).To(Succeed())

			Expect(os.WriteFile(filepath.Join(subdir, "variables.tf"), []byte(""), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(subdir, "image.png"), []byte(""), 0644)).To(Succeed())
		})

		AfterEach(func() {
			Expect(os.RemoveAll(tempDir)).To(Succeed())
		})

		Context("when given a valid directory", func() {
			It("should return all .tf files recursively", func() {
				input := TFValidatorInput{IACPath: tempDir}
				files, err := input.filesOfInterest()
				Expect(err).NotTo(HaveOccurred())

				expected := []string{
					filepath.Join(tempDir, "main.tf"),
					filepath.Join(subdir, "variables.tf"),
				}
				Expect(files).To(ConsistOf(expected))
			})
		})

		Context("when given a valid single file", func() {
			It("should return just that file", func() {
				targetFile := filepath.Join(tempDir, "main.tf")
				input := TFValidatorInput{IACPath: targetFile}
				files, err := input.filesOfInterest()
				Expect(err).NotTo(HaveOccurred())
				Expect(files).To(Equal([]string{targetFile}))
			})
		})

		Context("when given an invalid path", func() {
			It("should return an error", func() {
				input := TFValidatorInput{IACPath: filepath.Join(tempDir, "nonexistent")}
				_, err := input.filesOfInterest()
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when given a non-.tf file", func() {
			It("should return an error", func() {
				input := TFValidatorInput{IACPath: filepath.Join(tempDir, "readme.md")}
				_, err := input.filesOfInterest()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("is not a .tf file"))
			})
		})
	})
})
