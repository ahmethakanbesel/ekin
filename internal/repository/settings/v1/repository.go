// Package settings persists instance settings as JSON documents.
package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	"ekin/internal/db"
	"ekin/pkg/timeutil"
)

// Repository stores JSON documents by key.
type Repository interface {
	// Get decodes the document at key into v; NotFound if absent.
	Get(ctx context.Context, key string, v any) error
	Put(ctx context.Context, key string, v any, by uuid.UUID, now time.Time) error
	// Keys lists keys with prefix.
	Keys(ctx context.Context, prefix string) ([]string, error)
}

// SQLite is the production Repository.
type SQLite struct{ q db.QuerierSource }

// NewSQLite returns a Repository over q.
func NewSQLite(q db.QuerierSource) *SQLite { return &SQLite{q: q} }

func (r *SQLite) Get(ctx context.Context, key string, v any) error {
	var raw string
	err := r.q.R(ctx).QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return apperror.New(apperror.NotFound, "no setting "+key)
	}
	if err != nil {
		return fmt.Errorf("get setting: %w", err)
	}
	return json.Unmarshal([]byte(raw), v)
}

func (r *SQLite) Put(ctx context.Context, key string, v any, by uuid.UUID, now time.Time) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var byID sql.NullString
	if by != uuid.Nil {
		byID = sql.NullString{String: by.String(), Valid: true}
	}
	_, err = r.q.W(ctx).ExecContext(ctx, `INSERT INTO settings (key, value, updated_by, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_by = excluded.updated_by, updated_at = excluded.updated_at`,
		key, string(raw), byID, timeutil.ToMillis(now))
	if err != nil {
		return fmt.Errorf("put setting: %w", err)
	}
	return nil
}

func (r *SQLite) Keys(ctx context.Context, prefix string) ([]string, error) {
	rows, err := r.q.R(ctx).QueryContext(ctx, `SELECT key FROM settings WHERE key >= ? AND key < ? ORDER BY key`, prefix, prefix+"￿")
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// InMemory is a Repository for tests.
type InMemory struct {
	mu   sync.Mutex
	docs map[string][]byte
}

// NewInMemory returns an empty repository.
func NewInMemory() *InMemory { return &InMemory{docs: map[string][]byte{}} }

func (r *InMemory) Get(_ context.Context, key string, v any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	raw, ok := r.docs[key]
	if !ok {
		return apperror.New(apperror.NotFound, "no setting "+key)
	}
	return json.Unmarshal(raw, v)
}

func (r *InMemory) Put(_ context.Context, key string, v any, _ uuid.UUID, _ time.Time) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.docs[key] = raw
	return nil
}

func (r *InMemory) Keys(_ context.Context, prefix string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for k := range r.docs {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}
