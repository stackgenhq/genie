package email_test

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/tools/email"
)

type mockService struct {
	SendFunc func(ctx context.Context, req email.SendRequest) error
	ReadFunc func(ctx context.Context, filter string) ([]*email.Email, error)
}

func (m *mockService) Send(ctx context.Context, req email.SendRequest) error {
	if m.SendFunc != nil {
		return m.SendFunc(ctx, req)
	}
	return nil
}

func (m *mockService) Read(ctx context.Context, filter string) ([]*email.Email, error) {
	if m.ReadFunc != nil {
		return m.ReadFunc(ctx, filter)
	}
	return nil, nil
}

var _ = Describe("Email Tools", func() {
	Describe("SendEmailTool", func() {
		It("should send an email successfully", func() {
			svc := &mockService{
				SendFunc: func(_ context.Context, req email.SendRequest) error {
					Expect(req.To).To(ConsistOf("test@example.com"))
					Expect(req.Subject).To(Equal("Test Subject"))
					Expect(req.Body).To(Equal("Test Body"))
					return nil
				},
			}

			tool := email.NewSendEmailTool(svc)
			reqJSON, err := json.Marshal(email.SendEmailRequest{
				To:      []string{"test@example.com"},
				Subject: "Test Subject",
				Body:    "Test Body",
			})
			Expect(err).NotTo(HaveOccurred())

			resp, err := tool.Call(context.Background(), reqJSON)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).To(Equal("email sent successfully"))
		})
	})

	Describe("ReadEmailTool", func() {
		It("should return matching emails", func() {
			svc := &mockService{
				ReadFunc: func(_ context.Context, filter string) ([]*email.Email, error) {
					Expect(filter).To(Equal("from:boss"))
					return []*email.Email{
						{
							From:    "boss@example.com",
							Subject: "Urgent",
							Body:    "Do it now",
						},
					}, nil
				},
			}

			tool := email.NewReadEmailTool(svc)
			reqJSON, err := json.Marshal(email.ReadEmailRequest{Filter: "from:boss"})
			Expect(err).NotTo(HaveOccurred())

			resp, err := tool.Call(context.Background(), reqJSON)
			Expect(err).NotTo(HaveOccurred())

			emails, ok := resp.([]*email.Email)
			Expect(ok).To(BeTrue(), "expected []*email.Email")
			Expect(emails).To(HaveLen(1))
			Expect(emails[0].From).To(Equal("boss@example.com"))
			Expect(emails[0].Subject).To(Equal("Urgent"))
		})
	})
})

var _ = Describe("Config.New", func() {
	It("should create an smtp service with provider 'smtp'", func() {
		cfg := email.Config{Provider: "smtp"}
		svc, err := cfg.New()
		Expect(err).NotTo(HaveOccurred())
		Expect(svc).NotTo(BeNil())
	})

	It("should create an smtp service with empty provider (default)", func() {
		cfg := email.Config{Provider: ""}
		svc, err := cfg.New()
		Expect(err).NotTo(HaveOccurred())
		Expect(svc).NotTo(BeNil())
	})

	It("should return error for unsupported provider", func() {
		cfg := email.Config{Provider: "sparkpost"}
		_, err := cfg.New()
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unsupported"))
	})
})

var _ = Describe("AllTools", func() {
	It("should return 2 tools", func() {
		svc := &mockService{}
		tools := email.AllTools(svc)
		Expect(tools).To(HaveLen(2))
		Expect(tools[0].Declaration().Name).To(Equal("email_send"))
		Expect(tools[1].Declaration().Name).To(Equal("email_read"))
	})
})
