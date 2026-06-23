// Command gateway-agent is the internal jump-host agent for Oldtian's FRP
// Service (binary name: ofs-gateway-agent).
//
// Responsibilities: self-register with the control-api, heartbeat, pull tunnel
// config, render frpc.toml, manage the frpc process, run health checks and serve
// a local publish API. See docs/AGENT.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	agentconfig "github.com/oldtian123/oldtians-frp-service/agent/internal/config"
	"github.com/oldtian123/oldtians-frp-service/agent/internal/frpc"
	"github.com/oldtian123/oldtians-frp-service/agent/internal/healthcheck"
	"github.com/oldtian123/oldtians-frp-service/agent/internal/heartbeat"
	"github.com/oldtian123/oldtians-frp-service/agent/internal/localapi"
	"github.com/oldtian123/oldtians-frp-service/agent/internal/publish"
	"github.com/oldtian123/oldtians-frp-service/agent/internal/register"
	"github.com/oldtian123/oldtians-frp-service/agent/internal/serverapi"
	syncpkg "github.com/oldtian123/oldtians-frp-service/agent/internal/sync"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "0.1.0"

func main() {
	configPath := flag.String("config", envOr("OFS_AGENT_CONFIG", "config.yaml"), "path to the agent YAML config file")
	flag.Parse()

	logger := log.New(os.Stdout, "", log.LstdFlags|log.LUTC)
	logger.Printf("ofs-gateway-agent %s starting", version)

	cfg, err := agentconfig.Load(*configPath)
	if err != nil {
		logger.Fatalf("config error: %v", err)
	}
	state, err := agentconfig.LoadState(cfg.Agent.StatePath)
	if err != nil {
		logger.Fatalf("state error: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := serverapi.New(cfg.ServerURL)

	// 1. Register (or reuse stored credentials).
	if err := register.EnsureRegistered(ctx, client, cfg, state, version, logger); err != nil {
		logger.Fatalf("registration aborted: %v", err)
	}

	// 2. frpc process manager + synchronizer.
	manager := frpc.NewManager(cfg.Frpc.BinPath, cfg.Frpc.ConfigPath, cfg.Frpc.WorkDir, logger)
	syncer := syncpkg.New(client, manager, state, logger)

	// 3. Initial sync: generate frpc.toml and start frpc (best effort).
	if err := syncer.SyncOnce(ctx); err != nil {
		logger.Printf("initial sync failed (will retry): %v", err)
	}

	// 4. Background loops.
	go heartbeat.Run(ctx, client,
		time.Duration(cfg.Agent.HeartbeatIntervalSeconds)*time.Second,
		func() serverapi.HeartbeatRequest {
			return serverapi.HeartbeatRequest{
				CurrentConfigVersion: state.ConfigVersion(),
				LANIP:                register.LANIP(),
				AgentVersion:         version,
				FrpcStatus:           manager.Status(),
			}
		},
		syncer.Trigger,
		logger,
	)
	go syncer.Run(ctx, time.Duration(cfg.Agent.SyncIntervalSeconds)*time.Second)
	go healthcheck.Run(ctx, client,
		time.Duration(cfg.Agent.HealthCheckIntervalSeconds)*time.Second,
		syncer.Tunnels, logger)

	// 5. Local publish API (blocks until shutdown).
	publishSvc := publish.New(client, state, syncer, logger)
	statusFn := func() localapi.Status {
		id, _ := state.Credentials()
		return localapi.Status{
			GatewayID:     id,
			Registered:    state.Registered(),
			ConfigVersion: state.ConfigVersion(),
			FrpcStatus:    manager.Status(),
			TunnelCount:   len(syncer.Tunnels()),
			ServerURL:     cfg.ServerURL,
		}
	}
	addr := fmt.Sprintf("%s:%d", cfg.Agent.ListenAddr, cfg.Agent.ListenPort)
	local := localapi.New(addr, publishSvc, statusFn, logger)

	if err := local.Run(ctx); err != nil {
		logger.Printf("local API error: %v", err)
	}

	// 6. Shutdown: stop frpc.
	logger.Printf("shutting down, stopping frpc")
	manager.Stop()
	logger.Printf("stopped")
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
