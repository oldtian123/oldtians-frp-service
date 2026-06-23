package database

import (
	"database/sql"
	"errors"
	"time"
)

// EnsureConfigVersion creates the config_versions row for a gateway if missing,
// starting at version 1. Called during gateway registration.
func (s *Store) EnsureConfigVersion(gatewayID string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		INSERT INTO config_versions (gateway_id, version, updated_at)
		VALUES (?, 1, ?)
		ON CONFLICT(gateway_id) DO NOTHING`,
		gatewayID, formatTime(now),
	)
	return err
}

// GetConfigVersion returns the latest desired config version for a gateway.
// A gateway without a row yet is reported as version 0.
func (s *Store) GetConfigVersion(gatewayID string) (int64, error) {
	var v int64
	err := s.db.QueryRow(`SELECT version FROM config_versions WHERE gateway_id = ?`, gatewayID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return v, err
}

// BumpConfigVersion increments (and creates if missing) the gateway config
// version. Every tunnel mutation must call this so agents know to re-sync.
func (s *Store) BumpConfigVersion(gatewayID string) (int64, error) {
	now := time.Now().UTC()
	if _, err := s.db.Exec(`
		INSERT INTO config_versions (gateway_id, version, updated_at)
		VALUES (?, 1, ?)
		ON CONFLICT(gateway_id) DO UPDATE SET
			version = version + 1,
			updated_at = excluded.updated_at`,
		gatewayID, formatTime(now),
	); err != nil {
		return 0, err
	}
	return s.GetConfigVersion(gatewayID)
}
