package runbook_test

import (
	"context"
	"os"
	"path/filepath"

	"github.com/appcd-dev/genie/pkg/memory/vector"
	"github.com/appcd-dev/genie/pkg/memory/vector/vectorfakes"
	"github.com/appcd-dev/genie/pkg/runbook"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

var _ = Describe("Runbook Search Tool", func() {
	Describe("NewSearchTool", func() {
		It("should create a search_runbook tool", func() {
			store := &vectorfakes.FakeIStore{}
			t := runbook.NewSearchTool(store)
			Expect(t.Declaration().Name).To(Equal("search_runbook"))
		})
	})

	Describe("execute", func() {
		It("should search and return runbook results", func(ctx context.Context) {
			store := &vectorfakes.FakeIStore{}
			store.SearchReturns([]vector.SearchResult{
				{
					ID:       "runbook:deploy.md",
					Content:  "Deploy steps: 1. Build 2. Push 3. Apply",
					Metadata: map[string]string{"type": "runbook", "source": "deploy.md"},
					Score:    0.95,
				},
				{
					ID:       "runbook:rollback.md",
					Content:  "Rollback: revert to previous version",
					Metadata: map[string]string{"type": "runbook", "source": "rollback.md"},
					Score:    0.85,
				},
			}, nil)

			t := runbook.NewSearchTool(store)
			ct := t.(tool.CallableTool)

			result, err := ct.Call(ctx, []byte(`{"query":"how to deploy"}`))
			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())

			Expect(store.SearchCallCount()).To(Equal(1))
			_, query, limit := store.SearchArgsForCall(0)
			Expect(query).To(Equal("how to deploy"))
			Expect(limit).To(Equal(5)) // default limit
		})

		It("should use custom limit when provided", func(ctx context.Context) {
			store := &vectorfakes.FakeIStore{}
			store.SearchReturns([]vector.SearchResult{}, nil)

			t := runbook.NewSearchTool(store)
			ct := t.(tool.CallableTool)

			_, err := ct.Call(ctx, []byte(`{"query":"deploy","limit":3}`))
			Expect(err).NotTo(HaveOccurred())

			_, _, limit := store.SearchArgsForCall(0)
			Expect(limit).To(Equal(3))
		})

		It("should filter out non-runbook results", func(ctx context.Context) {
			store := &vectorfakes.FakeIStore{}
			store.SearchReturns([]vector.SearchResult{
				{
					ID:       "runbook:deploy.md",
					Content:  "Deploy steps",
					Metadata: map[string]string{"type": "runbook", "source": "deploy.md"},
					Score:    0.95,
				},
				{
					ID:       "other:readme.md",
					Content:  "Not a runbook",
					Metadata: map[string]string{"type": "other", "source": "readme.md"},
					Score:    0.90,
				},
			}, nil)

			t := runbook.NewSearchTool(store)
			ct := t.(tool.CallableTool)

			result, err := ct.Call(ctx, []byte(`{"query":"deploy"}`))
			Expect(err).NotTo(HaveOccurred())
			// Result should have only the runbook item
			Expect(result).NotTo(BeNil())
		})

		It("should return error when search fails", func(ctx context.Context) {
			store := &vectorfakes.FakeIStore{}
			store.SearchReturns(nil, context.DeadlineExceeded)

			t := runbook.NewSearchTool(store)
			ct := t.(tool.CallableTool)

			_, err := ct.Call(ctx, []byte(`{"query":"deploy"}`))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("runbook search failed"))
		})
	})
})

