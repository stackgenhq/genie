// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

// Package sqltool provides SQL database query tools for agents. It enables
// agents to query PostgreSQL, MySQL, and SQLite databases using SQL, making
// structured data accessible through natural language workflows.
//
// Problem: Agents frequently need to answer data questions like "how many
// users signed up this week?" or "what's the average order value?" Without
// this tool, agents cannot access relational databases and must rely on
// stale training data or ask humans to run queries manually.
//
// Safety guards:
//   - Read-only by default (only SELECT, SHOW, DESCRIBE, EXPLAIN allowed)
//   - Query timeout (30 seconds) prevents runaway queries
//   - Output truncated at 32 KB to limit LLM context consumption
//   - Row limit (1000) prevents massive result sets
//   - No credentials stored — DSN passed per-request or via config
//
// Dependencies:
//   - psql CLI (PostgreSQL) — install: brew install postgresql / apt-get install postgresql-client
//   - mysql CLI (MySQL) — install: brew install mysql-client / apt-get install mysql-client
//   - sqlite3 CLI (SQLite) — install: brew install sqlite / apt-get install sqlite3
//   - No Go database drivers required (uses exec-based approach)
package sqltool

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/stackgenhq/genie/pkg/osutils"
	"github.com/stackgenhq/genie/pkg/security"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"
)

const (
	maxOutputBytes = 32 << 10 // 32 KB
	queryTimeout   = 30 * time.Second
	maxRows        = 1000
)

// readOnlyPattern matches only safe, read-only SQL statements.
var readOnlyPattern = regexp.MustCompile(`(?i)^\s*(SELECT|SHOW|DESCRIBE|DESC|EXPLAIN|WITH)\b`)

// limitPattern detects an existing LIMIT clause (used by queryWithLimit).
var limitPattern = regexp.MustCompile(`(?i)\bLIMIT\b`)

// multiStatementPattern detects semicolons outside of quoted strings.
// This is a conservative check — it rejects any query containing a
// semicolon to prevent multi-statement injection via CLI clients.
var multiStatementPattern = regexp.MustCompile(`;`)

// ────────────────────── Request / Response ──────────────────────

type sqlRequest struct {
	Query    string `json:"query" jsonschema:"description=The SQL query to execute. Only SELECT, SHOW, DESCRIBE, and EXPLAIN are allowed by default."`
	Database string `json:"database" jsonschema:"description=Database type: postgresql, mysql, or sqlite.,enum=postgresql,enum=mysql,enum=sqlite"`
	DSN      string `json:"dsn" jsonschema:"description=Database connection string. For PostgreSQL: postgres://user:pass@host:5432/dbname. For MySQL: user:pass@tcp(host:3306)/dbname. For SQLite: /path/to/file.db"`
}

// validate checks required fields and enforces read-only access.
func (r sqlRequest) validate() error {
	if r.Query == "" {
		return fmt.Errorf("query is required")
	}
	if r.DSN == "" {
		return fmt.Errorf("dsn is required")
	}
	// Reject multi-statement queries to prevent injection via CLI clients.
	// CLI tools like psql -c and sqlite3 can execute multiple statements
	// separated by semicolons, which would bypass the read-only check.
	q := strings.TrimSpace(r.Query)
	q = strings.TrimRight(q, "; \t\n") // allow trailing semicolons
	if multiStatementPattern.MatchString(q) {
		return fmt.Errorf("multi-statement queries are not allowed; submit one statement at a time")
	}
	if !readOnlyPattern.MatchString(r.Query) {
		return fmt.Errorf("only SELECT, SHOW, DESCRIBE, DESC, EXPLAIN, and WITH queries are allowed (read-only mode)")
	}
	return nil
}

// dbType returns the normalized database type (defaults to "postgresql").
func (r sqlRequest) dbType() string {
	db := strings.ToLower(strings.TrimSpace(r.Database))
	if db == "" {
		return "postgresql"
	}
	return db
}

// queryWithLimit appends a LIMIT clause if one isn't already present.
func (r sqlRequest) queryWithLimit() string {
	q := r.Query
	if !limitPattern.MatchString(q) {
		q = strings.TrimRight(q, "; \t\n") + fmt.Sprintf(" LIMIT %d", maxRows)
	}
	return q
}

type sqlResponse struct {
	Query    string `json:"query"`
	Database string `json:"database"`
	Result   string `json:"result"`
	RowCount int    `json:"row_count,omitempty"`
	Message  string `json:"message"`
}

