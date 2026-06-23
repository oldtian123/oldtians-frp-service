// Package health implements gateway health reporting and the panel-facing
// health query endpoints.
package health

import (
	"errors"

	"github.com/gin-gonic/gin"

	"github.com/oldtian123/oldtians-frp-service/server/internal/database"
	"github.com/oldtian123/oldtians-frp-service/server/internal/eventlog"
	"github.com/oldtian123/oldtians-frp-service/server/internal/httpx"
)

// Handler serves the health endpoints.
type Handler struct {
	store  *database.Store
	events *eventlog.Logger
}

// New builds a health Handler.
func New(store *database.Store, events *eventlog.Logger) *Handler {
	return &Handler{store: store, events: events}
}

const (
	statusHealthy   = "healthy"
	statusUnhealthy = "unhealthy"
)

type reportCheck struct {
	TunnelName string `json:"tunnel_name"`
	Status     string `json:"status"`
	LatencyMs  int    `json:"latency_ms"`
	Message    string `json:"message"`
}

type reportRequest struct {
	Checks []reportCheck `json:"checks"`
}

// Report handles POST /api/gateways/:gateway_id/health (gateway auth).
func (h *Handler) Report(c *gin.Context) {
	g, ok := httpx.GatewayFromContext(c)
	if !ok {
		httpx.Unauthorized(c, "gateway not authenticated")
		return
	}
	var req reportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, "invalid JSON body")
		return
	}

	recorded := 0
	var skipped []string
	for _, ck := range req.Checks {
		t, err := h.store.GetTunnelByName(g.ID, ck.TunnelName)
		if errors.Is(err, database.ErrNotFound) {
			skipped = append(skipped, ck.TunnelName)
			continue
		}
		if err != nil {
			httpx.Internal(c, "failed to look up tunnel")
			return
		}

		prev, _ := h.store.GetHealthCheck(t.ID)
		if err := h.store.UpsertHealthCheck(&database.HealthCheck{
			TunnelID:  t.ID,
			Status:    ck.Status,
			LatencyMs: ck.LatencyMs,
			Message:   ck.Message,
		}); err != nil {
			httpx.Internal(c, "failed to record health")
			return
		}
		recorded++

		// Only log on transition into the unhealthy state to avoid log spam.
		if ck.Status == statusUnhealthy && (prev == nil || prev.Status != statusUnhealthy) {
			h.events.Warn("health.unhealthy", "tunnel "+t.Name+" became unhealthy: "+ck.Message,
				eventlog.WithGateway(g.ID), eventlog.WithTunnel(t.ID))
		}
	}

	httpx.OK(c, gin.H{"status": "ok", "recorded": recorded, "skipped": skipped})
}

// healthView enriches a health row with tunnel/gateway identifiers for the panel.
type healthView struct {
	*database.HealthCheck
	TunnelName string `json:"tunnel_name"`
	GatewayID  string `json:"gateway_id"`
}

// List handles GET /api/health (admin auth).
func (h *Handler) List(c *gin.Context) {
	h.list(c, "")
}

// ListByGateway handles GET /api/gateways/:gateway_id/health (admin auth).
func (h *Handler) ListByGateway(c *gin.Context) {
	h.list(c, c.Param("gateway_id"))
}

func (h *Handler) list(c *gin.Context, gatewayID string) {
	checks, err := h.store.ListHealthChecks(gatewayID)
	if err != nil {
		httpx.Internal(c, "failed to list health checks")
		return
	}
	views := make([]healthView, 0, len(checks))
	for _, hc := range checks {
		v := healthView{HealthCheck: hc}
		if t, err := h.store.GetTunnel(hc.TunnelID); err == nil {
			v.TunnelName = t.Name
			v.GatewayID = t.GatewayID
		}
		views = append(views, v)
	}
	httpx.OK(c, gin.H{"health": views})
}
