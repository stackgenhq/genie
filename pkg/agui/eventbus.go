package agui

import (
	"context"
	"fmt"
	"sync"

	"github.com/stackgenhq/genie/pkg/logger"
	"github.com/stackgenhq/genie/pkg/messenger"
)

// eventBus is a singleton registry that maps MessageOrigin → event channel.
// It allows any code with a context (containing a MessageOrigin) to emit
// events to the correct SSE stream without explicitly passing channels
// through every struct and method in the call chain.
//
// This follows the per-session channel registry pattern commonly used in
// Go SSE/WebSocket servers: a sync.Map keyed by session ID, with
// Register/Deregister lifecycle methods at the request boundary.
type eventBus struct {
	channels sync.Map // map[string]chan<- interface{}
}

// globalBus is the package-level singleton. Initialized eagerly since
// sync.Map requires no constructor.
var globalBus = &eventBus{}

// suppressEmitKey is the context key for suppressing AG-UI emissions.
// Used when handling front-desk responses (salutation, refuse, out-of-scope)
// so those messages are not shown in the chat panel.
var suppressEmitKey = &struct{ name string }{"agui_suppress_emit"}

// WithSuppressEmit returns a context that causes Emit to be a no-op for that
// subtree. Use this when generating front-desk responses (e.g. salutation)
// that should not appear in the AG-UI chat panel.
func WithSuppressEmit(ctx context.Context) context.Context {
	return context.WithValue(ctx, suppressEmitKey, true)
}

// IsSuppressEmit reports whether the context has suppress-emit set.
func IsSuppressEmit(ctx context.Context) bool {
	v, _ := ctx.Value(suppressEmitKey).(bool)
	return v
}

// Register stores the event channel for a request's MessageOrigin.
// Must be called at the request entry point (e.g. AG-UI handleRun).
// The origin string is used as the map key for O(1) lookup.
func Register(origin messenger.MessageOrigin, ch chan<- interface{}) {
	globalBus.channels.Store(origin.String(), ch)
}

// Deregister removes the channel for a MessageOrigin.
// Must be called (typically via defer) when the request completes,
// before the eventChan is closed, to prevent sends to a closed channel.
func Deregister(origin messenger.MessageOrigin) {
	globalBus.channels.Delete(origin.String())
}

// Emit sends an event to the channel registered for the context's
// MessageOrigin. If no channel is registered (e.g. background tasks,
// non-AGUI messengers), the event is silently dropped.
//
// This is the primary API. Any code that has a context.Context can
// emit events without needing an explicit channel reference.
func Emit(ctx context.Context, event interface{}) {
	if IsSuppressEmit(ctx) {
		return
	}
	ch := ChannelFor(ctx)
	if ch == nil {
		return
	}
	select {
	case ch <- event:
	case <-ctx.Done():
	default:
		logger.GetLogger(ctx).Warn("agui event dropped (channel full)",
			"eventType", typeNameOf(event))
	}
}

// ChannelFor returns the event channel for the context's MessageOrigin,
// or nil if none is registered. Useful when callers need the raw channel
// (e.g. for blocking sends without the default drop behavior).
func ChannelFor(ctx context.Context) chan<- interface{} {
	origin := messenger.MessageOriginFrom(ctx)
	if origin.IsZero() {
		return nil
	}
	if v, ok := globalBus.channels.Load(origin.String()); ok {
		return v.(chan<- interface{})
	}
	return nil
}

// typeNameOf returns a short type description for logging.
func typeNameOf(v interface{}) string {
	if v == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%T", v)
}
