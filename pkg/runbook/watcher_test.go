package runbook_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/appcd-dev/genie/pkg/memory/vector/vectorfakes"
	"github.com/appcd-dev/genie/pkg/runbook"
)

var _ = Describe("Watcher", func() {
	var (
		ctx       context.Context
		cancel    context.CancelFunc
		tmpDir    string
		fakeStore *vectorfakes.FakeIStore
		loader    *runbook.Loader
	)

	BeforeEach(func() {
		ctx, cancel = context.WithCancel(context.Background())
		tmpDir = GinkgoT().TempDir()
		fakeStore = &vectorfakes.FakeIStore{}
		loader = runbook.NewLoader(tmpDir, runbook.Config{}, fakeStore)
	})

	AfterEach(func() {
		cancel()
	})

	It("should ingest a new runbook file when created", func() {
		runbookDir := filepath.Join(tmpDir, ".genie", "runbooks")
		Expect(os.MkdirAll(runbookDir, 0755)).To(Succeed())

		watcher, err := runbook.NewWatcher(loader, fakeStore, []string{runbookDir})
		Expect(err).NotTo(HaveOccurred())
		defer watcher.Close()

		go watcher.Start(ctx)

		// Create a new runbook file — should trigger ingestion.
		Expect(os.WriteFile(
			filepath.Join(runbookDir, "deploy.md"),
			[]byte("# Deploy\nStep 1: run tests"),
			0644,
		)).To(Succeed())

		// Wait for the watcher to pick up the event and call Add.
		Eventually(fakeStore.AddCallCount, 2*time.Second, 50*time.Millisecond).
			Should(BeNumerically(">=", 1))

		// Verify that the document was added with correct metadata.
		_, items := fakeStore.AddArgsForCall(fakeStore.AddCallCount() - 1)
		Expect(items).NotTo(BeEmpty())
		lastItem := items[len(items)-1]
		Expect(lastItem.ID).To(HavePrefix("runbook:"))
		Expect(lastItem.Text).To(ContainSubstring("Deploy"))
		Expect(lastItem.Metadata["type"]).To(Equal("runbook"))
		Expect(lastItem.Metadata["source"]).To(Equal("deploy.md"))
	})

	It("should re-ingest a runbook file when modified", func() {
		runbookDir := filepath.Join(tmpDir, ".genie", "runbooks")
		Expect(os.MkdirAll(runbookDir, 0755)).To(Succeed())

		filePath := filepath.Join(runbookDir, "ops.md")
		Expect(os.WriteFile(filePath, []byte("# Ops v1"), 0644)).To(Succeed())

		watcher, err := runbook.NewWatcher(loader, fakeStore, []string{runbookDir})
		Expect(err).NotTo(HaveOccurred())
		defer watcher.Close()

		go watcher.Start(ctx)

		// Modify the file.
		time.Sleep(100 * time.Millisecond) // let watcher settle
		Expect(os.WriteFile(filePath, []byte("# Ops v2 updated"), 0644)).To(Succeed())

		Eventually(fakeStore.AddCallCount, 2*time.Second, 50*time.Millisecond).
			Should(BeNumerically(">=", 1))

		_, items := fakeStore.AddArgsForCall(fakeStore.AddCallCount() - 1)
		Expect(items).NotTo(BeEmpty())
		Expect(items[len(items)-1].Text).To(ContainSubstring("Ops v2 updated"))
	})

	It("should remove a runbook from the store when file is deleted", func() {
		runbookDir := filepath.Join(tmpDir, ".genie", "runbooks")
		Expect(os.MkdirAll(runbookDir, 0755)).To(Succeed())

		filePath := filepath.Join(runbookDir, "obsolete.md")
		Expect(os.WriteFile(filePath, []byte("# Old runbook"), 0644)).To(Succeed())

		watcher, err := runbook.NewWatcher(loader, fakeStore, []string{runbookDir})
		Expect(err).NotTo(HaveOccurred())
		defer watcher.Close()

		go watcher.Start(ctx)

		// Delete the file.
		time.Sleep(100 * time.Millisecond) // let watcher settle
		Expect(os.Remove(filePath)).To(Succeed())

		Eventually(fakeStore.DeleteCallCount, 2*time.Second, 50*time.Millisecond).
			Should(BeNumerically(">=", 1))

		_, deletedIDs := fakeStore.DeleteArgsForCall(fakeStore.DeleteCallCount() - 1)
		Expect(deletedIDs).To(ContainElement(HavePrefix("runbook:")))
	})

	It("should ignore unsupported file types", func() {
		runbookDir := filepath.Join(tmpDir, ".genie", "runbooks")
		Expect(os.MkdirAll(runbookDir, 0755)).To(Succeed())

		watcher, err := runbook.NewWatcher(loader, fakeStore, []string{runbookDir})
		Expect(err).NotTo(HaveOccurred())
		defer watcher.Close()

		go watcher.Start(ctx)

		// Create an unsupported file.
		Expect(os.WriteFile(
			filepath.Join(runbookDir, "image.png"),
			[]byte{0x89, 0x50, 0x4E, 0x47},
			0644,
		)).To(Succeed())

		// Wait a bit and confirm no Add calls were made.
		Consistently(fakeStore.AddCallCount, 500*time.Millisecond, 50*time.Millisecond).
			Should(Equal(0))
	})

	It("should auto-watch new subdirectories", func() {
		runbookDir := filepath.Join(tmpDir, ".genie", "runbooks")
		Expect(os.MkdirAll(runbookDir, 0755)).To(Succeed())

		watcher, err := runbook.NewWatcher(loader, fakeStore, []string{runbookDir})
		Expect(err).NotTo(HaveOccurred())
		defer watcher.Close()

		go watcher.Start(ctx)

		// Create a new subdirectory and then a file in it.
		subDir := filepath.Join(runbookDir, "k8s")
		Expect(os.MkdirAll(subDir, 0755)).To(Succeed())

		// Give the watcher time to pick up the new directory.
		time.Sleep(200 * time.Millisecond)

		Expect(os.WriteFile(
			filepath.Join(subDir, "troubleshoot.md"),
			[]byte("# K8s Troubleshooting\nCheck pod logs"),
			0644,
		)).To(Succeed())

		Eventually(fakeStore.AddCallCount, 2*time.Second, 50*time.Millisecond).
			Should(BeNumerically(">=", 1))

		_, items := fakeStore.AddArgsForCall(fakeStore.AddCallCount() - 1)
		Expect(items).NotTo(BeEmpty())
		lastItem := items[len(items)-1]
		Expect(lastItem.ID).To(HaveSuffix("troubleshoot.md"))
		Expect(lastItem.Text).To(ContainSubstring("K8s Troubleshooting"))
	})

	It("should stop when context is cancelled", func() {
		runbookDir := filepath.Join(tmpDir, ".genie", "runbooks")
		Expect(os.MkdirAll(runbookDir, 0755)).To(Succeed())

		watcher, err := runbook.NewWatcher(loader, fakeStore, []string{runbookDir})
		Expect(err).NotTo(HaveOccurred())
		defer watcher.Close()

		done := make(chan struct{})
		go func() {
			defer close(done)
			watcher.Start(ctx)
		}()

		cancel()

		Eventually(done, 2*time.Second).Should(BeClosed())
	})
})
