// Package db — session_store.go provides a GORM-backed implementation of the
// trpc-agent-go session.Service interface, modeled after the storage builder
// pattern in trpc-agent-go/storage (e.g. storage/mysql).
//
// Architecture: This wraps an inmemory.SessionService to handle all the complex
// session logic (event filtering, summarization, state merging) while persisting
// data to the database on writes and loading from DB on reads. The inmemory
// service acts as the hot cache; the DB is the durable store.
//
// Usage:
//
//	gormDB, _ := db.Open(db.DefaultPath())
//	sessionSvc := db.NewSessionStore(gormDB,
//	    db.WithSessionStoreInMemoryOpts(
//	        inmemory.WithSummarizer(summary.NewSummarizer(model, ...)),
//	    ),
//	    db.WithSessionStoreEventLimit(100),
//	)
//	runner.NewRunner("agent", agent, runner.WithSessionService(sessionSvc))
package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/stackgenhq/genie/pkg/logger"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/graph"
	graphsqlite "trpc.group/trpc-go/trpc-agent-go/graph/checkpoint/sqlite"
	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

// ---------------------------------------------------------------------------
// GORM Models
// ---------------------------------------------------------------------------

// SessionRow is the GORM model for the sessions table.
type SessionRow struct {
	AppName   string    `gorm:"primaryKey;type:text" json:"app_name"`
	UserID    string    `gorm:"primaryKey;type:text" json:"user_id"`
	SessionID string    `gorm:"primaryKey;type:text" json:"session_id"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;index:idx_sessions_updated" json:"updated_at"`
}

func (SessionRow) TableName() string { return "sessions" }

// SessionEvent is the GORM model for session_events table.
type SessionEvent struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	AppName   string    `gorm:"type:text;not null;index:idx_session_events_key" json:"app_name"`
	UserID    string    `gorm:"type:text;not null;index:idx_session_events_key" json:"user_id"`
	SessionID string    `gorm:"type:text;not null;index:idx_session_events_key" json:"session_id"`
	EventData string    `gorm:"type:text;not null" json:"event_data"` // JSON-encoded event.Event
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
}

func (SessionEvent) TableName() string { return "session_events" }

// SessionState is the GORM model for session-level state key-value pairs.
type SessionState struct {
	AppName   string    `gorm:"primaryKey;type:text" json:"app_name"`
	UserID    string    `gorm:"primaryKey;type:text" json:"user_id"`
	SessionID string    `gorm:"primaryKey;type:text" json:"session_id"`
	Key       string    `gorm:"primaryKey;type:text" json:"key"`
	Value     []byte    `gorm:"type:blob" json:"value"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
}

func (SessionState) TableName() string { return "session_states" }

// AppState is the GORM model for app-level state.
type AppState struct {
	AppName   string    `gorm:"primaryKey;type:text" json:"app_name"`
	Key       string    `gorm:"primaryKey;type:text" json:"key"`
	Value     []byte    `gorm:"type:blob" json:"value"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
}

func (AppState) TableName() string { return "app_states" }

// UserState is the GORM model for user-level state.
type UserState struct {
	AppName   string    `gorm:"primaryKey;type:text" json:"app_name"`
	UserID    string    `gorm:"primaryKey;type:text" json:"user_id"`
	Key       string    `gorm:"primaryKey;type:text" json:"key"`
	Value     []byte    `gorm:"type:blob" json:"value"`
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
}

func (UserState) TableName() string { return "user_states" }

// SessionSummaryRow is the GORM model for session summaries.
type SessionSummaryRow struct {
	AppName   string    `gorm:"primaryKey;type:text" json:"app_name"`
	UserID    string    `gorm:"primaryKey;type:text" json:"user_id"`
	SessionID string    `gorm:"primaryKey;type:text" json:"session_id"`
	FilterKey string    `gorm:"primaryKey;type:text" json:"filter_key"` // "" = full-session summary
	Summary   string    `gorm:"type:text;not null" json:"summary"`
	Topics    string    `gorm:"type:text;default:'[]'" json:"topics"` // JSON-encoded []string
	UpdatedAt time.Time `gorm:"not null" json:"updated_at"`
}

