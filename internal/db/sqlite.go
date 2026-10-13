// Package db opens the SQLite database, runs migrations and provides the
// transaction manager repositories are written against (CONVENTIONS.md §5).
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// readPoolSize bounds concurrent readers. WAL lets them run alongside the
// single writer; four is plenty for one API process and a worker.
const readPoolSize = 4

// DB holds the two pools over one SQLite file. Writer has exactly one
// connection, so every write and every transaction is serialised in
// process; BEGIN IMMEDIATE serialises across processes sharing the file.
type DB struct {
	Writer *sql.DB
	Reader *sql.DB
}

// Open opens (creating if needed) the database at path.
func Open(ctx context.Context, path string) (*DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	writer, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, fmt.Errorf("open writer: %w", err)
	}
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	writer.SetConnMaxLifetime(0)
	if err := writer.PingContext(ctx); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("ping writer: %w", err)
	}

	reader, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("open reader: %w", err)
	}
	reader.SetMaxOpenConns(readPoolSize)
	reader.SetMaxIdleConns(readPoolSize)
	if err := reader.PingContext(ctx); err != nil {
		_ = writer.Close()
		_ = reader.Close()
		return nil, fmt.Errorf("ping reader: %w", err)
	}
	return &DB{Writer: writer, Reader: reader}, nil
}

// Close closes both pools.
func (d *DB) Close() error {
	rerr := d.Reader.Close()
	if werr := d.Writer.Close(); werr != nil {
		return werr
	}
	return rerr
}

func dsn(path string, readOnly bool) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(ON)")
	q.Add("_pragma", "synchronous(NORMAL)")
	if readOnly {
		q.Add("_pragma", "query_only(ON)")
	} else {
		// IMMEDIATE takes the write lock at BEGIN, so two processes never
		// both read under a deferred transaction and then fail to upgrade.
		q.Set("_txlock", "immediate")
	}
	return "file:" + path + "?" + q.Encode()
}
