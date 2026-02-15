package messenger

import "context"

type contextKey struct {
	name string
}

var senderContextKey = &contextKey{name: "sender_context"}

// WithSenderContext returns a new context with the given sender context string.
func WithSenderContext(ctx context.Context, senderContext string) context.Context {
	return context.WithValue(ctx, senderContextKey, senderContext)
}

// SenderContextFrom returns the sender context string from the context.
func SenderContextFrom(ctx context.Context) string {
	val, _ := ctx.Value(senderContextKey).(string)
	return val
}
