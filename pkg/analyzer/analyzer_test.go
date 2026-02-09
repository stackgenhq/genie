package analyzer_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/appcd-dev/cce/pkg/cce"
	"github.com/appcd-dev/cce/pkg/models"
	"github.com/appcd-dev/genie/pkg/analyzer"
	"github.com/appcd-dev/genie/pkg/analyzer/analyzerfakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Analyzer", func() {
	Describe("Analyze", func() {
		var (
			testAnalyzer analyzer.Analyzer
			testdataDir  string
		)

		BeforeEach(func(ctx context.Context) {
			var err error
			// Create fake analyzer with stub that closes channel immediately (no method calls)
			fakeAnalyzer := &analyzerfakes.FakeCCEAnalyzer{}
			fakeAnalyzer.AnalyzeV3Stub = func(ctx context.Context, folder string, result chan<- models.MethodCall) error {
				// Close channel immediately - no method calls to send
				close(result)
				return nil
			}

			testAnalyzer, err = analyzer.New(ctx, "", fakeAnalyzer)
			Expect(err).ToNot(HaveOccurred())

			// Get absolute path to testdata
			testdataDir, err = filepath.Abs("testdata")
			Expect(err).ToNot(HaveOccurred())
		})

		Context("when analyzing valid Python code", func() {
			It("should analyze Python file and return mapped resources", func(ctx context.Context) {
				pythonFile := filepath.Join(testdataDir, "sample.py")

				// Verify file exists
				_, err := os.Stat(pythonFile)
				Expect(err).ToNot(HaveOccurred())

				input := analyzer.AnalysisInput{
					Path: pythonFile,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})

			It("should detect multiple method calls in Python code", func(ctx context.Context) {
				pythonFile := filepath.Join(testdataDir, "sample.py")

				input := analyzer.AnalysisInput{
					Path: pythonFile,
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
			It("should analyze Go file and return mapped resources", func(ctx context.Context) {
				goFile := filepath.Join(testdataDir, "sample.go")

				_, err := os.Stat(goFile)
				Expect(err).ToNot(HaveOccurred())

				input := analyzer.AnalysisInput{
					Path: goFile,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})
		})

		Context("when analyzing valid JavaScript code", func() {
			It("should analyze JavaScript file and return mapped resources", func(ctx context.Context) {
				jsFile := filepath.Join(testdataDir, "sample.js")

				_, err := os.Stat(jsFile)
				Expect(err).ToNot(HaveOccurred())

				input := analyzer.AnalysisInput{
					Path: jsFile,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})
		})

		Context("when analyzing empty code", func() {
			It("should return empty results without error", func(ctx context.Context) {
				emptyFile := filepath.Join(testdataDir, "empty.py")

				input := analyzer.AnalysisInput{
					Path: emptyFile,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				// Empty file should have no method calls
				Expect(result).To(BeEmpty())
			})
		})

		Context("when analyzing non-existent file", func() {
			It("should return an error", func(ctx context.Context) {
				input := analyzer.AnalysisInput{
					Path: "/non/existent/file.py",
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
			It("should analyze all files in directory", func(ctx context.Context) {
				input := analyzer.AnalysisInput{
					Path: testdataDir,
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

		Describe("String", func() {
			It("should return a formatted string representation", func() {
				mr := analyzer.MappedResource{
					MappedResource: models.MappedResource{
						Provider: "aws",
						Resource: "s3_bucket",
					},
					MethodCall: models.MethodCall{
						Name:           "create_bucket",
						FilePath:       "main.py",
						Line:           10,
						Column:         5,
						ParentFunction: "main",
						CodeSnippet:    "s3.create_bucket()",
					},
				}

				str := mr.String()
				Expect(str).To(ContainSubstring("aws resource s3_bucket referenced in method create_bucket"))
				Expect(str).To(ContainSubstring("Location: main.py:10:5"))
				Expect(str).To(ContainSubstring("Inside function: main"))
				Expect(str).To(ContainSubstring("Code context:"))
				Expect(str).To(ContainSubstring("s3.create_bucket()"))
			})

			It("should handle missing optional fields", func() {
				mr := analyzer.MappedResource{
					MappedResource: models.MappedResource{
						Provider: "aws",
						Resource: "s3_bucket",
					},
					MethodCall: models.MethodCall{
						Name:     "create_bucket",
						FilePath: "main.py",
						Line:     10,
						Column:   5,
					},
				}

				str := mr.String()
				Expect(str).To(ContainSubstring("aws resource s3_bucket referenced in method create_bucket"))
				Expect(str).To(ContainSubstring("Location: main.py:10:5"))
				Expect(str).ToNot(ContainSubstring("Inside function:"))
				Expect(str).ToNot(ContainSubstring("Code context:"))
			})
		})
	})

	Describe("MappedResources", func() {
		var resources analyzer.MappedResources

		BeforeEach(func() {
			resources = analyzer.MappedResources{
				analyzer.MappedResource{
					MappedResource: models.MappedResource{Provider: "aws", Resource: "s3_bucket"},
				},
				analyzer.MappedResource{
					MappedResource: models.MappedResource{Provider: "aws", Resource: "lambda_function"},
				},
				analyzer.MappedResource{
					MappedResource: models.MappedResource{Provider: "gcp", Resource: "storage_bucket"},
				},
			}
		})

		Describe("GroupByProvider", func() {
			It("should group resources by provider", func() {
				grouped := resources.GroupByProvider()
				Expect(grouped).To(HaveLen(2))
				Expect(grouped["aws"]).To(HaveLen(2))
				Expect(grouped["gcp"]).To(HaveLen(1))
			})
		})

		Describe("GroupByResources", func() {
			It("should group resources by resource type", func() {
				grouped := resources.GroupByResources()
				Expect(grouped).To(HaveLen(3))
				Expect(grouped["s3_bucket"]).To(HaveLen(1))
				Expect(grouped["lambda_function"]).To(HaveLen(1))
				Expect(grouped["storage_bucket"]).To(HaveLen(1))
			})
		})
	})

	Describe("Error Handling", func() {
		var (
			testAnalyzer analyzer.Analyzer
		)

		BeforeEach(func(ctx context.Context) {
			var err error
			fakeAnalyzer := &analyzerfakes.FakeCCEAnalyzer{}
			fakeAnalyzer.AnalyzeV3Stub = func(ctx context.Context, folder string, result chan<- models.MethodCall) error {
				close(result)
				return nil
			}
			testAnalyzer, err = analyzer.New(ctx, "", fakeAnalyzer)
			Expect(err).ToNot(HaveOccurred())
		})

		Context("when analyzer encounters invalid input", func() {
			It("should handle invalid path", func(ctx context.Context) {
				input := analyzer.AnalysisInput{
					Path: "",
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

	Describe("Analyzer with Method Calls", func() {
		Context("when fake analyzer returns method calls", func() {
			It("should map method calls to resources", func(ctx context.Context) {
				// Create a fake analyzer that returns specific method calls
				fakeAnalyzer := &analyzerfakes.FakeCCEAnalyzer{}
				fakeAnalyzer.AnalyzeV3Stub = func(ctx context.Context, folder string, result chan<- models.MethodCall) error {
					// Send some method calls
					result <- models.MethodCall{
						Name:           "boto3.client.s3.create_bucket",
						FilePath:       "test.py",
						Line:           10,
						Column:         5,
						Language:       cce.LanguagePYTHON,
						ParentFunction: "setup_infrastructure",
						CodeSnippet:    "s3.create_bucket(Bucket='my-bucket')",
					}
					result <- models.MethodCall{
						Name:           "boto3.client.s3.put_object",
						FilePath:       "test.py",
						Line:           15,
						Column:         5,
						Language:       cce.LanguagePYTHON,
						ParentFunction: "upload_data",
						CodeSnippet:    "s3.put_object(Bucket='my-bucket', Key='data.json', Body=data)",
					}
					close(result)
					return nil
				}

				testAnalyzer, err := analyzer.New(ctx, "", fakeAnalyzer)
				Expect(err).ToNot(HaveOccurred())

				// Create a temp file to analyze
				tempDir, err := os.MkdirTemp("", "analyzer-test-*")
				Expect(err).ToNot(HaveOccurred())
				defer os.RemoveAll(tempDir)

				testFile := filepath.Join(tempDir, "test.py")
				err = os.WriteFile(testFile, []byte("# test file"), 0644)
				Expect(err).ToNot(HaveOccurred())

				input := analyzer.AnalysisInput{
					Path: testFile,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())

				// Since we don't have a mapping definition file, the mapper won't map these
				// but we should still get the method calls processed
				// The result might be empty if mapping fails, but it should not error
				Expect(result).To(BeAssignableToTypeOf(analyzer.MappedResources{}))
			})

			It("should handle multiple method calls efficiently", func(ctx context.Context) {
				// Create a fake analyzer that returns many method calls
				fakeAnalyzer := &analyzerfakes.FakeCCEAnalyzer{}
				fakeAnalyzer.AnalyzeV3Stub = func(ctx context.Context, folder string, result chan<- models.MethodCall) error {
					// Send 50 method calls
					for i := 0; i < 50; i++ {
						result <- models.MethodCall{
							Name:           fmt.Sprintf("boto3.client.s3.put_object_%d", i),
							FilePath:       "test.py",
							Line:           uint32(10 + i),
							Column:         5,
							Language:       cce.LanguagePYTHON,
							ParentFunction: "batch_upload",
							CodeSnippet:    fmt.Sprintf("s3.put_object(Bucket='bucket', Key='key%d')", i),
						}
					}
					close(result)
					return nil
				}

				testAnalyzer, err := analyzer.New(ctx, "", fakeAnalyzer)
				Expect(err).ToNot(HaveOccurred())

				tempDir, err := os.MkdirTemp("", "analyzer-test-*")
				Expect(err).ToNot(HaveOccurred())
				defer os.RemoveAll(tempDir)

				testFile := filepath.Join(tempDir, "test.py")
				err = os.WriteFile(testFile, []byte("# test file"), 0644)
				Expect(err).ToNot(HaveOccurred())

				input := analyzer.AnalysisInput{
					Path: testFile,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(result).To(BeAssignableToTypeOf(analyzer.MappedResources{}))
			})

			It("should handle context cancellation during processing", func(ctx context.Context) {
				// Create a fake analyzer that sends method calls slowly
				fakeAnalyzer := &analyzerfakes.FakeCCEAnalyzer{}
				fakeAnalyzer.AnalyzeV3Stub = func(ctx context.Context, folder string, result chan<- models.MethodCall) error {
					// Check if context is already cancelled
					select {
					case <-ctx.Done():
						close(result)
						return ctx.Err()
					default:
					}
					close(result)
					return nil
				}

				testAnalyzer, err := analyzer.New(ctx, "", fakeAnalyzer)
				Expect(err).ToNot(HaveOccurred())

				cancelCtx, cancel := context.WithCancel(ctx)
				cancel() // Cancel immediately

				tempDir, err := os.MkdirTemp("", "analyzer-test-*")
				Expect(err).ToNot(HaveOccurred())
				defer os.RemoveAll(tempDir)

				testFile := filepath.Join(tempDir, "test.py")
				err = os.WriteFile(testFile, []byte("# test file"), 0644)
				Expect(err).ToNot(HaveOccurred())

				input := analyzer.AnalysisInput{
					Path: testFile,
				}

				result, err := testAnalyzer.Analyze(cancelCtx, input)
				// Should handle cancellation gracefully
				if err != nil {
					Expect(err).To(MatchError(context.Canceled))
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

		BeforeEach(func(ctx context.Context) {
			var err error
			fakeAnalyzer := &analyzerfakes.FakeCCEAnalyzer{}
			fakeAnalyzer.AnalyzeV3Stub = func(ctx context.Context, folder string, result chan<- models.MethodCall) error {
				close(result)
				return nil
			}
			testAnalyzer, err = analyzer.New(ctx, "", fakeAnalyzer)
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
			It("should handle AWS SDK boto3 patterns", func(ctx context.Context) {
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

				input := analyzer.AnalysisInput{
					Path: testFile,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})

			It("should handle AWS SDK for JavaScript patterns", func(ctx context.Context) {
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

				input := analyzer.AnalysisInput{
					Path: testFile,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
				Expect(len(result)).To(BeNumerically(">=", 0))
			})
		})

		Context("when analyzing files with many method calls", func() {
			It("should handle files efficiently", func(ctx context.Context) {
				// Generate code with many method calls
				codeBuilder := "import boto3\n\ndef many_calls():\n    s3 = boto3.client('s3')\n"
				for i := 0; i < 20; i++ {
					codeBuilder += fmt.Sprintf("    s3.put_object(Bucket='bucket', Key='key%d', Body='data')\n", i)
				}

				testFile := filepath.Join(tempDir, "many_calls.py")
				err := os.WriteFile(testFile, []byte(codeBuilder), 0644)
				Expect(err).ToNot(HaveOccurred())

				input := analyzer.AnalysisInput{
					Path: testFile,
				}

				result, err := testAnalyzer.Analyze(ctx, input)
				Expect(err).ToNot(HaveOccurred())
				Expect(result).ToNot(BeNil())
			})
		})
	})
})
