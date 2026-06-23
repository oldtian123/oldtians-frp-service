package tunnel

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/oldtian123/oldtians-frp-service/server/internal/database"
	"github.com/oldtian123/oldtians-frp-service/server/internal/httpx"
)

// Handler exposes the tunnel management endpoints over the Service.
type Handler struct {
	svc   *Service
	store *database.Store
}

// NewHandler builds a tunnel HTTP Handler.
func NewHandler(svc *Service, store *database.Store) *Handler {
	return &Handler{svc: svc, store: store}
}

// View decorates a tunnel with the fields the panel needs.
type View struct {
	*database.Tunnel
	PublicURL    string                `json:"public_url"`
	Target       string                `json:"target"`
	HealthStatus string                `json:"health_status"`
	Health       *database.HealthCheck `json:"health,omitempty"`
}

func (h *Handler) view(t *database.Tunnel) View {
	v := View{
		Tunnel:       t,
		PublicURL:    h.svc.PublicURL(t),
		Target:       targetAddr(t),
		HealthStatus: "unknown",
	}
	if hc, err := h.store.GetHealthCheck(t.ID); err == nil {
		v.Health = hc
		v.HealthStatus = hc.Status
	}
	return v
}

func targetAddr(t *database.Tunnel) string {
	return t.TargetHost + ":" + strconv.Itoa(t.TargetPort)
}

// --- POST /api/tunnels -------------------------------------------------------

type createRequest struct {
	GatewayID  string `json:"gateway_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	TargetHost string `json:"target_host"`
	TargetPort int    `json:"target_port"`
	Domain     string `json:"domain"`
	RemotePort int    `json:"remote_port"`
	Enabled    *bool  `json:"enabled"`
	Source     string `json:"source"`
}

// Create handles POST /api/tunnels (admin auth).
func (h *Handler) Create(c *gin.Context) {
	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, "invalid JSON body")
		return
	}
	t, err := h.svc.Create(CreateInput{
		GatewayID:  req.GatewayID,
		Name:       req.Name,
		Type:       req.Type,
		TargetHost: req.TargetHost,
		TargetPort: req.TargetPort,
		Domain:     req.Domain,
		RemotePort: req.RemotePort,
		Enabled:    req.Enabled,
		Source:     req.Source,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	httpx.Created(c, h.view(t))
}

// --- GET /api/tunnels and GET /api/gateways/:gateway_id/tunnels --------------

// List handles GET /api/tunnels (admin auth). Supports ?gateway_id= and ?enabled=.
func (h *Handler) List(c *gin.Context) {
	h.list(c, c.Query("gateway_id"))
}

// ListByGateway handles GET /api/gateways/:gateway_id/tunnels (admin auth).
func (h *Handler) ListByGateway(c *gin.Context) {
	h.list(c, c.Param("gateway_id"))
}

func (h *Handler) list(c *gin.Context, gatewayID string) {
	filter := database.TunnelFilter{GatewayID: gatewayID}
	if v := c.Query("enabled"); v != "" {
		b := v == "1" || v == "true"
		filter.Enabled = &b
	}
	tunnels, err := h.store.ListTunnels(filter)
	if err != nil {
		httpx.Internal(c, "failed to list tunnels")
		return
	}
	views := make([]View, 0, len(tunnels))
	for _, t := range tunnels {
		views = append(views, h.view(t))
	}
	httpx.OK(c, gin.H{"tunnels": views})
}

// --- PUT /api/tunnels/:id ----------------------------------------------------

type updateRequest struct {
	Enabled    *bool   `json:"enabled"`
	TargetHost *string `json:"target_host"`
	TargetPort *int    `json:"target_port"`
	Domain     *string `json:"domain"`
	RemotePort *int    `json:"remote_port"`
}

// Update handles PUT /api/tunnels/:id (admin auth).
func (h *Handler) Update(c *gin.Context) {
	id := c.Param("id")
	var req updateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, "invalid JSON body")
		return
	}
	t, err := h.svc.Update(id, UpdateInput{
		Enabled:    req.Enabled,
		TargetHost: req.TargetHost,
		TargetPort: req.TargetPort,
		Domain:     req.Domain,
		RemotePort: req.RemotePort,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	httpx.OK(c, h.view(t))
}

// --- DELETE /api/tunnels/:id -------------------------------------------------

// Delete handles DELETE /api/tunnels/:id (admin auth).
func (h *Handler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.Delete(id); err != nil {
		writeServiceError(c, err)
		return
	}
	httpx.OK(c, gin.H{"status": "ok", "deleted": id})
}

// writeServiceError maps a *ServiceError to the proper HTTP status + envelope.
func writeServiceError(c *gin.Context, err error) {
	var se *ServiceError
	if errors.As(err, &se) {
		switch se.Code {
		case "bad_request":
			httpx.BadRequest(c, se.Message)
		case "conflict":
			httpx.Conflict(c, se.Message)
		case "not_found":
			httpx.NotFound(c, se.Message)
		default:
			httpx.Internal(c, se.Message)
		}
		return
	}
	httpx.Internal(c, "unexpected error")
}
