// Package gateway implements the gateway-facing endpoints (register, heartbeat,
// config pull) plus the admin-facing gateway list and dashboard summary.
package gateway

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/oldtian123/oldtians-frp-service/server/internal/config"
	"github.com/oldtian123/oldtians-frp-service/server/internal/database"
	"github.com/oldtian123/oldtians-frp-service/server/internal/eventlog"
	"github.com/oldtian123/oldtians-frp-service/server/internal/httpx"
)

// Handler bundles the dependencies shared by the gateway endpoints.
type Handler struct {
	store  *database.Store
	events *eventlog.Logger
	cfg    *config.Config
}

// New builds a gateway Handler.
func New(store *database.Store, events *eventlog.Logger, cfg *config.Config) *Handler {
	return &Handler{store: store, events: events, cfg: cfg}
}

// --- Register ----------------------------------------------------------------

type registerRequest struct {
	RegisterToken string `json:"register_token"`
	Name          string `json:"name"`
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	AgentVersion  string `json:"agent_version"`
}

type registerResponse struct {
	GatewayID     string `json:"gateway_id"`
	GatewaySecret string `json:"gateway_secret"`
	ServerURL     string `json:"server_url"`
}

// Register handles POST /api/gateways/register.
func (h *Handler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, "invalid JSON body")
		return
	}
	if !httpx.ConstantTimeEqual(req.RegisterToken, h.cfg.Auth.RegisterToken) {
		httpx.Unauthorized(c, "invalid register token")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		httpx.BadRequest(c, "name is required")
		return
	}

	id, err := h.allocateGatewayID(req.Name)
	if err != nil {
		httpx.Internal(c, "failed to allocate gateway id")
		return
	}

	secret, err := generateSecret()
	if err != nil {
		httpx.Internal(c, "failed to generate gateway secret")
		return
	}
	secretHash, err := httpx.HashSecret(secret)
	if err != nil {
		httpx.Internal(c, "failed to hash gateway secret")
		return
	}

	g := &database.Gateway{
		ID:           id,
		Name:         req.Name,
		SecretHash:   secretHash,
		Status:       "offline",
		Hostname:     req.Hostname,
		OS:           req.OS,
		Arch:         req.Arch,
		AgentVersion: req.AgentVersion,
	}
	if err := h.store.CreateGateway(g); err != nil {
		httpx.Internal(c, "failed to create gateway")
		return
	}
	if err := h.store.EnsureConfigVersion(id); err != nil {
		httpx.Internal(c, "failed to initialise config version")
		return
	}

	h.events.Info("gateway.register", "gateway registered: "+id, eventlog.WithGateway(id))

	httpx.Created(c, registerResponse{
		GatewayID:     id,
		GatewaySecret: secret, // returned exactly once; only the hash is stored
		ServerURL:     h.cfg.Server.PublicBaseURL,
	})
}

// --- Heartbeat ---------------------------------------------------------------

type heartbeatRequest struct {
	CurrentConfigVersion int64  `json:"current_config_version"`
	LANIP                string `json:"lan_ip"`
	AgentVersion         string `json:"agent_version"`
	FrpcStatus           string `json:"frpc_status"`
}

type heartbeatResponse struct {
	Status              string `json:"status"`
	LatestConfigVersion int64  `json:"latest_config_version"`
	NeedSync            bool   `json:"need_sync"`
}

// Heartbeat handles POST /api/gateways/:gateway_id/heartbeat (gateway auth).
func (h *Handler) Heartbeat(c *gin.Context) {
	g, ok := httpx.GatewayFromContext(c)
	if !ok {
		httpx.Unauthorized(c, "gateway not authenticated")
		return
	}
	var req heartbeatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, "invalid JSON body")
		return
	}

	if err := h.store.TouchGatewayHeartbeat(g.ID, database.HeartbeatUpdate{
		CurrentConfigVersion: req.CurrentConfigVersion,
		LANIP:                req.LANIP,
		AgentVersion:         req.AgentVersion,
	}); err != nil {
		httpx.Internal(c, "failed to update heartbeat")
		return
	}

	latest, err := h.store.GetConfigVersion(g.ID)
	if err != nil {
		httpx.Internal(c, "failed to read config version")
		return
	}

	httpx.OK(c, heartbeatResponse{
		Status:              "ok",
		LatestConfigVersion: latest,
		NeedSync:            latest != req.CurrentConfigVersion,
	})
}

// --- Config pull -------------------------------------------------------------

