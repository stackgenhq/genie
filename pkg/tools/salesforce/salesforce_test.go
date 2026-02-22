package salesforce_test

import (
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/tools/salesforce"
	"github.com/appcd-dev/genie/pkg/tools/salesforce/salesforcefakes"
)

var _ = Describe("Salesforce Tools", func() {
	var fake *salesforcefakes.FakeService

	BeforeEach(func() {
		fake = new(salesforcefakes.FakeService)
	})

	Describe("salesforce_query", func() {
		It("should execute SOQL and return results", func(ctx context.Context) {
			fake.QueryReturns(&salesforce.QueryResult{
				TotalSize: 2, Done: true,
				Records: []map[string]interface{}{
					{"Id": "001A", "Name": "Acme"},
					{"Id": "001B", "Name": "Globex"},
				},
			}, nil)

			tool := salesforce.NewQueryTool(fake)
			reqJSON, _ := json.Marshal(struct {
				SOQL string `json:"soql"`
			}{SOQL: "SELECT Id, Name FROM Account LIMIT 10"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			result, ok := resp.(*salesforce.QueryResult)
			Expect(ok).To(BeTrue())
			Expect(result.TotalSize).To(Equal(2))
			Expect(result.Records).To(HaveLen(2))

			_, soql := fake.QueryArgsForCall(0)
			Expect(soql).To(ContainSubstring("SELECT"))
		})
	})

	Describe("salesforce_get_record", func() {
		It("should return record map", func(ctx context.Context) {
			fake.GetRecordReturns(map[string]interface{}{
				"Id": "001A", "Name": "Acme", "Industry": "Technology",
			}, nil)

			tool := salesforce.NewGetRecordTool(fake)
			reqJSON, _ := json.Marshal(struct {
				ObjectType string `json:"object_type"`
				ID         string `json:"id"`
			}{ObjectType: "Account", ID: "001A"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			record, ok := resp.(map[string]interface{})
			Expect(ok).To(BeTrue())
			Expect(record["Name"]).To(Equal("Acme"))
		})
	})

	Describe("salesforce_create_record", func() {
		It("should return success message", func(ctx context.Context) {
			fake.CreateRecordReturns("001NEW", nil)

			tool := salesforce.NewCreateRecordTool(fake)
			reqJSON, _ := json.Marshal(struct {
				ObjectType string                 `json:"object_type"`
				Fields     map[string]interface{} `json:"fields"`
			}{ObjectType: "Account", Fields: map[string]interface{}{"Name": "NewCo"}})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).To(ContainSubstring("001NEW"))
		})
	})

	Describe("salesforce_describe_object", func() {
		It("should return object description", func(ctx context.Context) {
			fake.DescribeObjectReturns(&salesforce.ObjectDescription{
				Name: "Account", Label: "Account",
				Fields: []salesforce.FieldInfo{
					{Name: "Name", Label: "Account Name", Type: "string", Required: true},
				},
			}, nil)

			tool := salesforce.NewDescribeObjectTool(fake)
			reqJSON, _ := json.Marshal(struct {
				ObjectType string `json:"object_type"`
			}{ObjectType: "Account"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			desc, ok := resp.(*salesforce.ObjectDescription)
			Expect(ok).To(BeTrue())
			Expect(desc.Fields[0].Required).To(BeTrue())
		})
	})

	Describe("salesforce_list_objects", func() {
		It("should return object list", func(ctx context.Context) {
			fake.ListObjectsReturns([]salesforce.ObjectInfo{
				{Name: "Account", Label: "Account"},
				{Name: "Contact", Label: "Contact"},
			}, nil)

			tool := salesforce.NewListObjectsTool(fake)

			resp, err := tool.Call(ctx, []byte(`{}`))
			Expect(err).NotTo(HaveOccurred())

			objects, ok := resp.([]salesforce.ObjectInfo)
			Expect(ok).To(BeTrue())
			Expect(objects).To(HaveLen(2))
		})
	})
})

var _ = Describe("AllTools", func() {
	It("should return 6 tools", func() {
		fake := new(salesforcefakes.FakeService)
		tools := salesforce.AllTools(fake)
		Expect(tools).To(HaveLen(6))
	})
})

var _ = Describe("New", func() {
	It("should return error when instance_url is missing", func() {
		_, err := salesforce.New(salesforce.Config{})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("instance_url"))
	})
})

var _ = Describe("Salesforce Error Paths", func() {
	var fake *salesforcefakes.FakeService

	BeforeEach(func() {
		fake = new(salesforcefakes.FakeService)
	})

	It("should propagate Query error", func(ctx context.Context) {
		fake.QueryReturns(nil, fmt.Errorf("invalid SOQL"))
		tool := salesforce.NewQueryTool(fake)
		_, err := tool.Call(ctx, []byte(`{"soql":"bad query"}`))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid SOQL"))
	})

	It("should propagate GetRecord error", func(ctx context.Context) {
		fake.GetRecordReturns(nil, fmt.Errorf("not found"))
		tool := salesforce.NewGetRecordTool(fake)
		_, err := tool.Call(ctx, []byte(`{"object_type":"Account","id":"BAD"}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate CreateRecord error", func(ctx context.Context) {
		fake.CreateRecordReturns("", fmt.Errorf("missing required field"))
		tool := salesforce.NewCreateRecordTool(fake)
		reqJSON, _ := json.Marshal(struct {
			ObjectType string                 `json:"object_type"`
			Fields     map[string]interface{} `json:"fields"`
		}{ObjectType: "Account", Fields: map[string]interface{}{}})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
	})

	It("should propagate UpdateRecord error", func(ctx context.Context) {
		fake.UpdateRecordReturns(fmt.Errorf("locked"))
		tool := salesforce.NewUpdateRecordTool(fake)
		reqJSON, _ := json.Marshal(struct {
			ObjectType string                 `json:"object_type"`
			ID         string                 `json:"id"`
			Fields     map[string]interface{} `json:"fields"`
		}{ObjectType: "Account", ID: "001A", Fields: map[string]interface{}{"Name": "x"}})
		_, err := tool.Call(ctx, reqJSON)
		Expect(err).To(HaveOccurred())
	})
})
