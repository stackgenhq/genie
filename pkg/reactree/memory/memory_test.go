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

			// Poll until the async store is visible rather than sleeping a fixed duration.
			Eventually(func(g Gomega) {
				results := ep.Retrieve(ctx, "deploy application", 5)
				g.Expect(results).To(HaveLen(1))
				g.Expect(results[0].Goal).To(Equal("deploy application"))
				g.Expect(results[0].Trajectory).To(Equal("kubectl apply -f deployment.yaml"))
				g.Expect(results[0].Status).To(Equal(reactreeMemory.EpisodeSuccess))
			}).WithTimeout(1 * time.Second).WithPolling(10 * time.Millisecond).Should(Succeed())
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

	Describe("DefaultEpisodicMemoryConfig", func() {
		It("should create a config with sensible defaults", func() {
			cfg := reactreeMemory.DefaultEpisodicMemoryConfig()
			Expect(cfg.AppName).To(Equal("reactree"))
			Expect(cfg.UserID).To(Equal("default"))
			Expect(cfg.Service).NotTo(BeNil())
		})

		It("should create a working episodic memory from default config", func(ctx context.Context) {
			cfg := reactreeMemory.DefaultEpisodicMemoryConfig()
			ep := cfg.NewEpisodicMemory()
			Expect(ep).NotTo(BeNil())

			// Store and retrieve with default config
			ep.Store(ctx, reactreeMemory.Episode{
				Goal:       "test goal",
				Trajectory: "test trajectory",
				Status:     reactreeMemory.EpisodeSuccess,
			})
			time.Sleep(10 * time.Millisecond)

			results := ep.Retrieve(ctx, "test goal", 5)
			Expect(results).To(HaveLen(1))
			Expect(results[0].Goal).To(Equal("test goal"))
		})
	})

	Describe("NoOpEpisodicMemory", func() {
		It("should return a non-nil implementation", func() {
			noop := reactreeMemory.NewNoOpEpisodicMemory()
			Expect(noop).NotTo(BeNil())
		})

		It("should be safe to call Store without side effects", func(ctx context.Context) {
			noop := reactreeMemory.NewNoOpEpisodicMemory()
			// Should not panic
			noop.Store(ctx, reactreeMemory.Episode{
				Goal:       "test",
				Trajectory: "trajectory",
				Status:     reactreeMemory.EpisodeSuccess,
			})
		})

		It("should always return nil from Retrieve", func(ctx context.Context) {
			noop := reactreeMemory.NewNoOpEpisodicMemory()
			results := noop.Retrieve(ctx, "any goal", 10)
			Expect(results).To(BeNil())
		})
	})

	Describe("StatusFromTopics via Retrieve fallback", func() {
		It("should extract status from topics when JSON is invalid", func(ctx context.Context) {
			svc := inmemory.NewMemoryService()
			defer svc.Close()

			userKey := memory.UserKey{AppName: "test-app", UserID: "test-user"}

			// Add a raw (non-JSON) memory entry with a status topic
			_ = svc.AddMemory(ctx, userKey, "raw non-json trajectory",
				[]string{"reactree:episode:failure"})

			time.Sleep(10 * time.Millisecond)

			ep := reactreeMemory.EpisodicMemoryConfig{
				Service: svc,
				AppName: "test-app",
				UserID:  "test-user",
			}.NewEpisodicMemory()

			results := ep.Retrieve(ctx, "raw non-json trajectory", 5)
			Expect(results).NotTo(BeEmpty())
			Expect(results[0].Status).To(Equal(reactreeMemory.EpisodeFailure))
			Expect(results[0].Trajectory).To(Equal("raw non-json trajectory"))
		})

		It("should default to success when no status topic is present", func(ctx context.Context) {
			svc := inmemory.NewMemoryService()
			defer svc.Close()

			userKey := memory.UserKey{AppName: "test-app2", UserID: "test-user2"}

			// Add memory with no status topic
			_ = svc.AddMemory(ctx, userKey, "plain text memory", []string{})

			time.Sleep(10 * time.Millisecond)

			ep := reactreeMemory.EpisodicMemoryConfig{
				Service: svc,
				AppName: "test-app2",
				UserID:  "test-user2",
			}.NewEpisodicMemory()

			results := ep.Retrieve(ctx, "plain text memory", 5)
			Expect(results).NotTo(BeEmpty())
			Expect(results[0].Status).To(Equal(reactreeMemory.EpisodeSuccess))
		})

		It("should handle expand status from topics", func(ctx context.Context) {
			svc := inmemory.NewMemoryService()
			defer svc.Close()

			userKey := memory.UserKey{AppName: "test-app3", UserID: "test-user3"}

			_ = svc.AddMemory(ctx, userKey, "not json",
				[]string{"reactree:episode:expand"})

			time.Sleep(10 * time.Millisecond)

			ep := reactreeMemory.EpisodicMemoryConfig{
				Service: svc,
				AppName: "test-app3",
				UserID:  "test-user3",
			}.NewEpisodicMemory()

			results := ep.Retrieve(ctx, "not json", 5)
			Expect(results).NotTo(BeEmpty())
			Expect(results[0].Status).To(Equal(reactreeMemory.EpisodeExpand))
		})
	})
})
