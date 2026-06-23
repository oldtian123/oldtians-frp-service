package database

import (
	"database/sql"
	"errors"
	"time"
)

// ErrNotFound is returned by Get* helpers when no row matches.
var ErrNotFound = errors.New("not found")

// CreateGateway inserts a new gateway row. created_at / updated_at are set here.
func (s *Store) CreateGateway(g *Gateway) error {
	now := time.Now().UTC()
	g.CreatedAt = now
	g.UpdatedAt = now
	if g.Status == "" {
		g.Status = "offline"
	}
	_, err := s.db.Exec(`
		INSERT INTO gateways (
			id, name, secret_hash, status, hostname, os, arch,
			lan_ip, public_ip, agent_version, current_config_version,
			last_seen_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		g.ID, g.Name, g.SecretHash, g.Status, g.Hostname, g.OS, g.Arch,
		g.LANIP, g.PublicIP, g.AgentVersion, g.CurrentConfigVersion,
		nullTime(g.LastSeenAt), formatTime(g.CreatedAt), formatTime(g.UpdatedAt),
	)
	return err
}

const gatewayColumns = `
	id, name, secret_hash, status,
	COALESCE(hostname,''), COALESCE(os,''), COALESCE(arch,''),
	COALESCE(lan_ip,''), COALESCE(public_ip,''), COALESCE(agent_version,''),
	COALESCE(current_config_version,0), COALESCE(last_seen_at,''),
	created_at, updated_at`

func scanGateway(sc interface{ Scan(...any) error }) (*Gateway, error) {
	var g Gateway
	var lastSeen, createdAt, updatedAt string
	if err := sc.Scan(
		&g.ID, &g.Name, &g.SecretHash, &g.Status,
		&g.Hostname, &g.OS, &g.Arch,
		&g.LANIP, &g.PublicIP, &g.AgentVersion,
		&g.CurrentConfigVersion, &lastSeen,
		&createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	var err error
	if g.LastSeenAt, err = parseOptionalTime(lastSeen); err != nil {
		return nil, err
	}
	if g.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if g.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &g, nil
}

// GetGateway returns the gateway with the given id, or ErrNotFound.
func (s *Store) GetGateway(id string) (*Gateway, error) {
	row := s.db.QueryRow(`SELECT `+gatewayColumns+` FROM gateways WHERE id = ?`, id)
	g, err := scanGateway(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return g, err
}

// GatewayNameExists reports whether a gateway with the exact name exists.
func (s *Store) GatewayNameExists(name string) (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM gateways WHERE name = ?`, name).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// GatewayIDExists reports whether a gateway with the given id exists.
func (s *Store) GatewayIDExists(id string) (bool, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM gateways WHERE id = ?`, id).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListGateways returns all gateways ordered by creation time.
func (s *Store) ListGateways() ([]*Gateway, error) {
	rows, err := s.db.Query(`SELECT ` + gatewayColumns + ` FROM gateways ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Gateway
	for rows.Next() {
		g, err := scanGateway(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// HeartbeatUpdate carries the mutable fields a heartbeat refreshes.
type HeartbeatUpdate struct {
	CurrentConfigVersion int64
	LANIP                string
	AgentVersion         string
}

// TouchGatewayHeartbeat marks the gateway online and updates the volatile fields
// reported by the agent. Empty lan_ip / agent_version are preserved (COALESCE).
func (s *Store) TouchGatewayHeartbeat(id string, u HeartbeatUpdate) error {
	now := time.Now().UTC()
	res, err := s.db.Exec(`
		UPDATE gateways SET
			status = 'online',
			current_config_version = ?,
			lan_ip = CASE WHEN ?2 <> '' THEN ?2 ELSE lan_ip END,
			agent_version = CASE WHEN ?3 <> '' THEN ?3 ELSE agent_version END,
			last_seen_at = ?4,
			updated_at = ?4
		WHERE id = ?5`,
		u.CurrentConfigVersion, u.LANIP, u.AgentVersion, formatTime(now), id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// IsGatewayOnline reports whether the gateway's last heartbeat is recent enough.
func IsGatewayOnline(g *Gateway) bool {
	if g.LastSeenAt == nil {
		return false
	}
	return time.Since(*g.LastSeenAt) <= OnlineThreshold
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}
