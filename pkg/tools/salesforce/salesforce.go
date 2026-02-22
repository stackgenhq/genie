package salesforce

import (
	"context"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

//go:generate go tool counterfeiter -generate

// Service defines the capabilities of the Salesforce connector. It exposes
// SOQL queries and standard CRUD operations on Salesforce objects.
//
//counterfeiter:generate . Service
type Service interface {
	// Query executes a SOQL query and returns the results.
	Query(ctx context.Context, soql string) (*QueryResult, error)

	// GetRecord retrieves a single record by SObject type and ID.
	GetRecord(ctx context.Context, objectType string, id string) (map[string]interface{}, error)

	// CreateRecord creates a new record of the given SObject type.
	CreateRecord(ctx context.Context, objectType string, fields map[string]interface{}) (string, error)

	// UpdateRecord updates an existing record.
	UpdateRecord(ctx context.Context, objectType string, id string, fields map[string]interface{}) error

	// DescribeObject returns metadata about a Salesforce object (fields, types).
	DescribeObject(ctx context.Context, objectType string) (*ObjectDescription, error)

	// ListObjects returns the available SObject types.
	ListObjects(ctx context.Context) ([]ObjectInfo, error)

	// Validate performs a lightweight health check.
	Validate(ctx context.Context) error
}

// Config holds connection parameters for Salesforce.
type Config struct {
	InstanceURL   string `yaml:"instance_url" toml:"instance_url"`     // e.g. https://yourco.my.salesforce.com
	Token         string `yaml:"token" toml:"token"`                   // Direct access token (preferred for agents)
	ClientID      string `yaml:"client_id" toml:"client_id"`           // OAuth2 client ID
	ClientSecret  string `yaml:"client_secret" toml:"client_secret"`   // OAuth2 client secret
	Username      string `yaml:"username" toml:"username"`             // Salesforce username (for password flow)
	Password      string `yaml:"password" toml:"password"`             // Salesforce password (for password flow)
	SecurityToken string `yaml:"security_token" toml:"security_token"` // Salesforce security token
}

// ── Domain Types ────────────────────────────────────────────────────────

// QueryResult represents the result of a SOQL query.
type QueryResult struct {
	TotalSize int                      `json:"total_size"`
	Records   []map[string]interface{} `json:"records"`
	Done      bool                     `json:"done"`
}

// ObjectDescription describes a Salesforce object's fields.
type ObjectDescription struct {
	Name   string      `json:"name"`
	Label  string      `json:"label"`
	Fields []FieldInfo `json:"fields"`
}

// FieldInfo describes a single field on a Salesforce object.
type FieldInfo struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// ObjectInfo provides basic information about a Salesforce object type.
type ObjectInfo struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// ── Factory ─────────────────────────────────────────────────────────────

// New creates a new Salesforce Service from the given configuration.
// It authenticates using the OAuth2 password flow. Without this factory,
// callers would need to handle token exchange manually.
func New(cfg Config) (Service, error) {

	if cfg.InstanceURL == "" {
		return nil, fmt.Errorf("salesforce: instance_url is required")
	}

	return newWrapper(cfg)
}

// ── Request Types ───────────────────────────────────────────────────────

type queryRequest struct {
	SOQL string `json:"soql" jsonschema:"description=SOQL query string (e.g. SELECT Id Name FROM Account LIMIT 10),required"`
}

type getRecordRequest struct {
	ObjectType string `json:"object_type" jsonschema:"description=Salesforce object type (e.g. Account/Contact/Opportunity),required"`
	ID         string `json:"id" jsonschema:"description=Record ID,required"`
}

type createRecordRequest struct {
	ObjectType string                 `json:"object_type" jsonschema:"description=Salesforce object type (e.g. Account/Contact/Opportunity),required"`
	Fields     map[string]interface{} `json:"fields" jsonschema:"description=Field name-value pairs for the new record,required"`
}

type updateRecordRequest struct {
	ObjectType string                 `json:"object_type" jsonschema:"description=Salesforce object type (e.g. Account/Contact/Opportunity),required"`
	ID         string                 `json:"id" jsonschema:"description=Record ID to update,required"`
	Fields     map[string]interface{} `json:"fields" jsonschema:"description=Field name-value pairs to update,required"`
}

type describeObjectRequest struct {
	ObjectType string `json:"object_type" jsonschema:"description=Salesforce object type (e.g. Account/Contact/Opportunity),required"`
}

// ── Tool Constructors ───────────────────────────────────────────────────

type toolSet struct {
	s Service
}

func NewQueryTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.query,
		function.WithName("salesforce_query"),
		function.WithDescription("Execute a SOQL query against Salesforce. Returns matching records."),
	)
}

func NewGetRecordTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.getRecord,
		function.WithName("salesforce_get_record"),
		function.WithDescription("Get a single Salesforce record by object type and ID."),
	)
}

func NewCreateRecordTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.createRecord,
		function.WithName("salesforce_create_record"),
		function.WithDescription("Create a new Salesforce record. Use salesforce_describe_object to discover required fields first."),
	)
}

func NewUpdateRecordTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.updateRecord,
		function.WithName("salesforce_update_record"),
		function.WithDescription("Update an existing Salesforce record's fields."),
	)
}

func NewDescribeObjectTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.describeObject,
		function.WithName("salesforce_describe_object"),
		function.WithDescription("Describe a Salesforce object's fields, types, and required status."),
	)
}

func NewListObjectsTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listObjects,
		function.WithName("salesforce_list_objects"),
		function.WithDescription("List available Salesforce object types."),
	)
}

// AllTools returns all Salesforce tools wired to the service.
func AllTools(s Service) []tool.Tool {
	return []tool.Tool{
		NewQueryTool(s),
		NewGetRecordTool(s),
		NewCreateRecordTool(s),
		NewUpdateRecordTool(s),
		NewDescribeObjectTool(s),
		NewListObjectsTool(s),
	}
}

// ── Tool Implementations ────────────────────────────────────────────────

func (ts *toolSet) query(ctx context.Context, req queryRequest) (*QueryResult, error) {
	return ts.s.Query(ctx, req.SOQL)
}

func (ts *toolSet) getRecord(ctx context.Context, req getRecordRequest) (map[string]interface{}, error) {
	return ts.s.GetRecord(ctx, req.ObjectType, req.ID)
}

func (ts *toolSet) createRecord(ctx context.Context, req createRecordRequest) (string, error) {
	id, err := ts.s.CreateRecord(ctx, req.ObjectType, req.Fields)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Created %s record: %s", req.ObjectType, id), nil
}

func (ts *toolSet) updateRecord(ctx context.Context, req updateRecordRequest) (string, error) {
	err := ts.s.UpdateRecord(ctx, req.ObjectType, req.ID, req.Fields)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Updated %s record: %s", req.ObjectType, req.ID), nil
}

func (ts *toolSet) describeObject(ctx context.Context, req describeObjectRequest) (*ObjectDescription, error) {
	return ts.s.DescribeObject(ctx, req.ObjectType)
}

func (ts *toolSet) listObjects(ctx context.Context, _ struct{}) ([]ObjectInfo, error) {
	return ts.s.ListObjects(ctx)
}
