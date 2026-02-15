package audit_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/appcd-dev/genie/pkg/audit"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("FileAuditor", func() {
	var (
		tmpDir  string
		logFile string
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "audit-test-*")
		Expect(err).NotTo(HaveOccurred())
		logFile = filepath.Join(tmpDir, "audit.log")
	})

	AfterEach(func() {
		os.RemoveAll(tmpDir)
	})

	Describe("NewFileAuditor", func() {
		It("should create an auditor that writes to the specified file", func() {
			auditor, err := audit.NewFileAuditor(logFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(auditor).NotTo(BeNil())
			Expect(auditor.Close()).To(Succeed())

			_, err = os.Stat(logFile)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create the file with 0600 permissions", func() {
			auditor, err := audit.NewFileAuditor(logFile)
			Expect(err).NotTo(HaveOccurred())
			defer auditor.Close()

			info, err := os.Stat(logFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
		})

		It("should return an error for an invalid path", func() {
			auditor, err := audit.NewFileAuditor("/nonexistent/dir/audit.log")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to open audit log file"))
			Expect(auditor).To(BeNil())
		})

		It("should append to an existing file", func() {
			// Create and write a first event
			auditor1, err := audit.NewFileAuditor(logFile)
			Expect(err).NotTo(HaveOccurred())
			auditor1.Log(context.Background(), audit.LogRequest{
				EventType: audit.EventConnection,
				Actor:     "first",
				Action:    "connect",
			})
			Expect(auditor1.Close()).To(Succeed())

			// Create a second auditor for the same file and write another event
			auditor2, err := audit.NewFileAuditor(logFile)
			Expect(err).NotTo(HaveOccurred())
			auditor2.Log(context.Background(), audit.LogRequest{
				EventType: audit.EventCommand,
				Actor:     "second",
				Action:    "run",
			})
			Expect(auditor2.Close()).To(Succeed())

			// Both events should be in the file
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
			auditor, err = audit.NewFileAuditor(logFile)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			auditor.Close()
		})

		It("should write a JSON log entry with all required fields", func() {
			auditor.Log(context.Background(), audit.LogRequest{
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

		It("should include metadata when provided", func() {
			auditor.Log(context.Background(), audit.LogRequest{
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

		It("should not include metadata when empty", func() {
			auditor.Log(context.Background(), audit.LogRequest{
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
		)

		It("should write multiple log entries", func() {
			for i := 0; i < 3; i++ {
				auditor.Log(context.Background(), audit.LogRequest{
					EventType: audit.EventCommand,
					Actor:     "user",
					Action:    "action",
				})
			}

			data, err := os.ReadFile(logFile)
			Expect(err).NotTo(HaveOccurred())

			// Each line is a separate JSON object (NDJSON format from slog)
			lines := 0
			for _, b := range data {
				if b == '\n' {
					lines++
				}
			}
			Expect(lines).To(Equal(3))
		})
	})

	Describe("Close", func() {
		It("should close the underlying file", func() {
			auditor, err := audit.NewFileAuditor(logFile)
			Expect(err).NotTo(HaveOccurred())

			Expect(auditor.Close()).To(Succeed())
			// Closing again should return an error (file already closed)
			Expect(auditor.Close()).To(HaveOccurred())
		})
	})
})

var _ = Describe("NoopAuditor", func() {
	It("should implement the Auditor interface", func() {
		var auditor audit.Auditor = &audit.NoopAuditor{}
		Expect(auditor).NotTo(BeNil())
	})

	It("should not panic when Log is called", func() {
		auditor := &audit.NoopAuditor{}
		Expect(func() {
			auditor.Log(context.Background(), audit.LogRequest{
				EventType: audit.EventConnection,
				Actor:     "test",
				Action:    "connect",
				Metadata:  map[string]interface{}{"key": "value"},
			})
		}).NotTo(Panic())
	})
})
