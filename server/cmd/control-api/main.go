// Command control-api is the public-server control plane for Oldtian's FRP
// Service (binary name: ofs-control-api).
//
// It exposes the gateway-facing API (register / heartbeat / config), the admin
// management API (tunnels / publish) and the panel data API (summary / events /
// health). State is stored in SQLite. See docs/API.md for the full surface.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/oldtian123/oldtians-frp-service/server/internal/config"
	"github.com/oldtian123/oldtians-frp-service/server/internal/database"
	"github.com/oldtian123/oldtians-frp-service/server/internal/eventlog"
	"github.com/oldtian123/oldtians-frp-service/server/internal/gateway"
	"github.com/oldtian123/oldtians-frp-service/server/internal/health"
	"github.com/oldtian123/oldtians-frp-service/server/internal/httpx"
	"github.com/oldtian123/oldtians-frp-service/server/internal/publish"
	"github.com/oldtian123/oldtians-frp-service/server/internal/tunnel"
)

// version is overridable at build time via -ldflags "-X main.version=...".
var version = "0.1.0"

func main() {
	configPath := flag.String("config", envOr("OFS_CONFIG", "config.yaml"), "path to the YAML config file")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.LUTC)
	log.Printf("ofs-control-api %s starting", version)

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	store, err := database.Open(cfg.Database.Path)
	if err != nil {
		log.Fatalf("database error: %v", err)
	}
	defer func() { _ = store.Close() }()
	log.Printf("database ready at %s", cfg.Database.Path)

	events := eventlog.New(store)

	// Build handlers.
	gwHandler := gateway.New(store, events, cfg)
	tunnelSvc := tunnel.NewService(store, events, cfg)
	tunnelHandler := tunnel.NewHandler(tunnelSvc, store)
	publishHandler := publish.New(tunnelSvc, store, cfg)
	healthHandler := health.New(store, events)

	router := buildRouter(cfg, store, gwHandler, tunnelHandler, publishHandler, healthHandler)

	// Background TTL janitor: disables expired tunnels and bumps config version.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go runJanitor(ctx, store, events, time.Minute)

	addr := fmt.Sprintf("%s:%d", cfg.Server.ListenAddr, cfg.Server.ListenPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("shutdown signal received, draining...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	log.Printf("stopped")
}

// buildRouter wires every route, grouping by the required authentication scheme.
func buildRouter(
	cfg *config.Config,
	store *database.Store,
	gw *gateway.Handler,
	tn *tunnel.Handler,
	pub *publish.Handler,
	hl *health.Handler,
) *gin.Engine {
	if os.Getenv("OFS_DEBUG") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())

	// Liveness probe (no auth) for Docker / 1Panel health checks.
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "version": version})
	})

	// Gateway self-registration (protected by register_token in the body).
	r.POST("/api/gateways/register", gw.Register)

	// Gateway-authenticated endpoints (Bearer = gateway secret).
	gwAuth := r.Group("/api/gateways/:gateway_id", httpx.GatewayAuth(store))
	{
		gwAuth.POST("/heartbeat", gw.Heartbeat)
		gwAuth.GET("/config", gw.Config)
		gwAuth.POST("/health", hl.Report)
		gwAuth.POST("/publish", pub.PublishScoped)
	}

	// Admin-authenticated endpoints (Bearer = admin token).
	admin := r.Group("/api", httpx.AdminAuth(cfg.Auth.AdminToken))
	{
		admin.POST("/tunnels", tn.Create)
		admin.GET("/tunnels", tn.List)
		admin.PUT("/tunnels/:id", tn.Update)
		admin.DELETE("/tunnels/:id", tn.Delete)

		admin.POST("/publish", pub.Publish)

		admin.GET("/gateways", gw.List)
		admin.GET("/gateways/:gateway_id/tunnels", tn.ListByGateway)
		admin.GET("/gateways/:gateway_id/health", hl.ListByGateway)

		admin.GET("/dashboard/summary", gw.Summary)
		admin.GET("/events", gw.Events)
		admin.GET("/health", hl.List)
	}

	return r
}

// runJanitor periodically disables tunnels whose TTL has expired, bumping the
// owning gateway's config version so the agent removes them on the next sync.
func runJanitor(ctx context.Context, store *database.Store, events *eventlog.Logger, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			expired, err := store.ListExpiredEnabledTunnels(time.Now().UTC())
			if err != nil {
				log.Printf("janitor: list expired failed: %v", err)
				continue
			}
			for _, t := range expired {
				if err := store.SetTunnelEnabled(t.ID, false); err != nil {
					log.Printf("janitor: disable %s failed: %v", t.ID, err)
					continue
				}
				if _, err := store.BumpConfigVersion(t.GatewayID); err != nil {
					log.Printf("janitor: bump version for %s failed: %v", t.GatewayID, err)
				}
				events.Info("tunnel.expired", "tunnel "+t.Name+" disabled (ttl expired)",
					eventlog.WithGateway(t.GatewayID), eventlog.WithTunnel(t.ID))
			}
		}
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
