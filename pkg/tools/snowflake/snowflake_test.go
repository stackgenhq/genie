package snowflake_test

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/tools/snowflake"
	"github.com/appcd-dev/genie/pkg/tools/snowflake/snowflakefakes"
)

var _ = Describe("Snowflake Tools", func() {
	var fake *snowflakefakes.FakeService

	BeforeEach(func() {
		fake = new(snowflakefakes.FakeService)
	})

	Describe("snowflake_query", func() {
		It("should execute SQL and return results", func(ctx context.Context) {
			fake.QueryReturns(&snowflake.QueryResult{
				Columns: []string{"ID", "NAME"},
				Rows: [][]interface{}{
					{1, "Alice"},
					{2, "Bob"},
				},
				RowCount: 2,
			}, nil)

			tool := snowflake.NewQueryTool(fake)
			reqJSON, _ := json.Marshal(struct {
				SQL string `json:"sql"`
			}{SQL: "SELECT * FROM users LIMIT 10"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			result, ok := resp.(*snowflake.QueryResult)
			Expect(ok).To(BeTrue())
			Expect(result.Columns).To(HaveLen(2))
			Expect(result.RowCount).To(Equal(2))
		})
	})

	Describe("snowflake_list_databases", func() {
		It("should return database names", func(ctx context.Context) {
			fake.ListDatabasesReturns([]string{"ANALYTICS", "RAW_DATA"}, nil)

			tool := snowflake.NewListDatabasesTool(fake)

			resp, err := tool.Call(ctx, []byte(`{}`))
			Expect(err).NotTo(HaveOccurred())

			dbs, ok := resp.([]string)
			Expect(ok).To(BeTrue())
			Expect(dbs).To(HaveLen(2))
			Expect(dbs[0]).To(Equal("ANALYTICS"))
		})
	})

	Describe("snowflake_list_schemas", func() {
		It("should return schema names", func(ctx context.Context) {
			fake.ListSchemasReturns([]string{"PUBLIC", "STAGING"}, nil)

			tool := snowflake.NewListSchemasTool(fake)
			reqJSON, _ := json.Marshal(struct {
				Database string `json:"database"`
			}{Database: "ANALYTICS"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			schemas, ok := resp.([]string)
			Expect(ok).To(BeTrue())
			Expect(schemas).To(ContainElement("PUBLIC"))

			_, db := fake.ListSchemasArgsForCall(0)
			Expect(db).To(Equal("ANALYTICS"))
		})
	})

	Describe("snowflake_list_tables", func() {
		It("should return tables", func(ctx context.Context) {
			fake.ListTablesReturns([]snowflake.TableInfo{
				{Name: "USERS", Type: "TABLE"},
				{Name: "REVENUE_VIEW", Type: "VIEW"},
			}, nil)

			tool := snowflake.NewListTablesTool(fake)
			reqJSON, _ := json.Marshal(struct {
				Database string `json:"database"`
				Schema   string `json:"schema"`
			}{Database: "ANALYTICS", Schema: "PUBLIC"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			tables, ok := resp.([]snowflake.TableInfo)
			Expect(ok).To(BeTrue())
			Expect(tables).To(HaveLen(2))
			Expect(tables[1].Type).To(Equal("VIEW"))
		})
	})

	Describe("snowflake_describe_table", func() {
		It("should return column info", func(ctx context.Context) {
			fake.DescribeTableReturns([]snowflake.ColumnInfo{
				{Name: "ID", Type: "NUMBER", Nullable: false},
				{Name: "NAME", Type: "VARCHAR", Nullable: true},
			}, nil)

			tool := snowflake.NewDescribeTableTool(fake)
			reqJSON, _ := json.Marshal(struct {
				Database string `json:"database"`
				Schema   string `json:"schema"`
				Table    string `json:"table"`
			}{Database: "ANALYTICS", Schema: "PUBLIC", Table: "USERS"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			cols, ok := resp.([]snowflake.ColumnInfo)
			Expect(ok).To(BeTrue())
			Expect(cols).To(HaveLen(2))
			Expect(cols[0].Nullable).To(BeFalse())
		})
	})
})

var _ = Describe("AllTools", func() {
	It("should return 5 tools", func() {
		fake := new(snowflakefakes.FakeService)
		tools := snowflake.AllTools(fake)
		Expect(tools).To(HaveLen(5))
	})
})

var _ = Describe("New", func() {
	It("should return error when account is missing", func() {
		_, err := snowflake.New(snowflake.Config{User: "u", Password: "p"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("account"))
	})

	It("should return error when user is missing", func() {
		_, err := snowflake.New(snowflake.Config{Account: "acct"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("user"))
	})
})

var _ = Describe("Snowflake Error Paths", func() {
	var fake *snowflakefakes.FakeService

	BeforeEach(func() {
		fake = new(snowflakefakes.FakeService)
	})

	It("should propagate Query error", func(ctx context.Context) {
		fake.QueryReturns(nil, fmt.Errorf("SQL compilation error"))
		tool := snowflake.NewQueryTool(fake)
		_, err := tool.Call(ctx, []byte(`{"sql":"SELECT bad"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("SQL compilation"))
	})

	It("should propagate ListDatabases error", func(ctx context.Context) {
		fake.ListDatabasesReturns(nil, fmt.Errorf("auth expired"))
		tool := snowflake.NewListDatabasesTool(fake)
		_, err := tool.Call(ctx, []byte(`{}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate ListSchemas error", func(ctx context.Context) {
		fake.ListSchemasReturns(nil, fmt.Errorf("database not found"))
		tool := snowflake.NewListSchemasTool(fake)
		_, err := tool.Call(ctx, []byte(`{"database":"BAD"}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate ListTables error", func(ctx context.Context) {
		fake.ListTablesReturns(nil, fmt.Errorf("timeout"))
		tool := snowflake.NewListTablesTool(fake)
		_, err := tool.Call(ctx, []byte(`{"database":"X","schema":"Y"}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate DescribeTable error", func(ctx context.Context) {
		fake.DescribeTableReturns(nil, fmt.Errorf("table not found"))
		tool := snowflake.NewDescribeTableTool(fake)
		_, err := tool.Call(ctx, []byte(`{"database":"X","schema":"Y","table":"Z"}`))
		Expect(err).To(HaveOccurred())
	})
})
