// Package publish implements the AI-friendly POST /api/publish endpoint. It is a
// thin convenience layer over the tunnel Service: it turns a subdomain + ttl
// into a concrete HTTP tunnel, auto-resolving subdomain collisions.
package publish

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/oldtian123/oldtians-frp-service/server/internal/config"
	"github.com/oldtian123/oldtians-frp-service/server/internal/database"
	"github.com/oldtian123/oldtians-frp-service/server/internal/httpx"
	"github.com/oldtian123/oldtians-frp-service/server/internal/tunnel"
)

// Handler serves the publish endpoint.
type Handler struct {
	svc   *tunnel.Service
	store *database.Store
	cfg   *config.Config
}

// New builds a publish Handler.
func New(svc *tunnel.Service, store *database.Store, cfg *config.Config) *Handler {
	return &Handler{svc: svc, store: store, cfg: cfg}
}

type publishRequest struct {
	GatewayID  string `json:"gateway_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	TargetHost string `json:"target_host"`
	TargetPort int    `json:"target_port"`
	Subdomain  string `json:"subdomain"`
	TTL        string `json:"ttl"`
	Source     string `json:"source"`
}

type publishResponse struct {
	Status    string `json:"status"`
	TunnelID  string `json:"tunnel_id"`
	PublicURL string `json:"public_url"`
	Target    string `json:"target"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

var slugInvalid = regexp.MustCompile(`[^a-z0-9]+`)

// Publish handles POST /api/publish (admin auth). gateway_id comes from the body.
func (h *Handler) Publish(c *gin.Context) {
	var req publishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, "invalid JSON body")
		return
	}
	if req.GatewayID == "" {
		httpx.BadRequest(c, "gateway_id is required")
		return
	}
	h.publish(c, req)
}

// PublishScoped handles POST /api/gateways/:gateway_id/publish (gateway auth).
// The gateway_id is forced to the authenticated gateway, so an agent can only
// publish to itself.
func (h *Handler) PublishScoped(c *gin.Context) {
	g, ok := httpx.GatewayFromContext(c)
	if !ok {
		httpx.Unauthorized(c, "gateway not authenticated")
		return
	}
	var req publishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.BadRequest(c, "invalid JSON body")
		return
	}
	req.GatewayID = g.ID // ignore any body-supplied gateway_id
	h.publish(c, req)
}

// publish contains the shared publish logic for both entry points.
func (h *Handler) publish(c *gin.Context, req publishRequest) {
	typ := strings.ToLower(strings.TrimSpace(req.Type))
	if typ == "" {
		typ = database.TunnelTypeHTTP
	}
	if typ != database.TunnelTypeHTTP {
		// TCP publish is reserved for a future version; the fields are accepted
		// but not yet acted upon (see docs/AI_PUBLISH.md).
		httpx.BadRequest(c, "only http publish is supported in this version; tcp publish is reserved")
		return
	}

	// Resolve the desired subdomain (falling back to name) to a unique domain.
	base := req.Subdomain
	if base == "" {
		base = req.Name
	}
	slug := slugify(base)
	if slug == "" {
		httpx.BadRequest(c, "subdomain or name is required")
		return
	}
	domain, finalSub, err := h.resolveDomain(slug)
	if err != nil {
		httpx.Internal(c, "failed to resolve a free subdomain")
		return
	}

	// Resolve TTL.
	ttl, err := h.resolveTTL(req.TTL)
	if err != nil {
		httpx.BadRequest(c, "invalid ttl: "+err.Error())
		return
	}
	var expiresAt *time.Time
	var ttlSeconds int64
	if ttl > 0 {
		exp := time.Now().Add(ttl)
		expiresAt = &exp
		ttlSeconds = int64(ttl.Seconds())
	}

	source := req.Source
	if source == "" {
		source = "manual"
	}

	t, err := h.svc.Create(tunnel.CreateInput{
		GatewayID:  req.GatewayID,
		Name:       finalSub, // name tracks the (unique) subdomain
		Type:       database.TunnelTypeHTTP,
		TargetHost: req.TargetHost,
		TargetPort: req.TargetPort,
		Domain:     domain,
		Source:     source,
		TTLSeconds: ttlSeconds,
		ExpiresAt:  expiresAt,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}

	resp := publishResponse{
		Status:    "ok",
		TunnelID:  t.ID,
		PublicURL: h.svc.PublicURL(t),
		Target:    fmt.Sprintf("%s:%d", t.TargetHost, t.TargetPort),
	}
	if expiresAt != nil {
		resp.ExpiresAt = expiresAt.Format(time.RFC3339)
	}
	httpx.Created(c, resp)
}

// resolveDomain finds a free domain under the configured suffix, appending a
// short random suffix to the subdomain if the base is already taken.
func (h *Handler) resolveDomain(slug string) (domain, subdomain string, err error) {
	suffix := h.cfg.FRP.HTTPDomainSuffix
	candidate := slug
	for i := 0; i < 10; i++ {
		domain = candidate + "." + suffix
		exists, derr := h.store.DomainExists(domain)
		if derr != nil {
			return "", "", derr
		}
		if !exists {
			return domain, candidate, nil
		}
		candidate = slug + "-" + shortSuffix()
	}
	return "", "", fmt.Errorf("could not allocate a free subdomain for %q", slug)
}

// resolveTTL parses the ttl string, falling back to the configured default.
func (h *Handler) resolveTTL(s string) (time.Duration, error) {
	if strings.TrimSpace(s) == "" {
		return time.Duration(h.cfg.Tunnel.DefaultTTLHours) * time.Hour, nil
	}
	return time.ParseDuration(s)
}

func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugInvalid.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// shortSuffix returns 4 random hex chars used to de-duplicate subdomains.
func shortSuffix() string {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "x0x0"
	}
	return hex.EncodeToString(b)
}

func writeServiceError(c *gin.Context, err error) {
	if se, ok := err.(*tunnel.ServiceError); ok {
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
