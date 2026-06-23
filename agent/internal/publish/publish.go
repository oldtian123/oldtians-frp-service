// Package publish implements the agent-side publish flow: forward the request to
// the control-api (gateway-scoped, using the gateway secret), then trigger an
// immediate config sync so the tunnel goes live without waiting for the next
// heartbeat.
package publish

import (
	"context"
	"log"

	agentconfig "github.com/oldtian123/oldtians-frp-service/agent/internal/config"
	"github.com/oldtian123/oldtians-frp-service/agent/internal/serverapi"
	syncpkg "github.com/oldtian123/oldtians-frp-service/agent/internal/sync"
)

// Service forwards publish requests and reconciles local frpc state.
type Service struct {
	client *serverapi.Client
	state  *agentconfig.State
	syncer *syncpkg.Synchronizer
	logger *log.Logger
}

// New builds a publish Service.
func New(client *serverapi.Client, state *agentconfig.State, syncer *syncpkg.Synchronizer, logger *log.Logger) *Service {
	if logger == nil {
		logger = log.Default()
	}
	return &Service{client: client, state: state, syncer: syncer, logger: logger}
}

// Result is the outcome returned to the local API caller.
type Result struct {
	Status    string `json:"status"` // "ok" or "pending_sync"
	TunnelID  string `json:"tunnel_id,omitempty"`
	PublicURL string `json:"public_url"`
	Target    string `json:"target"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// Publish forwards the request (auto-filling gateway_id) and triggers a sync.
// If the tunnel was created but the local sync failed, Status is "pending_sync"
// and the next heartbeat-driven sync will reconcile it.
func (s *Service) Publish(ctx context.Context, req serverapi.PublishRequest) (*Result, error) {
	id, _ := s.state.Credentials()
	req.GatewayID = id // auto-fill; the server also forces this from auth

	resp, err := s.client.Publish(ctx, req)
	if err != nil {
		return nil, err
	}

	status := "ok"
	if serr := s.syncer.SyncOnce(ctx); serr != nil {
		s.logger.Printf("publish: tunnel %s created but local sync failed: %v", resp.TunnelID, serr)
		status = "pending_sync"
	}

	return &Result{
		Status:    status,
		TunnelID:  resp.TunnelID,
		PublicURL: resp.PublicURL,
		Target:    resp.Target,
		ExpiresAt: resp.ExpiresAt,
	}, nil
}
