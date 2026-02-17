package runbook

import (
	"context"
	"os"
	"path/filepath"

	"github.com/appcd-dev/genie/pkg/memory/vector/vectorfakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Loader", func() {
	var (
		fakeVS *vectorfakes.FakeIStore
		ctx    context.Context
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		tmpDir = GinkgoT().TempDir()
		fakeVS = &vectorfakes.FakeIStore{}
	})

	Describe("LoadFiles", func() {
		It("should return nil when no runbook paths are configured and convention dir is absent", func() {
			loader := NewLoader(tmpDir, Config{}, fakeVS)
			result := loader.LoadFiles(ctx)
			Expect(result).To(BeEmpty())
		})

		It("should load a single markdown runbook from the convention directory", func() {
			runbookDir := filepath.Join(tmpDir, ".genie", "runbooks")
			Expect(os.MkdirAll(runbookDir, 0755)).To(Succeed())

			content := "# Deployment Guide\n\nAlways run tests before deploying."
			Expect(os.WriteFile(filepath.Join(runbookDir, "deploy.md"), []byte(content), 0644)).To(Succeed())

			loader := NewLoader(tmpDir, Config{}, fakeVS)
			result := loader.LoadFiles(ctx)

			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("deploy.md"))
			Expect(result[0].Content).To(ContainSubstring("Deployment Guide"))
			Expect(result[0].Content).To(ContainSubstring("Always run tests before deploying."))
		})

		It("should load multiple runbooks from convention directory", func() {
			runbookDir := filepath.Join(tmpDir, ".genie", "runbooks")
			Expect(os.MkdirAll(runbookDir, 0755)).To(Succeed())

			Expect(os.WriteFile(filepath.Join(runbookDir, "deploy.md"), []byte("# Deploy\nStep 1"), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(runbookDir, "runbook.txt"), []byte("Always check logs"), 0644)).To(Succeed())

			loader := NewLoader(tmpDir, Config{}, nil)
			result := loader.LoadFiles(ctx)

			Expect(result).To(HaveLen(2))
			names := []string{result[0].Name, result[1].Name}
			Expect(names).To(ContainElements("deploy.md", "runbook.txt"))
		})

		It("should load runbooks from explicit config paths", func() {
			customDir := filepath.Join(tmpDir, "custom-runbooks")
			Expect(os.MkdirAll(customDir, 0755)).To(Succeed())

			Expect(os.WriteFile(filepath.Join(customDir, "guide.md"), []byte("# Guide\nDo this."), 0644)).To(Succeed())

			loader := NewLoader(tmpDir, Config{
				Paths: []string{customDir},
			}, nil)
			result := loader.LoadFiles(ctx)

			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("guide.md"))
			Expect(result[0].Content).To(ContainSubstring("Do this."))
		})

		It("should load a single file from config paths", func() {
			singleFile := filepath.Join(tmpDir, "single.md")
			Expect(os.WriteFile(singleFile, []byte("# Single\nJust one file."), 0644)).To(Succeed())

			loader := NewLoader(tmpDir, Config{
				Paths: []string{singleFile},
			}, nil)
			result := loader.LoadFiles(ctx)

			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("single.md"))
			Expect(result[0].Content).To(ContainSubstring("Just one file."))
		})

		It("should resolve relative paths from working directory", func() {
			relDir := filepath.Join(tmpDir, "docs", "runbooks")
			Expect(os.MkdirAll(relDir, 0755)).To(Succeed())

			Expect(os.WriteFile(filepath.Join(relDir, "ops.md"), []byte("# Ops\nCheck K8s pods."), 0644)).To(Succeed())

			loader := NewLoader(tmpDir, Config{
				Paths: []string{"docs/runbooks"},
			}, nil)
			result := loader.LoadFiles(ctx)

			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("ops.md"))
			Expect(result[0].Content).To(ContainSubstring("Check K8s pods."))
		})

		It("should handle large files gracefully", func() {
			runbookDir := filepath.Join(tmpDir, ".genie", "runbooks")
			Expect(os.MkdirAll(runbookDir, 0755)).To(Succeed())

			largeContent := make([]byte, 200)
			for i := range largeContent {
				largeContent[i] = 'A'
			}
			Expect(os.WriteFile(filepath.Join(runbookDir, "large.txt"), largeContent, 0644)).To(Succeed())

			loader := NewLoader(tmpDir, Config{}, nil)
			result := loader.LoadFiles(ctx)

			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("large.txt"))
		})

		It("should skip unsupported file extensions silently", func() {
			runbookDir := filepath.Join(tmpDir, ".genie", "runbooks")
			Expect(os.MkdirAll(runbookDir, 0755)).To(Succeed())

			Expect(os.WriteFile(filepath.Join(runbookDir, "valid.md"), []byte("# Valid"), 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(runbookDir, "image.png"), []byte{0x89, 0x50, 0x4E, 0x47}, 0644)).To(Succeed())
			Expect(os.WriteFile(filepath.Join(runbookDir, "binary.exe"), []byte{0x00}, 0644)).To(Succeed())

			loader := NewLoader(tmpDir, Config{}, nil)
			result := loader.LoadFiles(ctx)

			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("valid.md"))
		})

		It("should deduplicate files found in both convention dir and config", func() {
			runbookDir := filepath.Join(tmpDir, ".genie", "runbooks")
			Expect(os.MkdirAll(runbookDir, 0755)).To(Succeed())

			Expect(os.WriteFile(filepath.Join(runbookDir, "shared.md"), []byte("# Shared"), 0644)).To(Succeed())

			loader := NewLoader(tmpDir, Config{
				Paths: []string{runbookDir},
			}, nil)
			result := loader.LoadFiles(ctx)

			// "shared.md" should appear exactly once.
			Expect(result).To(HaveLen(1))
			Expect(result[0].Name).To(Equal("shared.md"))
		})

		It("should gracefully handle missing config paths", func() {
			loader := NewLoader(tmpDir, Config{
				Paths: []string{"/nonexistent/path"},
			}, nil)
			result := loader.LoadFiles(ctx)
			Expect(result).To(BeEmpty())
		})

		It("should return nil for empty convention directory", func() {
			runbookDir := filepath.Join(tmpDir, ".genie", "runbooks")
			Expect(os.MkdirAll(runbookDir, 0755)).To(Succeed())

			loader := NewLoader(tmpDir, Config{}, nil)
			result := loader.LoadFiles(ctx)
			Expect(result).To(BeEmpty())
		})
	})

	Describe("RunbookID", func() {
		It("should produce stable IDs using relative paths", func() {
			Expect(RunbookID("/project", "/project/deploy.md")).To(Equal("runbook:deploy.md"))
			Expect(RunbookID("/project", "/project/ops/guide.txt")).To(Equal("runbook:ops/guide.txt"))
		})

		It("should prevent collisions for same-named files in different dirs", func() {
			id1 := RunbookID("/project", "/project/ops/deploy.md")
			id2 := RunbookID("/project", "/project/dev/deploy.md")
			Expect(id1).NotTo(Equal(id2))
		})
	})
})
