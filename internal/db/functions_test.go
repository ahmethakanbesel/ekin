package db_test

import (
	"context"
	"testing"

	"ekin/internal/db/dbtest"
)

func TestFoldFunction(t *testing.T) {
	ctx := context.Background()
	_, tx := dbtest.Open(t)
	tests := []struct {
		in   any
		want any
	}{
		{"Kaygı İLE Endişe", "kaygi ile endise"},
		{nil, nil},
	}
	for _, tt := range tests {
		var got any
		if err := tx.R(ctx).QueryRowContext(ctx, `SELECT fold(?)`, tt.in).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != tt.want {
			t.Errorf("fold(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
	// The writer has it too, for statements inside transactions.
	var got string
	if err := tx.W(ctx).QueryRowContext(ctx, `SELECT fold('Çocuk')`).Scan(&got); err != nil || got != "cocuk" {
		t.Fatalf("writer fold = %q, %v", got, err)
	}
}
