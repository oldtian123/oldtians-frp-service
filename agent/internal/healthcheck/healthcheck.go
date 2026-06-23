// Package healthcheck periodically probes each tunnel's target and reports the
// results back to the control-api.
package healthcheck

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/oldtian123/oldtians-frp-service/agent/internal/serverapi"
)

const (
	statusHealthy   = "healthy"
	statusUnhealthy = "unhealthy"
	probeTimeout    = 4 * time.Second
)

// TunnelProvider returns the current set of tunnels to probe.
type TunnelProvider func() []serverapi.ConfigTunnel

// Run probes all tunnels every interval and reports the results. Probing runs in
// its own goroutine per tick so a slow target never blocks the loop.
func Run(ctx context.Context, client *serverapi.Client, interval time.Duration, tunnels TunnelProvider, logger *log.Logger) {
	if logger == nil {
		logger = log.Default()
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkAndReport(ctx, client, tunnels(), logger)
		}
	}
}

func checkAndReport(ctx context.Context, client *serverapi.Client, tunnels []serverapi.ConfigTunnel, logger *log.Logger) {
	if len(tunnels) == 0 {
		return
	}
	checks := make([]serverapi.HealthCheckItem, 0, len(tunnels))
	for _, t := range tunnels {
		if !t.Enabled {
			continue
		}
		item := probe(t)
		if item.Status == statusUnhealthy {
			logger.Printf("healthcheck: tunnel %q unhealthy: %s", t.Name, item.Message)
		}
		checks = append(checks, item)
	}
	if len(checks) == 0 {
		return
	}
	reportCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := client.ReportHealth(reportCtx, serverapi.HealthReportRequest{Checks: checks}); err != nil {
		logger.Printf("healthcheck: report failed: %v", err)
	}
}

// probe checks a single tunnel target. HTTP tunnels are probed with an HTTP GET
// (any response counts as reachable); TCP tunnels use a plain TCP connect.
func probe(t serverapi.ConfigTunnel) serverapi.HealthCheckItem {
	addr := fmt.Sprintf("%s:%d", t.TargetHost, t.TargetPort)

	if t.Type == "http" {
		latency, err := probeHTTP(addr)
		if err != nil {
			// Fall back to a TCP connect before declaring it unhealthy.
			if l2, terr := probeTCP(addr); terr == nil {
				return ok(t.Name, l2, "tcp connect ok (http probe failed)")
			}
			return bad(t.Name, err.Error())
		}
		return ok(t.Name, latency, "http ok")
	}

	latency, err := probeTCP(addr)
	if err != nil {
		return bad(t.Name, err.Error())
	}
	return ok(t.Name, latency, "tcp connect ok")
}

func probeTCP(addr string) (int, error) {
	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, probeTimeout)
	if err != nil {
		return 0, err
	}
	_ = conn.Close()
	return int(time.Since(start).Milliseconds()), nil
}

func probeHTTP(addr string) (int, error) {
	client := &http.Client{
		Timeout: probeTimeout,
		// Do not follow redirects; any HTTP response means the service is up.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	start := time.Now()
	resp, err := client.Get("http://" + addr + "/")
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	return int(time.Since(start).Milliseconds()), nil
}

func ok(name string, latency int, msg string) serverapi.HealthCheckItem {
	return serverapi.HealthCheckItem{TunnelName: name, Status: statusHealthy, LatencyMs: latency, Message: msg}
}

func bad(name, msg string) serverapi.HealthCheckItem {
	return serverapi.HealthCheckItem{TunnelName: name, Status: statusUnhealthy, Message: msg}
}
