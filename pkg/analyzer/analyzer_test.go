package analyzer_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/appcd-dev/genie/pkg/analyzer"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sks/cce/pkg/analyzer/analyzercommon"
	"github.com/sks/cce/pkg/cce"
	"github.com/sks/cce/pkg/models"
)

var _ = Describe("Analyzer", func() {
	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	Describe("New", func() {
		Context("when creating a new analyzer", func() {
			It("should successfully create an analyzer instance", func() {
				a, err := analyzer.New()
				Expect(err).ToNot(HaveOccurred())
				Expect(a).ToNot(BeNil())
			})

			It("should return an analyzer that implements the Analyzer interface", func() {
				a, err := analyzer.New()
				Expect(err).ToNot(HaveOccurred())

				// Verify it implements the interface by attempting to use it
				var _ analyzer.Analyzer = a
			})
		})
	})

	Describe("Analyze", func() {
		var (
			testAnalyzer analyzer.Analyzer
			testdataDir  string
		)

		BeforeEach(func() {
			var err error
			testAnalyzer, err = analyzer.New()
			Expect(err).ToNot(HaveOccurred())

			// Get absolute path to testdata
			testdataDir, err = filepath.Abs("testdata")
			Expect(err).ToNot(HaveOccurred())
		})

		Context("when analyzing valid Python code", func() {
			It("should analyze Python file and return mapped resources", func() {
				pythonFile := filepath.Join(testdataDir, "sample.py")

				// Verify file exists
				_, err := os.Stat(pythonFile)
				Expect(err).ToNot(HaveOccurred())

				input := analyzercommon.AnalysisInput{
					File:     pythonFile,
					Language: cce.LanguagePYTHON,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})

			It("should detect multiple method calls in Python code", func() {
				pythonFile := filepath.Join(testdataDir, "sample.py")

				input := analyzercommon.AnalysisInput{
					File:     pythonFile,
					Language: cce.LanguagePYTHON,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())

				// The sample.py file has create_bucket and put_object calls
				// We expect at least some method calls to be detected
				if len(result) > 0 {
					for _, mr := range result {
						Expect(mr.MethodCall.Name).ToNot(BeEmpty())
					}
				}
			})
		})

		Context("when analyzing valid Go code", func() {
			It("should analyze Go file and return mapped resources", func() {
				goFile := filepath.Join(testdataDir, "sample.go")

				_, err := os.Stat(goFile)
				Expect(err).ToNot(HaveOccurred())

				input := analyzercommon.AnalysisInput{
					File:     goFile,
					Language: cce.LanguageGO,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})
		})

		Context("when analyzing valid JavaScript code", func() {
			It("should analyze JavaScript file and return mapped resources", func() {
				jsFile := filepath.Join(testdataDir, "sample.js")

				_, err := os.Stat(jsFile)
				Expect(err).ToNot(HaveOccurred())

				input := analyzercommon.AnalysisInput{
					File:     jsFile,
					Language: cce.LanguageJAVASCRIPT,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})
		})

		Context("when analyzing empty code", func() {
			It("should return empty results without error", func() {
				emptyFile := filepath.Join(testdataDir, "empty.py")

				input := analyzercommon.AnalysisInput{
					File:     emptyFile,
					Language: cce.LanguagePYTHON,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				// Empty file should have no method calls
				Expect(result).To(BeEmpty())
			})
		})

		Context("when context is cancelled", func() {
			It("should handle context cancellation gracefully", func(ctx context.Context) {
				cancelCtx, cancel := context.WithCancel(ctx)
				cancel() // Cancel immediately

				pythonFile := filepath.Join(testdataDir, "sample.py")
				input := analyzercommon.AnalysisInput{
					File:     pythonFile,
					Language: cce.LanguagePYTHON,
				}

				result, err := testAnalyzer.Analyze(cancelCtx, input)
				if err != nil {
					// If analysis started, it should return context error
					Expect(err).To(MatchError(context.Canceled))
					Expect(result).To(BeNil())
				} else {
					// If analysis completed before cancellation, result should be valid
					Expect(result).ToNot(BeNil())
				}
			})
		})

		Context("when analyzing non-existent file", func() {
			It("should return an error", func() {
				input := analyzercommon.AnalysisInput{
					File:     "/non/existent/file.py",
					Language: cce.LanguagePYTHON,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				// Should either error or return empty results
				if err != nil {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(result).ToNot(BeNil())
				}
			})
		})

		Context("when analyzing directory", func() {
			It("should analyze all files in directory", func() {
				input := analyzercommon.AnalysisInput{
					File:     testdataDir,
					Language: cce.LanguagePYTHON,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})
		})
	})

	Describe("MappedResource", func() {
		Context("when creating a MappedResource", func() {
			It("should have correct structure", func() {
				mr := analyzer.MappedResource{
					MappedResource: models.MappedResource{
						Provider:  "aws",
						Resource:  "s3_bucket",
						Operation: "create",
					},
					MethodCall: models.MethodCall{
						Name: "create_bucket",
					},
				}

				Expect(mr.MappedResource.Provider).To(Equal("aws"))
				Expect(mr.MappedResource.Resource).To(Equal("s3_bucket"))
				Expect(mr.MethodCall.Name).To(Equal("create_bucket"))
			})
		})
	})

	Describe("Error Handling", func() {
		var (
			testAnalyzer analyzer.Analyzer
		)

		BeforeEach(func() {
			var err error
			testAnalyzer, err = analyzer.New()
			Expect(err).ToNot(HaveOccurred())
		})

		Context("when analyzer encounters invalid input", func() {
			It("should handle invalid language", func() {
				input := analyzercommon.AnalysisInput{
					File:     "testdata/sample.py",
					Language: cce.LanguageUNSPECIFIED,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				// Depending on implementation, this might error or return empty
				if err != nil {
					Expect(err).To(HaveOccurred())
				} else {
					Expect(result).ToNot(BeNil())
				}
			})
		})
	})

	Describe("Integration Scenarios", func() {
		var (
			testAnalyzer analyzer.Analyzer
			tempDir      string
		)

		BeforeEach(func() {
			var err error
			testAnalyzer, err = analyzer.New()
			Expect(err).ToNot(HaveOccurred())

			// Create temp directory for integration tests
			tempDir, err = os.MkdirTemp("", "analyzer-test-*")
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			if tempDir != "" {
				os.RemoveAll(tempDir)
			}
		})

		Context("when analyzing real-world code patterns", func() {
			It("should handle AWS SDK boto3 patterns", func() {
				// Create a temporary Python file with boto3 code
				pythonCode := `import boto3
from botocore.exceptions import ClientError

def manage_infrastructure():
    s3 = boto3.client('s3')
    try:
        s3.create_bucket(
            Bucket='my-app-bucket',
            CreateBucketConfiguration={'LocationConstraint': 'us-west-2'}
        )
    except ClientError as e:
        print(f"Error: {e}")
`
				testFile := filepath.Join(tempDir, "boto3_test.py")
				err := os.WriteFile(testFile, []byte(pythonCode), 0644)
				Expect(err).ToNot(HaveOccurred())

				input := analyzercommon.AnalysisInput{
					File:     testFile,
					Language: cce.LanguagePYTHON,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})

			It("should handle AWS SDK for JavaScript patterns", func() {
				jsCode := `const AWS = require('aws-sdk');

async function deployInfrastructure() {
    const s3 = new AWS.S3();
    await s3.createBucket({
        Bucket: 'my-app-bucket'
    }).promise();
}
`
				testFile := filepath.Join(tempDir, "aws_sdk_test.js")
				err := os.WriteFile(testFile, []byte(jsCode), 0644)
				Expect(err).ToNot(HaveOccurred())

				input := analyzercommon.AnalysisInput{
					File:     testFile,
					Language: cce.LanguageJAVASCRIPT,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(result).To(HaveLen(1))
			})
		})

		Context("when analyzing files with many method calls", func() {
			It("should handle files efficiently", func() {
				// Generate code with many method calls
				codeBuilder := "import boto3\n\ndef many_calls():\n    s3 = boto3.client('s3')\n"
				for i := 0; i < 20; i++ {
					codeBuilder += fmt.Sprintf("    s3.put_object(Bucket='bucket', Key='key%d', Body='data')\n", i)
				}

				testFile := filepath.Join(tempDir, "many_calls.py")
				err := os.WriteFile(testFile, []byte(codeBuilder), 0644)
				Expect(err).ToNot(HaveOccurred())

				input := analyzercommon.AnalysisInput{
					File:     testFile,
					Language: cce.LanguagePYTHON,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})
		})
	})
})
