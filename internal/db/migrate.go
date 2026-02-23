package db

import (
	_ "embed"
	"fmt"
)

//go:embed migrations/001_init.sql
var schema string

func RunMigrations(d *DB) error {
	if _, err := d.pool.Exec(schema); err != nil {
		return fmt.Errorf("run schema migration: %w", err)
	}
	return nil
}
