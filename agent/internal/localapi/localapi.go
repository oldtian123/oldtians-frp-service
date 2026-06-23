// Package localapi exposes the agent's local HTTP API (default 127.0.0.1:8899)
// used by AI deploy scripts to publish internal services, plus a status probe.
package localapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/oldtian123/oldtians-frp-service/agent/internal/publish"
	"github.com/oldtian123/oldtians-frp-service/agent/internal/serverapi"
)

// Status is the agent runtime snapshot returned by GET /status.
type Status struct {
	GatewayID     string `json:"gateway_id"`
	Registered    bool   `json:"registered"`
	ConfigVersion int64  `json:"config_version"`
	FrpcStatus    string `json:"frpc_status"`
	TunnelCount   int    `json:"tunnel_count"`
	ServerURL     string `json:"server_url"`
}

// StatusProvider yields the current agent Status.
type StatusProvider func() Status

// Server is the local HTTP API.
type Server struct {
	addr       string
	publishSvc *publish.Service
	status     StatusProvider
	logger     *log.Logger
}

// New builds the local API server.
func New(addr string, publishSvc *publish.Service, status StatusProvider, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	return &Server{addr: addr, publishSvc: publishSvc, status: status, logger: logger}
}

type localPublishRequest struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	TargetHost string `json:"target_host"`
	TargetPort int    `json:"target_port"`
	Subdomain  string `json:"subdomain"`
	TTL        string `json:"ttl"`
	Source     string `json:"source"`
}

// Run starts the local API and blocks until ctx is cancelled, then drains.
func (s *Server) Run(ctx context.Context) error {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, s.status())
	})
	r.POST("/api/local/publish", s.handlePublish)

	srv := &http.Server{Addr: s.addr, Handler: r, ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Printf("local API listening on %s", s.addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) handlePublish(c *gin.Context) {
	var req localPublishRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if req.TargetHost == "" || req.TargetPort == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "target_host and target_port are required"})
		return
	}

	typ := req.Type
	if typ == "" {
		typ = "http"
	}

	result, err := s.publishSvc.Publish(c.Request.Context(), serverapi.PublishRequest{
		Name:       req.Name,
		Type:       typ,
		TargetHost: req.TargetHost,
		TargetPort: req.TargetPort,
		Subdomain:  req.Subdomain,
		TTL:        req.TTL,
		Source:     req.Source,
	})
	if err != nil {
		var apiErr *serverapi.APIError
		if errors.As(err, &apiErr) {
			c.JSON(http.StatusBadGateway, gin.H{
				"error":         apiErr.Message,
				"upstream_code": apiErr.Code,
				"upstream_http": apiErr.StatusCode,
			})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
