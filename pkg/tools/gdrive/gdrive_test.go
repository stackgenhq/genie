package gdrive_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/stackgenhq/genie/pkg/tools/gdrive"
	"github.com/stackgenhq/genie/pkg/tools/gdrive/gdrivefakes"
)

var _ = Describe("Google Drive Tools", func() {
	var fake *gdrivefakes.FakeService

	BeforeEach(func() {
		fake = new(gdrivefakes.FakeService)
	})

	Describe("gdrive_search", func() {
		It("should return matching files", func(ctx context.Context) {
			fake.SearchReturns([]gdrive.FileInfo{
				{ID: "f1", Name: "Q3 Report.pdf", MimeType: "application/pdf"},
				{ID: "f2", Name: "Architecture", MimeType: "application/vnd.google-apps.document", IsFolder: false},
			}, nil)

			tool := gdrive.NewSearchTool(fake)
			reqJSON, _ := json.Marshal(struct {
				Query      string `json:"query"`
				MaxResults int    `json:"max_results"`
			}{Query: "name contains 'report'", MaxResults: 10})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			files, ok := resp.([]gdrive.FileInfo)
			Expect(ok).To(BeTrue())
			Expect(files).To(HaveLen(2))
			Expect(files[0].Name).To(Equal("Q3 Report.pdf"))
		})

		It("should default max_results to 20", func(ctx context.Context) {
			fake.SearchReturns(nil, nil)

			tool := gdrive.NewSearchTool(fake)
			_, _ = tool.Call(ctx, []byte(`{"query":"test"}`))

			_, _, maxResults := fake.SearchArgsForCall(0)
			Expect(maxResults).To(Equal(20))
		})
	})

	Describe("gdrive_list_folder", func() {
		It("should return folder contents", func(ctx context.Context) {
			fake.ListFolderReturns([]gdrive.FileInfo{
				{ID: "d1", Name: "Subfolder", IsFolder: true},
				{ID: "f3", Name: "notes.txt", MimeType: "text/plain"},
			}, nil)

			tool := gdrive.NewListFolderTool(fake)
			reqJSON, _ := json.Marshal(struct {
				FolderID string `json:"folder_id"`
			}{FolderID: "root"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			files, ok := resp.([]gdrive.FileInfo)
			Expect(ok).To(BeTrue())
			Expect(files[0].IsFolder).To(BeTrue())
		})
	})

	Describe("gdrive_get_file", func() {
		It("should return file detail", func(ctx context.Context) {
			fake.GetFileReturns(&gdrive.FileDetail{
				ID: "f1", Name: "Report.pdf", MimeType: "application/pdf",
				Owners: []string{"Alice Smith"}, WebViewLink: "https://drive.google.com/...",
			}, nil)

			tool := gdrive.NewGetFileTool(fake)
			reqJSON, _ := json.Marshal(struct {
				FileID string `json:"file_id"`
			}{FileID: "f1"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())

			detail, ok := resp.(*gdrive.FileDetail)
			Expect(ok).To(BeTrue())
			Expect(detail.Owners).To(ContainElement("Alice Smith"))
		})
	})

	Describe("gdrive_read_file", func() {
		It("should return file content", func(ctx context.Context) {
			fake.ReadFileReturns("Hello, this is a test document.", nil)

			tool := gdrive.NewReadFileTool(fake)
			reqJSON, _ := json.Marshal(struct {
				FileID string `json:"file_id"`
			}{FileID: "f1"})

			resp, err := tool.Call(ctx, reqJSON)
			Expect(err).NotTo(HaveOccurred())
			Expect(resp).NotTo(BeNil())
		})

		It("should truncate very large files", func(ctx context.Context) {
			// Return a 150KB string
			largeContent := strings.Repeat("x", 150000)
			fake.ReadFileReturns(largeContent, nil)

			tool := gdrive.NewReadFileTool(fake)
			resp, err := tool.Call(ctx, []byte(`{"file_id":"big"}`))
			Expect(err).NotTo(HaveOccurred())

			// The response object should contain truncated content
			respBytes, _ := json.Marshal(resp)
			Expect(string(respBytes)).To(ContainSubstring("truncated"))
		})
	})
})

var _ = Describe("AllTools", func() {
	It("should return 4 tools", func() {
		fake := new(gdrivefakes.FakeService)
		tools := gdrive.AllTools(fake)
		Expect(tools).To(HaveLen(4))
	})
})

var _ = Describe("Google Drive Error Paths", func() {
	var fake *gdrivefakes.FakeService

	BeforeEach(func() {
		fake = new(gdrivefakes.FakeService)
	})

	It("should propagate Search error", func(ctx context.Context) {
		fake.SearchReturns(nil, fmt.Errorf("forbidden"))
		tool := gdrive.NewSearchTool(fake)
		_, err := tool.Call(ctx, []byte(`{"query":"x"}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate ReadFile error", func(ctx context.Context) {
		fake.ReadFileReturns("", fmt.Errorf("binary file"))
		tool := gdrive.NewReadFileTool(fake)
		_, err := tool.Call(ctx, []byte(`{"file_id":"bin"}`))
		Expect(err).To(HaveOccurred())
	})

	It("should propagate ListFolder error", func(ctx context.Context) {
		fake.ListFolderReturns(nil, fmt.Errorf("not found"))
		tool := gdrive.NewListFolderTool(fake)
		_, err := tool.Call(ctx, []byte(`{"folder_id":"BAD"}`))
		Expect(err).To(HaveOccurred())
	})
})