// ────────────────────── Tool constructors ──────────────────────

type sqlTools struct {
	secretProvider security.SecretProvider
	name           string
}

func NewSQLTools(name string, secretProvider security.SecretProvider) *sqlTools {
	return &sqlTools{
		secretProvider: secretProvider,
		name:           name,
	}
}

func (s *sqlTools) queryTool() tool.CallableTool {
	return function.NewFunctionTool(
		s.query,
		function.WithName(fmt.Sprintf("%s_sql_query", s.name)),
		function.WithDescription(
			"Execute a read-only SQL query against the "+s.name+" database. "+
				"Supports PostgreSQL (psql), MySQL (mysql), and SQLite (sqlite3). "+
				"Only SELECT, SHOW, DESCRIBE, and EXPLAIN queries are allowed. "+
				"Returns results as a formatted table. "+
				"Requires the appropriate CLI tool installed on the system.",
		),
	)
}

// ────────────────────── Implementation ──────────────────────

func (s *sqlTools) query(ctx context.Context, req sqlRequest) (sqlResponse, error) {
	resp := sqlResponse{
		Query:    req.Query,
		Database: req.Database,
	}

	if err := req.validate(); err != nil {
		return resp, err
	}

	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	db := req.dbType()
	queryWithLimit := req.queryWithLimit()

	var out []byte
	var err error

	switch db {
	case "postgresql", "postgres", "pg":
		out, err = s.queryPostgres(ctx, req.DSN, queryWithLimit)
		resp.Database = "postgresql"
	case "mysql":
		out, err = s.queryMySQL(ctx, req.DSN, queryWithLimit)
		resp.Database = "mysql"
	case "sqlite", "sqlite3":
		out, err = s.querySQLite(ctx, req.DSN, queryWithLimit)
		resp.Database = "sqlite"
	default:
		return resp, fmt.Errorf("unsupported database %q: must be postgresql, mysql, or sqlite", req.Database)
	}

	if err != nil {
		return resp, fmt.Errorf("query failed: %w", err)
	}

	result := strings.TrimSpace(string(out))
	resp.RowCount = max(0, strings.Count(result, "\n")) // approximate

	if len(result) > maxOutputBytes {
		result = result[:maxOutputBytes] + "\n\n[...truncated — output exceeded 32 KB limit]"
	}

	resp.Result = result
	resp.Message = fmt.Sprintf("Query executed (%d rows, %d chars)", resp.RowCount, len(result))
	return resp, nil
}

func (s *sqlTools) queryPostgres(ctx context.Context, dsn, query string) ([]byte, error) {
	if err := osutils.ValidateToolAvailability("psql", map[string]string{"brew": "postgresql", "apt": "postgresql-client"}); err != nil {
		return nil, err
	}
	if strings.HasPrefix(dsn, "-") {
		return nil, fmt.Errorf("invalid DSN: must not start with '-'")
	}
	// Use "--" to terminate options so the DSN is never interpreted as a flag.
	cmd := exec.CommandContext(ctx, "psql", "--no-psqlrc", "--pset=footer=off", "-c", query, "--", dsn)
	return cmd.CombinedOutput()
}

func (s *sqlTools) queryMySQL(ctx context.Context, dsn, query string) ([]byte, error) {
	if err := osutils.ValidateToolAvailability("mysql", map[string]string{"brew": "mysql-client", "pacman": "mariadb-client"}); err != nil {
		return nil, err
	}
	if strings.HasPrefix(dsn, "-") {
		return nil, fmt.Errorf("invalid DSN: must not start with '-'")
	}
	// MySQL 8+ accepts a URI-style connection string via the positional arg.
	// Place "--" before the DSN to prevent option injection.
	cmd := exec.CommandContext(ctx, "mysql", "--table", "--execute", query, "--", dsn)
	return cmd.CombinedOutput()
}

func (s *sqlTools) querySQLite(ctx context.Context, dsn, query string) ([]byte, error) {
	if err := osutils.ValidateToolAvailability("sqlite3", map[string]string{
		"brew": "sqlite",
		"apt":  "sqlite3",
	}); err != nil {
		return nil, err
	}
	if strings.HasPrefix(dsn, "-") {
		return nil, fmt.Errorf("invalid database path: must not start with '-'")
	}
	// Use "--" to terminate options so the DB path is never interpreted as a flag.
	cmd := exec.CommandContext(ctx, "sqlite3", "-header", "-column", "--", dsn, query)
	return cmd.CombinedOutput()
}
