package logger

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"os"
	"sync"
)

const (
	errorKey = "error"
)

type ctxKey string

var (
	logKey = ctxKey("logKey")
)

var (
	logger *slog.Logger
	once   sync.Once
	level  slog.Level = slog.LevelInfo
)

func SetLogHandler(handler slog.Handler) {
	logger = slog.New(handler)
}

func IsDebugEnabled() bool {
	return level == slog.LevelDebug
}

func LogLevel() slog.Level {
	return level
}

func SetLogLevel(newLevel slog.Level) {
	level = newLevel
}

func SendJSONTo(w io.Writer) {
	SetLogHandler(slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	}))
}

func ErrorAttr(err error) slog.Attr {
	return slog.Any(errorKey, err)
}

func isTestMode() bool {
	return flag.Lookup("test.v") != nil
}

func initLogger() {
	if logger != nil {
		return
	}
	// if in test mode, send logs to /dev/null
	var w io.Writer = os.Stderr
	if isTestMode() {
		w = io.Discard
	}
	SendJSONTo(w)
}

func GetLogger(ctx context.Context) *slog.Logger {
	once.Do(initLogger)
	attrs := make([]any, 0, 1)
	if additionalArgs := getAdditionalArgs(ctx); additionalArgs != nil {
		attrs = append(attrs, additionalArgs...)
	}
	return logger.With(attrs...)
}

func getAdditionalArgs(ctx context.Context) []any {
	if ctx == nil {
		return nil
	}
	val := ctx.Value(logKey)
	if val == nil {
		return nil
	}
	if v, ok := val.([]any); ok {
		return v
	}
	return nil
}

func WithArgs(ctx context.Context, args ...any) context.Context {
	if len(args) == 0 {
		return ctx
	}
	keys := getAdditionalArgs(ctx)
	return context.WithValue(ctx, logKey, append(keys, args...))
}
