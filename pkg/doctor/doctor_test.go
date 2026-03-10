// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package doctor_test

import (
	"context"
	"os"
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
		It("returns no model_config error when no providers configured", func(ctx context.Context) {
			if os.Getenv("CI") == "true" {
				Skip("Skipping in CI since API tokens are not present")
			}
			cfg := config.GenieConfig{}
			results := doctor.Run(ctx, cfg, "", security.NewEnvProvider())
			Expect(results).NotTo(BeNil())
			// With zero providers configured, ValidateAndFilter no longer
			// errors — it returns nil (nothing to validate).
			modelErr := results.GetSection("model_config")
			Expect(modelErr).To(BeNil())
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
