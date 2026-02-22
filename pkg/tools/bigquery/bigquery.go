package bigquery

import (
	"context"
	"fmt"

	"github.com/appcd-dev/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

//go:generate go tool counterfeiter -generate

// Service defines the capabilities of the BigQuery connector. It exposes
// read-only query and introspection operations. DDL/DML is intentionally
// omitted to limit the blast radius of AI agent access.
//
//counterfeiter:generate . Service
type Service interface {
	// Query executes a SQL query and returns the results.
	Query(ctx context.Context, sql string) (*QueryResult, error)

	// ListDatasets returns the datasets in the configured project.
	ListDatasets(ctx context.Context) ([]DatasetInfo, error)

	// ListTables returns the tables in a dataset.
	ListTables(ctx context.Context, datasetID string) ([]TableInfo, error)

	// DescribeTable returns column metadata for a table.
	DescribeTable(ctx context.Context, datasetID, tableID string) (*TableDescription, error)

	// Validate performs a lightweight health check.
	Validate(ctx context.Context) error
}

// Config holds configuration for the BigQuery connector.
type Config struct {
	ProjectID       string `yaml:"project_id" toml:"project_id"`             // GCP project ID
	CredentialsFile string `yaml:"credentials_file" toml:"credentials_file"` // Optional path to service account JSON
}

// ── Domain Types ────────────────────────────────────────────────────────

// QueryResult represents the result of a BigQuery SQL query.
type QueryResult struct {
	Columns    []string        `json:"columns"`
	Rows       [][]interface{} `json:"rows"`
	RowCount   int             `json:"row_count"`
	TotalBytes int64           `json:"total_bytes_processed"`
}

// DatasetInfo describes a BigQuery dataset.
type DatasetInfo struct {
	ID       string `json:"id"`
	Location string `json:"location,omitempty"`
}

// TableInfo describes a BigQuery table.
type TableInfo struct {
	ID   string `json:"id"`
	Type string `json:"type"` // TABLE, VIEW, etc.
}

// TableDescription contains column metadata for a table.
type TableDescription struct {
	DatasetID string       `json:"dataset_id"`
	TableID   string       `json:"table_id"`
	Columns   []ColumnInfo `json:"columns"`
	RowCount  int64        `json:"row_count"`
	SizeBytes int64        `json:"size_bytes"`
}

// ColumnInfo describes a column in a BigQuery table.
type ColumnInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Mode        string `json:"mode"` // NULLABLE, REQUIRED, REPEATED
	Description string `json:"description,omitempty"`
}

// ── Factory ─────────────────────────────────────────────────────────────

// New creates a new BigQuery Service from the given configuration.
// If CredentialsFile is empty, ADC (Application Default Credentials) is used.
func New(cfg Config) (Service, error) {
	log := logger.GetLogger(context.Background())
	log.Info("Initializing BigQuery service", "project_id", cfg.ProjectID)

	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("bigquery: project_id is required")
	}

	return newWrapper(cfg)
}

// ── Request Types ───────────────────────────────────────────────────────

type queryRequest struct {
	SQL string `json:"sql" jsonschema:"description=BigQuery SQL query (read-only),required"`
}

type listTablesRequest struct {
	DatasetID string `json:"dataset_id" jsonschema:"description=BigQuery dataset ID,required"`
}

type describeTableRequest struct {
	DatasetID string `json:"dataset_id" jsonschema:"description=BigQuery dataset ID,required"`
	TableID   string `json:"table_id" jsonschema:"description=BigQuery table ID,required"`
}

// ── Tool Constructors ───────────────────────────────────────────────────

type toolSet struct {
	s Service
}

func NewQueryTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.query,
		function.WithName("bigquery_query"),
		function.WithDescription("Execute a read-only SQL query against BigQuery. Returns columns, rows, and bytes processed."),
	)
}

func NewListDatasetsTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listDatasets,
		function.WithName("bigquery_list_datasets"),
		function.WithDescription("List BigQuery datasets in the configured project."),
	)
}

func NewListTablesTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listTables,
		function.WithName("bigquery_list_tables"),
		function.WithDescription("List tables in a BigQuery dataset."),
	)
}

func NewDescribeTableTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.describeTable,
		function.WithName("bigquery_describe_table"),
		function.WithDescription("Describe a BigQuery table's columns, row count, and size."),
	)
}

// AllTools returns all BigQuery tools wired to the service.
func AllTools(s Service) []tool.Tool {
	return []tool.Tool{
		NewQueryTool(s),
		NewListDatasetsTool(s),
		NewListTablesTool(s),
		NewDescribeTableTool(s),
	}
}

// ── Tool Implementations ────────────────────────────────────────────────

func (ts *toolSet) query(ctx context.Context, req queryRequest) (*QueryResult, error) {
	return ts.s.Query(ctx, req.SQL)
}

func (ts *toolSet) listDatasets(ctx context.Context, _ struct{}) ([]DatasetInfo, error) {
	return ts.s.ListDatasets(ctx)
}

func (ts *toolSet) listTables(ctx context.Context, req listTablesRequest) ([]TableInfo, error) {
	return ts.s.ListTables(ctx, req.DatasetID)
}

func (ts *toolSet) describeTable(ctx context.Context, req describeTableRequest) (*TableDescription, error) {
	return ts.s.DescribeTable(ctx, req.DatasetID, req.TableID)
}
