// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package app

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/config"
	geniedb "github.com/stackgenhq/genie/pkg/db"
	"github.com/stackgenhq/genie/pkg/expert/modelprovider"
	"github.com/stackgenhq/genie/pkg/messenger"
	"github.com/stackgenhq/genie/pkg/security"
)

// skipWithoutOpenAIKey skips a test if OPENAI_API_KEY is not set.
func skipWithoutOpenAIKey() {
	if os.Getenv("OPENAI_API_KEY") == "" {
		Skip("OPENAI_API_KEY not set — skipping integration test")
	}
}

// newIntegrationConfig returns a minimal GenieConfig suitable for integration
// testing. It uses a temp directory for the database, audit log, and picks up
// model provider configuration from environment variables (OPENAI_API_KEY, etc.).
func newIntegrationConfig(tmpDir string) config.GenieConfig {
	ctx := context.Background()
	sp := security.NewEnvProvider()

	return config.GenieConfig{
		ModelConfig: modelprovider.DefaultModelConfig(ctx, sp),
		DBConfig:    geniedb.Config{DBFile: filepath.Join(tmpDir, "test.db")},
		AuditPath:   filepath.Join(tmpDir, "genie_audit.ndjson"),
		// All other subsystems use zero-value defaults and degrade gracefully.
	}
}

var _ = Describe("NewApplication", func() {
	It("should fail when WorkingDir is empty", func() {
		_, err := NewApplication(config.GenieConfig{}, "")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("working directory is required"))
	})

	It("should create an application with valid params", func() {
		app, err := NewApplication(config.GenieConfig{}, "/tmp/test-genie")
		Expect(err).NotTo(HaveOccurred())
		Expect(app).NotTo(BeNil())
		Expect(app.workingDir).To(Equal("/tmp/test-genie"))
	})

	It("should default AuditPath to ~/.genie/genie.<date>.ndjson when AgentName is not set", func() {
		app, err := NewApplication(config.GenieConfig{}, "/tmp/test-genie")
		Expect(err).NotTo(HaveOccurred())
		Expect(app.auditPath).To(ContainSubstring(".genie"))
		Expect(app.auditPath).To(ContainSubstring("genie."))
		Expect(app.auditPath).To(HaveSuffix(".ndjson"))
	})

	It("should default AuditPath to ~/.genie/{agentname}.<date>.ndjson when AgentName is set", func() {
		cfg := config.GenieConfig{AgentName: "My Agent"}
		app, err := NewApplication(cfg, "/tmp/test-genie")
		Expect(err).NotTo(HaveOccurred())
		Expect(app.auditPath).To(ContainSubstring(".genie"))
		Expect(app.auditPath).To(ContainSubstring("my_agent"))
		Expect(app.auditPath).To(HaveSuffix(".ndjson"))
	})
})

var _ = Describe("Application Integration", Label("integration"), func() {
	var tmpDir string

	BeforeEach(func() {
		skipWithoutOpenAIKey()
		tmpDir = GinkgoT().TempDir()
	})

	Describe("Bootstrap + Close lifecycle", func() {
		It("should bootstrap with minimal config and close cleanly", func() {
			cfg := newIntegrationConfig(tmpDir)
			app, err := NewApplication(cfg, tmpDir)
			Expect(err).NotTo(HaveOccurred())

			ctx := context.Background()
			err = app.Bootstrap(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Verify core subsystems are initialised.
			Expect(app.db).NotTo(BeNil(), "database should be initialised")
			Expect(app.auditor).NotTo(BeNil(), "auditor should be initialised")
			Expect(app.codeOwner).NotTo(BeNil(), "codeOwner agent should be initialised")
			Expect(app.clarifyStore).NotTo(BeNil(), "clarify store should be initialised")
			Expect(app.cronStore).NotTo(BeNil(), "cron store should be initialised")
			Expect(app.shortMemory).NotTo(BeNil(), "short memory should be initialised")

			// Messenger defaults to AGUI when no external platform is configured.
			Expect(app.msgr).NotTo(BeNil(), "messenger should default to AGUI")
			Expect(app.msgr.Platform()).To(Equal(messenger.PlatformAGUI))

			// Clean up — should not panic even without Start().
			app.Close(ctx)
		})

		It("should create the database file on disk", func() {
			cfg := newIntegrationConfig(tmpDir)
			app, err := NewApplication(cfg, tmpDir)
			Expect(err).NotTo(HaveOccurred())

			ctx := context.Background()
			Expect(app.Bootstrap(ctx)).To(Succeed())
			defer app.Close(ctx)

			dbPath := filepath.Join(tmpDir, "test.db")
			_, err = os.Stat(dbPath)
			Expect(err).NotTo(HaveOccurred(), "database file should exist on disk")
		})

		It("should create the audit log file", func() {
			cfg := newIntegrationConfig(tmpDir)
			app, err := NewApplication(cfg, tmpDir)
			Expect(err).NotTo(HaveOccurred())

			ctx := context.Background()
			Expect(app.Bootstrap(ctx)).To(Succeed())
			defer app.Close(ctx)

			auditPath := filepath.Join(tmpDir, "genie_audit.ndjson")
			_, err = os.Stat(auditPath)
			Expect(err).NotTo(HaveOccurred(), "audit file should exist on disk")
		})
	})

	Describe("buildChatHandler", func() {
		It("should return a callable handler function", func() {
			cfg := newIntegrationConfig(tmpDir)
			app, err := NewApplication(cfg, tmpDir)
			Expect(err).NotTo(HaveOccurred())

			ctx := context.Background()
			Expect(app.Bootstrap(ctx)).To(Succeed())
			defer app.Close(ctx)

			handler := app.buildChatHandler()
			Expect(handler).NotTo(BeNil(), "chat handler function should be non-nil")
		})
	})

	Describe("Close idempotency", func() {
		It("should not panic when Close is called on a partially initialised application", func() {
			app, err := NewApplication(config.GenieConfig{}, tmpDir)
			Expect(err).NotTo(HaveOccurred())

			// Close without Bootstrap — all fields are nil.
			Expect(func() {
				app.Close(context.Background())
			}).NotTo(Panic())
		})
	})
})