type configTunnel struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	TargetHost string `json:"target_host"`
	TargetPort int    `json:"target_port"`
	Domain     string `json:"domain,omitempty"`
	RemotePort int    `json:"remote_port,omitempty"`
	Enabled    bool   `json:"enabled"`
}

type configResponse struct {
	ConfigVersion int64          `json:"config_version"`
	ServerAddr    string         `json:"server_addr"`
	ServerPort    int            `json:"server_port"`
	FrpAuthToken  string         `json:"frp_auth_token"`
	Tunnels       []configTunnel `json:"tunnels"`
}

// Config handles GET /api/gateways/:gateway_id/config (gateway auth). Only the
// authenticated gateway's enabled, non-expired tunnels are returned.
func (h *Handler) Config(c *gin.Context) {
	g, ok := httpx.GatewayFromContext(c)
	if !ok {
		httpx.Unauthorized(c, "gateway not authenticated")
		return
	}

	version, err := h.store.GetConfigVersion(g.ID)
	if err != nil {
		httpx.Internal(c, "failed to read config version")
		return
	}
	enabled := true
	tunnels, err := h.store.ListTunnels(database.TunnelFilter{GatewayID: g.ID, Enabled: &enabled})
	if err != nil {
		httpx.Internal(c, "failed to list tunnels")
		return
	}

	out := configResponse{
		ConfigVersion: version,
		ServerAddr:    h.cfg.FRP.ServerAddr,
		ServerPort:    h.cfg.FRP.ServerPort,
		FrpAuthToken:  h.cfg.FRP.AuthToken,
		Tunnels:       make([]configTunnel, 0, len(tunnels)),
	}
	for _, t := range tunnels {
		out.Tunnels = append(out.Tunnels, configTunnel{
			Name:       t.Name,
			Type:       t.Type,
			TargetHost: t.TargetHost,
			TargetPort: t.TargetPort,
			Domain:     t.Domain,
			RemotePort: t.RemotePort,
			Enabled:    t.Enabled,
		})
	}
	httpx.OK(c, out)
}

// --- Admin: list + summary ---------------------------------------------------

// gatewayView decorates a gateway with the derived online flag for the panel.
type gatewayView struct {
	*database.Gateway
	Online              bool  `json:"online"`
	LatestConfigVersion int64 `json:"latest_config_version"`
}

// List handles GET /api/gateways (admin auth).
func (h *Handler) List(c *gin.Context) {
	gateways, err := h.store.ListGateways()
	if err != nil {
		httpx.Internal(c, "failed to list gateways")
		return
	}
	views := make([]gatewayView, 0, len(gateways))
	for _, g := range gateways {
		latest, _ := h.store.GetConfigVersion(g.ID)
		views = append(views, gatewayView{
			Gateway:             g,
			Online:              database.IsGatewayOnline(g),
			LatestConfigVersion: latest,
		})
	}
	httpx.OK(c, gin.H{"gateways": views})
}

// Summary handles GET /api/dashboard/summary (admin auth).
func (h *Handler) Summary(c *gin.Context) {
	sum, err := h.store.Summary()
	if err != nil {
		httpx.Internal(c, "failed to compute summary")
		return
	}
	httpx.OK(c, sum)
}

// Events handles GET /api/events (admin auth). Supports ?limit= (default 100).
func (h *Handler) Events(c *gin.Context) {
	limit := 100
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	events, err := h.store.ListEvents(limit)
	if err != nil {
		httpx.Internal(c, "failed to list events")
		return
	}
	httpx.OK(c, gin.H{"events": events})
}

// --- helpers -----------------------------------------------------------------

var slugInvalid = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a free-form name into a DNS/ID-safe slug.
func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugInvalid.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "gateway"
	}
	return s
}

// allocateGatewayID returns a unique gateway id derived from the name, adding a
// short random suffix when the base slug is already taken.
func (h *Handler) allocateGatewayID(name string) (string, error) {
	base := slugify(name)
	exists, err := h.store.GatewayIDExists(base)
	if err != nil {
		return "", err
	}
	if !exists {
		return base, nil
	}
	for i := 0; i < 8; i++ {
		candidate := base + "-" + shortSuffix()
		exists, err := h.store.GatewayIDExists(candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
	}
	return "", errors.New("could not allocate unique gateway id")
}

// generateSecret returns a 256-bit random secret as a 64-char hex string.
func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// shortSuffix returns 4 random hex chars used to de-duplicate ids/subdomains.
func shortSuffix() string {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		// Fall back to a fixed marker; collision is then handled by the retry loop.
		return "x0x0"
	}
	return hex.EncodeToString(b)
}
