package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

func provider(d *DB) (*goose.Provider, error) {
	fsys, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return nil, fmt.Errorf("migration files: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectSQLite3, d.Writer, fsys)
	if err != nil {
		return nil, fmt.Errorf("migration provider: %w", err)
	}
	return p, nil
}

// MigrateUp applies all pending migrations.
func MigrateUp(ctx context.Context, d *DB) error {
	p, err := provider(d)
	if err != nil {
		return err
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// MigrateDown rolls back the most recent migration.
func MigrateDown(ctx context.Context, d *DB) error {
	p, err := provider(d)
	if err != nil {
		return err
	}
	if _, err := p.Down(ctx); err != nil {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}

// MigrationStatus returns "version applied" lines for every migration.
func MigrationStatus(ctx context.Context, d *DB) ([]string, error) {
	p, err := provider(d)
	if err != nil {
		return nil, err
	}
	statuses, err := p.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("migration status: %w", err)
	}
	out := make([]string, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, fmt.Sprintf("%d %s %s", s.Source.Version, s.Source.Path, s.State))
	}
	return out, nil
}
