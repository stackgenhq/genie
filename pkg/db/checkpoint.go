package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"trpc.group/trpc-go/trpc-agent-go/graph"
)

// checkpointRow is the GORM model for the checkpoints table.
// Each row stores a serialised graph checkpoint with its metadata,
// keyed by lineage, namespace, and checkpoint ID.
type checkpointRow struct {
	LineageID          string `gorm:"primaryKey;type:text;column:lineage_id"`
	CheckpointNS       string `gorm:"primaryKey;type:text;column:checkpoint_ns"`
	CheckpointID       string `gorm:"primaryKey;type:text;column:checkpoint_id"`
	ParentCheckpointID string `gorm:"type:text;column:parent_checkpoint_id"`
	TS                 int64  `gorm:"type:bigint;not null;column:ts"`
	CheckpointJSON     []byte `gorm:"type:bytea;not null;column:checkpoint_json"`
	MetadataJSON       []byte `gorm:"type:bytea;not null;column:metadata_json"`
}

func (checkpointRow) TableName() string { return "checkpoints" }

// checkpointWriteRow is the GORM model for the checkpoint_writes table.
// Each row represents a single pending write linked to a checkpoint,
// used for deterministic replay of graph execution.
type checkpointWriteRow struct {
	LineageID    string `gorm:"primaryKey;type:text;column:lineage_id"`
	CheckpointNS string `gorm:"primaryKey;type:text;column:checkpoint_ns"`
	CheckpointID string `gorm:"primaryKey;type:text;column:checkpoint_id"`
	TaskID       string `gorm:"primaryKey;type:text;column:task_id"`
	Idx          int    `gorm:"primaryKey;type:integer;column:idx"`
	Channel      string `gorm:"type:text;not null;column:channel"`
	ValueJSON    []byte `gorm:"type:bytea;not null;column:value_json"`
	TaskPath     string `gorm:"type:text;column:task_path"`
	Seq          int64  `gorm:"type:bigint;not null;column:seq"`
}

func (checkpointWriteRow) TableName() string { return "checkpoint_writes" }

// GormCheckpointSaver implements graph.CheckpointSaver for PostgreSQL via GORM.
// It mirrors the upstream SQLite saver but leverages GORM's dialect-aware
// query builder, removing hand-written SQL with dollar-sign placeholders.
//
// Without this, the graph checkpoint system cannot store execution state
// when the underlying database is PostgreSQL (the default for guild).
type GormCheckpointSaver struct {
	db *gorm.DB
}

// NewGormCheckpointSaver creates a new PostgreSQL checkpoint saver backed by GORM.
// It auto-migrates the checkpoints and checkpoint_writes tables on startup.
// Without this constructor, callers would have to manually create tables and
// wire up the GORM connection, which is error-prone.
func NewGormCheckpointSaver(db *gorm.DB) (*GormCheckpointSaver, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	if err := db.AutoMigrate(&checkpointRow{}, &checkpointWriteRow{}); err != nil {
		return nil, fmt.Errorf("auto-migrate checkpoint tables: %w", err)
	}
	return &GormCheckpointSaver{db: db}, nil
}

// Get returns the checkpoint for the given config.
// It delegates to GetTuple and unwraps the checkpoint field.
// Without this, callers that only need the raw Checkpoint struct would have
// to call GetTuple and discard the wrapper themselves.
func (s *GormCheckpointSaver) Get(ctx context.Context, config map[string]any) (*graph.Checkpoint, error) {
	t, err := s.GetTuple(ctx, config)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, nil
	}
	return t.Checkpoint, nil
}

// GetTuple returns the full checkpoint tuple (checkpoint + metadata + writes)
// for the given config. When checkpoint_id is empty, the most recent checkpoint
// for the lineage/namespace is returned.
// Without this, the graph engine cannot resume execution from a prior state.
func (s *GormCheckpointSaver) GetTuple(ctx context.Context, config map[string]any) (*graph.CheckpointTuple, error) {
	lineageID := graph.GetLineageID(config)
	checkpointNS := graph.GetNamespace(config)
	checkpointID := graph.GetCheckpointID(config)
	if lineageID == "" {
		return nil, errors.New("lineage_id is required")
	}

	var row checkpointRow
	q := s.db.WithContext(ctx).Where("lineage_id = ? AND checkpoint_ns = ?", lineageID, checkpointNS)
	if checkpointID == "" {
		q = q.Order("ts DESC")
	} else {
		q = q.Where("checkpoint_id = ?", checkpointID)
	}
	if err := q.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("select checkpoint: %w", err)
	}

	nsForTuple := checkpointNS
	if checkpointNS == "" {
		nsForTuple = row.CheckpointNS
	}
	return s.buildTuple(ctx, lineageID, nsForTuple, row.CheckpointID, row.ParentCheckpointID, row.CheckpointJSON, row.MetadataJSON)
}

