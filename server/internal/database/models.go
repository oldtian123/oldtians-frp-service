package database

import "time"

// Gateway is a registered internal jump host running ofs-gateway-agent + frpc.
type Gateway struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	SecretHash           string     `json:"-"` // never serialised to clients
	Status               string     `json:"status"`
	Hostname             string     `json:"hostname"`
	OS                   string     `json:"os"`
	Arch                 string     `json:"arch"`
	LANIP                string     `json:"lan_ip"`
	PublicIP             string     `json:"public_ip"`
	AgentVersion         string     `json:"agent_version"`
	CurrentConfigVersion int64      `json:"current_config_version"`
	LastSeenAt           *time.Time `json:"last_seen_at"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// Tunnel types.
const (
	TunnelTypeHTTP = "http"
	TunnelTypeTCP  = "tcp"
)

// Tunnel is a single frpc proxy definition belonging to a gateway.
type Tunnel struct {
	ID         string     `json:"id"`
	GatewayID  string     `json:"gateway_id"`
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	TargetHost string     `json:"target_host"`
	TargetPort int        `json:"target_port"`
	Domain     string     `json:"domain,omitempty"`
	RemotePort int        `json:"remote_port,omitempty"`
	Enabled    bool       `json:"enabled"`
	AuthMode   string     `json:"auth_mode"`
	TTLSeconds int64      `json:"ttl_seconds,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	Source     string     `json:"source"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// HealthCheck is the most recent health probe result for a tunnel.
type HealthCheck struct {
	TunnelID  string    `json:"tunnel_id"`
	Status    string    `json:"status"`
	LatencyMs int       `json:"latency_ms"`
	Message   string    `json:"message"`
	CheckedAt time.Time `json:"checked_at"`
}

// EventLog is an audit / activity record for important operations.
type EventLog struct {
	ID        string    `json:"id"`
	GatewayID string    `json:"gateway_id,omitempty"`
	TunnelID  string    `json:"tunnel_id,omitempty"`
	Level     string    `json:"level"`
	EventType string    `json:"event_type"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// ConfigVersion tracks the latest desired config version for a gateway.
type ConfigVersion struct {
	GatewayID string    `json:"gateway_id"`
	Version   int64     `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}