func (SessionSummaryRow) TableName() string { return "session_summaries" }

// ---------------------------------------------------------------------------
// Builder — mirrors the trpc-agent-go storage builder pattern.
// ---------------------------------------------------------------------------

// SessionStoreOption configures a SessionStore (analogous to ClientBuilderOpt
// in trpc-agent-go/storage/mysql).
type SessionStoreOption func(*sessionStoreOpts)

type sessionStoreOpts struct {
	inMemoryOpts      []inmemory.ServiceOpt
	sessionEventLimit int
}

// WithSessionStoreInMemoryOpts passes options through to the underlying
// inmemory.SessionService (e.g. WithSummarizer, WithSessionEventLimit).
func WithSessionStoreInMemoryOpts(opts ...inmemory.ServiceOpt) SessionStoreOption {
	return func(o *sessionStoreOpts) { o.inMemoryOpts = append(o.inMemoryOpts, opts...) }
}

// WithSessionStoreEventLimit caps the number of events persisted per session.
// Older events are pruned on append. 0 = unlimited.
func WithSessionStoreEventLimit(limit int) SessionStoreOption {
	return func(o *sessionStoreOpts) { o.sessionEventLimit = limit }
}

// SessionStoreBuilder is a function type that creates a SessionStore,
// analogous to mysql.clientBuilder in trpc-agent-go/storage/mysql.
type SessionStoreBuilder func(db *gorm.DB, opts ...SessionStoreOption) *SessionStore

// DefaultSessionStoreBuilder is the default builder.
var DefaultSessionStoreBuilder SessionStoreBuilder = NewSessionStore

// ---------------------------------------------------------------------------
// SessionStore — GORM-backed session.Service
// ---------------------------------------------------------------------------

// Compile-time interface check.
var _ session.Service = (*SessionStore)(nil)

// SessionStore implements session.Service backed by GORM/SQLite.
// It wraps an inmemory.SessionService for in-process session logic and
// persists all changes to the database for durability across restarts.
type SessionStore struct {
	db     *gorm.DB
	mem    *inmemory.SessionService
	opts   sessionStoreOpts
	mu     sync.Mutex // serializes DB writes
	loaded map[string]bool
	loadMu sync.Mutex
	// dbEvents caches events loaded from DB during ensureLoaded. These are
	// prepended to any new inmemory events on GetSession.
	dbEvents   map[string][]event.Event // sessionCacheKey -> events
	dbEventsMu sync.Mutex
	// dbSummaries caches summaries loaded from DB for sessions that haven't
	// been summarized in the current inmemory lifecycle.
	dbSummaries   map[string]map[string]*session.Summary // sessionCacheKey -> filterKey -> Summary
	dbSummariesMu sync.Mutex

	checkpointer graph.CheckpointSaver
}

// NewSessionStore creates a new GORM-backed session store. This is the
// default SessionStoreBuilder.
func NewSessionStore(db *gorm.DB, opts ...SessionStoreOption) *SessionStore {
	s := &SessionStore{
		db:          db,
		loaded:      make(map[string]bool),
		dbEvents:    make(map[string][]event.Event),
		dbSummaries: make(map[string]map[string]*session.Summary),
	}
	for _, o := range opts {
		o(&s.opts)
	}
	s.mem = inmemory.NewSessionService(s.opts.inMemoryOpts...)

	// Initialize the Checkpointer for Long-Horizon Task Resiliency
	sqlDB, err := db.DB()
	logr := logger.GetLogger(db.Statement.Context)
	if err != nil {
		logr.Warn("checkpointer: failed to get underlying *sql.DB, durable checkpointing disabled", "error", err)
		return s
	}
	if sqlDB == nil {
		logr.Warn("checkpointer: underlying *sql.DB is nil, durable checkpointing disabled")
		return s
	}
	saver, err := graphsqlite.NewSaver(sqlDB)
	if err != nil {
		logr.Warn("checkpointer: failed to initialize SQLite saver, durable checkpointing disabled", "error", err)
		return s
	}
	s.checkpointer = saver

	return s
}

