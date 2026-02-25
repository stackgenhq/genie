package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/graph"
)

const (
	pgCreateCheckpoints = `CREATE TABLE IF NOT EXISTS checkpoints (
		lineage_id TEXT NOT NULL,
		checkpoint_ns TEXT NOT NULL,
		checkpoint_id TEXT NOT NULL,
		parent_checkpoint_id TEXT,
		ts BIGINT NOT NULL,
		checkpoint_json BYTEA NOT NULL,
		metadata_json BYTEA NOT NULL,
		PRIMARY KEY (lineage_id, checkpoint_ns, checkpoint_id)
	)`

	pgCreateWrites = `CREATE TABLE IF NOT EXISTS checkpoint_writes (
		lineage_id TEXT NOT NULL,
		checkpoint_ns TEXT NOT NULL,
		checkpoint_id TEXT NOT NULL,
		task_id TEXT NOT NULL,
		idx INTEGER NOT NULL,
		channel TEXT NOT NULL,
		value_json BYTEA NOT NULL,
		task_path TEXT,
		seq BIGINT NOT NULL,
		PRIMARY KEY (lineage_id, checkpoint_ns, checkpoint_id, task_id, idx)
	)`

	pgInsertCheckpoint = `INSERT INTO checkpoints
		(lineage_id, checkpoint_ns, checkpoint_id, parent_checkpoint_id, ts, checkpoint_json, metadata_json)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (lineage_id, checkpoint_ns, checkpoint_id)
		DO UPDATE SET parent_checkpoint_id = EXCLUDED.parent_checkpoint_id,
			ts = EXCLUDED.ts, checkpoint_json = EXCLUDED.checkpoint_json,
			metadata_json = EXCLUDED.metadata_json`

	pgSelectLatest = `SELECT checkpoint_json, metadata_json, parent_checkpoint_id, checkpoint_ns, checkpoint_id
		FROM checkpoints WHERE lineage_id = $1 AND checkpoint_ns = $2
		ORDER BY ts DESC LIMIT 1`

	pgSelectByID = `SELECT checkpoint_json, metadata_json, parent_checkpoint_id, checkpoint_ns, checkpoint_id
		FROM checkpoints WHERE lineage_id = $1 AND checkpoint_ns = $2 AND checkpoint_id = $3 LIMIT 1`

	pgInsertWrite = `INSERT INTO checkpoint_writes
		(lineage_id, checkpoint_ns, checkpoint_id, task_id, idx, channel, value_json, task_path, seq)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (lineage_id, checkpoint_ns, checkpoint_id, task_id, idx)
		DO UPDATE SET channel = EXCLUDED.channel, value_json = EXCLUDED.value_json,
			task_path = EXCLUDED.task_path, seq = EXCLUDED.seq`

	pgSelectWrites = `SELECT task_id, idx, channel, value_json, task_path, seq FROM checkpoint_writes
		WHERE lineage_id = $1 AND checkpoint_ns = $2 AND checkpoint_id = $3 ORDER BY seq`

	pgDeleteLineageCkpts  = "DELETE FROM checkpoints WHERE lineage_id = $1"
	pgDeleteLineageWrites = "DELETE FROM checkpoint_writes WHERE lineage_id = $1"
)

// PgCheckpointSaver implements graph.CheckpointSaver for PostgreSQL.
// It mirrors the upstream SQLite saver but uses dollar-sign placeholders
// and ON CONFLICT upsert syntax required by PostgreSQL.
//
// Without this, the graph checkpoint system cannot store execution state
// when the underlying database is PostgreSQL (the default for guild).
type PgCheckpointSaver struct {
	db *sql.DB
}

// NewPgCheckpointSaver creates a new PostgreSQL checkpoint saver.
func NewPgCheckpointSaver(db *sql.DB) (*PgCheckpointSaver, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	if _, err := db.Exec(pgCreateCheckpoints); err != nil {
		return nil, fmt.Errorf("create checkpoints table: %w", err)
	}
	if _, err := db.Exec(pgCreateWrites); err != nil {
		return nil, fmt.Errorf("create writes table: %w", err)
	}
	return &PgCheckpointSaver{db: db}, nil
}

// Get returns the checkpoint for the given config.
func (s *PgCheckpointSaver) Get(ctx context.Context, config map[string]any) (*graph.Checkpoint, error) {
	t, err := s.GetTuple(ctx, config)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, nil
	}
	return t.Checkpoint, nil
}

