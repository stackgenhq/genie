package hubspot

import (
	"context"
	"fmt"

	"github.com/appcd-dev/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

//go:generate go tool counterfeiter -generate

// Service defines the capabilities of the HubSpot CRM connector. It
// exposes search, read, and write operations across key CRM objects:
// Contacts, Companies, Deals, and Tickets.
//
//counterfeiter:generate . Service
type Service interface {
	// SearchContacts searches HubSpot contacts with a filter query.
	SearchContacts(ctx context.Context, query string, limit int) ([]Contact, error)

	// GetContact retrieves a contact by ID.
	GetContact(ctx context.Context, contactID string) (*Contact, error)

	// CreateContact creates a new contact.
	CreateContact(ctx context.Context, properties map[string]string) (*Contact, error)

	// UpdateContact updates an existing contact.
	UpdateContact(ctx context.Context, contactID string, properties map[string]string) error

	// SearchCompanies searches HubSpot companies.
	SearchCompanies(ctx context.Context, query string, limit int) ([]Company, error)

	// GetCompany retrieves a company by ID.
	GetCompany(ctx context.Context, companyID string) (*Company, error)

	// SearchDeals searches HubSpot deals.
	SearchDeals(ctx context.Context, query string, limit int) ([]Deal, error)

	// GetDeal retrieves a deal by ID.
	GetDeal(ctx context.Context, dealID string) (*Deal, error)

	// CreateDeal creates a new deal.
	CreateDeal(ctx context.Context, properties map[string]string) (*Deal, error)

	// ListPipelines returns deal pipelines and their stages.
	ListPipelines(ctx context.Context) ([]Pipeline, error)

	// Validate performs a lightweight health check.
	Validate(ctx context.Context) error
}

// Config holds configuration for the HubSpot connector.
type Config struct {
	Token string `yaml:"token" toml:"token"` // HubSpot private app access token
}

// ── Domain Types ────────────────────────────────────────────────────────

// Contact represents a HubSpot CRM contact.
type Contact struct {
	ID         string            `json:"id"`
	Email      string            `json:"email"`
	FirstName  string            `json:"first_name"`
	LastName   string            `json:"last_name"`
	Phone      string            `json:"phone,omitempty"`
	Company    string            `json:"company,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// Company represents a HubSpot CRM company.
type Company struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Domain     string            `json:"domain,omitempty"`
	Industry   string            `json:"industry,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// Deal represents a HubSpot CRM deal.
type Deal struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Stage      string            `json:"stage"`
	Amount     string            `json:"amount,omitempty"`
	CloseDate  string            `json:"close_date,omitempty"`
	Pipeline   string            `json:"pipeline,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

// Pipeline represents a HubSpot deal pipeline.
type Pipeline struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Stages []Stage `json:"stages"`
}

// Stage represents a pipeline stage.
type Stage struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Order int    `json:"order"`
}

// ── Factory ─────────────────────────────────────────────────────────────

// New creates a new HubSpot Service from the given configuration.
// The token should be a HubSpot private app access token.
func New(cfg Config) (Service, error) {
	log := logger.GetLogger(context.Background())
	log.Info("Initializing HubSpot service", "has_token", cfg.Token != "")

	if cfg.Token == "" {
		return nil, fmt.Errorf("hubspot: token is required")
	}

	return newWrapper(cfg)
}

// ── Request Types ───────────────────────────────────────────────────────

type searchRequest struct {
	Query string `json:"query" jsonschema:"description=Search query string (searches across name and email fields),required"`
	Limit int    `json:"limit" jsonschema:"description=Maximum number of results (default 20)"`
}

type getByIDRequest struct {
	ID string `json:"id" jsonschema:"description=HubSpot record ID,required"`
}

type createContactRequest struct {
	Email     string            `json:"email" jsonschema:"description=Contact email address,required"`
	FirstName string            `json:"first_name" jsonschema:"description=First name"`
	LastName  string            `json:"last_name" jsonschema:"description=Last name"`
	Phone     string            `json:"phone" jsonschema:"description=Phone number"`
	Company   string            `json:"company" jsonschema:"description=Company name"`
	Extra     map[string]string `json:"extra_properties" jsonschema:"description=Additional properties as key-value pairs"`
}

type updateContactRequest struct {
	ContactID  string            `json:"contact_id" jsonschema:"description=HubSpot contact ID,required"`
	Properties map[string]string `json:"properties" jsonschema:"description=Properties to update as key-value pairs,required"`
}

type createDealRequest struct {
	Name      string `json:"name" jsonschema:"description=Deal name,required"`
	Stage     string `json:"stage" jsonschema:"description=Pipeline stage ID,required"`
	Amount    string `json:"amount" jsonschema:"description=Deal amount"`
	CloseDate string `json:"close_date" jsonschema:"description=Expected close date (YYYY-MM-DD)"`
	Pipeline  string `json:"pipeline" jsonschema:"description=Pipeline ID (default pipeline if empty)"`
}

// ── Tool Constructors ───────────────────────────────────────────────────

type toolSet struct {
	s Service
}

func NewSearchContactsTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.searchContacts,
		function.WithName("hubspot_search_contacts"),
		function.WithDescription("Search HubSpot CRM contacts by name or email."),
	)
}

func NewGetContactTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.getContact,
		function.WithName("hubspot_get_contact"),
		function.WithDescription("Get detailed information about a HubSpot contact by ID."),
	)
}

func NewCreateContactTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.createContact,
		function.WithName("hubspot_create_contact"),
		function.WithDescription("Create a new contact in HubSpot CRM."),
	)
}

func NewUpdateContactTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.updateContact,
		function.WithName("hubspot_update_contact"),
		function.WithDescription("Update an existing HubSpot contact's properties."),
	)
}

func NewSearchCompaniesTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.searchCompanies,
		function.WithName("hubspot_search_companies"),
		function.WithDescription("Search HubSpot CRM companies by name or domain."),
	)
}

func NewGetCompanyTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.getCompany,
		function.WithName("hubspot_get_company"),
		function.WithDescription("Get detailed information about a HubSpot company by ID."),
	)
}

func NewSearchDealsTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.searchDeals,
		function.WithName("hubspot_search_deals"),
		function.WithDescription("Search HubSpot CRM deals by name."),
	)
}

func NewGetDealTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.getDeal,
		function.WithName("hubspot_get_deal"),
		function.WithDescription("Get detailed information about a HubSpot deal by ID."),
	)
}

func NewCreateDealTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.createDeal,
		function.WithName("hubspot_create_deal"),
		function.WithDescription("Create a new deal in HubSpot CRM. Use hubspot_list_pipelines to discover stage IDs."),
	)
}

func NewListPipelinesTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listPipelines,
		function.WithName("hubspot_list_pipelines"),
		function.WithDescription("List HubSpot deal pipelines and their stages."),
	)
}

// AllTools returns all HubSpot tools wired to the service.
func AllTools(s Service) []tool.Tool {
	return []tool.Tool{
		NewSearchContactsTool(s),
		NewGetContactTool(s),
		NewCreateContactTool(s),
		NewUpdateContactTool(s),
		NewSearchCompaniesTool(s),
		NewGetCompanyTool(s),
		NewSearchDealsTool(s),
		NewGetDealTool(s),
		NewCreateDealTool(s),
		NewListPipelinesTool(s),
	}
}

// ── Tool Implementations ────────────────────────────────────────────────

func (ts *toolSet) searchContacts(ctx context.Context, req searchRequest) ([]Contact, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	return ts.s.SearchContacts(ctx, req.Query, limit)
}

func (ts *toolSet) getContact(ctx context.Context, req getByIDRequest) (*Contact, error) {
	return ts.s.GetContact(ctx, req.ID)
}

func (ts *toolSet) createContact(ctx context.Context, req createContactRequest) (*Contact, error) {
	props := map[string]string{
		"email": req.Email,
	}
	if req.FirstName != "" {
		props["firstname"] = req.FirstName
	}
	if req.LastName != "" {
		props["lastname"] = req.LastName
	}
	if req.Phone != "" {
		props["phone"] = req.Phone
	}
	if req.Company != "" {
		props["company"] = req.Company
	}
	for k, v := range req.Extra {
		props[k] = v
	}
	return ts.s.CreateContact(ctx, props)
}

func (ts *toolSet) updateContact(ctx context.Context, req updateContactRequest) (string, error) {
	err := ts.s.UpdateContact(ctx, req.ContactID, req.Properties)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Contact %s updated successfully", req.ContactID), nil
}

func (ts *toolSet) searchCompanies(ctx context.Context, req searchRequest) ([]Company, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	return ts.s.SearchCompanies(ctx, req.Query, limit)
}

func (ts *toolSet) getCompany(ctx context.Context, req getByIDRequest) (*Company, error) {
	return ts.s.GetCompany(ctx, req.ID)
}

func (ts *toolSet) searchDeals(ctx context.Context, req searchRequest) ([]Deal, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	return ts.s.SearchDeals(ctx, req.Query, limit)
}

func (ts *toolSet) getDeal(ctx context.Context, req getByIDRequest) (*Deal, error) {
	return ts.s.GetDeal(ctx, req.ID)
}

func (ts *toolSet) createDeal(ctx context.Context, req createDealRequest) (*Deal, error) {
	props := map[string]string{
		"dealname":  req.Name,
		"dealstage": req.Stage,
	}
	if req.Amount != "" {
		props["amount"] = req.Amount
	}
	if req.CloseDate != "" {
		props["closedate"] = req.CloseDate
	}
	if req.Pipeline != "" {
		props["pipeline"] = req.Pipeline
	}
	return ts.s.CreateDeal(ctx, props)
}

func (ts *toolSet) listPipelines(ctx context.Context, _ struct{}) ([]Pipeline, error) {
	return ts.s.ListPipelines(ctx)
}
