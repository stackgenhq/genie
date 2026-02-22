package hubspot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	hs "github.com/belong-inc/go-hubspot"
)

// hubspotWrapper implements the Service interface using the
// belong-inc/go-hubspot SDK. This replaces the previous direct HTTP
// implementation, gaining typed CRM objects and proper auth flow handling.
type hubspotWrapper struct {
	client *hs.Client
	token  string // kept for raw API calls the SDK doesn't cover
}

// newWrapper constructs a HubSpot client from the given Config.
func newWrapper(cfg Config) (*hubspotWrapper, error) {
	client, err := hs.NewClient(hs.SetPrivateAppToken(cfg.Token))
	if err != nil {
		return nil, fmt.Errorf("hubspot: failed to create client: %w", err)
	}
	return &hubspotWrapper{
		client: client,
		token:  cfg.Token,
	}, nil
}

// hsStrVal safely dereferences an HsStr pointer.
func hsStrVal(s *hs.HsStr) string {
	if s == nil {
		return ""
	}
	return string(*s)
}

// ── Contact Operations ──────────────────────────────────────────────────

func (w *hubspotWrapper) SearchContacts(ctx context.Context, query string, limit int) ([]Contact, error) {
	searchPayload := map[string]interface{}{
		"query": query,
		"limit": limit,
		"properties": []string{
			"email", "firstname", "lastname", "mobilephone", "company",
		},
	}

	body, err := w.doRawPost(ctx, "/crm/v3/objects/contacts/search", searchPayload)
	if err != nil {
		return nil, fmt.Errorf("hubspot: search contacts failed: %w", err)
	}

	var result struct {
		Results []struct {
			ID         string            `json:"id"`
			Properties map[string]string `json:"properties"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("hubspot: failed to decode contacts: %w", err)
	}

	contacts := make([]Contact, 0, len(result.Results))
	for _, r := range result.Results {
		contacts = append(contacts, Contact{
			ID:         r.ID,
			Email:      r.Properties["email"],
			FirstName:  r.Properties["firstname"],
			LastName:   r.Properties["lastname"],
			Phone:      r.Properties["mobilephone"],
			Company:    r.Properties["company"],
			Properties: r.Properties,
		})
	}
	return contacts, nil
}

func (w *hubspotWrapper) GetContact(ctx context.Context, id string) (*Contact, error) {
	res, err := w.client.CRM.Contact.Get(id, &hs.Contact{}, nil)
	if err != nil {
		return nil, fmt.Errorf("hubspot: get contact %s failed: %w", id, err)
	}

	contact, ok := res.Properties.(*hs.Contact)
	if !ok {
		return nil, fmt.Errorf("hubspot: unexpected contact type")
	}

	props := make(map[string]string)
	email := hsStrVal(contact.Email)
	firstName := hsStrVal(contact.FirstName)
	lastName := hsStrVal(contact.LastName)
	phone := hsStrVal(contact.MobilePhone)

	if email != "" {
		props["email"] = email
	}
	if firstName != "" {
		props["firstname"] = firstName
	}
	if lastName != "" {
		props["lastname"] = lastName
	}
	if phone != "" {
		props["phone"] = phone
	}

	return &Contact{
		ID:         res.ID,
		Email:      email,
		FirstName:  firstName,
		LastName:   lastName,
		Phone:      phone,
		Properties: props,
	}, nil
}

func (w *hubspotWrapper) CreateContact(ctx context.Context, properties map[string]string) (*Contact, error) {
	req := &hs.Contact{}
	if v, ok := properties["email"]; ok {
		req.Email = hs.NewString(v)
	}
	if v, ok := properties["firstname"]; ok {
		req.FirstName = hs.NewString(v)
	}
	if v, ok := properties["lastname"]; ok {
		req.LastName = hs.NewString(v)
	}
	if v, ok := properties["phone"]; ok {
		req.MobilePhone = hs.NewString(v)
	}

	res, err := w.client.CRM.Contact.Create(req)
	if err != nil {
		return nil, fmt.Errorf("hubspot: create contact failed: %w", err)
	}

	contact, ok := res.Properties.(*hs.Contact)
	if !ok {
		return nil, fmt.Errorf("hubspot: unexpected contact type in response")
	}

	return &Contact{
		ID:        res.ID,
		Email:     hsStrVal(contact.Email),
		FirstName: hsStrVal(contact.FirstName),
		LastName:  hsStrVal(contact.LastName),
	}, nil
}

func (w *hubspotWrapper) UpdateContact(ctx context.Context, id string, properties map[string]string) error {
	req := &hs.Contact{}
	if v, ok := properties["email"]; ok {
		req.Email = hs.NewString(v)
	}
	if v, ok := properties["firstname"]; ok {
		req.FirstName = hs.NewString(v)
	}
	if v, ok := properties["lastname"]; ok {
		req.LastName = hs.NewString(v)
	}
	if v, ok := properties["phone"]; ok {
		req.MobilePhone = hs.NewString(v)
	}

	_, err := w.client.CRM.Contact.Update(id, req)
	if err != nil {
		return fmt.Errorf("hubspot: update contact %s failed: %w", id, err)
	}
	return nil
}

// ── Company Operations ──────────────────────────────────────────────────

func (w *hubspotWrapper) SearchCompanies(ctx context.Context, query string, limit int) ([]Company, error) {
	searchPayload := map[string]interface{}{
		"query": query,
		"limit": limit,
		"properties": []string{
			"name", "domain", "industry",
		},
	}

	body, err := w.doRawPost(ctx, "/crm/v3/objects/companies/search", searchPayload)
	if err != nil {
		return nil, fmt.Errorf("hubspot: search companies failed: %w", err)
	}

	var result struct {
		Results []struct {
			ID         string            `json:"id"`
			Properties map[string]string `json:"properties"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("hubspot: failed to decode companies: %w", err)
	}

	companies := make([]Company, 0, len(result.Results))
	for _, r := range result.Results {
		companies = append(companies, Company{
			ID:         r.ID,
			Name:       r.Properties["name"],
			Domain:     r.Properties["domain"],
			Industry:   r.Properties["industry"],
			Properties: r.Properties,
		})
	}
	return companies, nil
}

func (w *hubspotWrapper) GetCompany(ctx context.Context, id string) (*Company, error) {
	res, err := w.client.CRM.Company.Get(id, &hs.Company{}, nil)
	if err != nil {
		return nil, fmt.Errorf("hubspot: get company %s failed: %w", id, err)
	}

	company, ok := res.Properties.(*hs.Company)
	if !ok {
		return nil, fmt.Errorf("hubspot: unexpected company type")
	}

	return &Company{
		ID:     res.ID,
		Name:   hsStrVal(company.Name),
		Domain: hsStrVal(company.Domain),
	}, nil
}

// ── Deal Operations ─────────────────────────────────────────────────────

func (w *hubspotWrapper) SearchDeals(ctx context.Context, query string, limit int) ([]Deal, error) {
	searchPayload := map[string]interface{}{
		"query": query,
		"limit": limit,
		"properties": []string{
			"dealname", "dealstage", "amount", "pipeline",
		},
	}

	body, err := w.doRawPost(ctx, "/crm/v3/objects/deals/search", searchPayload)
	if err != nil {
		return nil, fmt.Errorf("hubspot: search deals failed: %w", err)
	}

	var result struct {
		Results []struct {
			ID         string            `json:"id"`
			Properties map[string]string `json:"properties"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("hubspot: failed to decode deals: %w", err)
	}

	deals := make([]Deal, 0, len(result.Results))
	for _, r := range result.Results {
		deals = append(deals, Deal{
			ID:         r.ID,
			Name:       r.Properties["dealname"],
			Stage:      r.Properties["dealstage"],
			Amount:     r.Properties["amount"],
			Pipeline:   r.Properties["pipeline"],
			Properties: r.Properties,
		})
	}
	return deals, nil
}

func (w *hubspotWrapper) GetDeal(ctx context.Context, id string) (*Deal, error) {
	res, err := w.client.CRM.Deal.Get(id, &hs.Deal{}, nil)
	if err != nil {
		return nil, fmt.Errorf("hubspot: get deal %s failed: %w", id, err)
	}

	deal, ok := res.Properties.(*hs.Deal)
	if !ok {
		return nil, fmt.Errorf("hubspot: unexpected deal type")
	}

	return &Deal{
		ID:     res.ID,
		Name:   hsStrVal(deal.DealName),
		Stage:  hsStrVal(deal.DealStage),
		Amount: hsStrVal(deal.Amount),
	}, nil
}

func (w *hubspotWrapper) CreateDeal(ctx context.Context, properties map[string]string) (*Deal, error) {
	req := &hs.Deal{}
	if v, ok := properties["dealname"]; ok {
		req.DealName = hs.NewString(v)
	}
	if v, ok := properties["dealstage"]; ok {
		req.DealStage = hs.NewString(v)
	}
	if v, ok := properties["amount"]; ok {
		req.Amount = hs.NewString(v)
	}

	res, err := w.client.CRM.Deal.Create(req)
	if err != nil {
		return nil, fmt.Errorf("hubspot: create deal failed: %w", err)
	}

	deal, ok := res.Properties.(*hs.Deal)
	if !ok {
		return nil, fmt.Errorf("hubspot: unexpected deal type in response")
	}

	return &Deal{
		ID:    res.ID,
		Name:  hsStrVal(deal.DealName),
		Stage: hsStrVal(deal.DealStage),
	}, nil
}

// ── Pipeline Operations ─────────────────────────────────────────────────

func (w *hubspotWrapper) ListPipelines(ctx context.Context) ([]Pipeline, error) {
	body, err := w.doRawGet(ctx, "/crm/v3/pipelines/deals")
	if err != nil {
		return nil, fmt.Errorf("hubspot: list pipelines failed: %w", err)
	}

	var result struct {
		Results []struct {
			ID     string `json:"id"`
			Label  string `json:"label"`
			Stages []struct {
				ID    string `json:"id"`
				Label string `json:"label"`
				Order int    `json:"displayOrder"`
			} `json:"stages"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("hubspot: failed to decode pipelines: %w", err)
	}

	pipelines := make([]Pipeline, 0, len(result.Results))
	for _, p := range result.Results {
		stages := make([]Stage, 0, len(p.Stages))
		for _, s := range p.Stages {
			stages = append(stages, Stage{ID: s.ID, Label: s.Label, Order: s.Order})
		}
		pipelines = append(pipelines, Pipeline{ID: p.ID, Label: p.Label, Stages: stages})
	}
	return pipelines, nil
}

// Validate verifies that the HubSpot credentials are valid.
func (w *hubspotWrapper) Validate(ctx context.Context) error {
	_, err := w.doRawGet(ctx, "/crm/v3/objects/contacts?limit=1")
	if err != nil {
		return fmt.Errorf("hubspot: validate failed: %w", err)
	}
	return nil
}

// ── Raw HTTP helpers for endpoints the SDK doesn't cover ────────────────

func (w *hubspotWrapper) doRawGet(_ context.Context, path string) ([]byte, error) {
	url := "https://api.hubapi.com" + path
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("hubspot: API error (status %d): %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func (w *hubspotWrapper) doRawPost(_ context.Context, path string, payload interface{}) ([]byte, error) {
	url := "https://api.hubapi.com" + path
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+w.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("hubspot: API error (status %d): %s", resp.StatusCode, string(body))
	}
	return body, nil
}
