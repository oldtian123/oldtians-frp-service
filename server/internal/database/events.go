package database

import "time"

// CreateEventLog inserts an audit/activity record. created_at is set here.
func (s *Store) CreateEventLog(e *EventLog) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if e.Level == "" {
		e.Level = "info"
	}
	_, err := s.db.Exec(`
		INSERT INTO event_logs (id, gateway_id, tunnel_id, level, event_type, message, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.ID, nullString(e.GatewayID), nullString(e.TunnelID), e.Level, e.EventType,
		nullString(e.Message), formatTime(e.CreatedAt),
	)
	return err
}

const eventColumns = `
	id, COALESCE(gateway_id,''), COALESCE(tunnel_id,''), level, event_type,
	COALESCE(message,''), created_at`

func scanEvent(sc interface{ Scan(...any) error }) (*EventLog, error) {
	var e EventLog
	var createdAt string
	if err := sc.Scan(&e.ID, &e.GatewayID, &e.TunnelID, &e.Level, &e.EventType, &e.Message, &createdAt); err != nil {
		return nil, err
	}
	var err error
	if e.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &e, nil
}

// ListEvents returns the most recent event logs, capped by limit (default 100).
func (s *Store) ListEvents(limit int) ([]*EventLog, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.Query(`SELECT `+eventColumns+` FROM event_logs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*EventLog
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
