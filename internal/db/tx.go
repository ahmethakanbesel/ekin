package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

// Querier is the subset of database/sql that both *sql.DB and *sql.Tx
// satisfy, so repository methods work the same inside and outside a
// transaction.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// TxManager runs fn within a transactional boundary. Nested calls join
// the outer transaction.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// QuerierSource hands a repository the querier for a context: the active
// transaction when there is one, otherwise the writer (W) or reader (R)
// pool.
type QuerierSource interface {
	W(ctx context.Context) Querier
	R(ctx context.Context) Querier
}

type txKey struct{}

// SQLiteTxManager is the production TxManager and QuerierSource.
type SQLiteTxManager struct {
	db *DB
}

var (
	_ TxManager     = (*SQLiteTxManager)(nil)
	_ QuerierSource = (*SQLiteTxManager)(nil)
)

// NewTxManager returns a TxManager over d.
func NewTxManager(d *DB) *SQLiteTxManager {
	return &SQLiteTxManager{db: d}
}

// WithinTx begins a transaction on the writer, runs fn with it stashed in
// the context, and commits unless fn returns an error or panics.
func (m *SQLiteTxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if _, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return fn(ctx)
	}
	tx, err := m.db.Writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// W returns the active transaction or the writer pool.
func (m *SQLiteTxManager) W(ctx context.Context) Querier {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return tx
	}
	return m.db.Writer
}

// R returns the active transaction (so a transaction reads its own
// writes) or the reader pool.
func (m *SQLiteTxManager) R(ctx context.Context) Querier {
	if tx, ok := ctx.Value(txKey{}).(*sql.Tx); ok {
		return tx
	}
	return m.db.Reader
}

// MemTxManager serialises WithinTx calls with one mutex. It backs the
// in-memory repositories in service tests: crude, but callers observe
// serialisable behaviour. It does not roll back.
type MemTxManager struct {
	mu sync.Mutex
}

type memTxKey struct{}

// WithinTx runs fn under the mutex, joining an outer call.
func (m *MemTxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if ctx.Value(memTxKey{}) != nil {
		return fn(ctx)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn(context.WithValue(ctx, memTxKey{}, true))
}
