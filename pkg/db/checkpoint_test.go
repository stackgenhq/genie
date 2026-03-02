package db_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	geniedb "github.com/stackgenhq/genie/pkg/db"
	"gorm.io/gorm"
	"trpc.group/trpc-go/trpc-agent-go/graph"
)

func openCheckpointTestDB() *gorm.DB {
	path := GinkgoT().TempDir() + "/test_checkpoint.db"
	db, err := geniedb.Open(path)
	Expect(err).NotTo(HaveOccurred())
	return db
}

func makeCheckpoint(id string, parentID string) *graph.Checkpoint {
	return &graph.Checkpoint{
		Version:            graph.CheckpointVersion,
		ID:                 id,
		Timestamp:          time.Now().UTC(),
		ChannelValues:      map[string]any{"messages": "hello"},
		ChannelVersions:    map[string]int64{"messages": 1},
		ParentCheckpointID: parentID,
	}
}

func makeConfig(lineageID, checkpointID, ns string) map[string]any {
	if lineageID == "" {
		return map[string]any{
			"configurable": map[string]any{
				"thread_id":     lineageID,
				"checkpoint_id": checkpointID,
				"checkpoint_ns": ns,
			},
		}
	}
	return graph.CreateCheckpointConfig(lineageID, checkpointID, ns)
}

