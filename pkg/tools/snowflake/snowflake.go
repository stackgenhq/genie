package snowflake

import (
	"context"
	"fmt"

	"github.com/appcd-dev/genie/pkg/logger"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

//go:generate go tool counterfeiter -generate

// Service defines the capabilities of the Snowflake connector. It wraps
// read-only query operations — write/DDL operations are intentionally
// omitted to reduce the blast radius when an AI agent has access.
//
//counterfeiter:generate . Service
type Service interface {
	// Query executes a SQL query and returns the results as a table.
	Query(ctx context.Context, sql string) (*QueryResult, error)

	// ListDatabases returns the list of accessible databases.
	ListDatabases(ctx context.Context) ([]string, error)

	// ListSchemas returns the schemas in a given database.
	ListSchemas(ctx context.Context, database string) ([]string, error)

	// ListTables returns the tables in a given database and schema.
	ListTables(ctx context.Context, database string, schema string) ([]TableInfo, error)

	// DescribeTable returns column metadata for a table.
	DescribeTable(ctx context.Context, database, schema, table string) ([]ColumnInfo, error)

	// Validate performs a lightweight health check (SELECT 1).
	Validate(ctx context.Context) error
}

// Config holds connection parameters for Snowflake.
type Config struct {
	Account   string `yaml:"account" toml:"account"`     // Snowflake account identifier
	User      string `yaml:"user" toml:"user"`           // Login username
	Password  string `yaml:"password" toml:"password"`   // Login password
	Database  string `yaml:"database" toml:"database"`   // Default database
	Schema    string `yaml:"schema" toml:"schema"`       // Default schema
	Warehouse string `yaml:"warehouse" toml:"warehouse"` // Compute warehouse
	Role      string `yaml:"role" toml:"role"`           // Snowflake role
}

// ── Domain Types ────────────────────────────────────────────────────────

// QueryResult represents tabular query results.
type QueryResult struct {
	Columns  []string        `json:"columns"`
	Rows     [][]interface{} `json:"rows"`
	RowCount int             `json:"row_count"`
}

// TableInfo describes a table in a schema.
type TableInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"` // TABLE, VIEW, etc.
	Comment string `json:"comment,omitempty"`
}

// ColumnInfo describes a column in a table.
type ColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Comment  string `json:"comment,omitempty"`
}

// ── Factory ─────────────────────────────────────────────────────────────

// New creates a new Snowflake Service from the given configuration.
// It opens a database/sql connection using the gosnowflake driver.
// Without this factory, callers would need to build DSN strings manually.
func New(cfg Config) (Service, error) {
	log := logger.GetLogger(context.Background())
	log.Info("Initializing Snowflake service", "account", cfg.Account, "database", cfg.Database)

	if cfg.Account == "" {
		return nil, fmt.Errorf("snowflake: account is required")
	}
	if cfg.User == "" {
		return nil, fmt.Errorf("snowflake: user is required")
	}

	return newWrapper(cfg)
}

// ── Request Types ───────────────────────────────────────────────────────

type queryRequest struct {
	SQL string `json:"sql" jsonschema:"description=SQL query to execute (read-only queries only),required"`
}

type listSchemasRequest struct {
	Database string `json:"database" jsonschema:"description=Database name,required"`
}

type listTablesRequest struct {
	Database string `json:"database" jsonschema:"description=Database name,required"`
	Schema   string `json:"schema" jsonschema:"description=Schema name,required"`
}

type describeTableRequest struct {
	Database string `json:"database" jsonschema:"description=Database name,required"`
	Schema   string `json:"schema" jsonschema:"description=Schema name,required"`
	Table    string `json:"table" jsonschema:"description=Table name,required"`
}

// ── Tool Constructors ───────────────────────────────────────────────────

type toolSet struct {
	s Service
}

func NewQueryTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.query,
		function.WithName("snowflake_query"),
		function.WithDescription("Execute a read-only SQL query against Snowflake. Returns columns and rows. Do NOT use for DDL or DML operations."),
	)
}

func NewListDatabasesTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listDatabases,
		function.WithName("snowflake_list_databases"),
		function.WithDescription("List all accessible Snowflake databases."),
	)
}

func NewListSchemasTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listSchemas,
		function.WithName("snowflake_list_schemas"),
		function.WithDescription("List schemas in a Snowflake database."),
	)
}

func NewListTablesTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.listTables,
		function.WithName("snowflake_list_tables"),
		function.WithDescription("List tables and views in a Snowflake database schema."),
	)
}

func NewDescribeTableTool(s Service) tool.CallableTool {
	ts := &toolSet{s: s}
	return function.NewFunctionTool(
		ts.describeTable,
		function.WithName("snowflake_describe_table"),
		function.WithDescription("Describe the columns of a Snowflake table including types and nullability."),
	)
}

// AllTools returns all Snowflake tools wired to the service.
func AllTools(s Service) []tool.Tool {
	return []tool.Tool{
		NewQueryTool(s),
		NewListDatabasesTool(s),
		NewListSchemasTool(s),
		NewListTablesTool(s),
		NewDescribeTableTool(s),
	}
}

// ── Tool Implementations ────────────────────────────────────────────────

func (ts *toolSet) query(ctx context.Context, req queryRequest) (*QueryResult, error) {
	return ts.s.Query(ctx, req.SQL)
}

func (ts *toolSet) listDatabases(ctx context.Context, _ struct{}) ([]string, error) {
	return ts.s.ListDatabases(ctx)
}

func (ts *toolSet) listSchemas(ctx context.Context, req listSchemasRequest) ([]string, error) {
	return ts.s.ListSchemas(ctx, req.Database)
}

func (ts *toolSet) listTables(ctx context.Context, req listTablesRequest) ([]TableInfo, error) {
	return ts.s.ListTables(ctx, req.Database, req.Schema)
}

func (ts *toolSet) describeTable(ctx context.Context, req describeTableRequest) ([]ColumnInfo, error) {
	return ts.s.DescribeTable(ctx, req.Database, req.Schema, req.Table)
}
