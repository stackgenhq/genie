package doctor_test

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/config"
	"github.com/stackgenhq/genie/pkg/doctor"
	"github.com/stackgenhq/genie/pkg/mcp"
	"github.com/stackgenhq/genie/pkg/security"
)

func TestDoctor(t *testing.T) {
	t.Parallel()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Doctor Suite")
}

var _ = Describe("Doctor", func() {
	Describe("Run", func() {
		It("returns model_config error when no providers configured", func(ctx context.Context) {
			cfg := config.GenieConfig{}
			results := doctor.Run(ctx, cfg, "", security.NewEnvProvider())
			Expect(results).NotTo(BeNil())
			Expect(doctor.HasErrors(results)).To(BeTrue())
			var modelErr *doctor.Result
			for i := range results {
				if results[i].Section == "model_config" {
					modelErr = &results[i]
					break
				}
			}
			Expect(modelErr).NotTo(BeNil())
			Expect(modelErr.ErrCode).To(Equal(doctor.ErrCodeModelNoProviders))
		})

		It("reports MCP config invalid when transport missing", func(ctx context.Context) {
			cfg := config.GenieConfig{
				MCP: mcp.MCPConfig{
					Servers: []mcp.MCPServerConfig{
						{Name: "x", Command: "npx"}, // transport missing
					},
				},
			}
			results := doctor.Run(ctx, cfg, "", security.NewEnvProvider())
			var mcpErr *doctor.Result
			for i := range results {
				if results[i].Section == "mcp" && results[i].ErrCode == doctor.ErrCodeMCPConfigInvalid {
					mcpErr = &results[i]
					break
				}
			}
			Expect(mcpErr).NotTo(BeNil())
		})
	})

	Describe("HasErrors", func() {
		It("returns false when no errors", func() {
			Expect(doctor.HasErrors(nil)).To(BeFalse())
			Expect(doctor.HasErrors([]doctor.Result{
				{Level: doctor.SeverityInfo},
				{Level: doctor.SeverityWarning},
			})).To(BeFalse())
		})

		It("returns true when any result is error", func() {
			Expect(doctor.HasErrors([]doctor.Result{
				{Level: doctor.SeverityError},
			})).To(BeTrue())
			Expect(doctor.HasErrors([]doctor.Result{
				{Level: doctor.SeverityInfo},
				{Level: doctor.SeverityError},
			})).To(BeTrue())
		})
	})
})
