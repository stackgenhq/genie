package snowflake

import (
	"context"
	"database/sql"
	"fmt"

	sf "github.com/snowflakedb/gosnowflake"
)

// snowflakeWrapper implements the Service interface using the official
// gosnowflake driver via database/sql.
type snowflakeWrapper struct {
	db *sql.DB
}

// newWrapper opens a database/sql connection to Snowflake using the
// gosnowflake driver.
func newWrapper(cfg Config) (*snowflakeWrapper, error) {
	dsnConfig := &sf.Config{
		Account:   cfg.Account,
		User:      cfg.User,
		Password:  cfg.Password,
		Database:  cfg.Database,
		Schema:    cfg.Schema,
		Warehouse: cfg.Warehouse,
		Role:      cfg.Role,
	}

	dsn, err := sf.DSN(dsnConfig)
	if err != nil {
		return nil, fmt.Errorf("snowflake: failed to build DSN: %w", err)
	}

	db, err := sql.Open("snowflake", dsn)
	if err != nil {
		return nil, fmt.Errorf("snowflake: failed to open connection: %w", err)
	}

	return &snowflakeWrapper{db: db}, nil
}

// Query executes a SQL query and returns results as columns + rows.
func (w *snowflakeWrapper) Query(ctx context.Context, sqlQuery string) (*QueryResult, error) {
	rows, err := w.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("snowflake: query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("snowflake: failed to get columns: %w", err)
	}

	result := &QueryResult{
		Columns: columns,
		Rows:    make([][]interface{}, 0),
	}

	const maxRows = 500
	for rows.Next() && result.RowCount < maxRows {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("snowflake: failed to scan row: %w", err)
		}

		// Convert byte slices to strings for JSON serialisation.
		row := make([]interface{}, len(columns))
		for i, v := range values {
			if b, ok := v.([]byte); ok {
				row[i] = string(b)
			} else {
				row[i] = v
			}
		}

		result.Rows = append(result.Rows, row)
		result.RowCount++
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("snowflake: row iteration error: %w", err)
	}

	return result, nil
}

// ListDatabases returns accessible databases.
func (w *snowflakeWrapper) ListDatabases(ctx context.Context) ([]string, error) {
	rows, err := w.db.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		return nil, fmt.Errorf("snowflake: failed to list databases: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanNameColumn(rows)
}

// ListSchemas returns schemas in the given database.
func (w *snowflakeWrapper) ListSchemas(ctx context.Context, database string) ([]string, error) {
	query := fmt.Sprintf("SHOW SCHEMAS IN DATABASE %q", database)
	rows, err := w.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("snowflake: failed to list schemas in %s: %w", database, err)
	}
	defer func() { _ = rows.Close() }()

	return scanNameColumn(rows)
}

// ListTables returns tables in the given database and schema.
func (w *snowflakeWrapper) ListTables(ctx context.Context, database, schema string) ([]TableInfo, error) {
	query := fmt.Sprintf("SHOW TABLES IN %q.%q", database, schema)
	rows, err := w.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("snowflake: failed to list tables in %s.%s: %w", database, schema, err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("snowflake: failed to get columns: %w", err)
	}

	nameIdx, kindIdx, commentIdx := -1, -1, -1
	for i, c := range columns {
		switch c {
		case "name":
			nameIdx = i
		case "kind":
			kindIdx = i
		case "comment":
			commentIdx = i
		}
	}

	var tables []TableInfo
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("snowflake: failed to scan table row: %w", err)
		}

		t := TableInfo{}
		if nameIdx >= 0 {
			t.Name = fmt.Sprintf("%v", values[nameIdx])
		}
		if kindIdx >= 0 {
			t.Type = fmt.Sprintf("%v", values[kindIdx])
		}
		if commentIdx >= 0 && values[commentIdx] != nil {
			t.Comment = fmt.Sprintf("%v", values[commentIdx])
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

// DescribeTable returns column metadata for a table.
func (w *snowflakeWrapper) DescribeTable(ctx context.Context, database, schema, table string) ([]ColumnInfo, error) {
	query := fmt.Sprintf("DESCRIBE TABLE %q.%q.%q", database, schema, table)
	rows, err := w.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("snowflake: failed to describe table %s.%s.%s: %w", database, schema, table, err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("snowflake: failed to get columns: %w", err)
	}

	nameIdx, typeIdx, nullIdx, commentIdx := -1, -1, -1, -1
	for i, c := range columns {
		switch c {
		case "name":
			nameIdx = i
		case "type":
			typeIdx = i
		case "null?":
			nullIdx = i
		case "comment":
			commentIdx = i
		}
	}

	var cols []ColumnInfo
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("snowflake: failed to scan column row: %w", err)
		}

		col := ColumnInfo{}
		if nameIdx >= 0 {
			col.Name = fmt.Sprintf("%v", values[nameIdx])
		}
		if typeIdx >= 0 {
			col.Type = fmt.Sprintf("%v", values[typeIdx])
		}
		if nullIdx >= 0 {
			col.Nullable = fmt.Sprintf("%v", values[nullIdx]) == "Y"
		}
		if commentIdx >= 0 && values[commentIdx] != nil {
			col.Comment = fmt.Sprintf("%v", values[commentIdx])
		}
		cols = append(cols, col)
	}
	return cols, rows.Err()
}

// Validate performs a lightweight health check using SELECT 1.
func (w *snowflakeWrapper) Validate(ctx context.Context) error {
	var result int
	if err := w.db.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil {
		return fmt.Errorf("snowflake: validate failed: %w", err)
	}
	return nil
}

// scanNameColumn scans the "name" column from SHOW commands.
// Snowflake SHOW commands return variable columns, so we scan all
// and extract the one named "name".
func scanNameColumn(rows *sql.Rows) ([]string, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("snowflake: failed to get columns: %w", err)
	}

	nameIdx := -1
	for i, c := range columns {
		if c == "name" {
			nameIdx = i
			break
		}
	}
	if nameIdx == -1 {
		return nil, fmt.Errorf("snowflake: 'name' column not found in SHOW results")
	}

	var names []string
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("snowflake: failed to scan row: %w", err)
		}
		if nameIdx < len(values) && values[nameIdx] != nil {
			names = append(names, fmt.Sprintf("%v", values[nameIdx]))
		}
	}
	return names, rows.Err()
}