// GetTuple returns the checkpoint tuple for the given config.
func (s *PgCheckpointSaver) GetTuple(ctx context.Context, config map[string]any) (*graph.CheckpointTuple, error) {
	lineageID := graph.GetLineageID(config)
	checkpointNS := graph.GetNamespace(config)
	checkpointID := graph.GetCheckpointID(config)
	if lineageID == "" {
		return nil, errors.New("lineage_id is required")
	}

	var row *sql.Row
	if checkpointID == "" {
		row = s.db.QueryRowContext(ctx, pgSelectLatest, lineageID, checkpointNS)
	} else {
		row = s.db.QueryRowContext(ctx, pgSelectByID, lineageID, checkpointNS, checkpointID)
	}

	var checkpointJSON, metadataJSON []byte
	var parentID, ns, ckptID string
	if err := row.Scan(&checkpointJSON, &metadataJSON, &parentID, &ns, &ckptID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("select checkpoint: %w", err)
	}

	nsForTuple := checkpointNS
	if checkpointNS == "" {
		nsForTuple = ns
	}
	return s.buildTuple(ctx, lineageID, nsForTuple, ckptID, parentID, checkpointJSON, metadataJSON)
}

func (s *PgCheckpointSaver) buildTuple(ctx context.Context, lineageID, checkpointNS, checkpointID, parentID string, checkpointJSON, metadataJSON []byte) (*graph.CheckpointTuple, error) {
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

func (s *PgCheckpointSaver) findCheckpointNamespace(ctx context.Context, lineageID, checkpointID string) (string, error) {
	if checkpointID == "" || lineageID == "" {
		return "", nil
	}
	row := s.db.QueryRowContext(ctx,
		"SELECT checkpoint_ns FROM checkpoints WHERE lineage_id = $1 AND checkpoint_id = $2 LIMIT 1",
		lineageID, checkpointID,
	)
	var ns string
	if err := row.Scan(&ns); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("lookup parent namespace: %w", err)
	}
	return ns, nil
}

// List returns checkpoints for the lineage/namespace, with optional filters.
func (s *PgCheckpointSaver) List(ctx context.Context, config map[string]any, filter *graph.CheckpointFilter) ([]*graph.CheckpointTuple, error) {
	lineageID := graph.GetLineageID(config)
	checkpointNS := graph.GetNamespace(config)
	if lineageID == "" {
		return nil, errors.New("lineage_id is required")
	}

	var q string
	var args []any
	argN := 1

	if checkpointNS == "" {
		q = fmt.Sprintf("SELECT checkpoint_id, checkpoint_ns, ts FROM checkpoints WHERE lineage_id=$%d", argN)
		args = append(args, lineageID)
		argN++
	} else {
		q = fmt.Sprintf("SELECT checkpoint_id, ts FROM checkpoints WHERE lineage_id=$%d AND checkpoint_ns=$%d", argN, argN+1)
		args = append(args, lineageID, checkpointNS)
		argN += 2
	}

	if filter != nil && filter.Before != nil {
		beforeID := graph.GetCheckpointID(filter.Before)
		if beforeID != "" {
			var beforeTS int64
			bRow := s.db.QueryRowContext(ctx,
				fmt.Sprintf("SELECT ts FROM checkpoints WHERE lineage_id=$1 AND checkpoint_ns=$2 AND checkpoint_id=$3 LIMIT 1"),
				lineageID, checkpointNS, beforeID,
			)
			if err := bRow.Scan(&beforeTS); err == nil {
				q += fmt.Sprintf(" AND ts < $%d", argN)
				args = append(args, beforeTS)
				argN++
			}
		}
	}

	q += " ORDER BY ts DESC"

	if filter != nil && filter.Limit > 0 {
		q += fmt.Sprintf(" LIMIT $%d", argN)
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("select checkpoints: %w", err)
	}
	defer rows.Close()

	var tuples []*graph.CheckpointTuple
	for rows.Next() {
		var ckptID string
		var ts int64
		if checkpointNS == "" {
			var ns string
			if err := rows.Scan(&ckptID, &ns, &ts); err != nil {
				return nil, fmt.Errorf("scan checkpoint: %w", err)
			}
			cfg := graph.CreateCheckpointConfig(lineageID, ckptID, ns)
			t, err := s.GetTuple(ctx, cfg)
			if err != nil {
				return nil, err
			}
			if t != nil {
				tuples = append(tuples, t)
			}
		} else {
			if err := rows.Scan(&ckptID, &ts); err != nil {
				return nil, fmt.Errorf("scan checkpoint: %w", err)
			}
			cfg := graph.CreateCheckpointConfig(lineageID, ckptID, checkpointNS)
			t, err := s.GetTuple(ctx, cfg)
			if err != nil {
				return nil, err
			}
			if t != nil {
				tuples = append(tuples, t)
			}
		}

		if filter != nil && filter.Limit > 0 && len(tuples) >= filter.Limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iter checkpoints: %w", err)
	}
	return tuples, nil
}

