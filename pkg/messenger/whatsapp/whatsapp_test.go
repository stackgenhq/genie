package whatsapp_test

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/messenger"
	"github.com/appcd-dev/genie/pkg/messenger/whatsapp"
)

var _ = Describe("WhatsApp Messenger", func() {
	Describe("New", func() {
		It("should create a messenger with default store path", func() {
			m, err := whatsapp.New(whatsapp.Config{})
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
		})

		It("should create a messenger with custom store path", func() {
			m, err := whatsapp.New(whatsapp.Config{
				StorePath: "/tmp/test-whatsapp-store",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
		})

		It("should expand tilde in store path", func() {
			m, err := whatsapp.New(whatsapp.Config{
				StorePath: "~/test-whatsapp-store",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(m).NotTo(BeNil())
		})
	})

	Describe("Platform", func() {
		It("should return PlatformWhatsApp", func() {
			m, err := whatsapp.New(whatsapp.Config{})
			Expect(err).NotTo(HaveOccurred())
			Expect(m.Platform()).To(Equal(messenger.PlatformWhatsApp))
		})
	})

	Describe("Connection state guards", func() {
		var m *whatsapp.Messenger

		BeforeEach(func() {
			var err error
			m, err = whatsapp.New(whatsapp.Config{
				StorePath: filepath.Join(os.TempDir(), "whatsapp-test-guard"),
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return ErrNotConnected when Send is called before Connect", func() {
			_, err := m.Send(context.Background(), messenger.SendRequest{
				Channel: messenger.Channel{ID: "1234567890"},
				Content: messenger.MessageContent{Text: "test"},
			})
			Expect(err).To(MatchError(messenger.ErrNotConnected))
		})

		It("should return ErrNotConnected when Receive is called before Connect", func() {
			ch, err := m.Receive(context.Background())
			Expect(err).To(MatchError(messenger.ErrNotConnected))
			Expect(ch).To(BeNil())
		})

		It("should return ErrNotConnected when Disconnect is called before Connect", func() {
			err := m.Disconnect(context.Background())
			Expect(err).To(MatchError(messenger.ErrNotConnected))
		})
	})

	Describe("Interface compliance", func() {
		It("should satisfy the messenger.Messenger interface", func() {
			m, err := whatsapp.New(whatsapp.Config{})
			Expect(err).NotTo(HaveOccurred())
			var _ messenger.Messenger = m
		})
	})

	Describe("FormatApproval", func() {
		It("should return the request unchanged", func() {
			m, err := whatsapp.New(whatsapp.Config{})
			Expect(err).NotTo(HaveOccurred())

			req := messenger.SendRequest{
				Channel: messenger.Channel{ID: "1234567890"},
				Content: messenger.MessageContent{Text: "Approval needed"},
			}
			result := m.FormatApproval(req, messenger.ApprovalInfo{ID: "a1", ToolName: "write_file"})
			Expect(result.Content.Text).To(Equal("Approval needed"))
		})
	})

	Describe("FormatClarification", func() {
		It("should return the request unchanged", func() {
			m, err := whatsapp.New(whatsapp.Config{})
			Expect(err).NotTo(HaveOccurred())

			req := messenger.SendRequest{
				Channel: messenger.Channel{ID: "1234567890"},
				Content: messenger.MessageContent{Text: "Question?"},
			}
			result := m.FormatClarification(req, messenger.ClarificationInfo{RequestID: "r1", Question: "what?"})
			Expect(result.Content.Text).To(Equal("Question?"))
		})
	})
})