// buildTuple deserialises the raw JSON fields and assembles a CheckpointTuple
// including its associated pending writes and parent config.
func (s *GormCheckpointSaver) buildTuple(ctx context.Context, lineageID, checkpointNS, checkpointID, parentID string, checkpointJSON, metadataJSON []byte) (*graph.CheckpointTuple, error) {
	var ckpt graph.Checkpoint
	if err := json.Unmarshal(checkpointJSON, &ckpt); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint: %w", err)
	}
	var meta graph.CheckpointMetadata
	if err := json.Unmarshal(metadataJSON, &meta); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}

	cfg := graph.CreateCheckpointConfig(lineageID, checkpointID, checkpointNS)
	writes, err := s.loadWrites(ctx, lineageID, checkpointNS, checkpointID)
	if err != nil {
		return nil, err
	}

	var parentCfg map[string]any
	if parentID != "" {
		parentNS, err := s.findCheckpointNamespace(ctx, lineageID, parentID)
		if err != nil {
			return nil, err
		}
		parentCfg = graph.CreateCheckpointConfig(lineageID, parentID, parentNS)
	}
	return &graph.CheckpointTuple{
		Config:        cfg,
		Checkpoint:    &ckpt,
		Metadata:      &meta,
		ParentConfig:  parentCfg,
		PendingWrites: writes,
	}, nil
}

// findCheckpointNamespace looks up the namespace for a given checkpoint ID.
// Parent checkpoints may reside in a different namespace, so this is needed
// to build the correct parent config reference.
func (s *GormCheckpointSaver) findCheckpointNamespace(ctx context.Context, lineageID, checkpointID string) (string, error) {
	if checkpointID == "" || lineageID == "" {
		return "", nil
	}
	var row checkpointRow
	err := s.db.WithContext(ctx).
		Select("checkpoint_ns").
		Where("lineage_id = ? AND checkpoint_id = ?", lineageID, checkpointID).
		First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("lookup parent namespace: %w", err)
	}
	return row.CheckpointNS, nil
}

// List returns checkpoints for the lineage/namespace, with optional filters
// (before cursor and limit). Each returned tuple includes writes and metadata.
// Without this, the graph system cannot enumerate prior execution states for
// replay, branching, or administrative inspection.
func (s *GormCheckpointSaver) List(ctx context.Context, config map[string]any, filter *graph.CheckpointFilter) ([]*graph.CheckpointTuple, error) {
	lineageID := graph.GetLineageID(config)
	checkpointNS := graph.GetNamespace(config)
	if lineageID == "" {
		return nil, errors.New("lineage_id is required")
	}

	q := s.db.WithContext(ctx).Model(&checkpointRow{}).Where("lineage_id = ?", lineageID)
	if checkpointNS != "" {
		q = q.Where("checkpoint_ns = ?", checkpointNS)
	}

	if filter != nil && filter.Before != nil {
		beforeID := graph.GetCheckpointID(filter.Before)
		if beforeID != "" {
			var beforeRow checkpointRow
			err := s.db.WithContext(ctx).
				Select("ts").
				Where("lineage_id = ? AND checkpoint_ns = ? AND checkpoint_id = ?", lineageID, checkpointNS, beforeID).
				First(&beforeRow).Error
			if err == nil {
				q = q.Where("ts < ?", beforeRow.TS)
			}
		}
	}

	q = q.Order("ts DESC")

	if filter != nil && filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}

	var rows []checkpointRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("select checkpoints: %w", err)
	}

	tuples := make([]*graph.CheckpointTuple, 0, len(rows))
	for _, r := range rows {
		ns := checkpointNS
		if ns == "" {
			ns = r.CheckpointNS
		}
		cfg := graph.CreateCheckpointConfig(lineageID, r.CheckpointID, ns)
		t, err := s.GetTuple(ctx, cfg)
		if err != nil {
			return nil, err
		}
		if t != nil {
			tuples = append(tuples, t)
		}
	}
	return tuples, nil
}

