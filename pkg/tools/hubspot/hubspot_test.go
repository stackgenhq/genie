package hubspot_test

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/tools/hubspot"
	"github.com/appcd-dev/genie/pkg/tools/hubspot/hubspotfakes"
)

var _ = Describe("HubSpot Tools", func() {
	var fake *hubspotfakes.FakeService

	BeforeEach(func() {
		fake = new(hubspotfakes.FakeService)
	})

	Describe("hubspot_search_contacts", func() {
		It("should return matching contacts", func(ctx context.Context) {
			fake.SearchContactsReturns([]hubspot.Contact{
				{ID: "1", Email: "alice@acme.com", FirstName: "Alice", LastName: "Smith"},
				{ID: "2", Email: "bob@acme.com", FirstName: "Bob", LastName: "Jones"},
			}, nil)

			tool := hubspot.NewSearchContactsTool(fake)
			reqJSON, _ := json.Marshal(struct {
				Query string `json:"query"`
			}{Query: "acme"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			contacts, ok := resp.([]hubspot.Contact)
			Expect(ok).To(BeTrue())
			Expect(contacts).To(HaveLen(2))
			Expect(contacts[0].Email).To(Equal("alice@acme.com"))
		})

		It("should default limit to 20", func(ctx context.Context) {
			fake.SearchContactsReturns(nil, nil)

			tool := hubspot.NewSearchContactsTool(fake)
			_, _ = tool.Call(ctx, []byte(`{"query":"x"}`))

			_, _, limit := fake.SearchContactsArgsForCall(0)
			Expect(limit).To(Equal(20))
		})
	})

	Describe("hubspot_get_contact", func() {
		It("should return contact detail", func(ctx context.Context) {
			fake.GetContactReturns(&hubspot.Contact{
				ID: "1", Email: "alice@acme.com", FirstName: "Alice",
				Properties: map[string]string{"jobtitle": "CTO"},
			}, nil)

			tool := hubspot.NewGetContactTool(fake)
			reqJSON, _ := json.Marshal(struct {
				ID string `json:"id"`
			}{ID: "1"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			contact, ok := resp.(*hubspot.Contact)
			Expect(ok).To(BeTrue())
			Expect(contact.Properties["jobtitle"]).To(Equal("CTO"))
		})
	})

	Describe("hubspot_create_contact", func() {
		It("should create and return contact", func(ctx context.Context) {
			fake.CreateContactReturns(&hubspot.Contact{
				ID: "99", Email: "new@acme.com", FirstName: "New",
			}, nil)

			tool := hubspot.NewCreateContactTool(fake)
			reqJSON, _ := json.Marshal(struct {
				Email     string `json:"email"`
				FirstName string `json:"first_name"`
			}{Email: "new@acme.com", FirstName: "New"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			contact, ok := resp.(*hubspot.Contact)
			Expect(ok).To(BeTrue())
			Expect(contact.ID).To(Equal("99"))

			// Verify properties were composed correctly
			_, props := fake.CreateContactArgsForCall(0)
			Expect(props["email"]).To(Equal("new@acme.com"))
			Expect(props["firstname"]).To(Equal("New"))
		})
	})

	Describe("hubspot_search_deals", func() {
		It("should return matching deals", func(ctx context.Context) {
			fake.SearchDealsReturns([]hubspot.Deal{
				{ID: "D1", Name: "Big Deal", Stage: "contractsent", Amount: "50000"},
			}, nil)

			tool := hubspot.NewSearchDealsTool(fake)
			reqJSON, _ := json.Marshal(struct {
				Query string `json:"query"`
			}{Query: "big deal"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			deals, ok := resp.([]hubspot.Deal)
			Expect(ok).To(BeTrue())
			Expect(deals[0].Amount).To(Equal("50000"))
		})
	})

	Describe("hubspot_create_deal", func() {
		It("should create deal with pipeline", func(ctx context.Context) {
			fake.CreateDealReturns(&hubspot.Deal{
				ID: "D99", Name: "New Deal", Stage: "appointmentscheduled",
			}, nil)

			tool := hubspot.NewCreateDealTool(fake)
			reqJSON, _ := json.Marshal(struct {
				Name     string `json:"name"`
				Stage    string `json:"stage"`
				Amount   string `json:"amount"`
				Pipeline string `json:"pipeline"`
			}{Name: "New Deal", Stage: "appointmentscheduled", Amount: "10000", Pipeline: "default"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			deal, ok := resp.(*hubspot.Deal)
			Expect(ok).To(BeTrue())
			Expect(deal.ID).To(Equal("D99"))

			_, props := fake.CreateDealArgsForCall(0)
			Expect(props["dealname"]).To(Equal("New Deal"))
			Expect(props["pipeline"]).To(Equal("default"))
			Expect(props["amount"]).To(Equal("10000"))
		})
	})

	Describe("hubspot_list_pipelines", func() {
		It("should return pipelines with stages", func(ctx context.Context) {
			fake.ListPipelinesReturns([]hubspot.Pipeline{
				{
					ID: "default", Label: "Sales Pipeline",
					Stages: []hubspot.Stage{
						{ID: "s1", Label: "New", Order: 0},
						{ID: "s2", Label: "Qualified", Order: 1},
					},
				},
			}, nil)

			tool := hubspot.NewListPipelinesTool(fake)

			resp, err := tool.Call(ctx, []byte(`{}`))
			Expect(err).NotTo(HaveOccurred())

			pipelines, ok := resp.([]hubspot.Pipeline)
			Expect(ok).To(BeTrue())
			Expect(pipelines[0].Stages).To(HaveLen(2))
		})
	})
})

var _ = Describe("AllTools", func() {
	It("should return 10 tools", func() {
		fake := new(hubspotfakes.FakeService)
		tools := hubspot.AllTools(fake)
		Expect(tools).To(HaveLen(10))
	})
})

var _ = Describe("New", func() {
	It("should return error when token is missing", func() {
		_, err := hubspot.New(hubspot.Config{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("token"))
	})
})

var _ = Describe("HubSpot Error Paths", func() {
	var fake *hubspotfakes.FakeService

	BeforeEach(func() {
		fake = new(hubspotfakes.FakeService)
	})

	It("should propagate SearchContacts error", func(ctx context.Context) {
		fake.SearchContactsReturns(nil, fmt.Errorf("auth failed"))
		tool := hubspot.NewSearchContactsTool(fake)
		_, err := tool.Call(ctx, []byte(`{"query":"x"}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate GetContact error", func(ctx context.Context) {
		fake.GetContactReturns(nil, fmt.Errorf("not found"))
		tool := hubspot.NewGetContactTool(fake)
		_, err := tool.Call(ctx, []byte(`{"id":"BAD"}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate CreateDeal error", func(ctx context.Context) {
		fake.CreateDealReturns(nil, fmt.Errorf("missing required field"))
		tool := hubspot.NewCreateDealTool(fake)
		reqJSON, _ := json.Marshal(struct {
			Name  string `json:"name"`
			Stage string `json:"stage"`
		}{Name: "x", Stage: "y"})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
	})

	It("should propagate UpdateContact error", func(ctx context.Context) {
		fake.UpdateContactReturns(fmt.Errorf("conflict"))
		tool := hubspot.NewUpdateContactTool(fake)
		reqJSON, _ := json.Marshal(struct {
			ContactID  string            `json:"contact_id"`
			Properties map[string]string `json:"properties"`
		}{ContactID: "1", Properties: map[string]string{"email": "x"}})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
	})

	It("should propagate SearchCompanies error", func(ctx context.Context) {
		fake.SearchCompaniesReturns(nil, fmt.Errorf("timeout"))
		tool := hubspot.NewSearchCompaniesTool(fake)
		_, err := tool.Call(ctx, []byte(`{"query":"x"}`))
		Expect(err).To(HaveOccurred())
	})
})
