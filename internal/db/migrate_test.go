package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// TestSelfReviewMovesIntoProduction checks 00032: a pipeline's
// self_review column becomes production.selfReview (1, the old default,
// becomes absent; 0, off, becomes -1), and rolling back restores it.
func TestSelfReviewMovesIntoProduction(t *testing.T) {
	ctx := context.Background()
	d, err := Open(ctx, filepath.Join(t.TempDir(), "kurgu.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	p, err := provider(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 31); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Writer.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO workspaces (id, name, timezone, created_at, updated_at) VALUES ('w', 'W', 'UTC', 0, 0)`)
	tests := []struct {
		id         string
		selfReview int
		production any // nil for NULL
		want       sql.NullString
	}{
		{"default", 1, nil, sql.NullString{}},
		{"default with settings", 1, `{"thumbnails":2}`, sql.NullString{String: `{"thumbnails":2}`, Valid: true}},
		{"off", 0, nil, sql.NullString{String: `{"selfReview":-1}`, Valid: true}},
		{"three with settings", 3, `{"reviewAt":"script"}`, sql.NullString{String: `{"reviewAt":"script","selfReview":3}`, Valid: true}},
	}
	for _, tt := range tests {
		exec(`INSERT INTO pipelines (id, workspace_id, name, language, outputs, source, status, created_at, updated_at, self_review, production)
			VALUES (?, 'w', ?, 'tr', '{}', '{}', 'active', 0, 0, ?, ?)`, tt.id, tt.id, tt.selfReview, tt.production)
	}
	if err := MigrateUp(ctx, d); err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		var got sql.NullString
		if err := d.Writer.QueryRowContext(ctx, `SELECT production FROM pipelines WHERE id = ?`, tt.id).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("%s: production = %+v, want %+v", tt.id, got, tt.want)
		}
	}
	if _, err := p.DownTo(ctx, 31); err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		var (
			rounds int
			got    sql.NullString
		)
		if err := d.Writer.QueryRowContext(ctx, `SELECT self_review, production FROM pipelines WHERE id = ?`, tt.id).Scan(&rounds, &got); err != nil {
			t.Fatal(err)
		}
		want := sql.NullString{}
		if s, ok := tt.production.(string); ok {
			want = sql.NullString{String: s, Valid: true}
		}
		if rounds != tt.selfReview || got != want {
			t.Errorf("%s after down: self_review %d production %+v, want %d %+v", tt.id, rounds, got, tt.selfReview, want)
		}
	}
}