// Put stores the checkpoint and returns the updated config.
// An upsert is used so that re-saving the same checkpoint ID overwrites
// the previous data instead of producing a constraint violation.
// Without this, the graph engine cannot persist execution state.
func (s *GormCheckpointSaver) Put(ctx context.Context, req graph.PutRequest) (map[string]any, error) {
	if req.Checkpoint == nil {
		return nil, errors.New("checkpoint cannot be nil")
	}
	lineageID := graph.GetLineageID(req.Config)
	checkpointNS := graph.GetNamespace(req.Config)
	if lineageID == "" {
		return nil, errors.New("lineage_id is required")
	}

	checkpointJSON, err := json.Marshal(req.Checkpoint)
	if err != nil {
		return nil, fmt.Errorf("marshal checkpoint: %w", err)
	}
	if req.Metadata == nil {
		req.Metadata = &graph.CheckpointMetadata{Source: graph.CheckpointSourceUpdate, Step: 0}
	}
	metadataJSON, err := json.Marshal(req.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}

	ts := req.Checkpoint.Timestamp.UnixNano()
	if ts == 0 {
		ts = time.Now().UTC().UnixNano()
	}

	row := checkpointRow{
		LineageID:          lineageID,
		CheckpointNS:       checkpointNS,
		CheckpointID:       req.Checkpoint.ID,
		ParentCheckpointID: req.Checkpoint.ParentCheckpointID,
		TS:                 ts,
		CheckpointJSON:     checkpointJSON,
		MetadataJSON:       metadataJSON,
	}
	result := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "lineage_id"}, {Name: "checkpoint_ns"}, {Name: "checkpoint_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"parent_checkpoint_id", "ts", "checkpoint_json", "metadata_json"}),
		}).
		Create(&row)
	if result.Error != nil {
		return nil, fmt.Errorf("insert checkpoint: %w", result.Error)
	}
	return graph.CreateCheckpointConfig(lineageID, req.Checkpoint.ID, checkpointNS), nil
}

// PutWrites stores write entries for a checkpoint.
// Each write is upserted individually so that retries don't produce duplicates.
// Without this, the graph engine cannot record intermediate channel writes
// needed for deterministic replay.
func (s *GormCheckpointSaver) PutWrites(ctx context.Context, req graph.PutWritesRequest) error {
	lineageID := graph.GetLineageID(req.Config)
	checkpointNS := graph.GetNamespace(req.Config)
	checkpointID := graph.GetCheckpointID(req.Config)
	if lineageID == "" || checkpointID == "" {
		return errors.New("lineage_id and checkpoint_id are required")
	}
	for idx, w := range req.Writes {
		valueJSON, err := json.Marshal(w.Value)
		if err != nil {
			return fmt.Errorf("marshal write: %w", err)
		}
		seq := w.Sequence
		if seq == 0 {
			seq = int64(idx)
		}
		row := checkpointWriteRow{
			LineageID:    lineageID,
			CheckpointNS: checkpointNS,
			CheckpointID: checkpointID,
			TaskID:       req.TaskID,
			Idx:          idx,
			Channel:      w.Channel,
			ValueJSON:    valueJSON,
			TaskPath:     req.TaskPath,
			Seq:          seq,
		}
		result := s.db.WithContext(ctx).
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "lineage_id"}, {Name: "checkpoint_ns"}, {Name: "checkpoint_id"}, {Name: "task_id"}, {Name: "idx"}},
				DoUpdates: clause.AssignmentColumns([]string{"channel", "value_json", "task_path", "seq"}),
			}).
			Create(&row)
		if result.Error != nil {
			return fmt.Errorf("insert write: %w", result.Error)
		}
	}
	return nil
}

