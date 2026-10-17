// Package publishing persists YouTube channels and blog targets.
package publishing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"ekin/internal/apperror"
	"ekin/internal/db"
	publishingdomain "ekin/internal/domain/publishing/v1"
	"ekin/pkg/timeutil"
)

// Repository stores one channel and one blog target per workspace.
type Repository interface {
	PutChannel(ctx context.Context, c publishingdomain.Channel) error
	// Channel returns NotFound when none is connected.
	Channel(ctx context.Context, workspaceID uuid.UUID) (publishingdomain.Channel, error)
	DeleteChannel(ctx context.Context, workspaceID uuid.UUID) error
	PutBlog(ctx context.Context, b publishingdomain.BlogTarget) error
	// Blog returns NotFound when none is configured.
	Blog(ctx context.Context, workspaceID uuid.UUID) (publishingdomain.BlogTarget, error)
	DeleteBlog(ctx context.Context, workspaceID uuid.UUID) error
}

// SQLite is the production Repository.
type SQLite struct{ q db.QuerierSource }

// NewSQLite returns a Repository over q.
func NewSQLite(q db.QuerierSource) *SQLite { return &SQLite{q: q} }

func (r *SQLite) PutChannel(ctx context.Context, c publishingdomain.Channel) error {
	var by sql.NullString
	if c.ConnectedBy != uuid.Nil {
		by = sql.NullString{String: c.ConnectedBy.String(), Valid: true}
	}
	_, err := r.q.W(ctx).ExecContext(ctx, `INSERT INTO youtube_channels
		(workspace_id, channel_id, title, sealed_token, privacy, connected_by, connected_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (workspace_id) DO UPDATE SET channel_id = excluded.channel_id, title = excluded.title,
		  sealed_token = excluded.sealed_token, privacy = excluded.privacy, connected_by = excluded.connected_by,
		  connected_at = excluded.connected_at, updated_at = excluded.updated_at`,
		c.WorkspaceID.String(), c.ChannelID, c.Title, c.SealedToken, string(c.Privacy), by,
		timeutil.ToMillis(c.ConnectedAt), timeutil.ToMillis(c.UpdatedAt))
	if err != nil {
		return fmt.Errorf("put channel: %w", err)
	}
	return nil
}

func (r *SQLite) Channel(ctx context.Context, workspaceID uuid.UUID) (publishingdomain.Channel, error) {
	var (
		c                  publishingdomain.Channel
		privacy            string
		by                 sql.NullString
		connected, updated int64
	)
	err := r.q.R(ctx).QueryRowContext(ctx, `SELECT channel_id, title, sealed_token, privacy, connected_by, connected_at, updated_at
		FROM youtube_channels WHERE workspace_id = ?`, workspaceID.String()).
		Scan(&c.ChannelID, &c.Title, &c.SealedToken, &privacy, &by, &connected, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return c, apperror.New(apperror.NotFound, "no YouTube channel connected")
	}
	if err != nil {
		return c, fmt.Errorf("get channel: %w", err)
	}
	c.WorkspaceID, c.Privacy = workspaceID, publishingdomain.Privacy(privacy)
	if by.Valid {
		c.ConnectedBy, _ = uuid.Parse(by.String)
	}
	c.ConnectedAt, c.UpdatedAt = timeutil.FromMillis(connected), timeutil.FromMillis(updated)
	return c, nil
}

func (r *SQLite) DeleteChannel(ctx context.Context, workspaceID uuid.UUID) error {
	_, err := r.q.W(ctx).ExecContext(ctx, `DELETE FROM youtube_channels WHERE workspace_id = ?`, workspaceID.String())
	return err
}

func (r *SQLite) PutBlog(ctx context.Context, b publishingdomain.BlogTarget) error {
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	_, err = r.q.W(ctx).ExecContext(ctx, `INSERT INTO blog_targets (workspace_id, config, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (workspace_id) DO UPDATE SET config = excluded.config, updated_at = excluded.updated_at`,
		b.WorkspaceID.String(), string(raw), timeutil.ToMillis(b.UpdatedAt))
	if err != nil {
		return fmt.Errorf("put blog target: %w", err)
	}
	return nil
}

func (r *SQLite) Blog(ctx context.Context, workspaceID uuid.UUID) (publishingdomain.BlogTarget, error) {
	var raw string
	err := r.q.R(ctx).QueryRowContext(ctx, `SELECT config FROM blog_targets WHERE workspace_id = ?`, workspaceID.String()).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return publishingdomain.BlogTarget{}, apperror.New(apperror.NotFound, "no blog target configured")
	}
	if err != nil {
		return publishingdomain.BlogTarget{}, fmt.Errorf("get blog target: %w", err)
	}
	var b publishingdomain.BlogTarget
	return b, json.Unmarshal([]byte(raw), &b)
}

func (r *SQLite) DeleteBlog(ctx context.Context, workspaceID uuid.UUID) error {
	_, err := r.q.W(ctx).ExecContext(ctx, `DELETE FROM blog_targets WHERE workspace_id = ?`, workspaceID.String())
	return err
}

// InMemory is a Repository for tests.
type InMemory struct {
	mu       sync.Mutex
	channels map[uuid.UUID]publishingdomain.Channel
	blogs    map[uuid.UUID]publishingdomain.BlogTarget
}

// NewInMemory returns an empty repository.
func NewInMemory() *InMemory {
	return &InMemory{channels: map[uuid.UUID]publishingdomain.Channel{}, blogs: map[uuid.UUID]publishingdomain.BlogTarget{}}
}

func (r *InMemory) PutChannel(_ context.Context, c publishingdomain.Channel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channels[c.WorkspaceID] = c
	return nil
}

func (r *InMemory) Channel(_ context.Context, ws uuid.UUID) (publishingdomain.Channel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.channels[ws]
	if !ok {
		return c, apperror.New(apperror.NotFound, "no YouTube channel connected")
	}
	return c, nil
}

func (r *InMemory) DeleteChannel(_ context.Context, ws uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.channels, ws)
	return nil
}

func (r *InMemory) PutBlog(_ context.Context, b publishingdomain.BlogTarget) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blogs[b.WorkspaceID] = b
	return nil
}

func (r *InMemory) Blog(_ context.Context, ws uuid.UUID) (publishingdomain.BlogTarget, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.blogs[ws]
	if !ok {
		return b, apperror.New(apperror.NotFound, "no blog target configured")
	}
	return b, nil
}

func (r *InMemory) DeleteBlog(_ context.Context, ws uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.blogs, ws)
	return nil
}
