// Copyright (C) 2026 StackGen, Inc. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/adhocore/gronx"
	"github.com/google/uuid"
	"github.com/stackgenhq/genie/pkg/agui"
	"github.com/stackgenhq/genie/pkg/db"
	"github.com/stackgenhq/genie/pkg/logger"
)

//go:generate go tool counterfeiter -generate

//counterfeiter:generate . IScheduler
type IScheduler interface {
	agui.BGWorker
	Stop() error
}

// EventDispatcher is a function that dispatches a cron event for processing.
// Returns a run ID and an error. This decouples the cron scheduler from
// the concrete BackgroundWorker (and thus pkg/agui), avoiding import cycles.
type EventDispatcher func(ctx context.Context, req agui.EventRequest) (string, error)

// Scheduler is a simple DB-polling ticker that checks every minute for cron
// tasks whose NextRunAt has passed. It dispatches due tasks via EventDispatcher,
// records execution history, and computes the next run time.
//
// This approach avoids an in-memory cron scheduler: the database is the single
// source of truth, making it naturally restart-safe.
type Scheduler struct {
	store          ICronStore
	dispatcher     EventDispatcher
	cfg            Config
	mu             sync.Mutex
	running        bool
	dispatching    bool // guards against overlapping checkAndDispatch calls
	stopCh         chan struct{}
	cancelFn       context.CancelFunc // cancels the ticker loop context on Stop()
	tickerInterval time.Duration
	inFlight       sync.Map       // per-task in-flight guard (taskID → struct{})
	wg             sync.WaitGroup // tracks in-flight executeAndAdvance goroutines
}

// NewScheduler creates a Scheduler with the given store, configuration, and
// event dispatcher. The dispatcher is called for each cron task that becomes
// due, routing it through the background worker pipeline.
func (cfg Config) NewScheduler(
	store ICronStore,
	dispatcher EventDispatcher,
) *Scheduler {
	return &Scheduler{
		store:          store,
		dispatcher:     dispatcher,
		cfg:            cfg,
		tickerInterval: time.Minute,
	}
}

// Start upserts config-defined tasks, computes initial NextRunAt values,
// and begins the ticker loop that polls the DB every minute.
func (s *Scheduler) Start(ctx context.Context) error {
	logr := logger.GetLogger(ctx).With("fn", "cron.Scheduler.Start")

	if s.dispatcher == nil {
		return fmt.Errorf("cron scheduler requires a dispatcher but none is configured")
	}

	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("cron scheduler is already running")
	}
	s.stopCh = make(chan struct{})
	s.running = true
	s.mu.Unlock()

	// Upsert config-defined tasks into the database.
	for _, entry := range s.cfg.Tasks {
		if entry.Name == "" || entry.Expression == "" || entry.Action == "" {
			logr.Warn("Skipping invalid cron config entry", "name", entry.Name)
			continue
		}
		if _, err := s.store.CreateTask(ctx, CreateTaskRequest{
			Name:       entry.Name,
			Expression: entry.Expression,
			Action:     entry.Action,
			Source:     "config",
		}); err != nil {
			logr.Debug("Config cron task upsert", "name", entry.Name, "note", err)
		}
	}

	// Compute NextRunAt for any tasks that don't have it set.
	tasks, err := s.store.ListTasks(ctx, ListTasksRequest{EnabledOnly: true})
	if err != nil {
		return fmt.Errorf("failed to load cron tasks: %w", err)
	}
	now := time.Now()
	for _, task := range tasks {
		if task.NextRunAt == nil || task.NextRunAt.Before(now) {
			if nextErr := s.computeAndSetNextRun(ctx, task, now); nextErr != nil {
				logr.Warn("Failed to compute next run for task", "name", task.Name, "error", nextErr)
			}
		}
	}
	logr.Info("Cron scheduler started", "task_count", len(tasks))

	// Start the ticker goroutine with a dedicated cancellable context.
	// We derive from a background context but inject the caller's logger args
	// so structured logging works inside the ticker loop.
	loopCtx := logger.WithArgs(context.Background(), "fn", "cron.Scheduler.tickLoop")
	loopCtx, cancelFn := context.WithCancel(loopCtx)
	s.cancelFn = cancelFn
	go s.tickLoop(loopCtx)
	return nil
}