// Checkpointer returns the underlying graph.CheckpointSaver if initialized.
func (s *SessionStore) Checkpointer() graph.CheckpointSaver {
	return s.checkpointer
}

// sessionCacheKey returns a string key for de-duplicating loads.
func sessionCacheKey(key session.Key) string {
	return key.AppName + ":" + key.UserID + ":" + key.SessionID
}

// ---------------------------------------------------------------------------
// ensureLoaded loads a session from DB into the inmemory cache if not loaded.
// ---------------------------------------------------------------------------

func (s *SessionStore) ensureLoaded(ctx context.Context, key session.Key) error {
	sk := sessionCacheKey(key)
	s.loadMu.Lock()
	if s.loaded[sk] {
		s.loadMu.Unlock()
		return nil
	}
	s.loadMu.Unlock()

	// Check if session exists in DB.
	var row SessionRow
	err := s.db.WithContext(ctx).
		Where("app_name = ? AND user_id = ? AND session_id = ?",
			key.AppName, key.UserID, key.SessionID).
		First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			s.loadMu.Lock()
			s.loaded[sk] = true
			s.loadMu.Unlock()
			return nil
		}
		return fmt.Errorf("check session existence: %w", err)
	}

	// Load events from DB.
	events, err := s.loadEvents(ctx, key)
	if err != nil {
		return err
	}

	// Load session state from DB.
	sessState, err := s.loadSessionState(ctx, key)
	if err != nil {
		return err
	}

	// Load summaries from DB.
	summaries, err := s.loadSummaries(ctx, key)
	if err != nil {
		return err
	}

	// Create session in inmemory with restored state (no events — we cache
	// DB events separately to avoid inmemory's EnsureEventStartWithUser
	// filtering which clears events that don't start with a user message).
	_, err = s.mem.CreateSession(ctx, key, sessState)
	if err != nil {
		return fmt.Errorf("restore session to memory: %w", err)
	}

	// Cache DB events and summaries for GetSession to merge.
	if len(events) > 0 {
		s.dbEventsMu.Lock()
		s.dbEvents[sk] = events
		s.dbEventsMu.Unlock()
	}
	if len(summaries) > 0 {
		s.dbSummariesMu.Lock()
		s.dbSummaries[sk] = summaries
		s.dbSummariesMu.Unlock()
	}

	// Mark as loaded.
	s.loadMu.Lock()
	s.loaded[sk] = true
	s.loadMu.Unlock()

	return nil
}

// ---------------------------------------------------------------------------
// CreateSession
// ---------------------------------------------------------------------------

func (s *SessionStore) CreateSession(
	ctx context.Context,
	key session.Key,
	state session.StateMap,
	opts ...session.Option,
) (*session.Session, error) {
	if err := key.CheckUserKey(); err != nil {
		return nil, err
	}
	if key.SessionID == "" {
		key.SessionID = uuid.New().String()
	}

	// Create in inmemory first.
	sess, err := s.mem.CreateSession(ctx, key, state, opts...)
	if err != nil {
		return nil, err
	}

	// Persist to DB.
	now := time.Now()
	row := SessionRow{
		AppName:   key.AppName,
		UserID:    key.UserID,
		SessionID: key.SessionID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&row).Error; err != nil {
		return nil, fmt.Errorf("persist session: %w", err)
	}

	// Persist initial state.
	for k, v := range state {
		if err := s.upsertSessionState(ctx, key, k, v); err != nil {
			return nil, fmt.Errorf("persist initial state: %w", err)
		}
	}

	sk := sessionCacheKey(key)
	s.loadMu.Lock()
	s.loaded[sk] = true
	s.loadMu.Unlock()

	return sess, nil
}

// ---------------------------------------------------------------------------
// GetSession
// ---------------------------------------------------------------------------

