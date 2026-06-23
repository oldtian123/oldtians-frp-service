// Package register handles first-boot self-registration of the gateway-agent
// against the control-api and exposes host-info helpers reused by heartbeats.
package register

import (
	"context"
	"log"
	"net"
	"os"
	"runtime"
	"time"

	agentconfig "github.com/oldtian123/oldtians-frp-service/agent/internal/config"
	"github.com/oldtian123/oldtians-frp-service/agent/internal/serverapi"
)

// EnsureRegistered makes sure the agent has credentials. If state already holds
// a gateway id + secret it simply primes the client. Otherwise it registers,
// retrying with capped backoff until it succeeds or the context is cancelled.
func EnsureRegistered(ctx context.Context, client *serverapi.Client, cfg *agentconfig.Config, state *agentconfig.State, version string, logger *log.Logger) error {
	if logger == nil {
		logger = log.Default()
	}

	if state.Registered() {
		id, secret := state.Credentials()
		client.SetCredentials(id, secret)
		logger.Printf("register: using existing gateway id %s", id)
		return nil
	}

	req := serverapi.RegisterRequest{
		RegisterToken: cfg.RegisterToken,
		Name:          cfg.Gateway.Name,
		Hostname:      hostname(),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		AgentVersion:  version,
	}

	backoff := 2 * time.Second
	const maxBackoff = 30 * time.Second
	for {
		regCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		resp, err := client.Register(regCtx, req)
		cancel()
		if err == nil {
			if err := state.SetCredentials(resp.GatewayID, resp.GatewaySecret); err != nil {
				return err
			}
			client.SetCredentials(resp.GatewayID, resp.GatewaySecret)
			logger.Printf("register: registered as gateway id %s", resp.GatewayID)
			return nil
		}

		logger.Printf("register: failed (%v), retrying in %s", err, backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

func hostname() string {
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "unknown-host"
}

// LANIP returns the agent's primary LAN IP by inspecting the route to a public
// address (no packets are actually sent). Falls back to interface enumeration.
func LANIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			return addr.IP.String()
		}
	}
	// Fallback: first non-loopback IPv4 on any up interface.
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return ""
}
