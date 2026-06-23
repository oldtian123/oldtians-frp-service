// Package database wraps the SQLite store used by the control-api.
//
// It owns the schema (auto-applied on startup), the data models and the CRUD
// helpers for every table. Timestamps are persisted as RFC3339 UTC strings so
// that behaviour is identical across drivers and platforms.
package database

import (
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGO required)
)

//go:embed schema.sql
var schemaSQL string

// timeLayout is the canonical on-disk representation for all DATETIME columns.
const timeLayout = time.RFC3339

// OnlineThreshold is how long after the last heartbeat a gateway is still
// considered "online" by list/summary queries.
const OnlineThreshold = 60 * time.Second

// Store is a thin wrapper around *sql.DB exposing typed data-access helpers.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the SQLite database at path and applies the
// embedded schema. The caller is responsible for calling Close.
func Open(path string) (*Store, error) {
	// _pragma options enable WAL + foreign keys + a busy timeout so concurrent
	// readers/writers (HTTP handlers + background janitor) do not collide.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}

	// SQLite handles a single writer at a time; keep the pool small to avoid
	// "database is locked" churn while still allowing concurrent reads.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite %q: %w", path, err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// migrate applies the embedded schema. CREATE TABLE IF NOT EXISTS makes this
// idempotent and safe to run on every boot.
func (s *Store) migrate() error {
	if _, err := s.db.Exec(schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the raw handle for advanced callers (rarely needed).
func (s *Store) DB() *sql.DB { return s.db }

// --- timestamp helpers -------------------------------------------------------

func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(timeLayout, s)
}

// parseOptionalTime parses a possibly-empty timestamp into a *time.Time.
func parseOptionalTime(s string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
