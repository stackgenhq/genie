package contacts

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/api/people/v1"
)

type mockService struct {
	ListContactsFunc   func(ctx context.Context, req listContactsRequest) (contactsResponse, error)
	SearchContactsFunc func(ctx context.Context, req searchContactsRequest) (contactsResponse, error)
}

func (m *mockService) ListContacts(ctx context.Context, req listContactsRequest) (contactsResponse, error) {
	if m.ListContactsFunc != nil {
		return m.ListContactsFunc(ctx, req)
	}
	return contactsResponse{}, nil
}

func (m *mockService) SearchContacts(ctx context.Context, req searchContactsRequest) (contactsResponse, error) {
	if m.SearchContactsFunc != nil {
		return m.SearchContactsFunc(ctx, req)
	}
	return contactsResponse{}, nil
}

var _ = Describe("Google Contacts Tools", func() {
	var (
		svc *mockService
		ctx context.Context
		tt  *contactsTools
	)

	BeforeEach(func() {
		ctx = context.Background()
		svc = &mockService{}
		tt = newContactsTools("test", svc)
	})

	Describe("handleListContacts", func() {
		It("calls svc.ListContacts", func() {
			svc.ListContactsFunc = func(ctx context.Context, req listContactsRequest) (contactsResponse, error) {
				return contactsResponse{Count: 1, Contacts: []contactEntry{{DisplayName: "Alice"}}}, nil
			}
			req := listContactsRequest{PageSize: 10}
			res, err := tt.handleListContacts(ctx, req)
			Expect(err).To(Not(HaveOccurred()))
			Expect(res.Count).To(Equal(1))
			Expect(res.Contacts[0].DisplayName).To(Equal("Alice"))
		})

		It("returns error when svc.ListContacts fails", func() {
			svc.ListContactsFunc = func(ctx context.Context, req listContactsRequest) (contactsResponse, error) {
				return contactsResponse{}, errors.New("list error")
			}
			req := listContactsRequest{PageSize: 10}
			_, err := tt.handleListContacts(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("list error"))
		})
	})

	Describe("handleSearchContacts", func() {
		It("calls svc.SearchContacts", func() {
			svc.SearchContactsFunc = func(ctx context.Context, req searchContactsRequest) (contactsResponse, error) {
				return contactsResponse{Count: 1, Contacts: []contactEntry{{DisplayName: "Alice"}}}, nil
			}
			req := searchContactsRequest{Query: "Alice", PageSize: 10}
			res, err := tt.handleSearchContacts(ctx, req)
			Expect(err).To(Not(HaveOccurred()))
			Expect(res.Count).To(Equal(1))
		})
		It("returns error when svc.SearchContacts fails", func() {
			svc.SearchContactsFunc = func(ctx context.Context, req searchContactsRequest) (contactsResponse, error) {
				return contactsResponse{}, errors.New("search error")
			}
			req := searchContactsRequest{Query: "Alice", PageSize: 10}
			_, err := tt.handleSearchContacts(ctx, req)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(Equal("search error"))
		})
	})

	Describe("personToEntry", func() {
		It("maps person correctly", func() {
			p := &people.Person{
				ResourceName:   "people/123",
				Names:          []*people.Name{{DisplayName: "Alice"}},
				EmailAddresses: []*people.EmailAddress{{Value: "alice@example.com"}},
				PhoneNumbers:   []*people.PhoneNumber{{Value: "12345"}},
			}
			entry := personToEntry(p)
			Expect(entry.ResourceName).To(Equal("people/123"))
			Expect(entry.DisplayName).To(Equal("Alice"))
			Expect(entry.Emails).To(ContainElement("alice@example.com"))
			Expect(entry.Phones).To(ContainElement("12345"))
		})
	})
})
