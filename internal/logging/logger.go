package logging

import (
	"context"
	"log/slog"
	"os"
)

type contextKey string

const (
	RequestIDKey contextKey = "request_id"
	WorkspaceKey contextKey = "workspace_slug"
	UserIDKey    contextKey = "user_id"
)

// ContextHandler wraps slog.Handler to automatically extract values
// from context and include them in every log line.
type ContextHandler struct {
	inner slog.Handler
}

func NewContextHandler(inner slog.Handler) *ContextHandler {
	return &ContextHandler{inner: inner}
}

func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if requestID, ok := ctx.Value(RequestIDKey).(string); ok {
		r.AddAttrs(slog.String("request_id", requestID))
	}
	if workspace, ok := ctx.Value(WorkspaceKey).(string); ok {
		r.AddAttrs(slog.String("workspace", workspace))
	}
	if userID, ok := ctx.Value(UserIDKey).(string); ok {
		r.AddAttrs(slog.String("user_id", userID))
	}
	return h.inner.Handle(ctx, r)
}

func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return NewContextHandler(h.inner.WithAttrs(attrs))
}

func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return NewContextHandler(h.inner.WithGroup(name))
}

// New creates a structured logger. JSON in production, text in development.
func New(env string) *slog.Logger {
	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	if env == "development" {
		opts.Level = slog.LevelDebug
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	return slog.New(NewContextHandler(handler))
}
