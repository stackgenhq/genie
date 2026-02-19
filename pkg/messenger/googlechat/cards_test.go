package googlechat

import (
	"encoding/json"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/api/chat/v1"

	"github.com/appcd-dev/genie/pkg/messenger"
)

var _ = Describe("extractCardsV2", func() {
	Describe("nil / missing metadata", func() {
		It("returns nil for nil metadata", func() {
			Expect(extractCardsV2(nil)).To(BeNil())
		})

		It("returns nil when cards_v2 key is absent", func() {
			meta := map[string]any{"other": "value"}
			Expect(extractCardsV2(meta)).To(BeNil())
		})
	})

	Describe("typed []*chat.CardWithId pass-through", func() {
		It("returns the same slice unchanged", func() {
			cards := []*chat.CardWithId{
				{CardId: "test-card"},
			}
			result := extractCardsV2(map[string]any{"cards_v2": cards})
			Expect(result).To(HaveLen(1))
			Expect(result[0].CardId).To(Equal("test-card"))
		})

		It("returns nil for empty typed slice", func() {
			result := extractCardsV2(map[string]any{"cards_v2": []*chat.CardWithId{}})
			Expect(result).To(BeEmpty())
		})
	})

	Describe("JSON round-trip from golden testdata", func() {
		It("parses cards_v2.json into typed CardWithId", func() {
			raw := loadGoldenCards("testdata/cards_v2.json")
			meta := map[string]any{"cards_v2": raw}
			result := extractCardsV2(meta)
			Expect(result).To(HaveLen(1))
			Expect(result[0].CardId).To(Equal("approval_test-001"))
		})
	})

	Describe("JSON round-trip from []any maps (HITL store pattern)", func() {
		It("parses generic map cards into typed CardWithId", func() {
			cards := []any{
				map[string]any{
					"cardId": "dynamic-card",
					"card": map[string]any{
						"header": map[string]any{
							"title": "Test Card",
						},
					},
				},
			}
			meta := map[string]any{"cards_v2": cards}
			result := extractCardsV2(meta)
			Expect(result).To(HaveLen(1))
			Expect(result[0].CardId).To(Equal("dynamic-card"))
		})
	})

	Describe("invalid data", func() {
		It("returns nil for unmarshalable data", func() {
			meta := map[string]any{"cards_v2": make(chan int)}
			Expect(extractCardsV2(meta)).To(BeNil())
		})
	})
})

var _ = Describe("FormatApproval", func() {
	var m *Messenger

	BeforeEach(func() {
		m = New(Config{})
	})

	It("populates cards_v2 metadata with correct structure", func() {
		req := messenger.SendRequest{
			Channel: messenger.Channel{ID: "spaces/test"},
		}
		info := messenger.ApprovalInfo{
			ID:       "approval-001",
			ToolName: "write_file",
			Args:     `{"path": "/tmp/test.txt"}`,
		}

		result := m.FormatApproval(req, info)

		Expect(result.Metadata).To(HaveKey("cards_v2"))
		cards, ok := result.Metadata["cards_v2"].([]any)
		Expect(ok).To(BeTrue())
		Expect(cards).To(HaveLen(1))

		card := cards[0].(map[string]any)
		Expect(card["cardId"]).To(Equal("approval_approval-001"))
	})

	It("includes justification section when feedback is present", func() {
		req := messenger.SendRequest{
			Channel: messenger.Channel{ID: "spaces/test"},
		}
		info := messenger.ApprovalInfo{
			ID:       "approval-002",
			ToolName: "exec_command",
			Args:     `{"cmd": "ls"}`,
			Feedback: "Listing directory contents",
		}

		result := m.FormatApproval(req, info)

		cards := result.Metadata["cards_v2"].([]any)
		card := cards[0].(map[string]any)
		cardBody := card["card"].(map[string]any)
		sections := cardBody["sections"].([]any)

		// With feedback: justification + args + footer = 3
		Expect(sections).To(HaveLen(3))
		firstSection := sections[0].(map[string]any)
		Expect(firstSection["header"]).To(Equal("💡 Justification"))
	})

	It("omits justification section when feedback is empty", func() {
		req := messenger.SendRequest{}
		info := messenger.ApprovalInfo{
			ID:       "id-1",
			ToolName: "shell",
			Args:     "{}",
		}

		result := m.FormatApproval(req, info)
		cards := result.Metadata["cards_v2"].([]any)
		card := cards[0].(map[string]any)
		cardBody := card["card"].(map[string]any)
		sections := cardBody["sections"].([]any)
		// Without feedback: args + footer = 2
		Expect(sections).To(HaveLen(2))
	})

	It("round-trips the generated cards through extractCardsV2", func() {
		req := messenger.SendRequest{}
		info := messenger.ApprovalInfo{
			ID:       "round-trip-001",
			ToolName: "write_file",
			Args:     `{"path": "/tmp/out.txt"}`,
			Feedback: "Creating output file",
		}

		result := m.FormatApproval(req, info)
		sdkCards := extractCardsV2(result.Metadata)
		Expect(sdkCards).To(HaveLen(1))
		Expect(sdkCards[0].CardId).To(Equal("approval_round-trip-001"))
	})
})

var _ = Describe("FormatClarification", func() {
	var m *Messenger

	BeforeEach(func() {
		m = New(Config{})
	})

	It("returns the request unchanged (passthrough for now)", func() {
		req := messenger.SendRequest{
			Channel: messenger.Channel{ID: "spaces/test"},
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

// loadGoldenCards reads a golden JSON file and returns the "cards_v2" value as []any.
func loadGoldenCards(path string) []any {
	data, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())

	var wrapper map[string]json.RawMessage
	Expect(json.Unmarshal(data, &wrapper)).To(Succeed())

	var cards []any
	Expect(json.Unmarshal(wrapper["cards_v2"], &cards)).To(Succeed())
	return cards
}
