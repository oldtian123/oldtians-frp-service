package database

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// CreateTunnel inserts a new tunnel. Timestamps are managed here.
func (s *Store) CreateTunnel(t *Tunnel) error {
	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now
	if t.AuthMode == "" {
		t.AuthMode = "none"
	}
	if t.Source == "" {
		t.Source = "manual"
	}
	_, err := s.db.Exec(`
		INSERT INTO tunnels (
			id, gateway_id, name, type, target_host, target_port,
			domain, remote_port, enabled, auth_mode, ttl_seconds,
			expires_at, source, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.GatewayID, t.Name, t.Type, t.TargetHost, t.TargetPort,
		nullString(t.Domain), nullInt(t.RemotePort), boolToInt(t.Enabled), t.AuthMode,
		nullInt64(t.TTLSeconds), nullTime(t.ExpiresAt), t.Source,
		formatTime(t.CreatedAt), formatTime(t.UpdatedAt),
	)
	return err
}

const tunnelColumns = `
	id, gateway_id, name, type, target_host, target_port,
	COALESCE(domain,''), COALESCE(remote_port,0), COALESCE(enabled,1),
	COALESCE(auth_mode,'none'), COALESCE(ttl_seconds,0),
	COALESCE(expires_at,''), COALESCE(source,'manual'),
	created_at, updated_at`

func scanTunnel(sc interface{ Scan(...any) error }) (*Tunnel, error) {
	var t Tunnel
	var enabled int
	var expiresAt, createdAt, updatedAt string
	if err := sc.Scan(
		&t.ID, &t.GatewayID, &t.Name, &t.Type, &t.TargetHost, &t.TargetPort,
		&t.Domain, &t.RemotePort, &enabled, &t.AuthMode, &t.TTLSeconds,
		&expiresAt, &t.Source, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	t.Enabled = enabled != 0
	var err error
	if t.ExpiresAt, err = parseOptionalTime(expiresAt); err != nil {
		return nil, err
	}
	if t.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if t.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

// GetTunnel returns a tunnel by id, or ErrNotFound.
func (s *Store) GetTunnel(id string) (*Tunnel, error) {
	row := s.db.QueryRow(`SELECT `+tunnelColumns+` FROM tunnels WHERE id = ?`, id)
	t, err := scanTunnel(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

// TunnelFilter narrows ListTunnels results. Nil pointers mean "no filter".
type TunnelFilter struct {
	GatewayID string
	Enabled   *bool
}

// ListTunnels returns tunnels matching the filter, newest first.
func (s *Store) ListTunnels(f TunnelFilter) ([]*Tunnel, error) {
	var (
		clauses []string
		args    []any
	)
	if f.GatewayID != "" {
		clauses = append(clauses, "gateway_id = ?")
		args = append(args, f.GatewayID)
	}
	if f.Enabled != nil {
		clauses = append(clauses, "enabled = ?")
		args = append(args, boolToInt(*f.Enabled))
	}
	query := `SELECT ` + tunnelColumns + ` FROM tunnels`
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Tunnel
	for rows.Next() {
		t, err := scanTunnel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TunnelUpdate carries the mutable fields of a tunnel. Nil pointers are ignored.
type TunnelUpdate struct {
	Enabled    *bool
	TargetHost *string
	TargetPort *int
	Domain     *string
	RemotePort *int
}

// UpdateTunnel applies a partial update and refreshes updated_at.
func (s *Store) UpdateTunnel(id string, u TunnelUpdate) (*Tunnel, error) {
	t, err := s.GetTunnel(id)
	if err != nil {
		return nil, err
	}
	if u.Enabled != nil {
		t.Enabled = *u.Enabled
	}
	if u.TargetHost != nil {
		t.TargetHost = *u.TargetHost
	}
	if u.TargetPort != nil {
		t.TargetPort = *u.TargetPort
	}
	if u.Domain != nil {
		t.Domain = *u.Domain
	}
	if u.RemotePort != nil {
		t.RemotePort = *u.RemotePort
	}
	t.UpdatedAt = time.Now().UTC()

	_, err = s.db.Exec(`
		UPDATE tunnels SET
			target_host = ?, target_port = ?, domain = ?, remote_port = ?,
			enabled = ?, updated_at = ?
		WHERE id = ?`,
		t.TargetHost, t.TargetPort, nullString(t.Domain), nullInt(t.RemotePort),
		boolToInt(t.Enabled), formatTime(t.UpdatedAt), id,
	)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// SetTunnelEnabled flips the enabled flag (used by the TTL janitor).
func (s *Store) SetTunnelEnabled(id string, enabled bool) error {
	_, err := s.db.Exec(`UPDATE tunnels SET enabled = ?, updated_at = ? WHERE id = ?`,
		boolToInt(enabled), formatTime(time.Now().UTC()), id)
	return err
}

// DeleteTunnel removes the tunnel and its health_check row.
func (s *Store) DeleteTunnel(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM health_checks WHERE tunnel_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM tunnels WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// --- uniqueness / lookup helpers --------------------------------------------

// TunnelNameExists reports whether a tunnel name is already used by the gateway.
func (s *Store) TunnelNameExists(gatewayID, name string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM tunnels WHERE gateway_id = ? AND name = ?`, gatewayID, name).Scan(&n)
	return n > 0, err
}

// DomainExists reports whether any tunnel already uses the domain.
func (s *Store) DomainExists(domain string) (bool, error) {
	if domain == "" {
		return false, nil
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM tunnels WHERE domain = ?`, domain).Scan(&n)
	return n > 0, err
}

// RemotePortExists reports whether any tunnel already uses the TCP remote port.
func (s *Store) RemotePortExists(port int) (bool, error) {
	if port == 0 {
		return false, nil
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM tunnels WHERE remote_port = ?`, port).Scan(&n)
	return n > 0, err
}

// GetTunnelByName returns the gateway's tunnel with the given name, or ErrNotFound.
func (s *Store) GetTunnelByName(gatewayID, name string) (*Tunnel, error) {
	row := s.db.QueryRow(`SELECT `+tunnelColumns+` FROM tunnels WHERE gateway_id = ? AND name = ?`, gatewayID, name)
	t, err := scanTunnel(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

// ListExpiredEnabledTunnels returns enabled tunnels whose expires_at has passed.
func (s *Store) ListExpiredEnabledTunnels(now time.Time) ([]*Tunnel, error) {
	rows, err := s.db.Query(`SELECT `+tunnelColumns+`
		FROM tunnels
		WHERE enabled = 1 AND expires_at IS NOT NULL AND expires_at <> '' AND expires_at <= ?`,
		formatTime(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Tunnel
	for rows.Next() {
		t, err := scanTunnel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// --- small SQL value helpers -------------------------------------------------

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(i int) any {
	if i == 0 {
		return nil
	}
	return i
}

func nullInt64(i int64) any {
	if i == 0 {
		return nil
	}
	return i
}