func (s *SessionStore) GetSession(
	ctx context.Context,
	key session.Key,
	opts ...session.Option,
) (*session.Session, error) {
	if err := key.CheckSessionKey(); err != nil {
		return nil, err
	}

	if err := s.ensureLoaded(ctx, key); err != nil {
		return nil, err
	}

	sess, err := s.mem.GetSession(ctx, key, opts...)
	if err != nil || sess == nil {
		return sess, err
	}

	// Merge cached DB events: prepend DB events before any new inmemory events.
	sk := sessionCacheKey(key)
	s.dbEventsMu.Lock()
	dbEvts := s.dbEvents[sk]
	s.dbEventsMu.Unlock()
	if len(dbEvts) > 0 {
		sess.EventMu.Lock()
		merged := make([]event.Event, 0, len(dbEvts)+len(sess.Events))
		merged = append(merged, dbEvts...)
		merged = append(merged, sess.Events...)
		sess.Events = merged
		sess.EventMu.Unlock()
	}

	// Merge cached DB summaries.
	s.dbSummariesMu.Lock()
	dbSums := s.dbSummaries[sk]
	s.dbSummariesMu.Unlock()
	if len(dbSums) > 0 {
		sess.SummariesMu.Lock()
		if sess.Summaries == nil {
			sess.Summaries = make(map[string]*session.Summary)
		}
		for fk, sum := range dbSums {
			if _, exists := sess.Summaries[fk]; !exists {
				sess.Summaries[fk] = sum
			}
		}
		sess.SummariesMu.Unlock()
	}

	return sess, nil
}

// ---------------------------------------------------------------------------
// ListSessions
// ---------------------------------------------------------------------------

func (s *SessionStore) ListSessions(
	ctx context.Context,
	userKey session.UserKey,
	opts ...session.Option,
) ([]*session.Session, error) {
	if err := userKey.CheckUserKey(); err != nil {
		return nil, err
	}

	var rows []SessionRow
	if err := s.db.WithContext(ctx).
		Where("app_name = ? AND user_id = ?", userKey.AppName, userKey.UserID).
		Order("updated_at DESC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	for _, row := range rows {
		if err := s.ensureLoaded(ctx, session.Key{
			AppName:   row.AppName,
			UserID:    row.UserID,
			SessionID: row.SessionID,
		}); err != nil {
			return nil, err
		}
	}

	return s.mem.ListSessions(ctx, userKey, opts...)
}

// ---------------------------------------------------------------------------
// DeleteSession
// ---------------------------------------------------------------------------

func (s *SessionStore) DeleteSession(
	ctx context.Context,
	key session.Key,
	opts ...session.Option,
) error {
	if err := key.CheckSessionKey(); err != nil {
		return err
	}

	// Delete from inmemory.
	if err := s.mem.DeleteSession(ctx, key, opts...); err != nil {
		return err
	}

	// Delete from DB.
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		where := "app_name = ? AND user_id = ? AND session_id = ?"
		args := []interface{}{key.AppName, key.UserID, key.SessionID}

		if err := tx.Where(where, args...).Delete(&SessionEvent{}).Error; err != nil {
			return err
		}
		if err := tx.Where(where, args...).Delete(&SessionState{}).Error; err != nil {
			return err
		}
		if err := tx.Where(where, args...).Delete(&SessionSummaryRow{}).Error; err != nil {
			return err
		}
		return tx.Where(where, args...).Delete(&SessionRow{}).Error
	})

	sk := sessionCacheKey(key)
	s.loadMu.Lock()
	delete(s.loaded, sk)
	s.loadMu.Unlock()

	s.dbEventsMu.Lock()
	delete(s.dbEvents, sk)
	s.dbEventsMu.Unlock()

	s.dbSummariesMu.Lock()
	delete(s.dbSummaries, sk)
	s.dbSummariesMu.Unlock()

	return err
}

// ---------------------------------------------------------------------------
// AppendEvent
// ---------------------------------------------------------------------------

