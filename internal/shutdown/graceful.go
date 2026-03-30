package shutdown

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

const DefaultTimeout = 30 * time.Second

// Closer is anything that needs cleanup on shutdown.
type Closer interface {
	Close() error
}

// NamedCloser pairs a Closer with a name for logging.
type NamedCloser struct {
	Name   string
	Closer Closer
}

// Manager coordinates graceful shutdown of all components.
type Manager struct {
	timeout time.Duration
	closers []NamedCloser
	mu      sync.Mutex
}

func NewManager(timeout time.Duration) *Manager {
	return &Manager{timeout: timeout}
}

// Register adds a resource to be closed on shutdown. Resources are closed
// in reverse registration order (LIFO) - database connections close after
// HTTP server stops accepting, so in-flight requests can finish their queries.
func (m *Manager) Register(name string, c Closer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closers = append(m.closers, NamedCloser{Name: name, Closer: c})
}

// Wait blocks until SIGTERM or SIGINT, then shuts down all registered
// resources within the timeout. Returns an error if shutdown exceeds timeout.
func (m *Manager) Wait(server *http.Server) error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	sig := <-quit
	slog.Info("shutdown signal received", "signal", sig.String())

	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()

	// 1. Stop accepting new HTTP connections, finish in-flight requests
	slog.Info("draining HTTP connections")
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("HTTP server shutdown error", "error", err)
	}

	// 2. Close resources in reverse order (LIFO)
	m.mu.Lock()
	closers := make([]NamedCloser, len(m.closers))
	copy(closers, m.closers)
	m.mu.Unlock()

	for i := len(closers) - 1; i >= 0; i-- {
		nc := closers[i]
		slog.Info("closing resource", "name", nc.Name)
		if err := nc.Closer.Close(); err != nil {
			slog.Error("failed to close resource", "name", nc.Name, "error", err)
		}
	}

	slog.Info("shutdown complete")
	return ctx.Err()
}