var _ = Describe("GormCheckpointSaver", func() {
	var (
		db    *gorm.DB
		saver *geniedb.GormCheckpointSaver
	)

	BeforeEach(func() {
		db = openCheckpointTestDB()
		var err error
		saver, err = geniedb.NewGormCheckpointSaver(db)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		geniedb.Close(db)
	})

	// -------------------------------------------------------------------
	// Constructor
	// -------------------------------------------------------------------

	Describe("NewGormCheckpointSaver", func() {
		It("returns an error when db is nil", func(ctx context.Context) {
			_, err := geniedb.NewGormCheckpointSaver(nil)
			Expect(err).To(MatchError("db is nil"))
		})

		It("succeeds with a valid db", func(ctx context.Context) {
			testDB := openCheckpointTestDB()
			defer geniedb.Close(testDB)
			s, err := geniedb.NewGormCheckpointSaver(testDB)
			Expect(err).NotTo(HaveOccurred())
			Expect(s).NotTo(BeNil())
		})
	})

	// -------------------------------------------------------------------
	// Put + Get
	// -------------------------------------------------------------------

	Describe("Put", func() {
		It("stores a checkpoint and returns the config", func(ctx context.Context) {
			ckpt := makeCheckpoint("ckpt-1", "")
			cfg := makeConfig("lineage-1", "", "ns-1")

			resultCfg, err := saver.Put(ctx, graph.PutRequest{
				Config:     cfg,
				Checkpoint: ckpt,
				Metadata:   &graph.CheckpointMetadata{Source: graph.CheckpointSourceInput, Step: 0},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(graph.GetCheckpointID(resultCfg)).To(Equal("ckpt-1"))
		})

		It("returns an error when checkpoint is nil", func(ctx context.Context) {
			_, err := saver.Put(ctx, graph.PutRequest{
				Config:     makeConfig("lineage-1", "", "ns-1"),
				Checkpoint: nil,
			})
			Expect(err).To(MatchError("checkpoint cannot be nil"))
		})

		It("returns an error when lineage_id is empty", func(ctx context.Context) {
			_, err := saver.Put(ctx, graph.PutRequest{
				Config:     makeConfig("", "", "ns-1"),
				Checkpoint: makeCheckpoint("ckpt-1", ""),
			})
			Expect(err).To(MatchError("lineage_id is required"))
		})

		It("upserts when the same checkpoint ID is stored twice", func(ctx context.Context) {
			cfg := makeConfig("lineage-1", "", "ns-1")
			ckpt := makeCheckpoint("ckpt-1", "")

			_, err := saver.Put(ctx, graph.PutRequest{
				Config: cfg, Checkpoint: ckpt,
				Metadata: &graph.CheckpointMetadata{Source: graph.CheckpointSourceInput, Step: 0},
			})
			Expect(err).NotTo(HaveOccurred())

			ckpt.ChannelValues = map[string]any{"messages": "updated"}
			_, err = saver.Put(ctx, graph.PutRequest{
				Config: cfg, Checkpoint: ckpt,
				Metadata: &graph.CheckpointMetadata{Source: graph.CheckpointSourceUpdate, Step: 1},
			})
			Expect(err).NotTo(HaveOccurred())

			got, err := saver.Get(ctx, makeConfig("lineage-1", "ckpt-1", "ns-1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(got.ChannelValues["messages"]).To(Equal("updated"))
		})

		It("defaults metadata when nil", func(ctx context.Context) {
			cfg := makeConfig("lineage-1", "", "ns-1")
			ckpt := makeCheckpoint("ckpt-meta-nil", "")

			_, err := saver.Put(ctx, graph.PutRequest{
				Config: cfg, Checkpoint: ckpt, Metadata: nil,
			})
			Expect(err).NotTo(HaveOccurred())

			tuple, err := saver.GetTuple(ctx, makeConfig("lineage-1", "ckpt-meta-nil", "ns-1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(tuple).NotTo(BeNil())
			Expect(tuple.Metadata.Source).To(Equal(graph.CheckpointSourceUpdate))
		})
	})

	Describe("Get", func() {
		It("returns nil for a non-existent checkpoint", func(ctx context.Context) {
			got, err := saver.Get(ctx, makeConfig("lineage-1", "nope", "ns-1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil())
		})

		It("returns the stored checkpoint by ID", func(ctx context.Context) {
			ckpt := makeCheckpoint("ckpt-2", "")
			cfg := makeConfig("lineage-1", "", "ns-1")
			_, err := saver.Put(ctx, graph.PutRequest{
				Config: cfg, Checkpoint: ckpt,
			})
			Expect(err).NotTo(HaveOccurred())

			got, err := saver.Get(ctx, makeConfig("lineage-1", "ckpt-2", "ns-1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(got).NotTo(BeNil())
			Expect(got.ID).To(Equal("ckpt-2"))
		})
	})

	// -------------------------------------------------------------------
	// GetTuple
	// -------------------------------------------------------------------

	Describe("GetTuple", func() {
		It("returns an error when lineage_id is empty", func(ctx context.Context) {
			_, err := saver.GetTuple(ctx, makeConfig("", "", ""))
			Expect(err).To(MatchError("lineage_id is required"))
		})

		It("returns the latest checkpoint when checkpoint_id is empty", func(ctx context.Context) {
			cfg := makeConfig("lineage-latest", "", "ns-1")
			for _, id := range []string{"old", "new"} {
				ckpt := makeCheckpoint(id, "")
				ckpt.Timestamp = time.Now().UTC()
				_, err := saver.Put(ctx, graph.PutRequest{
					Config: cfg, Checkpoint: ckpt,
				})
				Expect(err).NotTo(HaveOccurred())
				time.Sleep(2 * time.Millisecond)
			}

			tuple, err := saver.GetTuple(ctx, makeConfig("lineage-latest", "", "ns-1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(tuple).NotTo(BeNil())
			Expect(tuple.Checkpoint.ID).To(Equal("new"))
		})

		It("includes parent config when parent exists", func(ctx context.Context) {
			cfg := makeConfig("lineage-parent", "", "ns-1")
			parent := makeCheckpoint("parent-1", "")
			_, err := saver.Put(ctx, graph.PutRequest{Config: cfg, Checkpoint: parent})
			Expect(err).NotTo(HaveOccurred())

			child := makeCheckpoint("child-1", "parent-1")
			_, err = saver.Put(ctx, graph.PutRequest{Config: cfg, Checkpoint: child})
			Expect(err).NotTo(HaveOccurred())

			tuple, err := saver.GetTuple(ctx, makeConfig("lineage-parent", "child-1", "ns-1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(tuple).NotTo(BeNil())
			Expect(tuple.ParentConfig).NotTo(BeNil())
			Expect(graph.GetCheckpointID(tuple.ParentConfig)).To(Equal("parent-1"))
		})

		It("returns nil parent config when no parent exists", func(ctx context.Context) {
			cfg := makeConfig("lineage-no-parent", "", "ns-1")
			ckpt := makeCheckpoint("orphan-1", "")
			_, err := saver.Put(ctx, graph.PutRequest{Config: cfg, Checkpoint: ckpt})
			Expect(err).NotTo(HaveOccurred())

			tuple, err := saver.GetTuple(ctx, makeConfig("lineage-no-parent", "orphan-1", "ns-1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(tuple.ParentConfig).To(BeNil())
		})
	})

	// -------------------------------------------------------------------
	// PutWrites + loadWrites
	// -------------------------------------------------------------------

	Describe("PutWrites", func() {
		It("stores and retrieves writes via GetTuple", func(ctx context.Context) {
			cfg := makeConfig("lineage-w", "", "ns-1")
			ckpt := makeCheckpoint("ckpt-w", "")
			_, err := saver.Put(ctx, graph.PutRequest{Config: cfg, Checkpoint: ckpt})
			Expect(err).NotTo(HaveOccurred())

			err = saver.PutWrites(ctx, graph.PutWritesRequest{
				Config: makeConfig("lineage-w", "ckpt-w", "ns-1"),
				TaskID: "task-1",
				Writes: []graph.PendingWrite{
					{Channel: "messages", Value: "write-1", Sequence: 1},
					{Channel: "messages", Value: "write-2", Sequence: 2},
				},
			})
			Expect(err).NotTo(HaveOccurred())

			tuple, err := saver.GetTuple(ctx, makeConfig("lineage-w", "ckpt-w", "ns-1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(tuple.PendingWrites).To(HaveLen(2))
			Expect(tuple.PendingWrites[0].Channel).To(Equal("messages"))
			Expect(tuple.PendingWrites[0].Value).To(Equal("write-1"))
		})

		It("returns an error when lineage_id is empty", func(ctx context.Context) {
			err := saver.PutWrites(ctx, graph.PutWritesRequest{
				Config: makeConfig("", "ckpt-1", "ns-1"),
				TaskID: "task-1",
				Writes: []graph.PendingWrite{{Channel: "ch", Value: "v", Sequence: 1}},
			})
			Expect(err).To(MatchError("lineage_id and checkpoint_id are required"))
		})

		It("returns an error when checkpoint_id is empty", func(ctx context.Context) {
			err := saver.PutWrites(ctx, graph.PutWritesRequest{
				Config: makeConfig("lineage-1", "", "ns-1"),
				TaskID: "task-1",
				Writes: []graph.PendingWrite{{Channel: "ch", Value: "v", Sequence: 1}},
			})
			Expect(err).To(MatchError("lineage_id and checkpoint_id are required"))
		})

		It("upserts writes without duplicating", func(ctx context.Context) {
			cfg := makeConfig("lineage-wdup", "", "ns-1")
			ckpt := makeCheckpoint("ckpt-wdup", "")
			_, err := saver.Put(ctx, graph.PutRequest{Config: cfg, Checkpoint: ckpt})
			Expect(err).NotTo(HaveOccurred())

			writeCfg := makeConfig("lineage-wdup", "ckpt-wdup", "ns-1")
			writes := []graph.PendingWrite{{Channel: "ch", Value: "v1", Sequence: 1}}

			Expect(saver.PutWrites(ctx, graph.PutWritesRequest{
				Config: writeCfg, TaskID: "t1", Writes: writes,
			})).To(Succeed())

			writes[0].Value = "v2"
			Expect(saver.PutWrites(ctx, graph.PutWritesRequest{
				Config: writeCfg, TaskID: "t1", Writes: writes,
			})).To(Succeed())

			tuple, err := saver.GetTuple(ctx, writeCfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(tuple.PendingWrites).To(HaveLen(1))
			Expect(tuple.PendingWrites[0].Value).To(Equal("v2"))
		})
	})

	// -------------------------------------------------------------------
	// PutFull
	// -------------------------------------------------------------------

	Describe("PutFull", func() {
		It("atomically stores checkpoint and writes", func(ctx context.Context) {
			cfg := makeConfig("lineage-full", "", "ns-1")
			ckpt := makeCheckpoint("ckpt-full", "")

			resultCfg, err := saver.PutFull(ctx, graph.PutFullRequest{
				Config:     cfg,
				Checkpoint: ckpt,
				Metadata:   &graph.CheckpointMetadata{Source: graph.CheckpointSourceLoop, Step: 1},
				PendingWrites: []graph.PendingWrite{
					{TaskID: "t1", Channel: "messages", Value: "full-write", Sequence: 10},
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(graph.GetCheckpointID(resultCfg)).To(Equal("ckpt-full"))

			tuple, err := saver.GetTuple(ctx, makeConfig("lineage-full", "ckpt-full", "ns-1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(tuple.Checkpoint.ID).To(Equal("ckpt-full"))
			Expect(tuple.Metadata.Source).To(Equal(graph.CheckpointSourceLoop))
			Expect(tuple.PendingWrites).To(HaveLen(1))
			Expect(tuple.PendingWrites[0].Value).To(Equal("full-write"))
		})

		It("returns an error when lineage_id is empty", func(ctx context.Context) {
			_, err := saver.PutFull(ctx, graph.PutFullRequest{
				Config:     makeConfig("", "", "ns-1"),
				Checkpoint: makeCheckpoint("ckpt-1", ""),
			})
			Expect(err).To(MatchError("lineage_id is required"))
		})

		It("returns an error when checkpoint is nil", func(ctx context.Context) {
			_, err := saver.PutFull(ctx, graph.PutFullRequest{
				Config:     makeConfig("lineage-1", "", "ns-1"),
				Checkpoint: nil,
			})
			Expect(err).To(MatchError("checkpoint cannot be nil"))
		})
	})

	// -------------------------------------------------------------------
	// List
	// -------------------------------------------------------------------

	Describe("List", func() {
		It("returns an error when lineage_id is empty", func(ctx context.Context) {
			_, err := saver.List(ctx, makeConfig("", "", "ns-1"), nil)
			Expect(err).To(MatchError("lineage_id is required"))
		})

		It("returns all checkpoints for a lineage in descending order", func(ctx context.Context) {
			cfg := makeConfig("lineage-list", "", "ns-1")
			for _, id := range []string{"a", "b", "c"} {
				ckpt := makeCheckpoint(id, "")
				ckpt.Timestamp = time.Now().UTC()
				_, err := saver.Put(ctx, graph.PutRequest{Config: cfg, Checkpoint: ckpt})
				Expect(err).NotTo(HaveOccurred())
				time.Sleep(2 * time.Millisecond)
			}

			tuples, err := saver.List(ctx, makeConfig("lineage-list", "", "ns-1"), nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(tuples).To(HaveLen(3))
			Expect(tuples[0].Checkpoint.ID).To(Equal("c"))
			Expect(tuples[2].Checkpoint.ID).To(Equal("a"))
		})

		It("applies the Limit filter", func(ctx context.Context) {
			cfg := makeConfig("lineage-limit", "", "ns-1")
			for _, id := range []string{"x", "y", "z"} {
				ckpt := makeCheckpoint(id, "")
				ckpt.Timestamp = time.Now().UTC()
				_, err := saver.Put(ctx, graph.PutRequest{Config: cfg, Checkpoint: ckpt})
				Expect(err).NotTo(HaveOccurred())
				time.Sleep(2 * time.Millisecond)
			}

			filter := &graph.CheckpointFilter{Limit: 2}
			tuples, err := saver.List(ctx, makeConfig("lineage-limit", "", "ns-1"), filter)
			Expect(err).NotTo(HaveOccurred())
			Expect(tuples).To(HaveLen(2))
		})

		It("applies the Before filter", func(ctx context.Context) {
			cfg := makeConfig("lineage-before", "", "ns-1")
			for _, id := range []string{"p", "q", "r"} {
				ckpt := makeCheckpoint(id, "")
				ckpt.Timestamp = time.Now().UTC()
				_, err := saver.Put(ctx, graph.PutRequest{Config: cfg, Checkpoint: ckpt})
				Expect(err).NotTo(HaveOccurred())
				time.Sleep(2 * time.Millisecond)
			}

			filter := &graph.CheckpointFilter{
				Before: makeConfig("lineage-before", "r", "ns-1"),
			}
			tuples, err := saver.List(ctx, makeConfig("lineage-before", "", "ns-1"), filter)
			Expect(err).NotTo(HaveOccurred())
			Expect(tuples).To(HaveLen(2))
			for _, t := range tuples {
				Expect(t.Checkpoint.ID).NotTo(Equal("r"))
			}
		})

		It("returns empty when lineage has no checkpoints", func(ctx context.Context) {
			tuples, err := saver.List(ctx, makeConfig("lineage-empty", "", "ns-1"), nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(tuples).To(BeEmpty())
		})
	})

	// -------------------------------------------------------------------
	// DeleteLineage
	// -------------------------------------------------------------------

	Describe("DeleteLineage", func() {
		It("removes all checkpoints and writes for a lineage", func(ctx context.Context) {
			cfg := makeConfig("lineage-del", "", "ns-1")
			ckpt := makeCheckpoint("del-1", "")
			_, err := saver.Put(ctx, graph.PutRequest{Config: cfg, Checkpoint: ckpt})
			Expect(err).NotTo(HaveOccurred())

			Expect(saver.PutWrites(ctx, graph.PutWritesRequest{
				Config: makeConfig("lineage-del", "del-1", "ns-1"),
				TaskID: "t1",
				Writes: []graph.PendingWrite{{Channel: "ch", Value: "v", Sequence: 1}},
			})).To(Succeed())

			Expect(saver.DeleteLineage(ctx, "lineage-del")).To(Succeed())

			got, err := saver.Get(ctx, makeConfig("lineage-del", "del-1", "ns-1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil())
		})

		It("returns an error when lineage_id is empty", func(ctx context.Context) {
			err := saver.DeleteLineage(ctx, "")
			Expect(err).To(MatchError("lineage_id is required"))
		})

		It("succeeds when lineage does not exist", func(ctx context.Context) {
			Expect(saver.DeleteLineage(ctx, "nonexistent")).To(Succeed())
		})
	})

	// -------------------------------------------------------------------
	// Close
	// -------------------------------------------------------------------

	Describe("Close", func() {
		It("returns nil (no-op)", func(ctx context.Context) {
			Expect(saver.Close()).To(Succeed())
		})
	})

	// -------------------------------------------------------------------
	// Cross-namespace / isolation
	// -------------------------------------------------------------------

	Describe("Namespace isolation", func() {
		It("keeps checkpoints in separate namespaces independent", func(ctx context.Context) {
			cfgA := makeConfig("lineage-ns", "", "ns-a")
			cfgB := makeConfig("lineage-ns", "", "ns-b")

			_, err := saver.Put(ctx, graph.PutRequest{
				Config: cfgA, Checkpoint: makeCheckpoint("ckpt-a", ""),
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = saver.Put(ctx, graph.PutRequest{
				Config: cfgB, Checkpoint: makeCheckpoint("ckpt-b", ""),
			})
			Expect(err).NotTo(HaveOccurred())

			gotA, err := saver.Get(ctx, makeConfig("lineage-ns", "ckpt-a", "ns-a"))
			Expect(err).NotTo(HaveOccurred())
			Expect(gotA).NotTo(BeNil())
			Expect(gotA.ID).To(Equal("ckpt-a"))

			gotB, err := saver.Get(ctx, makeConfig("lineage-ns", "ckpt-b", "ns-b"))
			Expect(err).NotTo(HaveOccurred())
			Expect(gotB).NotTo(BeNil())
			Expect(gotB.ID).To(Equal("ckpt-b"))

			gotCross, err := saver.Get(ctx, makeConfig("lineage-ns", "ckpt-a", "ns-b"))
			Expect(err).NotTo(HaveOccurred())
			Expect(gotCross).To(BeNil())
		})
	})

	// -------------------------------------------------------------------
	// Lineage isolation
	// -------------------------------------------------------------------

	Describe("Lineage isolation", func() {
		It("keeps checkpoints in separate lineages independent", func(ctx context.Context) {
			_, err := saver.Put(ctx, graph.PutRequest{
				Config: makeConfig("lineage-A", "", "ns-1"), Checkpoint: makeCheckpoint("ckpt-A", ""),
			})
			Expect(err).NotTo(HaveOccurred())

			got, err := saver.Get(ctx, makeConfig("lineage-B", "ckpt-A", "ns-1"))
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(BeNil())
		})
	})
})