func (s *SessionStore) AppendEvent(
	ctx context.Context,
	sess *session.Session,
	evt *event.Event,
	opts ...session.Option,
) error {
	if sess == nil {
		return session.ErrNilSession
	}
	key := session.Key{AppName: sess.AppName, UserID: sess.UserID, SessionID: sess.ID}
	if err := key.CheckSessionKey(); err != nil {
		return err
	}

	// Append to inmemory (handles UpdateUserSession, state delta, etc.).
	if err := s.mem.AppendEvent(ctx, sess, evt, opts...); err != nil {
		return err
	}

	// Only persist meaningful events to DB.
	if evt.Response == nil || evt.IsPartial || !evt.IsValidContent() {
		return s.persistStateDelta(ctx, key, evt)
	}

	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if err := s.db.WithContext(ctx).Create(&SessionEvent{
		AppName:   key.AppName,
		UserID:    key.UserID,
		SessionID: key.SessionID,
		EventData: string(data),
		CreatedAt: now,
	}).Error; err != nil {
		return fmt.Errorf("persist event: %w", err)
	}

	// Update the in-memory DB events cache so GetSession can merge it
	// even within the same store lifecycle (before any restart).
	sk := sessionCacheKey(key)
	s.dbEventsMu.Lock()
	s.dbEvents[sk] = append(s.dbEvents[sk], *evt)
	// Prune from cache if limit is set.
	if s.opts.sessionEventLimit > 0 && len(s.dbEvents[sk]) > s.opts.sessionEventLimit {
		s.dbEvents[sk] = s.dbEvents[sk][len(s.dbEvents[sk])-s.opts.sessionEventLimit:]
	}
	s.dbEventsMu.Unlock()

	// Prune old events from DB if limit is set.
	if s.opts.sessionEventLimit > 0 {
		if err := s.pruneEvents(ctx, key); err != nil {
			return err
		}
	}

	// Persist state delta.
	if err := s.persistStateDelta(ctx, key, evt); err != nil {
		return err
	}

	// Update session timestamp.
	return s.db.WithContext(ctx).Model(&SessionRow{}).
		Where("app_name = ? AND user_id = ? AND session_id = ?",
			key.AppName, key.UserID, key.SessionID).
		Update("updated_at", now).Error
}

// ---------------------------------------------------------------------------
// State Management — delegates to inmemory and persists to DB
// ---------------------------------------------------------------------------

func (s *SessionStore) UpdateAppState(ctx context.Context, appName string, state session.StateMap) error {
	if appName == "" {
		return session.ErrAppNameRequired
	}
	if err := s.mem.UpdateAppState(ctx, appName, state); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for k, v := range state {
		k = strings.TrimPrefix(k, session.StateAppPrefix)
		row := AppState{AppName: appName, Key: k, Value: v, UpdatedAt: now}
		if err := s.db.WithContext(ctx).
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "app_name"}, {Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
			}).Create(&row).Error; err != nil {
			return fmt.Errorf("persist app state: %w", err)
		}
	}
	return nil
}

func (s *SessionStore) DeleteAppState(ctx context.Context, appName string, key string) error {
	if appName == "" {
		return session.ErrAppNameRequired
	}
	if err := s.mem.DeleteAppState(ctx, appName, key); err != nil {
		return err
	}
	key = strings.TrimPrefix(key, session.StateAppPrefix)
	return s.db.WithContext(ctx).
		Where("app_name = ? AND key = ?", appName, key).
		Delete(&AppState{}).Error
}

func (s *SessionStore) ListAppStates(ctx context.Context, appName string) (session.StateMap, error) {
	if appName == "" {
		return nil, session.ErrAppNameRequired
	}
	var rows []AppState
	if err := s.db.WithContext(ctx).
		Where("app_name = ?", appName).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list app states: %w", err)
	}
	state := make(session.StateMap, len(rows))
	for _, r := range rows {
		val := make([]byte, len(r.Value))
		copy(val, r.Value)
		state[r.Key] = val
	}
	return state, nil
}

func (s *SessionStore) UpdateUserState(ctx context.Context, userKey session.UserKey, state session.StateMap) error {
	if err := userKey.CheckUserKey(); err != nil {
		return err
	}
	if err := s.mem.UpdateUserState(ctx, userKey, state); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for k, v := range state {
		k = strings.TrimPrefix(k, session.StateUserPrefix)
		row := UserState{AppName: userKey.AppName, UserID: userKey.UserID, Key: k, Value: v, UpdatedAt: now}
		if err := s.db.WithContext(ctx).
			Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "app_name"}, {Name: "user_id"}, {Name: "key"}},
				DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
			}).Create(&row).Error; err != nil {
			return fmt.Errorf("persist user state: %w", err)
		}
	}
	return nil
}

