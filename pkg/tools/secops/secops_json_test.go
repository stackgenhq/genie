package secops

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PolicyCheckRequest JSON Serialization", func() {
	Context("when unmarshaling from JSON", func() {
		It("should correctly unmarshal iac_path to IACSource field", func() {
			jsonData := []byte(`{"iac_path": "/path/to/terraform"}`)
			var req PolicyCheckRequest
			err := json.Unmarshal(jsonData, &req)
			Expect(err).ToNot(HaveOccurred())
			Expect(req.IACSource).To(Equal("/path/to/terraform"))
		})

		It("should handle empty iac_path", func() {
			jsonData := []byte(`{"iac_path": ""}`)
			var req PolicyCheckRequest
			err := json.Unmarshal(jsonData, &req)
			Expect(err).ToNot(HaveOccurred())
			Expect(req.IACSource).To(Equal(""))
		})

		It("should handle missing iac_path field", func() {
			jsonData := []byte(`{}`)
			var req PolicyCheckRequest
			err := json.Unmarshal(jsonData, &req)
			Expect(err).ToNot(HaveOccurred())
			Expect(req.IACSource).To(Equal(""))
		})

		It("should handle absolute paths", func() {
			jsonData := []byte(`{"iac_path": "/Users/test/genie/genie_output"}`)
			var req PolicyCheckRequest
			err := json.Unmarshal(jsonData, &req)
			Expect(err).ToNot(HaveOccurred())
			Expect(req.IACSource).To(Equal("/Users/test/genie/genie_output"))
		})

		It("should handle relative paths", func() {
			jsonData := []byte(`{"iac_path": "./terraform"}`)
			var req PolicyCheckRequest
			err := json.Unmarshal(jsonData, &req)
			Expect(err).ToNot(HaveOccurred())
			Expect(req.IACSource).To(Equal("./terraform"))
		})

		It("should handle paths with spaces", func() {
			jsonData := []byte(`{"iac_path": "/path/with spaces/terraform"}`)
			var req PolicyCheckRequest
			err := json.Unmarshal(jsonData, &req)
			Expect(err).ToNot(HaveOccurred())
			Expect(req.IACSource).To(Equal("/path/with spaces/terraform"))
		})
	})

	Context("when marshaling to JSON", func() {
		It("should correctly marshal IACSource to iac_path field", func() {
			req := PolicyCheckRequest{
				IACSource: "/path/to/terraform",
			}
			jsonData, err := json.Marshal(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(jsonData)).To(Equal(`{"iac_path":"/path/to/terraform"}`))
		})

		It("should handle empty IACSource", func() {
			req := PolicyCheckRequest{
				IACSource: "",
			}
			jsonData, err := json.Marshal(req)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(jsonData)).To(Equal(`{"iac_path":""}`))
		})
	})

	Context("round-trip serialization", func() {
		It("should preserve the path through marshal and unmarshal", func() {
			original := PolicyCheckRequest{
				IACSource: "/Users/sabithks/src/github.com/appcd-dev/genie/genie_output",
			}

			// Marshal to JSON
			jsonData, err := json.Marshal(original)
			Expect(err).ToNot(HaveOccurred())

			// Unmarshal back
			var decoded PolicyCheckRequest
			err = json.Unmarshal(jsonData, &decoded)
			Expect(err).ToNot(HaveOccurred())

			// Verify they match
			Expect(decoded.IACSource).To(Equal(original.IACSource))
		})
	})
})

var _ = Describe("PolicyCheckResponse JSON Serialization", func() {
	Context("when marshaling to JSON", func() {
		It("should correctly marshal compliant response with no violations", func() {
			resp := PolicyCheckResponse{
				Compliant:  true,
				Violations: []PolicyViolation{},
			}
			jsonData, err := json.Marshal(resp)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(jsonData)).To(ContainSubstring(`"Compliant":true`))
			Expect(string(jsonData)).To(ContainSubstring(`"Violations":[]`))
		})

		It("should correctly marshal non-compliant response with violations", func() {
			resp := PolicyCheckResponse{
				Compliant: false,
				Violations: []PolicyViolation{
					{
						PolicyName:  "S3 Bucket Encryption",
						Description: "S3 bucket does not have encryption enabled",
						FilePath:    "main.tf",
						Severity:    "high",
					},
				},
			}
			jsonData, err := json.Marshal(resp)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(jsonData)).To(ContainSubstring(`"Compliant":false`))
			Expect(string(jsonData)).To(ContainSubstring(`"S3 Bucket Encryption"`))
			Expect(string(jsonData)).To(ContainSubstring(`"main.tf"`))
			Expect(string(jsonData)).To(ContainSubstring(`"high"`))
		})
	})
})
