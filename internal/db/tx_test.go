package db_test

import (
	"context"
	"errors"
	"testing"

	"kurgu/internal/db/dbtest"
)

func TestWithinTx(t *testing.T) {
	ctx := context.Background()
	errBoom := errors.New("boom")

	tests := []struct {
		name      string
		fn        func(ctx context.Context, insert func(context.Context, string) error) error
		wantErr   error
		wantNames []string
	}{
		{
			name: "commit",
			fn: func(ctx context.Context, insert func(context.Context, string) error) error {
				return insert(ctx, "a")
			},
			wantNames: []string{"a"},
		},
		{
			name: "rollback on error",
			fn: func(ctx context.Context, insert func(context.Context, string) error) error {
				if err := insert(ctx, "a"); err != nil {
					return err
				}
				return errBoom
			},
			wantErr: errBoom,
		},
		{
			name: "nested call joins and rolls back with the outer",
			fn: func(ctx context.Context, insert func(context.Context, string) error) error {
				if err := insert(ctx, "outer"); err != nil {
					return err
				}
				return errBoom
			},
			wantErr: errBoom,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, tx := dbtest.Open(t)
			if _, err := tx.W(ctx).ExecContext(ctx, `CREATE TABLE t (name TEXT NOT NULL)`); err != nil {
				t.Fatal(err)
			}
			insert := func(ctx context.Context, name string) error {
				// A nested WithinTx must reuse the outer transaction, not
				// deadlock on the single writer connection.
				return tx.WithinTx(ctx, func(ctx context.Context) error {
					_, err := tx.W(ctx).ExecContext(ctx, `INSERT INTO t (name) VALUES (?)`, name)
					return err
				})
			}
			err := tx.WithinTx(ctx, func(ctx context.Context) error { return tt.fn(ctx, insert) })
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			rows, err := tx.R(ctx).QueryContext(ctx, `SELECT name FROM t ORDER BY name`)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = rows.Close() }()
			var got []string
			for rows.Next() {
				var n string
				if err := rows.Scan(&n); err != nil {
					t.Fatal(err)
				}
				got = append(got, n)
			}
			if len(got) != len(tt.wantNames) {
				t.Fatalf("rows = %v, want %v", got, tt.wantNames)
			}
		})
	}
}

// A snapshot's reads all see one committed state; reads outside one see
// each commit as it lands.
func TestSnapshotReadsOneState(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		snapshot bool
		want     [2]int // rows before and after a concurrent insert
	}{
		{name: "plain reads", want: [2]int{1, 2}},
		{name: "snapshot", snapshot: true, want: [2]int{1, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, tx := dbtest.Open(t)
			if _, err := tx.W(ctx).ExecContext(ctx, `CREATE TABLE t (n INTEGER); INSERT INTO t VALUES (1)`); err != nil {
				t.Fatal(err)
			}
			read := func(ctx context.Context) error {
				var got [2]int
				for i := range got {
					if err := tx.R(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM t`).Scan(&got[i]); err != nil {
						return err
					}
					if i == 0 {
						if _, err := tx.W(context.Background()).ExecContext(ctx, `INSERT INTO t VALUES (2)`); err != nil {
							return err
						}
					}
				}
				if got != tt.want {
					t.Fatalf("counts = %v, want %v", got, tt.want)
				}
				return nil
			}
			var err error
			if tt.snapshot {
				err = tx.Snapshot(ctx, read)
			} else {
				err = read(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