func (s *SessionStore) ListUserStates(ctx context.Context, userKey session.UserKey) (session.StateMap, error) {
	if err := userKey.CheckUserKey(); err != nil {
		return nil, err
	}
	var rows []UserState
	if err := s.db.WithContext(ctx).
		Where("app_name = ? AND user_id = ?", userKey.AppName, userKey.UserID).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list user states: %w", err)
	}
	state := make(session.StateMap, len(rows))
	for _, r := range rows {
		val := make([]byte, len(r.Value))
		copy(val, r.Value)
		state[r.Key] = val
	}
	return state, nil
}

func (s *SessionStore) DeleteUserState(ctx context.Context, userKey session.UserKey, key string) error {
	if err := userKey.CheckUserKey(); err != nil {
		return err
	}
	if err := s.mem.DeleteUserState(ctx, userKey, key); err != nil {
		return err
	}
	key = strings.TrimPrefix(key, session.StateUserPrefix)
	return s.db.WithContext(ctx).
		Where("app_name = ? AND user_id = ? AND key = ?",
			userKey.AppName, userKey.UserID, key).
		Delete(&UserState{}).Error
}

func (s *SessionStore) UpdateSessionState(ctx context.Context, key session.Key, state session.StateMap) error {
	if err := key.CheckSessionKey(); err != nil {
		return err
	}
	if err := s.ensureLoaded(ctx, key); err != nil {
		return err
	}
	if err := s.mem.UpdateSessionState(ctx, key, state); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	for k, v := range state {
		if err := s.upsertSessionState(ctx, key, k, v); err != nil {
			return err
		}
	}
	return s.db.WithContext(ctx).Model(&SessionRow{}).
		Where("app_name = ? AND user_id = ? AND session_id = ?",
			key.AppName, key.UserID, key.SessionID).
		Update("updated_at", time.Now()).Error
}

// ---------------------------------------------------------------------------
// Summary — delegates to inmemory and persists results to DB
// ---------------------------------------------------------------------------

func (s *SessionStore) CreateSessionSummary(ctx context.Context, sess *session.Session, filterKey string, force bool) error {
	if err := s.mem.CreateSessionSummary(ctx, sess, filterKey, force); err != nil {
		return err
	}
	if sess != nil {
		key := session.Key{AppName: sess.AppName, UserID: sess.UserID, SessionID: sess.ID}
		sess.SummariesMu.RLock()
		sum := sess.Summaries[filterKey]
		sess.SummariesMu.RUnlock()
		if sum != nil {
			return s.persistSummary(ctx, key, filterKey, sum)
		}
	}
	return nil
}

func (s *SessionStore) EnqueueSummaryJob(ctx context.Context, sess *session.Session, filterKey string, force bool) error {
	if err := s.mem.EnqueueSummaryJob(ctx, sess, filterKey, force); err != nil {
		return err
	}
	if sess != nil {
		key := session.Key{AppName: sess.AppName, UserID: sess.UserID, SessionID: sess.ID}
		sess.SummariesMu.RLock()
		sum := sess.Summaries[filterKey]
		sess.SummariesMu.RUnlock()
		if sum != nil {
			return s.persistSummary(ctx, key, filterKey, sum)
		}
	}
	return nil
}

func (s *SessionStore) GetSessionSummaryText(ctx context.Context, sess *session.Session, opts ...session.SummaryOption) (string, bool) {
	return s.mem.GetSessionSummaryText(ctx, sess, opts...)
}

// Close stops the inmemory service. DB lifecycle is managed externally.
func (s *SessionStore) Close() error {
	return s.mem.Close()
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func (s *SessionStore) upsertSessionState(ctx context.Context, key session.Key, k string, v []byte) error {
	row := SessionState{
		AppName:   key.AppName,
		UserID:    key.UserID,
		SessionID: key.SessionID,
		Key:       k,
		Value:     v,
		UpdatedAt: time.Now(),
	}
	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "app_name"}, {Name: "user_id"}, {Name: "session_id"}, {Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
		}).Create(&row).Error
}