// Put stores the checkpoint and returns the updated config.
func (s *PgCheckpointSaver) Put(ctx context.Context, req graph.PutRequest) (map[string]any, error) {
	if req.Checkpoint == nil {
		return nil, errors.New("checkpoint cannot be nil")
	}
	lineageID := graph.GetLineageID(req.Config)
	checkpointNS := graph.GetNamespace(req.Config)
	if lineageID == "" {
		return nil, errors.New("lineage_id is required")
	}

	parentID := req.Checkpoint.ParentCheckpointID
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
	_, err = s.db.ExecContext(ctx, pgInsertCheckpoint,
		lineageID, checkpointNS, req.Checkpoint.ID, parentID, ts, checkpointJSON, metadataJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("insert checkpoint: %w", err)
	}
	return graph.CreateCheckpointConfig(lineageID, req.Checkpoint.ID, checkpointNS), nil
}

// PutWrites stores write entries for a checkpoint.
func (s *PgCheckpointSaver) PutWrites(ctx context.Context, req graph.PutWritesRequest) error {
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
		_, err = s.db.ExecContext(ctx, pgInsertWrite,
			lineageID, checkpointNS, checkpointID, req.TaskID, idx, w.Channel, valueJSON, req.TaskPath, seq,
		)
		if err != nil {
			return fmt.Errorf("insert write: %w", err)
		}
	}
	return nil
}

// PutFull atomically stores a checkpoint with its pending writes.
func (s *PgCheckpointSaver) PutFull(ctx context.Context, req graph.PutFullRequest) (map[string]any, error) {
	lineageID := graph.GetLineageID(req.Config)
	checkpointNS := graph.GetNamespace(req.Config)
	if lineageID == "" {
		return nil, errors.New("lineage_id is required")
	}
	if req.Checkpoint == nil {
		return nil, errors.New("checkpoint cannot be nil")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	checkpointJSON, err := json.Marshal(req.Checkpoint)
	if err != nil {
		return nil, fmt.Errorf("marshal checkpoint: %w", err)
	}
	metadataJSON, err := json.Marshal(req.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}

	parentID := req.Checkpoint.ParentCheckpointID
	_, err = tx.ExecContext(ctx, pgInsertCheckpoint,
		lineageID, checkpointNS, req.Checkpoint.ID, parentID, req.Checkpoint.Timestamp.UnixNano(), checkpointJSON, metadataJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("insert checkpoint: %w", err)
	}

	for idx, w := range req.PendingWrites {
		valueJSON, err := json.Marshal(w.Value)
		if err != nil {
			return nil, fmt.Errorf("marshal write value: %w", err)
		}
		seq := w.Sequence
		if seq == 0 {
			seq = time.Now().UnixNano()
		}
		_, err = tx.ExecContext(ctx, pgInsertWrite,
			lineageID, checkpointNS, req.Checkpoint.ID, w.TaskID, idx, w.Channel, valueJSON, "", seq,
		)
		if err != nil {
			return nil, fmt.Errorf("insert write: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}
	return graph.CreateCheckpointConfig(lineageID, req.Checkpoint.ID, checkpointNS), nil
}

// DeleteLineage deletes all checkpoints and writes for the lineage.
func (s *PgCheckpointSaver) DeleteLineage(ctx context.Context, lineageID string) error {
	if lineageID == "" {
		return errors.New("lineage_id is required")
	}
	if _, err := s.db.ExecContext(ctx, pgDeleteLineageCkpts, lineageID); err != nil {
		return fmt.Errorf("delete checkpoints: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, pgDeleteLineageWrites, lineageID); err != nil {
		return fmt.Errorf("delete writes: %w", err)
	}
	return nil
}

// Close releases resources held by the saver.
func (s *PgCheckpointSaver) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *PgCheckpointSaver) loadWrites(ctx context.Context, lineageID, checkpointNS, checkpointID string) ([]graph.PendingWrite, error) {
	rows, err := s.db.QueryContext(ctx, pgSelectWrites, lineageID, checkpointNS, checkpointID)
	if err != nil {
		return nil, fmt.Errorf("select writes: %w", err)
	}
	defer rows.Close()

	var writes []graph.PendingWrite
	for rows.Next() {
		var taskID, channel, taskPath string
		var idx int
		var valueJSON []byte
		var seq int64
		if err := rows.Scan(&taskID, &idx, &channel, &valueJSON, &taskPath, &seq); err != nil {
			return nil, fmt.Errorf("scan write: %w", err)
		}
		var value any
		if err := json.Unmarshal(valueJSON, &value); err != nil {
			return nil, fmt.Errorf("unmarshal write: %w", err)
		}
		writes = append(writes, graph.PendingWrite{
			Channel:  channel,
			Value:    value,
			TaskID:   taskID,
			Sequence: seq,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iter writes: %w", err)
	}
	return writes, nil
}
