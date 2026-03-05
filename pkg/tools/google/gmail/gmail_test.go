package gmail

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type mockService struct {
	ListMessagesFunc func(ctx context.Context, query string, maxResults int) ([]*MessageSummary, error)
	GetMessageFunc   func(ctx context.Context, id string) (*MessageDetail, error)
	SendFunc         func(ctx context.Context, to []string, subject, body string) error
	ValidateFunc     func(ctx context.Context) error
}

func (m *mockService) ListMessages(ctx context.Context, query string, maxResults int) ([]*MessageSummary, error) {
	if m.ListMessagesFunc != nil {
		return m.ListMessagesFunc(ctx, query, maxResults)
	}
	return nil, nil
}
func (m *mockService) GetMessage(ctx context.Context, id string) (*MessageDetail, error) {
	if m.GetMessageFunc != nil {
		return m.GetMessageFunc(ctx, id)
	}
	return nil, nil
}
func (m *mockService) Send(ctx context.Context, to []string, subject, body string) error {
	if m.SendFunc != nil {
		return m.SendFunc(ctx, to, subject, body)
	}
	return nil
}
func (m *mockService) Validate(ctx context.Context) error {
	if m.ValidateFunc != nil {
		return m.ValidateFunc(ctx)
	}
	return nil
}

var _ = Describe("Google Gmail Tools", func() {
	var (
		svc *mockService
		ctx context.Context
		tt  *gmailTools
	)

	BeforeEach(func() {
		ctx = context.Background()
		svc = &mockService{}
		tt = newGmailTools("test_gmail", svc)
	})

	Describe("handleListMessages", func() {
		It("calls svc.ListMessages", func() {
			svc.ListMessagesFunc = func(ctx context.Context, query string, maxResults int) ([]*MessageSummary, error) {
				return []*MessageSummary{{ID: "1", Snippet: "hello"}}, nil
			}
			req := listMessagesRequest{Query: "in:inbox", MaxResults: 10}
			res, err := tt.handleListMessages(ctx, req)
			Expect(err).To(Not(HaveOccurred()))
			Expect(res).To(HaveLen(1))
			Expect(res[0].ID).To(Equal("1"))
		})
	})

	Describe("handleGetMessage", func() {
		It("calls svc.GetMessage", func() {
			svc.GetMessageFunc = func(ctx context.Context, id string) (*MessageDetail, error) {
				return &MessageDetail{ID: "m1", Subject: "subj"}, nil
			}
			req := getMessageRequest{MessageID: "m1"}
			res, err := tt.handleGetMessage(ctx, req)
			Expect(err).To(Not(HaveOccurred()))
			Expect(res.Subject).To(Equal("subj"))
		})
	})

	Describe("handleSend", func() {
		It("calls svc.Send successfully", func() {
			svc.SendFunc = func(ctx context.Context, to []string, subject, body string) error {
				return nil
			}
			req := sendMessageRequest{To: []string{"a@b.com"}, Subject: "subj", Body: "body"}
			res, err := tt.handleSend(ctx, req)
			Expect(err).To(Not(HaveOccurred()))
			Expect(res).To(ContainSubstring("Sent email"))
		})

		It("returns error when missing fields", func() {
			req := sendMessageRequest{To: []string{}}
			_, err := tt.handleSend(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("required"))
		})

		It("returns error when Send fails", func() {
			svc.SendFunc = func(ctx context.Context, to []string, subject, body string) error {
				return errors.New("send err")
			}
			req := sendMessageRequest{To: []string{"a@b.com"}, Subject: "subj", Body: "body"}
			_, err := tt.handleSend(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("send err"))
		})
	})

	Describe("AllTools", func() {
		It("returns the correct number of tools", func() {
			tools := AllTools("test", svc)
			Expect(tools).To(HaveLen(3))
		})
	})
})
