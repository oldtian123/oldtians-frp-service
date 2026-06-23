// Package sync pulls the tunnel config from the control-api, regenerates
// frpc.toml and reloads frpc. It also caches the latest tunnel snapshot for the
// health-check loop.
package sync

import (
	"context"
	"log"
	"sync"
	"time"

	agentconfig "github.com/oldtian123/oldtians-frp-service/agent/internal/config"
	"github.com/oldtian123/oldtians-frp-service/agent/internal/frpc"
	"github.com/oldtian123/oldtians-frp-service/agent/internal/serverapi"
)

// Synchronizer coordinates config pull -> render -> reload.
type Synchronizer struct {
	client  *serverapi.Client
	manager *frpc.Manager
	state   *agentconfig.State
	logger  *log.Logger

	mu      sync.Mutex // serialises SyncOnce
	trigger chan struct{}

	tunnelsMu   sync.RWMutex
	lastTunnels []serverapi.ConfigTunnel
}

// New builds a Synchronizer.
func New(client *serverapi.Client, manager *frpc.Manager, state *agentconfig.State, logger *log.Logger) *Synchronizer {
	if logger == nil {
		logger = log.Default()
	}
	return &Synchronizer{
		client:  client,
		manager: manager,
		state:   state,
		logger:  logger,
		trigger: make(chan struct{}, 1),
	}
}

// Trigger requests an out-of-band sync without blocking the caller.
func (s *Synchronizer) Trigger() {
	select {
	case s.trigger <- struct{}{}:
	default: // a sync is already queued
	}
}

// Tunnels returns a copy of the most recent tunnel snapshot for health checks.
func (s *Synchronizer) Tunnels() []serverapi.ConfigTunnel {
	s.tunnelsMu.RLock()
	defer s.tunnelsMu.RUnlock()
	out := make([]serverapi.ConfigTunnel, len(s.lastTunnels))
	copy(out, s.lastTunnels)
	return out
}

func (s *Synchronizer) setTunnels(t []serverapi.ConfigTunnel) {
	s.tunnelsMu.Lock()
	s.lastTunnels = t
	s.tunnelsMu.Unlock()
}

// SyncOnce pulls the config and, if the version changed (or frpc is not running),
// regenerates frpc.toml and reloads frpc. It returns an error on failure so that
// callers (e.g. publish) can surface pending_sync.
func (s *Synchronizer) SyncOnce(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, err := s.client.GetConfig(ctx)
	if err != nil {
		s.logger.Printf("sync: failed to pull config: %v", err)
		return err
	}
	s.setTunnels(cfg.Tunnels)

	// Fast path: nothing changed and frpc is healthy -> do nothing.
	if cfg.ConfigVersion == s.state.ConfigVersion() && s.manager.Status() == frpc.StatusRunning {
		return nil
	}

	content, warnings, err := frpc.Render(frpc.RenderInput{
		ServerAddr: cfg.ServerAddr,
		ServerPort: cfg.ServerPort,
		AuthToken:  cfg.FrpAuthToken,
		Proxies:    toProxies(cfg.Tunnels),
	})
	if err != nil {
		s.logger.Printf("sync: failed to render frpc.toml: %v", err)
		return err
	}
	for _, w := range warnings {
		s.logger.Printf("sync: render warning: %s", w)
	}

	if err := s.manager.Apply(content); err != nil {
		s.logger.Printf("sync: frpc reload failed (version %d): %v", cfg.ConfigVersion, err)
		return err
	}

	if err := s.state.SetConfigVersion(cfg.ConfigVersion); err != nil {
		s.logger.Printf("sync: failed to persist config version: %v", err)
		// Not fatal: frpc already reloaded; the next heartbeat will re-sync.
	}
	s.logger.Printf("sync: applied config version %d (%d tunnels), frpc reloaded", cfg.ConfigVersion, len(cfg.Tunnels))
	return nil
}

// Run periodically syncs and also reacts to Trigger().
func (s *Synchronizer) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.SyncOnce(ctx)
		case <-s.trigger:
			_ = s.SyncOnce(ctx)
		}
	}
}

// toProxies converts the server tunnel config into frpc proxies (enabled only).
func toProxies(tunnels []serverapi.ConfigTunnel) []frpc.Proxy {
	out := make([]frpc.Proxy, 0, len(tunnels))
	for _, t := range tunnels {
		if !t.Enabled {
			continue
		}
		out = append(out, frpc.Proxy{
			Name:       t.Name,
			Type:       t.Type,
			LocalIP:    t.TargetHost,
			LocalPort:  t.TargetPort,
			Domain:     t.Domain,
			RemotePort: t.RemotePort,
		})
	}
	return out
}