// tickLoop runs every minute, queries the DB for due tasks, dispatches them,
// records history, and computes the next run time. It also performs periodic
// history cleanup to prevent unbounded cron_history table growth.
func (s *Scheduler) tickLoop(ctx context.Context) {
	log := logger.GetLogger(ctx).With("fn", "cron.Scheduler.tickLoop")

	// Sleep until the next minute boundary so ticks align to :00 seconds.
	now := time.Now()
	nextMinute := now.Truncate(s.tickerInterval).Add(s.tickerInterval)
	select {
	case <-time.After(nextMinute.Sub(now)):
	case <-ctx.Done():
		return
	case <-s.stopCh:
		return
	}

	ticker := time.NewTicker(s.tickerInterval)
	defer ticker.Stop()

	// Cleanup old history entries every hour.
	cleanupTicker := time.NewTicker(time.Hour)
	defer cleanupTicker.Stop()

	// Run immediately at the first aligned minute.
	s.checkAndDispatch(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.checkAndDispatch(ctx)
		case <-cleanupTicker.C:
			if deleted, err := s.store.CleanupHistory(ctx, 30*24*time.Hour); err != nil {
				log.Warn("Failed to cleanup cron history", "error", err)
			} else if deleted > 0 {
				log.Info("Cleaned up old cron history entries", "deleted", deleted)
			}
		}
	}
}

// checkAndDispatch queries the DB for enabled tasks whose NextRunAt <= now,
// dispatches each one, and updates the next run time.
// If a previous dispatch cycle is still in-flight, the tick is skipped.
func (s *Scheduler) checkAndDispatch(ctx context.Context) {
	log := logger.GetLogger(ctx).With("fn", "cron.Scheduler.checkAndDispatch")

	// Skip if a previous cycle is still running to prevent double-dispatch.
	s.mu.Lock()
	if s.dispatching {
		s.mu.Unlock()
		log.Debug("Skipping tick, previous dispatch still in-flight")
		return
	}
	s.dispatching = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.dispatching = false
		s.mu.Unlock()
	}()

	dueTasks, err := s.store.DueTasks(ctx, time.Now())
	if err != nil {
		log.Warn("Failed to query due cron tasks", "error", err)
		return
	}

	var wg sync.WaitGroup
	for _, task := range dueTasks {
		// Per-task in-flight guard: skip tasks that are still executing
		// from a previous tick to prevent duplicate dispatch.
		taskKey := task.ID.String()
		if _, alreadyRunning := s.inFlight.Load(taskKey); alreadyRunning {
			log.Debug("Skipping task still in-flight from previous tick", "task", task.Name)
			continue
		}
		s.inFlight.Store(taskKey, struct{}{})

		// Check s.running under the mutex before calling s.wg.Add(1).
		// This prevents a data race where Stop()'s s.wg.Wait() runs
		// concurrently with s.wg.Add(1) — calling Add after Wait has
		// started is a WaitGroup misuse that triggers a race.
		s.mu.Lock()
		if !s.running {
			s.mu.Unlock()
			s.inFlight.Delete(taskKey)
			break
		}
		wg.Add(1)
		s.wg.Add(1)
		s.mu.Unlock()

		go func() {
			defer wg.Done()
			defer s.wg.Done()
			defer s.inFlight.Delete(taskKey)
			if err := s.executeAndAdvance(ctx, task); err != nil {
				log.Warn("failed to execute the task", "task", task.Action, "error", err)
			}
		}()
	}
	wg.Wait()
}