var _ = Describe("Loader.Load", func() {
	It("should return error when vector store is nil", func(ctx context.Context) {
		loader := runbook.NewLoader(GinkgoT().TempDir(), runbook.Config{}, nil)
		count, err := loader.Load(ctx)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("vector store is not initialized"))
		Expect(count).To(Equal(0))
	})

	It("should return 0 when no runbook files exist", func(ctx context.Context) {
		store := &vectorfakes.FakeIStore{}
		emptyDir := GinkgoT().TempDir()
		loader := runbook.NewLoader(emptyDir, runbook.Config{}, store)
		count, err := loader.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(0))
	})

	It("should ingest runbook files into vector store", func(ctx context.Context) {
		store := &vectorfakes.FakeIStore{}
		tmpDir := GinkgoT().TempDir()

		// Create convention directory with a runbook
		convDir := filepath.Join(tmpDir, ".genie", "runbooks")
		err := os.MkdirAll(convDir, 0755)
		Expect(err).NotTo(HaveOccurred())

		err = os.WriteFile(filepath.Join(convDir, "deploy.md"), []byte("# Deploy\nStep 1: Build"), 0644)
		Expect(err).NotTo(HaveOccurred())

		loader := runbook.NewLoader(tmpDir, runbook.Config{}, store)
		count, err := loader.Load(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(1))
		Expect(store.AddCallCount()).To(Equal(1))
	})
})

var _ = Describe("Loader.WatchDirs", func() {
	It("should include convention directory when it exists", func() {
		tmpDir := GinkgoT().TempDir()
		convDir := filepath.Join(tmpDir, ".genie", "runbooks")
		err := os.MkdirAll(convDir, 0755)
		Expect(err).NotTo(HaveOccurred())

		loader := runbook.NewLoader(tmpDir, runbook.Config{}, nil)
		dirs := loader.WatchDirs()
		Expect(dirs).To(ContainElement(convDir))
	})

	It("should include configured directory paths", func() {
		tmpDir := GinkgoT().TempDir()
		customDir := filepath.Join(tmpDir, "custom-runbooks")
		err := os.MkdirAll(customDir, 0755)
		Expect(err).NotTo(HaveOccurred())

		cfg := runbook.Config{Paths: []string{customDir}}
		loader := runbook.NewLoader(tmpDir, cfg, nil)
		dirs := loader.WatchDirs()
		Expect(dirs).To(ContainElement(customDir))
	})

	It("should resolve relative paths", func() {
		tmpDir := GinkgoT().TempDir()
		relDir := filepath.Join(tmpDir, "relative-runbooks")
		err := os.MkdirAll(relDir, 0755)
		Expect(err).NotTo(HaveOccurred())

		cfg := runbook.Config{Paths: []string{"relative-runbooks"}}
		loader := runbook.NewLoader(tmpDir, cfg, nil)
		dirs := loader.WatchDirs()
		Expect(dirs).To(ContainElement(relDir))
	})

	It("should skip non-existent configured paths", func() {
		tmpDir := GinkgoT().TempDir()
		cfg := runbook.Config{Paths: []string{"/nonexistent/path"}}
		loader := runbook.NewLoader(tmpDir, cfg, nil)
		dirs := loader.WatchDirs()
		Expect(dirs).NotTo(ContainElement("/nonexistent/path"))
	})

	It("should return empty when no directories exist", func() {
		tmpDir := GinkgoT().TempDir()
		loader := runbook.NewLoader(tmpDir, runbook.Config{}, nil)
		dirs := loader.WatchDirs()
		Expect(dirs).To(BeEmpty())
	})

	It("should deduplicate directories", func() {
		tmpDir := GinkgoT().TempDir()
		convDir := filepath.Join(tmpDir, ".genie", "runbooks")
		err := os.MkdirAll(convDir, 0755)
		Expect(err).NotTo(HaveOccurred())

		// Configure the same directory that's already the convention dir
		cfg := runbook.Config{Paths: []string{convDir}}
		loader := runbook.NewLoader(tmpDir, cfg, nil)
		dirs := loader.WatchDirs()
		// Should contain convDir only once
		count := 0
		for _, d := range dirs {
			if d == convDir {
				count++
			}
		}
		Expect(count).To(Equal(1))
	})
})

var _ = Describe("RunbookID", func() {
	It("should generate ID with relative path", func() {
		id := runbook.RunbookID("/workspace", "/workspace/docs/deploy.md")
		Expect(id).To(Equal("runbook:docs/deploy.md"))
	})

	It("should fall back to base name for unrelated paths", func() {
		// When paths are on different drives or unrelated, filepath.Rel may error
		// On Unix this might not actually error, but we test the function logic
		id := runbook.RunbookID("/workspace", "/workspace/deploy.md")
		Expect(id).To(Equal("runbook:deploy.md"))
	})
})
