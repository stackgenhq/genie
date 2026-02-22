package bigquery_test

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/tools/bigquery"
	"github.com/appcd-dev/genie/pkg/tools/bigquery/bigqueryfakes"
)

var _ = Describe("BigQuery Tools", func() {
	var fake *bigqueryfakes.FakeService

	BeforeEach(func() {
		fake = new(bigqueryfakes.FakeService)
	})

	Describe("bigquery_query", func() {
		It("should execute SQL and return tabular results", func(ctx context.Context) {
			fake.QueryReturns(&bigquery.QueryResult{
				Columns:    []string{"id", "name", "revenue"},
				Rows:       [][]interface{}{{1, "Acme", 1000000}},
				RowCount:   1,
				TotalBytes: 42000,
			}, nil)

			tool := bigquery.NewQueryTool(fake)
			reqJSON, _ := json.Marshal(struct {
				SQL string `json:"sql"`
			}{SQL: "SELECT * FROM sales.customers LIMIT 1"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			result, ok := resp.(*bigquery.QueryResult)
			Expect(ok).To(BeTrue())
			Expect(result.Columns).To(HaveLen(3))
			Expect(result.TotalBytes).To(Equal(int64(42000)))
		})
	})

	Describe("bigquery_list_datasets", func() {
		It("should return datasets", func(ctx context.Context) {
			fake.ListDatasetsReturns([]bigquery.DatasetInfo{
				{ID: "sales", Location: "US"},
				{ID: "analytics", Location: "EU"},
			}, nil)

			tool := bigquery.NewListDatasetsTool(fake)

			resp, err := tool.Call(ctx, []byte(`{}`))
			Expect(err).NotTo(HaveOccurred())

			datasets, ok := resp.([]bigquery.DatasetInfo)
			Expect(ok).To(BeTrue())
			Expect(datasets).To(HaveLen(2))
		})
	})

	Describe("bigquery_list_tables", func() {
		It("should return tables in a dataset", func(ctx context.Context) {
			fake.ListTablesReturns([]bigquery.TableInfo{
				{ID: "customers", Type: "TABLE"},
				{ID: "revenue_view", Type: "VIEW"},
			}, nil)

			tool := bigquery.NewListTablesTool(fake)
			reqJSON, _ := json.Marshal(struct {
				DatasetID string `json:"dataset_id"`
			}{DatasetID: "sales"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			tables, ok := resp.([]bigquery.TableInfo)
			Expect(ok).To(BeTrue())
			Expect(tables).To(HaveLen(2))
			Expect(tables[1].Type).To(Equal("VIEW"))
		})
	})

	Describe("bigquery_describe_table", func() {
		It("should return column metadata", func(ctx context.Context) {
			fake.DescribeTableReturns(&bigquery.TableDescription{
				DatasetID: "sales",
				TableID:   "customers",
				Columns: []bigquery.ColumnInfo{
					{Name: "id", Type: "INTEGER", Mode: "REQUIRED"},
					{Name: "name", Type: "STRING", Mode: "NULLABLE"},
				},
				RowCount:  1000000,
				SizeBytes: 50000000,
			}, nil)

			tool := bigquery.NewDescribeTableTool(fake)
			reqJSON, _ := json.Marshal(struct {
				DatasetID string `json:"dataset_id"`
				TableID   string `json:"table_id"`
			}{DatasetID: "sales", TableID: "customers"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			desc, ok := resp.(*bigquery.TableDescription)
			Expect(ok).To(BeTrue())
			Expect(desc.Columns).To(HaveLen(2))
			Expect(desc.RowCount).To(Equal(int64(1000000)))
		})
	})
})

var _ = Describe("AllTools", func() {
	It("should return 4 tools", func() {
		fake := new(bigqueryfakes.FakeService)
		tools := bigquery.AllTools(fake)
		Expect(tools).To(HaveLen(4))
	})
})

var _ = Describe("New", func() {
	It("should return error when project_id is missing", func() {
		_, err := bigquery.New(bigquery.Config{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("project_id"))
	})
})

var _ = Describe("BigQuery Error Paths", func() {
	var fake *bigqueryfakes.FakeService

	BeforeEach(func() {
		fake = new(bigqueryfakes.FakeService)
	})

	It("should propagate Query error", func(ctx context.Context) {
		fake.QueryReturns(nil, fmt.Errorf("query too expensive"))
		tool := bigquery.NewQueryTool(fake)
		_, err := tool.Call(ctx, []byte(`{"sql":"SELECT *"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("expensive"))
	})

	It("should propagate ListDatasets error", func(ctx context.Context) {
		fake.ListDatasetsReturns(nil, fmt.Errorf("permission denied"))
		tool := bigquery.NewListDatasetsTool(fake)
		_, err := tool.Call(ctx, []byte(`{}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate DescribeTable error", func(ctx context.Context) {
		fake.DescribeTableReturns(nil, fmt.Errorf("table not found"))
		tool := bigquery.NewDescribeTableTool(fake)
		_, err := tool.Call(ctx, []byte(`{"dataset_id":"x","table_id":"y"}`))
		Expect(err).To(HaveOccurred())
	})
})