// executeAndAdvance dispatches a single task, records history, and advances NextRunAt.
func (s *Scheduler) executeAndAdvance(ctx context.Context, task CronTask) error {
	logr := logger.GetLogger(ctx).With("fn", "cron.Scheduler.executeAndAdvance", "task", task.Name)

	// Record the run start.
	history, err := s.store.RecordRun(ctx, RecordRunRequest{
		TaskID:   task.ID,
		TaskName: task.Name,
		Status:   db.CronStatusRunning,
	})
	if err != nil {
		return fmt.Errorf("error recording the run: %w", err)
	}

	// Dispatch.
	// Inject notify-on-failure instruction at dispatch-time (not persisted)
	// so it applies to all cron tasks and avoids duplication on upsert.
	dispatchAction := task.Action + "\n\nCRITICAL INSTRUCTION: If you are unable to successfully complete your assigned task or encounter blocking issues during this run, you MUST use the notify tool (if available) to alert the user."
	payload, _ := json.Marshal(map[string]string{
		"task_name":  task.Name,
		"action":     dispatchAction,
		"expression": task.Expression,
		"message":    fmt.Sprintf("Cron task [%s]: %s", task.Name, dispatchAction),
	})
	runID, dispatchErr := s.dispatcher(ctx, agui.EventRequest{
		Source:  "cron:" + task.Name,
		Payload: payload,
	})

	// Update history with dispatch outcome.
	// NOTE: This records whether the task was successfully *enqueued* to the
	// background worker, not whether the agent completed the task. Full
	// execution tracking would require a callback from the background worker.
	if history != nil {
		status := db.CronStatusSuccess
		errMsg := ""
		if dispatchErr != nil {
			status = db.CronStatusFailed
			errMsg = dispatchErr.Error()
			logr.Warn("Cron task dispatch failed", "name", task.Name, "error", dispatchErr)
		}
		if updateErr := s.store.UpdateRun(ctx, UpdateRunRequest{
			HistoryID: history.ID,
			Status:    status,
			Error:     errMsg,
			RunID:     runID,
		}); updateErr != nil {
			logr.Warn("Failed to update cron run history", "error", updateErr)
		}
	}

	// Advance NextRunAt.
	now := time.Now()
	if err := s.store.MarkTriggered(ctx, task.ID, now); err != nil {
		logr.Warn("Failed to mark task as triggered", "error", err)
	}
	if err := s.computeAndSetNextRun(ctx, task, now); err != nil {
		logr.Warn("Failed to advance next run", "error", err)
	}
	return nil
}

// computeAndSetNextRun parses the cron expression and updates NextRunAt in the DB.
// It uses `from` as the reference time so the next tick is always relative to the
// actual trigger time rather than wall-clock now.
func (s *Scheduler) computeAndSetNextRun(ctx context.Context, task CronTask, from time.Time) error {
	nextTick, err := gronx.NextTickAfter(task.Expression, from, false)
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", task.Expression, err)
	}
	return s.store.SetNextRun(ctx, task.ID, nextTick)
}

// Stop gracefully shuts down the ticker and waits for in-flight task
// goroutines to complete. Safe to call multiple times.
func (s *Scheduler) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	close(s.stopCh)
	if s.cancelFn != nil {
		s.cancelFn() // cancel the ticker loop context so in-flight DB/dispatch calls unblock
	}
	s.running = false
	s.mu.Unlock()

	// Wait for in-flight executeAndAdvance goroutines to drain.
	s.wg.Wait()
	return nil
}

// HealthCheck queries the store for recent cron failures and returns a
// per-task health summary. Used by the heartbeat ticker to detect problems.
func (s *Scheduler) HealthCheck(ctx context.Context) []agui.HealthResult {
	logr := logger.GetLogger(ctx).With("fn", "cron.Scheduler.HealthCheck")

	failures, err := s.store.RecentFailures(ctx, RecentFailuresRequest{Limit: 50})
	if err != nil {
		logr.Warn("Failed to query cron failures for health check", "error", err)
		return nil
	}

	if len(failures) == 0 {
		return nil
	}

	type taskInfo struct {
		Name         string
		FailureCount int
		LastError    string
	}
	taskFailures := make(map[uuid.UUID]*taskInfo)
	for _, f := range failures {
		info, ok := taskFailures[f.TaskID]
		if !ok {
			info = &taskInfo{Name: f.TaskName}
			taskFailures[f.TaskID] = info
		}
		info.FailureCount++
		if info.LastError == "" {
			info.LastError = f.Error
		}
	}

	results := make([]agui.HealthResult, 0, len(taskFailures))
	for _, info := range taskFailures {
		results = append(results, agui.HealthResult{
			Name:         info.Name,
			Healthy:      false,
			LastError:    info.LastError,
			FailureCount: info.FailureCount,
		})
	}
	return results
}
