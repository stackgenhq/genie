package ttlcache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/stackgenhq/genie/pkg/logger"
)

// ValueRetriever is a function type used to retrieve or refresh cached values.
// It is called by the cache to obtain the value for an item, and may be invoked
// multiple times throughout the cache item's lifetime, such as when the value
// expires or needs to be refreshed.
type ValueRetriever[V any] func(context.Context) (V, error)

type Item[V any] struct {
	retriever    ValueRetriever[V]
	currentValue *V
	ttlDuration  time.Duration
	expiresAt    time.Time
	lock         sync.RWMutex
}

func NewItem[V any](retriever ValueRetriever[V], duration time.Duration) *Item[V] {
	return &Item[V]{
		retriever:   retriever,
		ttlDuration: duration,
		expiresAt:   time.Time{},
	}
}

func (i *Item[V]) isExpired() bool {
	return time.Now().After(i.expiresAt)
}

func (i *Item[V]) GetValue(ctx context.Context) (V, error) {
	return i.GetValueWithOptions(ctx, GetOption{})
}

type GetOption struct {
	ForceRefresh bool
}

func (i *Item[V]) GetValueWithOptions(ctx context.Context, opts GetOption) (V, error) {
	i.lock.RLock()
	if !opts.ForceRefresh && i.currentValue != nil && !i.isExpired() {
		// Copy the value while holding the lock to avoid race condition
		value := *i.currentValue
		i.lock.RUnlock()
		return value, nil
	}
	i.lock.RUnlock()

	return i.loadValue(ctx, opts)
}

func (i *Item[V]) loadValue(ctx context.Context, opts GetOption) (V, error) {
	// Acquire write lock to ensure only one goroutine refreshes the value
	i.lock.Lock()
	defer i.lock.Unlock()

	// Double-check if value is still expired after acquiring write lock
	// Another goroutine might have already refreshed it
	if !opts.ForceRefresh && i.currentValue != nil && !i.isExpired() {
		return *i.currentValue, nil
	}

	// Value is still expired or nil, so refresh it
	return i.refreshValue(ctx)
}

func (i *Item[V]) refreshValue(ctx context.Context) (V, error) {
	value, err := i.retriever(ctx)
	if err != nil {
		var zero V
		return zero, err
	}
	i.currentValue = &value
	i.expiresAt = time.Now().Add(i.ttlDuration)
	return *i.currentValue, nil
}

func (i *Item[V]) KeepItFresh(ctx context.Context) error {
	interval := i.ttlDuration / 2
	if interval <= 0 {
		return fmt.Errorf("ttlcache: ttlDuration too small for periodic refresh (%s)", i.ttlDuration)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	logger := logger.GetLogger(ctx).With("fn", "ttlcache.Item.KeepItFresh")
	logger.Info("starting periodic refresh of cache item", "interval", (i.ttlDuration / 2).String())

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			_, err := i.GetValueWithOptions(ctx, GetOption{
				ForceRefresh: true,
			})
			if err != nil {
				logger.Error("failed to refresh cache item", "error", err)
				continue
			}
			logger.Debug("successfully refreshed cache item")
		}
	}
}
