package database

import (
	"database/sql"
	"errors"
	"time"
)

// UpsertHealthCheck inserts or replaces the latest health result for a tunnel.
func (s *Store) UpsertHealthCheck(h *HealthCheck) error {
	if h.CheckedAt.IsZero() {
		h.CheckedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(`
		INSERT INTO health_checks (tunnel_id, status, latency_ms, message, checked_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(tunnel_id) DO UPDATE SET
			status = excluded.status,
			latency_ms = excluded.latency_ms,
			message = excluded.message,
			checked_at = excluded.checked_at`,
		h.TunnelID, h.Status, h.LatencyMs, h.Message, formatTime(h.CheckedAt),
	)
	return err
}

const healthColumns = `tunnel_id, status, COALESCE(latency_ms,0), COALESCE(message,''), checked_at`

func scanHealth(sc interface{ Scan(...any) error }) (*HealthCheck, error) {
	var h HealthCheck
	var checkedAt string
	if err := sc.Scan(&h.TunnelID, &h.Status, &h.LatencyMs, &h.Message, &checkedAt); err != nil {
		return nil, err
	}
	var err error
	if h.CheckedAt, err = parseTime(checkedAt); err != nil {
		return nil, err
	}
	return &h, nil
}

// GetHealthCheck returns the latest health result for a tunnel, or ErrNotFound.
func (s *Store) GetHealthCheck(tunnelID string) (*HealthCheck, error) {
	row := s.db.QueryRow(`SELECT `+healthColumns+` FROM health_checks WHERE tunnel_id = ?`, tunnelID)
	h, err := scanHealth(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return h, err
}

// ListHealthChecks returns all health rows; if gatewayID is set, only the rows
// for tunnels belonging to that gateway are returned.
func (s *Store) ListHealthChecks(gatewayID string) ([]*HealthCheck, error) {
	var (
		query string
		args  []any
	)
	if gatewayID == "" {
		query = `SELECT ` + healthColumns + ` FROM health_checks ORDER BY checked_at DESC`
	} else {
		query = `SELECT h.tunnel_id, h.status, COALESCE(h.latency_ms,0), COALESCE(h.message,''), h.checked_at
			FROM health_checks h
			JOIN tunnels t ON t.id = h.tunnel_id
			WHERE t.gateway_id = ?
			ORDER BY h.checked_at DESC`
		args = append(args, gatewayID)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*HealthCheck
	for rows.Next() {
		h, err := scanHealth(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// CountUnhealthy returns the number of tunnels whose latest status is unhealthy.
func (s *Store) CountUnhealthy() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM health_checks WHERE status = 'unhealthy'`).Scan(&n)
	return n, err
}
