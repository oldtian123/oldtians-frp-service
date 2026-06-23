package database

// DashboardSummary is the aggregate snapshot consumed by the cloud panel.
type DashboardSummary struct {
	GatewayCount   int `json:"gateway_count"`
	GatewayOnline  int `json:"gateway_online"`
	TunnelCount    int `json:"tunnel_count"`
	TunnelEnabled  int `json:"tunnel_enabled"`
	HealthyCount   int `json:"healthy_count"`
	UnhealthyCount int `json:"unhealthy_count"`
}

// Summary computes the dashboard summary in a few cheap aggregate queries.
// Online gateways are derived from last_seen_at freshness (OnlineThreshold),
// not the stored status column, so a crashed agent is reflected correctly.
func (s *Store) Summary() (*DashboardSummary, error) {
	var sum DashboardSummary

	gateways, err := s.ListGateways()
	if err != nil {
		return nil, err
	}
	sum.GatewayCount = len(gateways)
	for _, g := range gateways {
		if IsGatewayOnline(g) {
			sum.GatewayOnline++
		}
	}

	if err := s.db.QueryRow(`SELECT COUNT(1) FROM tunnels`).Scan(&sum.TunnelCount); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM tunnels WHERE enabled = 1`).Scan(&sum.TunnelEnabled); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM health_checks WHERE status = 'healthy'`).Scan(&sum.HealthyCount); err != nil {
		return nil, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM health_checks WHERE status = 'unhealthy'`).Scan(&sum.UnhealthyCount); err != nil {
		return nil, err
	}
	return &sum, nil
}
