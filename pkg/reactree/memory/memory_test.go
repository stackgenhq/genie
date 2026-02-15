package memory_test

import (
	"context"
	"time"

	reactreeMemory "github.com/appcd-dev/genie/pkg/reactree/memory"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
)

var _ = Describe("Memory", func() {

	Describe("WorkingMemory", func() {
		var wm *reactreeMemory.WorkingMemory

		BeforeEach(func() {
			wm = reactreeMemory.NewWorkingMemory()
		})

		It("should store and recall values", func() {
			wm.Store("file:main.tf", "resource aws_s3_bucket ...")
			val, ok := wm.Recall("file:main.tf")
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal("resource aws_s3_bucket ..."))
		})

		It("should return false for unknown keys", func() {
			_, ok := wm.Recall("nonexistent")
			Expect(ok).To(BeFalse())
		})

		It("should overwrite existing keys", func() {
			wm.Store("location", "kitchen")
			wm.Store("location", "bedroom")

			val, ok := wm.Recall("location")
			Expect(ok).To(BeTrue())
			Expect(val).To(Equal("bedroom"))
		})

		It("should list all keys", func() {
			wm.Store("a", "1")
			wm.Store("b", "2")
			wm.Store("c", "3")

			keys := wm.Keys()
			Expect(keys).To(HaveLen(3))
			Expect(keys).To(ContainElements("a", "b", "c"))
		})

		It("should clear all entries", func() {
			wm.Store("a", "1")
			wm.Store("b", "2")
			wm.Clear()

			keys := wm.Keys()
			Expect(keys).To(BeEmpty())
		})

		It("should return a snapshot copy", func() {
			wm.Store("x", "100")
			snap := wm.Snapshot()

			Expect(snap).To(HaveKeyWithValue("x", "100"))

			// Modifying snapshot should not affect original
			snap["x"] = "modified"
			val, _ := wm.Recall("x")
			Expect(val).To(Equal("100"))
		})
	})

	Describe("ServiceEpisodicMemory", func() {
		var (
			svc memory.Service
			ep  reactreeMemory.EpisodicMemory
		)

		BeforeEach(func() {
			svc = inmemory.NewMemoryService()

			ep = reactreeMemory.EpisodicMemoryConfig{
				Service: svc,
				AppName: "reactree-test",
				UserID:  "test-user",
			}.NewEpisodicMemory()
		})

		AfterEach(func() {
			if svc != nil {
				_ = svc.Close()
			}
		})

		It("should store and retrieve episodes", func(ctx context.Context) {
			ep.Store(ctx, reactreeMemory.Episode{
				Goal:       "deploy application",
				Trajectory: "kubectl apply -f deployment.yaml",
				Status:     reactreeMemory.EpisodeSuccess,
			})

			// Small delay for async processing if any
			time.Sleep(10 * time.Millisecond)

			results := ep.Retrieve(ctx, "deploy application", 5)
			Expect(results).To(HaveLen(1))
			Expect(results[0].Goal).To(Equal("deploy application"))
			Expect(results[0].Trajectory).To(Equal("kubectl apply -f deployment.yaml"))
			Expect(results[0].Status).To(Equal(reactreeMemory.EpisodeSuccess))
		})

		It("should store multiple episodes", func(ctx context.Context) {
			ep.Store(ctx, reactreeMemory.Episode{
				Goal:       "create database",
				Trajectory: "CREATE TABLE ...",
				Status:     reactreeMemory.EpisodeSuccess,
			})
			ep.Store(ctx, reactreeMemory.Episode{
				Goal:       "run migration",
				Trajectory: "migrate up failed",
				Status:     reactreeMemory.EpisodeFailure,
			})

			time.Sleep(10 * time.Millisecond)

			// ReadMemories to verify both are stored
			entries, err := svc.ReadMemories(
				context.Background(),
				memory.UserKey{AppName: "reactree-test", UserID: "test-user"},
				10,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(HaveLen(2))
		})

		It("should limit results to k", func(ctx context.Context) {
			for i := 0; i < 5; i++ {
				ep.Store(ctx, reactreeMemory.Episode{
					Goal:       "repeated goal",
					Trajectory: "trajectory",
					Status:     reactreeMemory.EpisodeSuccess,
				})
			}

			time.Sleep(10 * time.Millisecond)

			results := ep.Retrieve(ctx, "repeated goal", 2)
			Expect(len(results)).To(BeNumerically("<=", 2))
		})

		It("should return nil for no matches", func(ctx context.Context) {
			results := ep.Retrieve(ctx, "nonexistent goal", 5)
			Expect(results).To(BeNil())
		})
	})

	Describe("FormatEpisodeForPrompt", func() {
		It("should format an episode for LLM prompt inclusion", func() {
			formatted := reactreeMemory.Episode{
				Goal:       "deploy app",
				Trajectory: "kubectl apply -f ...",
				Status:     reactreeMemory.EpisodeSuccess,
			}.String()
			Expect(formatted).To(ContainSubstring("deploy app"))
			Expect(formatted).To(ContainSubstring("success"))
			Expect(formatted).To(ContainSubstring("kubectl apply"))
		})
	})

	Describe("EpisodeStatus constants", func() {
		It("should have expected values", func() {
			Expect(string(reactreeMemory.EpisodeSuccess)).To(Equal("success"))
			Expect(string(reactreeMemory.EpisodeFailure)).To(Equal("failure"))
			Expect(string(reactreeMemory.EpisodeExpand)).To(Equal("expand"))
		})
	})
})
