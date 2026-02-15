package email_test

import (
	"context"
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/tools/email"
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
