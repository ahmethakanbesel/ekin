// Package dbtest opens a migrated SQLite database in a test's temp dir.
package dbtest

import (
	"context"
	"path/filepath"
	"testing"

	"kurgu/internal/db"
)

// Open returns a migrated database and its TxManager, closed at cleanup.
func Open(t testing.TB) (*db.DB, *db.SQLiteTxManager) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "kurgu.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.MigrateUp(ctx, d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return d, db.NewTxManager(d)
}
