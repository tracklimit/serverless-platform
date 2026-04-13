package db

import (
	"errors"
	"io/fs"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	platform "serverless-platform"
)

// RunMigrations applies all pending migrations using the migration files
// embedded into the binary at build time (see migrations.go at the module
// root). The second argument is retained for API compatibility but is no
// longer used — callers may pass an empty string.
func RunMigrations(d *DB, _ string) error {
	sub, err := fs.Sub(platform.MigrationsFS, "migrations")
	if err != nil {
		return err
	}

	source, err := iofs.New(sub, ".")
	if err != nil {
		return err
	}
	// iofs is a pure in-memory wrapper around the embedded FS, so Close is
	// cheap. We own `source` and close it on every exit path.
	//
	// We intentionally do NOT close `driver` or call m.Close() — the
	// postgres driver created by WithInstance takes ownership of d.pool,
	// and closing it would tear down the connection pool that the rest of
	// the application still uses.
	defer func() { _ = source.Close() }()

	driver, err := postgres.WithInstance(d.pool, &postgres.Config{})
	if err != nil {
		return err
	}

	m, err := migrate.NewWithInstance("iofs", source, "postgres", driver)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}

	version, dirty, _ := m.Version()
	slog.Info("database migrations applied",
		"version", version,
		"dirty", dirty,
	)

	return nil
}
