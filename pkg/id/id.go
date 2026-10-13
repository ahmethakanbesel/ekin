// Package id provides shared UUIDv7 generation for all entity IDs.
package id

import "github.com/google/uuid"

// New returns a new time-ordered UUIDv7.
func New() uuid.UUID { return uuid.Must(uuid.NewV7()) }
