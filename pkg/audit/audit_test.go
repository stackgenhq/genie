// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package audit_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackgenhq/genie/pkg/audit"
)

// expectedRotatingPath returns the path where NewRotatingFileAuditor(agentName)
// writes when HOME is set to homeDir: ~/.genie/{agent}.<today>.ndjson.
func expectedRotatingPath(homeDir, agentName string) string {
	safe := agentName
	if safe == "" {
		safe = "genie"
	}
	today := time.Now().UTC().Format("2006_01_02")
	return filepath.Join(homeDir, ".genie", safe+"."+today+".ndjson")
}

var _ = Describe("FileAuditor", func() {
	var (
		tmpDir    string
		restore   func()
		logFile   string
		agentName string
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "audit-test-*")
		Expect(err).NotTo(HaveOccurred())
		restore = setEnv("HOME", tmpDir)
		agentName = "test_agent"
		logFile = expectedRotatingPath(tmpDir, agentName)
	})

	AfterEach(func() {
		restore()
		os.RemoveAll(tmpDir)
	})

	Describe("NewRotatingFileAuditor", func() {
		It("should create an auditor that writes to the default path on first Log", func(ctx context.Context) {
			auditor, err := audit.NewRotatingFileAuditor("test-agent")
			Expect(err).NotTo(HaveOccurred())
			Expect(auditor).NotTo(BeNil())
			auditor.Log(ctx, audit.LogRequest{
				EventType: audit.EventConnection,
				Actor:     "user",
				Action:    "connect",
			})
			Expect(auditor.Close()).To(Succeed())

			_, err = os.Stat(logFile)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create the file with 0600 permissions", func(ctx context.Context) {
			auditor, err := audit.NewRotatingFileAuditor("test-agent")
			Expect(err).NotTo(HaveOccurred())
			defer auditor.Close()
			auditor.Log(ctx, audit.LogRequest{
				EventType: audit.EventConnection,
				Actor:     "user",
				Action:    "connect",
			})

			info, err := os.Stat(logFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
		})

		It("should append to an existing file when multiple auditors use the same agent", func(ctx context.Context) {
			auditor1, err := audit.NewRotatingFileAuditor("test-agent")
			Expect(err).NotTo(HaveOccurred())
			auditor1.Log(ctx, audit.LogRequest{
				EventType: audit.EventConnection,
				Actor:     "first",
				Action:    "connect",
			})
			Expect(auditor1.Close()).To(Succeed())

			auditor2, err := audit.NewRotatingFileAuditor("test-agent")
			Expect(err).NotTo(HaveOccurred())
			auditor2.Log(ctx, audit.LogRequest{
				EventType: audit.EventCommand,
				Actor:     "second",
				Action:    "run",
			})
			Expect(auditor2.Close()).To(Succeed())

			data, err := os.ReadFile(logFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(ContainSubstring("first"))
			Expect(string(data)).To(ContainSubstring("second"))
		})
	})

	Describe("Log", func() {
		var auditor *audit.FileAuditor

		BeforeEach(func() {
			var err error
			auditor, err = audit.NewRotatingFileAuditor("test-agent")
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			auditor.Close()
		})

		It("should write a JSON log entry with all required fields", func(ctx context.Context) {
			auditor.Log(ctx, audit.LogRequest{
				EventType: audit.EventConnection,
				Actor:     "user@example.com",
				Action:    "connect",
			})

			data, err := os.ReadFile(logFile)
			Expect(err).NotTo(HaveOccurred())

			var entry map[string]interface{}
			Expect(json.Unmarshal(data, &entry)).To(Succeed())

			Expect(entry).To(HaveKey("event_type"))
			Expect(entry["event_type"]).To(Equal("connection"))
			Expect(entry).To(HaveKey("actor"))
			Expect(entry["actor"]).To(Equal("user@example.com"))
			Expect(entry).To(HaveKey("action"))
			Expect(entry["action"]).To(Equal("connect"))
			Expect(entry).To(HaveKey("timestamp"))
			Expect(entry).To(HaveKey("msg"))
			Expect(entry["msg"]).To(Equal("audit_event"))
		})

		It("should include metadata when provided", func(ctx context.Context) {
			auditor.Log(ctx, audit.LogRequest{
				EventType: audit.EventCommand,
				Actor:     "admin",
				Action:    "deploy",
				Metadata: map[string]interface{}{
					"target": "production",
					"count":  float64(3),
				},
			})

			data, err := os.ReadFile(logFile)
			Expect(err).NotTo(HaveOccurred())

			var entry map[string]interface{}
			Expect(json.Unmarshal(data, &entry)).To(Succeed())

			Expect(entry).To(HaveKey("metadata"))
			metadata, ok := entry["metadata"].(map[string]interface{})
			Expect(ok).To(BeTrue(), "metadata should be a JSON object")
			Expect(metadata["target"]).To(Equal("production"))
		})

		It("should not include metadata when empty", func(ctx context.Context) {
			auditor.Log(ctx, audit.LogRequest{
				EventType: audit.EventError,
				Actor:     "system",
				Action:    "crash",
			})

			data, err := os.ReadFile(logFile)
			Expect(err).NotTo(HaveOccurred())

			var entry map[string]interface{}
			Expect(json.Unmarshal(data, &entry)).To(Succeed())

			Expect(entry).NotTo(HaveKey("metadata"))
		})

		DescribeTable("should record different event types",
			func(eventType audit.EventType, expected string) {
				auditor.Log(context.Background(), audit.LogRequest{
					EventType: eventType,
					Actor:     "test",
					Action:    "test-action",
				})

				data, err := os.ReadFile(logFile)
				Expect(err).NotTo(HaveOccurred())

				var entry map[string]interface{}
				Expect(json.Unmarshal(data, &entry)).To(Succeed())
				Expect(entry["event_type"]).To(Equal(expected))
			},
			Entry("connection", audit.EventConnection, "connection"),
			Entry("disconnection", audit.EventDisconnection, "disconnection"),
			Entry("command", audit.EventCommand, "command"),
			Entry("error", audit.EventError, "error"),
			Entry("secret_access", audit.EventSecretAccess, "secret_access"),
		)

		It("should write multiple log entries", func(ctx context.Context) {
			for i := 0; i < 3; i++ {
				auditor.Log(ctx, audit.LogRequest{
					EventType: audit.EventCommand,
					Actor:     "user",
					Action:    "action",
				})
			}

			data, err := os.ReadFile(logFile)
			Expect(err).NotTo(HaveOccurred())

			lines := 0
			for _, b := range data {
				if b == '\n' {
					lines++
				}
			}
			Expect(lines).To(Equal(3))
		})
	})

	Describe("Recent", func() {
		It("returns empty when no audit files exist", func(ctx context.Context) {
			auditor, err := audit.NewRotatingFileAuditor("my_agent")
			Expect(err).NotTo(HaveOccurred())
			defer auditor.Close()

			events, err := auditor.Recent(ctx, audit.LookupRequest{
				AgentName: "my_agent",
				Since:     time.Now().Add(-24 * time.Hour),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(BeEmpty())
		})

		It("reads and parses audit_event lines from the audit file", func(ctx context.Context) {
			reportDate := time.Date(2026, 2, 27, 0, 0, 0, 0, time.UTC)
			path := audit.DefaultAuditPathForDate("test_agent", reportDate)
			Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
			line1 := `{"msg":"audit_event","event_type":"tool_call","actor":"user","action":"memory_search","timestamp":"2026-02-27T10:00:00Z"}
`
			line2 := `{"msg":"audit_event","event_type":"conversation","actor":"agent","action":"Hello","timestamp":"2026-02-27T10:01:00Z"}
`
			line3 := `{"msg":"other","event_type":"skip"}
`
			Expect(os.WriteFile(path, []byte(line1+line2+line3), 0o600)).To(Succeed())

			auditor, err := audit.NewRotatingFileAuditor("test_agent")
			Expect(err).NotTo(HaveOccurred())
			defer auditor.Close()

			events, err := auditor.Recent(ctx, audit.LookupRequest{
				AgentName: "test_agent",
				Since:     reportDate,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(2))
			Expect(events[0].EventType).To(Equal("tool_call"))
			Expect(events[0].Action).To(Equal("memory_search"))
			Expect(events[1].EventType).To(Equal("conversation"))
			Expect(events[1].Action).To(Equal("Hello"))
		})

		It("returns empty when since is in the future", func(ctx context.Context) {
			auditor, err := audit.NewRotatingFileAuditor("agent")
			Expect(err).NotTo(HaveOccurred())
			defer auditor.Close()

			events, err := auditor.Recent(ctx, audit.LookupRequest{
				AgentName: "agent",
				Since:     time.Now().Add(24 * time.Hour),
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(BeEmpty())
		})
	})

	Describe("Close", func() {
		It("should close the underlying file and be idempotent", func() {
			auditor, err := audit.NewRotatingFileAuditor("test-agent")
			Expect(err).NotTo(HaveOccurred())

			Expect(auditor.Close()).To(Succeed())
			Expect(auditor.Close()).To(Succeed())
		})
	})
})

func setEnv(key, value string) (restore func()) {
	prev, had := os.LookupEnv(key)
	os.Setenv(key, value)
	return func() {
		if had {
			os.Setenv(key, prev)
		} else {
			os.Unsetenv(key)
		}
	}
}

var _ = Describe("DefaultAuditPathForDate", func() {
	It("embeds the given date in the path", func() {
		path := audit.DefaultAuditPathForDate("my-agent", time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC))
		Expect(path).To(ContainSubstring("2026_03_15"))
		Expect(path).To(ContainSubstring("my_agent"))
		Expect(path).To(HaveSuffix(".ndjson"))
	})
})

var _ = Describe("NewRotatingFileAuditor", func() {
	It("writes to a file with today's date (path resolved at log time)", func(ctx context.Context) {
		tmpDir, err := os.MkdirTemp("", "audit-rotating-*")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)
		// Point HOME at tmpDir so GenieDir() is under our test dir
		restore := setEnv("HOME", tmpDir)
		defer restore()

		auditor, err := audit.NewRotatingFileAuditor("test-agent")
		Expect(err).NotTo(HaveOccurred())
		defer auditor.Close()

		auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventConnection,
			Actor:     "test",
			Action:    "connect",
		})

		today := time.Now().UTC().Format("2006_01_02")
		expectedPath := filepath.Join(tmpDir, ".genie", "test_agent."+today+".ndjson")
		_, err = os.Stat(expectedPath)
		Expect(err).NotTo(HaveOccurred(), "rotating auditor should create file for today at ~/.genie/{agent}.<date>.ndjson")
	})

	It("accepts empty agentName and uses genie as basename", func(ctx context.Context) {
		tmpDir, err := os.MkdirTemp("", "audit-rotating-*")
		Expect(err).NotTo(HaveOccurred())
		defer os.RemoveAll(tmpDir)
		restore := setEnv("HOME", tmpDir)
		defer restore()

		auditor, err := audit.NewRotatingFileAuditor("")
		Expect(err).NotTo(HaveOccurred())
		defer auditor.Close()

		auditor.Log(ctx, audit.LogRequest{
			EventType: audit.EventConnection,
			Actor:     "test",
			Action:    "connect",
		})

		today := time.Now().UTC().Format("2006_01_02")
		expectedPath := filepath.Join(tmpDir, ".genie", "genie."+today+".ndjson")
		_, err = os.Stat(expectedPath)
		Expect(err).NotTo(HaveOccurred(), "empty agentName should use genie.<date>.ndjson")
	})
})
