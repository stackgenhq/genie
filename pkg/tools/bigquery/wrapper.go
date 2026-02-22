package bigquery

import (
	"context"
	"fmt"
	"os"

	bq "cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// bigqueryWrapper implements the Service interface using the official
// cloud.google.com/go/bigquery client library.
type bigqueryWrapper struct {
	client    *bq.Client
	projectID string
}

// newWrapper creates a BigQuery client. If CredentialsFile is provided,
// it's used; otherwise Application Default Credentials are used.
func newWrapper(cfg Config) (*bigqueryWrapper, error) {
	ctx := context.Background()
	var opts []option.ClientOption

	if cfg.CredentialsFile != "" {
		if _, err := os.Stat(cfg.CredentialsFile); err != nil {
			return nil, fmt.Errorf("bigquery: credentials file not found: %w", err)
		}
		opts = append(opts, option.WithCredentialsFile(cfg.CredentialsFile)) //nolint:staticcheck // TODO: migrate to non-deprecated credentials API
	}

	client, err := bq.NewClient(ctx, cfg.ProjectID, opts...)
	if err != nil {
		return nil, fmt.Errorf("bigquery: failed to create client: %w", err)
	}

	return &bigqueryWrapper{client: client, projectID: cfg.ProjectID}, nil
}

// Query executes a SQL query and returns tabular results.
func (w *bigqueryWrapper) Query(ctx context.Context, sql string) (*QueryResult, error) {
	q := w.client.Query(sql)
	q.DryRun = false

	job, err := q.Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("bigquery: query run failed: %w", err)
	}

	it, err := job.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("bigquery: query read failed: %w", err)
	}

	// Extract column names from schema.
	schema := it.Schema
	columns := make([]string, len(schema))
	for i, fs := range schema {
		columns[i] = fs.Name
	}

	result := &QueryResult{
		Columns: columns,
		Rows:    make([][]interface{}, 0),
	}

	const maxRows = 500
	for {
		var row []bq.Value
		err := it.Next(&row)
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("bigquery: row iteration error: %w", err)
		}

		rowData := make([]interface{}, len(row))
		for i, v := range row {
			rowData[i] = v
		}
		result.Rows = append(result.Rows, rowData)
		result.RowCount++

		if result.RowCount >= maxRows {
			break
		}
	}

	// Get job statistics for bytes processed.
	status := job.LastStatus()
	if status != nil && status.Statistics != nil {
		result.TotalBytes = status.Statistics.TotalBytesProcessed
	}

	return result, nil
}

// ListDatasets returns datasets in the project.
func (w *bigqueryWrapper) ListDatasets(ctx context.Context) ([]DatasetInfo, error) {
	it := w.client.Datasets(ctx)
	var datasets []DatasetInfo

	for {
		ds, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("bigquery: list datasets failed: %w", err)
		}

		meta, err := ds.Metadata(ctx)
		location := ""
		if err == nil && meta != nil {
			location = meta.Location
		}

		datasets = append(datasets, DatasetInfo{
			ID:       ds.DatasetID,
			Location: location,
		})
	}
	return datasets, nil
}

// ListTables returns tables in a dataset.
func (w *bigqueryWrapper) ListTables(ctx context.Context, datasetID string) ([]TableInfo, error) {
	ds := w.client.Dataset(datasetID)
	it := ds.Tables(ctx)
	var tables []TableInfo

	for {
		t, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("bigquery: list tables failed: %w", err)
		}

		meta, err := t.Metadata(ctx)
		tableType := "TABLE"
		if err == nil && meta != nil {
			tableType = string(meta.Type)
		}

		tables = append(tables, TableInfo{
			ID:   t.TableID,
			Type: tableType,
		})
	}
	return tables, nil
}

// DescribeTable returns column metadata for a table.
func (w *bigqueryWrapper) DescribeTable(ctx context.Context, datasetID, tableID string) (*TableDescription, error) {
	table := w.client.Dataset(datasetID).Table(tableID)
	meta, err := table.Metadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("bigquery: describe table failed: %w", err)
	}

	columns := make([]ColumnInfo, 0, len(meta.Schema))
	for _, fs := range meta.Schema {
		col := ColumnInfo{
			Name:        fs.Name,
			Type:        string(fs.Type),
			Description: fs.Description,
		}
		if fs.Required {
			col.Mode = "REQUIRED"
		} else if fs.Repeated {
			col.Mode = "REPEATED"
		} else {
			col.Mode = "NULLABLE"
		}
		columns = append(columns, col)
	}

	return &TableDescription{
		DatasetID: datasetID,
		TableID:   tableID,
		Columns:   columns,
		RowCount:  int64(meta.NumRows),
		SizeBytes: meta.NumBytes,
	}, nil
}

// Validate verifies that the client can access the project.
func (w *bigqueryWrapper) Validate(ctx context.Context) error {
	it := w.client.Datasets(ctx)
	_, err := it.Next()
	if err != nil && err != iterator.Done {
		return fmt.Errorf("bigquery: validate failed: %w", err)
	}
	return nil
}
