package salesforce

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	sf "github.com/k-capehart/go-salesforce/v3"
)

// salesforceWrapper implements the Service interface using the
// k-capehart/go-salesforce SDK (v3). This replaces the previous direct HTTP
// implementation, gaining automatic session refresh, typed error handling,
// and Bulk/Composite support for future use.
type salesforceWrapper struct {
	client *sf.Salesforce
}

// newWrapper constructs a Salesforce client from the given Config.
// It supports two auth flows:
//   - Access Token (preferred for agents): set Token
//   - Client Credentials: set ClientID + ClientSecret
func newWrapper(cfg Config) (*salesforceWrapper, error) {
	creds := sf.Creds{
		Domain: cfg.InstanceURL,
	}

	if cfg.Token != "" {
		creds.AccessToken = cfg.Token
	} else if cfg.ClientID != "" && cfg.ClientSecret != "" {
		creds.ConsumerKey = cfg.ClientID
		creds.ConsumerSecret = cfg.ClientSecret
	} else {
		return nil, fmt.Errorf("salesforce: either token or client_id+client_secret required")
	}

	client, err := sf.Init(creds)
	if err != nil {
		return nil, fmt.Errorf("salesforce: failed to initialize client: %w", err)
	}

	return &salesforceWrapper{client: client}, nil
}

// doJSON performs a DoRequest with JSON marshalling and response parsing.
func (w *salesforceWrapper) doJSON(method, uri string, payload interface{}) ([]byte, error) {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("salesforce: marshal failed: %w", err)
		}
	}

	resp, err := w.client.DoRequest(method, uri, body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("salesforce: failed to read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("salesforce: API error (status %d): %s", resp.StatusCode, string(respBody))
	}
	return respBody, nil
}

// Query executes a SOQL query and returns the results.
func (w *salesforceWrapper) Query(ctx context.Context, soql string) (*QueryResult, error) {
	var records []map[string]interface{}
	err := w.client.Query(soql, &records)
	if err != nil {
		return nil, fmt.Errorf("salesforce: query failed: %w", err)
	}

	return &QueryResult{
		TotalSize: len(records),
		Done:      true,
		Records:   records,
	}, nil
}

// GetRecord retrieves a single record by object type and ID.
func (w *salesforceWrapper) GetRecord(ctx context.Context, objectType, id string) (map[string]interface{}, error) {
	soql := fmt.Sprintf("SELECT FIELDS(ALL) FROM %s WHERE Id = '%s' LIMIT 1", objectType, id)
	var records []map[string]interface{}
	err := w.client.Query(soql, &records)
	if err != nil {
		return nil, fmt.Errorf("salesforce: get record failed: %w", err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("salesforce: record %s/%s not found", objectType, id)
	}
	return records[0], nil
}

// CreateRecord creates a new record and returns the ID.
func (w *salesforceWrapper) CreateRecord(ctx context.Context, objectType string, fields map[string]interface{}) (string, error) {
	respBody, err := w.doJSON("POST", "/services/data/v62.0/sobjects/"+objectType, fields)
	if err != nil {
		return "", fmt.Errorf("salesforce: create record failed: %w", err)
	}

	var result struct {
		ID      string `json:"id"`
		Success bool   `json:"success"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("salesforce: failed to parse create response: %w", err)
	}
	if !result.Success {
		return "", fmt.Errorf("salesforce: create record returned success=false")
	}
	return result.ID, nil
}

// UpdateRecord updates an existing record.
func (w *salesforceWrapper) UpdateRecord(ctx context.Context, objectType, id string, fields map[string]interface{}) error {
	_, err := w.doJSON("PATCH",
		fmt.Sprintf("/services/data/v62.0/sobjects/%s/%s", objectType, id), fields)
	if err != nil {
		return fmt.Errorf("salesforce: update record failed: %w", err)
	}
	return nil
}

// DescribeObject returns metadata about a Salesforce object.
func (w *salesforceWrapper) DescribeObject(ctx context.Context, objectType string) (*ObjectDescription, error) {
	respBody, err := w.doJSON("GET",
		fmt.Sprintf("/services/data/v62.0/sobjects/%s/describe", objectType), nil)
	if err != nil {
		return nil, fmt.Errorf("salesforce: describe object failed: %w", err)
	}

	var raw struct {
		Name   string `json:"name"`
		Label  string `json:"label"`
		Fields []struct {
			Name     string `json:"name"`
			Label    string `json:"label"`
			Type     string `json:"type"`
			Nillable bool   `json:"nillable"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("salesforce: failed to parse describe response: %w", err)
	}

	fields := make([]FieldInfo, 0, len(raw.Fields))
	for _, f := range raw.Fields {
		fields = append(fields, FieldInfo{
			Name:     f.Name,
			Label:    f.Label,
			Type:     f.Type,
			Required: !f.Nillable,
		})
	}

	return &ObjectDescription{
		Name:   raw.Name,
		Label:  raw.Label,
		Fields: fields,
	}, nil
}

// ListObjects returns a list of all accessible Salesforce objects.
func (w *salesforceWrapper) ListObjects(ctx context.Context) ([]ObjectInfo, error) {
	respBody, err := w.doJSON("GET", "/services/data/v62.0/sobjects", nil)
	if err != nil {
		return nil, fmt.Errorf("salesforce: list objects failed: %w", err)
	}

	var raw struct {
		SObjects []struct {
			Name  string `json:"name"`
			Label string `json:"label"`
		} `json:"sobjects"`
	}
	if err := json.Unmarshal(respBody, &raw); err != nil {
		return nil, fmt.Errorf("salesforce: failed to parse list response: %w", err)
	}

	objects := make([]ObjectInfo, 0, len(raw.SObjects))
	for _, o := range raw.SObjects {
		objects = append(objects, ObjectInfo{
			Name:  o.Name,
			Label: o.Label,
		})
	}
	return objects, nil
}

// Validate verifies that the Salesforce credentials are valid.
func (w *salesforceWrapper) Validate(ctx context.Context) error {
	_, err := w.doJSON("GET", "/services/data/v62.0/limits", nil)
	if err != nil {
		return fmt.Errorf("salesforce: validate failed: %w", err)
	}
	return nil
}