// PutFull atomically stores a checkpoint with its pending writes in a single
// GORM transaction. Without this, a crash between the checkpoint insert and the
// writes insert could leave the store in an inconsistent state.
func (s *GormCheckpointSaver) PutFull(ctx context.Context, req graph.PutFullRequest) (map[string]any, error) {
	lineageID := graph.GetLineageID(req.Config)
	checkpointNS := graph.GetNamespace(req.Config)
	if lineageID == "" {
		return nil, errors.New("lineage_id is required")
	}
	if req.Checkpoint == nil {
		return nil, errors.New("checkpoint cannot be nil")
	}

	checkpointJSON, err := json.Marshal(req.Checkpoint)
	if err != nil {
		return nil, fmt.Errorf("marshal checkpoint: %w", err)
	}
	metadataJSON, err := json.Marshal(req.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}

	var resultCfg map[string]any

	txErr := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ckptRow := checkpointRow{
			LineageID:          lineageID,
			CheckpointNS:       checkpointNS,
			CheckpointID:       req.Checkpoint.ID,
			ParentCheckpointID: req.Checkpoint.ParentCheckpointID,
			TS:                 req.Checkpoint.Timestamp.UnixNano(),
			CheckpointJSON:     checkpointJSON,
			MetadataJSON:       metadataJSON,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "lineage_id"}, {Name: "checkpoint_ns"}, {Name: "checkpoint_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"parent_checkpoint_id", "ts", "checkpoint_json", "metadata_json"}),
		}).Create(&ckptRow).Error; err != nil {
			return fmt.Errorf("insert checkpoint: %w", err)
		}

		for idx, w := range req.PendingWrites {
			valueJSON, mErr := json.Marshal(w.Value)
			if mErr != nil {
				return fmt.Errorf("marshal write value: %w", mErr)
			}
			seq := w.Sequence
			if seq == 0 {
				seq = time.Now().UnixNano()
			}
			writeRow := checkpointWriteRow{
				LineageID:    lineageID,
				CheckpointNS: checkpointNS,
				CheckpointID: req.Checkpoint.ID,
				TaskID:       w.TaskID,
				Idx:          idx,
				Channel:      w.Channel,
				ValueJSON:    valueJSON,
				TaskPath:     "",
				Seq:          seq,
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "lineage_id"}, {Name: "checkpoint_ns"}, {Name: "checkpoint_id"}, {Name: "task_id"}, {Name: "idx"}},
				DoUpdates: clause.AssignmentColumns([]string{"channel", "value_json", "task_path", "seq"}),
			}).Create(&writeRow).Error; err != nil {
				return fmt.Errorf("insert write: %w", err)
			}
		}
		resultCfg = graph.CreateCheckpointConfig(lineageID, req.Checkpoint.ID, checkpointNS)
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return resultCfg, nil
}

// DeleteLineage deletes all checkpoints and writes for the given lineage.
// Without this, old graph sessions would accumulate indefinitely, consuming
// disk and slowing queries.
func (s *GormCheckpointSaver) DeleteLineage(ctx context.Context, lineageID string) error {
	if lineageID == "" {
		return errors.New("lineage_id is required")
	}
	if err := s.db.WithContext(ctx).Where("lineage_id = ?", lineageID).Delete(&checkpointRow{}).Error; err != nil {
		return fmt.Errorf("delete checkpoints: %w", err)
	}
	if err := s.db.WithContext(ctx).Where("lineage_id = ?", lineageID).Delete(&checkpointWriteRow{}).Error; err != nil {
		return fmt.Errorf("delete writes: %w", err)
	}
	return nil
}

// Close is a no-op because GORM manages the connection pool lifecycle
// externally. The caller that created the *gorm.DB is responsible for closing
// it. Without this method GormCheckpointSaver would not satisfy the
// graph.CheckpointSaver interface.
func (s *GormCheckpointSaver) Close() error {
	return nil
}

// loadWrites fetches all pending writes for a checkpoint, ordered by sequence.
func (s *GormCheckpointSaver) loadWrites(ctx context.Context, lineageID, checkpointNS, checkpointID string) ([]graph.PendingWrite, error) {
	var rows []checkpointWriteRow
	err := s.db.WithContext(ctx).
		Where("lineage_id = ? AND checkpoint_ns = ? AND checkpoint_id = ?", lineageID, checkpointNS, checkpointID).
		Order("seq").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("select writes: %w", err)
	}

	writes := make([]graph.PendingWrite, 0, len(rows))
	for _, r := range rows {
		var value any
		if err := json.Unmarshal(r.ValueJSON, &value); err != nil {
			return nil, fmt.Errorf("unmarshal write: %w", err)
		}
		writes = append(writes, graph.PendingWrite{
			Channel:  r.Channel,
			Value:    value,
			TaskID:   r.TaskID,
			Sequence: r.Seq,
		})
	}
	return writes, nil
}