func (s *SessionStore) loadEvents(ctx context.Context, key session.Key) ([]event.Event, error) {
	var rows []SessionEvent
	if err := s.db.WithContext(ctx).
		Where("app_name = ? AND user_id = ? AND session_id = ?",
			key.AppName, key.UserID, key.SessionID).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load events: %w", err)
	}

	events := make([]event.Event, 0, len(rows))
	for _, r := range rows {
		var ev event.Event
		if err := json.Unmarshal([]byte(r.EventData), &ev); err != nil {
			continue // Skip corrupt events.
		}
		events = append(events, ev)
	}
	return events, nil
}

func (s *SessionStore) loadSessionState(ctx context.Context, key session.Key) (session.StateMap, error) {
	var rows []SessionState
	if err := s.db.WithContext(ctx).
		Where("app_name = ? AND user_id = ? AND session_id = ?",
			key.AppName, key.UserID, key.SessionID).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load session state: %w", err)
	}
	state := make(session.StateMap, len(rows))
	for _, r := range rows {
		val := make([]byte, len(r.Value))
		copy(val, r.Value)
		state[r.Key] = val
	}
	return state, nil
}

func (s *SessionStore) loadSummaries(ctx context.Context, key session.Key) (map[string]*session.Summary, error) {
	var rows []SessionSummaryRow
	if err := s.db.WithContext(ctx).
		Where("app_name = ? AND user_id = ? AND session_id = ?",
			key.AppName, key.UserID, key.SessionID).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("load summaries: %w", err)
	}
	summaries := make(map[string]*session.Summary, len(rows))
	for _, r := range rows {
		var topics []string
		if r.Topics != "" && r.Topics != "[]" {
			_ = json.Unmarshal([]byte(r.Topics), &topics)
		}
		summaries[r.FilterKey] = &session.Summary{
			Summary:   r.Summary,
			Topics:    topics,
			UpdatedAt: r.UpdatedAt,
		}
	}
	return summaries, nil
}

func (s *SessionStore) persistStateDelta(ctx context.Context, key session.Key, evt *event.Event) error {
	if evt == nil || len(evt.StateDelta) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range evt.StateDelta {
		if err := s.upsertSessionState(ctx, key, k, v); err != nil {
			return fmt.Errorf("persist state delta: %w", err)
		}
	}
	return nil
}

func (s *SessionStore) persistSummary(ctx context.Context, key session.Key, filterKey string, sum *session.Summary) error {
	topicsJSON, _ := json.Marshal(sum.Topics)
	row := SessionSummaryRow{
		AppName:   key.AppName,
		UserID:    key.UserID,
		SessionID: key.SessionID,
		FilterKey: filterKey,
		Summary:   sum.Summary,
		Topics:    string(topicsJSON),
		UpdatedAt: sum.UpdatedAt,
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "app_name"}, {Name: "user_id"}, {Name: "session_id"}, {Name: "filter_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"summary", "topics", "updated_at"}),
		}).Create(&row).Error
}

func (s *SessionStore) pruneEvents(ctx context.Context, key session.Key) error {
	if s.opts.sessionEventLimit <= 0 {
		return nil
	}

	var count int64
	if err := s.db.WithContext(ctx).Model(&SessionEvent{}).
		Where("app_name = ? AND user_id = ? AND session_id = ?",
			key.AppName, key.UserID, key.SessionID).
		Count(&count).Error; err != nil {
		return fmt.Errorf("count events: %w", err)
	}

	excess := int(count) - s.opts.sessionEventLimit
	if excess <= 0 {
		return nil
	}

	subQuery := s.db.Model(&SessionEvent{}).
		Select("id").
		Where("app_name = ? AND user_id = ? AND session_id = ?",
			key.AppName, key.UserID, key.SessionID).
		Order("id ASC").
		Limit(excess)

	return s.db.WithContext(ctx).
		Where("id IN (?)", subQuery).
		Delete(&SessionEvent{}).Error
}
