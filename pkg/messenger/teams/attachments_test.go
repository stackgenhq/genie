package teams

import (
	"encoding/json"
	"os"

	schema "github.com/infracloudio/msbotbuilder-go/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/messenger"
)

var _ = Describe("extractAttachments", func() {
	Describe("nil / missing metadata", func() {
		It("returns nil for nil metadata", func() {
			Expect(extractAttachments(nil)).To(BeNil())
		})

		It("returns nil when attachments key is absent", func() {
			meta := map[string]any{"other": "value"}
			Expect(extractAttachments(meta)).To(BeNil())
		})
	})

	Describe("typed []schema.Attachment pass-through", func() {
		It("returns the same slice unchanged", func() {
			attachments := []schema.Attachment{
				{ContentType: "application/vnd.microsoft.card.adaptive"},
			}
			result := extractAttachments(map[string]any{"attachments": attachments})
			Expect(result).To(HaveLen(1))
			Expect(result[0].ContentType).To(Equal("application/vnd.microsoft.card.adaptive"))
		})

		It("returns nil for empty typed slice", func() {
			result := extractAttachments(map[string]any{"attachments": []schema.Attachment{}})
			Expect(result).To(BeEmpty())
		})
	})

	Describe("JSON round-trip from golden testdata", func() {
		It("parses attachments.json into typed Attachment", func() {
			raw := loadGoldenAttachments("testdata/attachments.json")
			meta := map[string]any{"attachments": raw}
			result := extractAttachments(meta)
			Expect(result).To(HaveLen(1))
			Expect(result[0].ContentType).To(Equal("application/vnd.microsoft.card.adaptive"))
		})
	})

	Describe("JSON round-trip from []any maps (HITL store pattern)", func() {
		It("parses generic map attachments into typed Attachment", func() {
			attachments := []any{
				map[string]any{
					"contentType": "application/vnd.microsoft.card.adaptive",
					"content": map[string]any{
						"type":    "AdaptiveCard",
						"version": "1.4",
					},
				},
			}
			meta := map[string]any{"attachments": attachments}
			result := extractAttachments(meta)
			Expect(result).To(HaveLen(1))
			Expect(result[0].ContentType).To(Equal("application/vnd.microsoft.card.adaptive"))
		})
	})

	Describe("invalid data", func() {
		It("returns nil for unmarshalable data", func() {
			meta := map[string]any{"attachments": make(chan int)}
			Expect(extractAttachments(meta)).To(BeNil())
		})
	})
})

var _ = Describe("FormatApproval", func() {
	var m *Messenger

	BeforeEach(func() {
		var err error
		m, err = New(Config{
			AppID:       "test-app-id",
			AppPassword: "test-app-password",
			ListenAddr:  ":0",
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("populates attachments metadata with correct structure", func() {
		req := messenger.SendRequest{
			Channel: messenger.Channel{ID: "conv-123"},
		}
		info := messenger.ApprovalInfo{
			ID:       "approval-001",
			ToolName: "write_file",
			Args:     `{"path": "/tmp/test.txt"}`,
		}

		result := m.FormatApproval(req, info)

		Expect(result.Metadata).To(HaveKey("attachments"))
		attachments, ok := result.Metadata["attachments"].([]any)
		Expect(ok).To(BeTrue())
		Expect(attachments).To(HaveLen(1))

		card := attachments[0].(map[string]any)
		Expect(card["contentType"]).To(Equal("application/vnd.microsoft.card.adaptive"))
	})

	It("includes justification when feedback is present", func() {
		req := messenger.SendRequest{
			Channel: messenger.Channel{ID: "conv-123"},
		}
		info := messenger.ApprovalInfo{
			ID:       "approval-002",
			ToolName: "exec_command",
			Args:     `{"cmd": "ls"}`,
			Feedback: "Listing directory contents",
		}

		result := m.FormatApproval(req, info)

		attachments := result.Metadata["attachments"].([]any)
		card := attachments[0].(map[string]any)
		content := card["content"].(map[string]any)
		body := content["body"].([]any)

		// With feedback: header + justification + args label + args + footer = 5
		Expect(body).To(HaveLen(5))
		justification := body[1].(map[string]any)
		Expect(justification["text"]).To(ContainSubstring("Listing directory"))
	})

	It("omits justification when feedback is empty", func() {
		req := messenger.SendRequest{}
		info := messenger.ApprovalInfo{
			ID:       "id-1",
			ToolName: "shell",
			Args:     "{}",
		}

		result := m.FormatApproval(req, info)
		attachments := result.Metadata["attachments"].([]any)
		card := attachments[0].(map[string]any)
		content := card["content"].(map[string]any)
		body := content["body"].([]any)
		// Without feedback: header + args label + args + footer = 4
		Expect(body).To(HaveLen(4))
	})

	It("round-trips the generated attachments through extractAttachments", func() {
		req := messenger.SendRequest{}
		info := messenger.ApprovalInfo{
			ID:       "round-trip-001",
			ToolName: "write_file",
			Args:     `{"path": "/tmp/out.txt"}`,
			Feedback: "Creating output file",
		}

		result := m.FormatApproval(req, info)
		sdkAttachments := extractAttachments(result.Metadata)
		Expect(sdkAttachments).To(HaveLen(1))
		Expect(sdkAttachments[0].ContentType).To(Equal("application/vnd.microsoft.card.adaptive"))
	})
})

var _ = Describe("FormatClarification", func() {
	var m *Messenger

	BeforeEach(func() {
		var err error
		m, err = New(Config{
			AppID:       "test-app-id",
			AppPassword: "test-app-password",
			ListenAddr:  ":0",
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("returns the request unchanged (passthrough for now)", func() {
		req := messenger.SendRequest{
			Channel: messenger.Channel{ID: "conv-123"},
			Content: messenger.MessageContent{Text: "original text"},
		}
		info := messenger.ClarificationInfo{
			RequestID: "clr-001",
			Question:  "What is the target environment?",
			Context:   "Deploying the application",
		}

		result := m.FormatClarification(req, info)
		Expect(result.Content.Text).To(Equal("original text"))
		Expect(result.Metadata).To(BeNil())
	})
})

// loadGoldenAttachments reads a golden JSON file and returns the "attachments" value as []any.
func loadGoldenAttachments(path string) []any {
	data, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())

	var wrapper map[string]json.RawMessage
	Expect(json.Unmarshal(data, &wrapper)).To(Succeed())

	var attachments []any
	Expect(json.Unmarshal(wrapper["attachments"], &attachments)).To(Succeed())
	return attachments
}
