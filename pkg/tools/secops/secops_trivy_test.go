package secops

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"unsafe"

	"github.com/aquasecurity/trivy/pkg/iac/scan"
	"github.com/aquasecurity/trivy/pkg/iac/severity"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// mockScanner implements TrivyScanner for testing
type mockScanner struct {
	MockScanFS func(ctx context.Context, target fs.FS, path string) (scan.Results, error)
}

func (m *mockScanner) ScanFS(ctx context.Context, target fs.FS, path string) (scan.Results, error) {
	if m.MockScanFS != nil {
		return m.MockScanFS(ctx, target, path)
	}
	return nil, nil // Return empty results by default
}

// setUnexported sets a value on an unexported struct field using access
func setUnexported(field reflect.Value, value interface{}) {
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(value))
}

var _ = Describe("TrivyPolicyChecker", func() {
	var (
		ctx     context.Context
		checker TrivyPolicyChecker
		cfg     SecOpsConfig
		mock    *mockScanner
	)

	BeforeEach(func() {
		ctx = context.Background()
		cfg = SecOpsConfig{
			Scanner: "trivy",
			SeverityThresholds: SeverityThresholds{
				High:   0,
				Medium: 0,
				Low:    0,
			},
		}

		// Initialize with real constructor but swap scanner
		var err error
		checker, err = newTrivyPolicyChecker(ctx, cfg)
		Expect(err).NotTo(HaveOccurred())

		mock = &mockScanner{}
		checker.scanner = mock
	})

	Describe("Declaration", func() {
		It("should return the correct tool declaration", func() {
			decl := checker.Declaration()
			Expect(decl.Name).To(Equal("check_iac_policy"))
			Expect(decl.Description).To(ContainSubstring("Trivy"))
			Expect(decl.InputSchema.Required).To(ContainElement("iac_source"))
		})
	})

	Describe("Call", func() {
		It("should return error for invalid JSON", func() {
			_, err := checker.Call(ctx, []byte(`{invalid`))
			Expect(err).To(HaveOccurred())
		})

		It("should call CheckPolicy with valid arguments", func() {
			// Create a temp dir to be valid
			tmpDir, err := os.MkdirTemp("", "trivy-test")
			Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(tmpDir)

			req := PolicyCheckRequest{
				IACSource: tmpDir,
			}
			jsonArgs, err := json.Marshal(req)
			Expect(err).NotTo(HaveOccurred())

			// Mock success empty response
			mock.MockScanFS = func(ctx context.Context, target fs.FS, path string) (scan.Results, error) {
				return scan.Results{}, nil
			}

			// We expect no violations on an empty dir
			resp, err := checker.Call(ctx, jsonArgs)
			Expect(err).NotTo(HaveOccurred())
			policyResp, ok := resp.(PolicyCheckResponse)
			Expect(ok).To(BeTrue())
			Expect(policyResp.Compliant).To(BeTrue())
		})
	})

	Describe("CheckPolicy", func() {
		Context("when the IAC source does not exist", func() {
			It("should return an error", func() {
				req := PolicyCheckRequest{
					IACSource: "/path/to/non/existent/dir",
				}
				_, err := checker.CheckPolicy(ctx, req)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("path does not exist"))
			})
		})

		Context("with non-compliant results", func() {
			var tmpDir string

			BeforeEach(func() {
				var err error
				tmpDir, err = os.MkdirTemp("", "trivy-check-fail")
				Expect(err).NotTo(HaveOccurred())
			})

			AfterEach(func() {
				os.RemoveAll(tmpDir)
			})

			It("should report violations matching the scanner results", func() {
				req := PolicyCheckRequest{
					IACSource: tmpDir,
				}

				// Construct mocked results using reflection
				res := scan.Result{}
				rs := reflect.ValueOf(&res).Elem()

				// Set Status to Failed
				fStatus := rs.FieldByName("status")
				if fStatus.IsValid() && fStatus.CanAddr() {
					// Assuming scan.StatusFailed is available and compatible
					setUnexported(fStatus, scan.StatusFailed)
				} else {
					// If field name is wrong/unavailable
					GinkgoWriter.Println("Could not reflect status")
				}

				// Set Description
				fDesc := rs.FieldByName("description")
				if fDesc.IsValid() {
					setUnexported(fDesc, "Test Violation Description")
				}

				// Set Rule
				// Try to construct Rule. Logic assumes Rule struct has exported fields.
				// If Rule also has unexported fields, we might need to reflect on it too.
				// Based on typical usage, let's try populating public fields.
				rule := scan.Rule{
					Summary:  "Test High Severity Rule",
					AVDID:    "AVD-TEST-001",
					Severity: severity.High,
				}
				fRule := rs.FieldByName("rule")
				if fRule.IsValid() {
					setUnexported(fRule, rule)
				}

				// NOTE: Range/Filename?
				// Result.Range() might be a method iterating over something.
				// If we leave it default, it might return empty path.
				// secops_trivy.go: result.Range().GetFilename()
				// We won't verify filename in this unit test if we can't easily set it.

				mock.MockScanFS = func(ctx context.Context, target fs.FS, path string) (scan.Results, error) {
					// scan.Results is typically a slice type
					return scan.Results{res}, nil
				}

				resp, err := checker.CheckPolicy(ctx, req)
				Expect(err).NotTo(HaveOccurred())

				Expect(resp.Compliant).To(BeFalse(), "Should be non-compliant for High severity")
				Expect(resp.Violations).To(HaveLen(1))
				Expect(resp.Violations[0].PolicyName).To(Or(Equal("Test High Severity Rule"), Equal("AVD-TEST-001")))
				Expect(resp.Violations[0].Severity).To(Equal("high"))
				Expect(resp.Violations[0].Description).To(Equal("Test Violation Description"))
			})
		})

		Context("when scanner returns error", func() {
			var tmpDir string
			BeforeEach(func() {
				var err error
				tmpDir, err = os.MkdirTemp("", "trivy-err")
				Expect(err).NotTo(HaveOccurred())
			})
			AfterEach(func() { os.RemoveAll(tmpDir) })

			It("should return error", func() {
				mock.MockScanFS = func(ctx context.Context, target fs.FS, path string) (scan.Results, error) {
					return nil, fmt.Errorf("mock error")
				}
				req := PolicyCheckRequest{IACSource: tmpDir}
				_, err := checker.CheckPolicy(ctx, req)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("mock error"))
			})
		})
	})
})
