// Package serverapi is the agent's typed HTTP client for the control-api
// (ofs-control-api). It mirrors the request/response shapes documented in
// docs/API.md. Credentials are held in memory and never logged.
package serverapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Client talks to the control-api. It is safe for concurrent use.
type Client struct {
	baseURL string
	http    *http.Client

	mu     sync.RWMutex
	id     string // gateway id (set after register)
	secret string // gateway secret (set after register)
}

// New returns a client for the given base URL (e.g. https://api.tunnel.oldtian.top).
func New(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// SetCredentials stores the gateway id + secret used for authenticated calls.
func (c *Client) SetCredentials(id, secret string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.id, c.secret = id, secret
}

func (c *Client) creds() (string, string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.id, c.secret
}

// --- request/response types --------------------------------------------------

type RegisterRequest struct {
	RegisterToken string `json:"register_token"`
	Name          string `json:"name"`
	Hostname      string `json:"hostname"`
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	AgentVersion  string `json:"agent_version"`
}

type RegisterResponse struct {
	GatewayID     string `json:"gateway_id"`
	GatewaySecret string `json:"gateway_secret"`
	ServerURL     string `json:"server_url"`
}

type HeartbeatRequest struct {
	CurrentConfigVersion int64  `json:"current_config_version"`
	LANIP                string `json:"lan_ip"`
	AgentVersion         string `json:"agent_version"`
	FrpcStatus           string `json:"frpc_status"`
}

type HeartbeatResponse struct {
	Status              string `json:"status"`
	LatestConfigVersion int64  `json:"latest_config_version"`
	NeedSync            bool   `json:"need_sync"`
}

type ConfigTunnel struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	TargetHost string `json:"target_host"`
	TargetPort int    `json:"target_port"`
	Domain     string `json:"domain"`
	RemotePort int    `json:"remote_port"`
	Enabled    bool   `json:"enabled"`
}

type ConfigResponse struct {
	ConfigVersion int64          `json:"config_version"`
	ServerAddr    string         `json:"server_addr"`
	ServerPort    int            `json:"server_port"`
	FrpAuthToken  string         `json:"frp_auth_token"`
	Tunnels       []ConfigTunnel `json:"tunnels"`
}

type HealthCheckItem struct {
	TunnelName string `json:"tunnel_name"`
	Status     string `json:"status"`
	LatencyMs  int    `json:"latency_ms"`
	Message    string `json:"message"`
}

type HealthReportRequest struct {
	Checks []HealthCheckItem `json:"checks"`
}

type PublishRequest struct {
	GatewayID  string `json:"gateway_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	TargetHost string `json:"target_host"`
	TargetPort int    `json:"target_port"`
	Subdomain  string `json:"subdomain"`
	TTL        string `json:"ttl"`
	Source     string `json:"source"`
}

type PublishResponse struct {
	Status    string `json:"status"`
	TunnelID  string `json:"tunnel_id"`
	PublicURL string `json:"public_url"`
	Target    string `json:"target"`
	ExpiresAt string `json:"expires_at"`
}

// APIError carries the server's structured error envelope.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("server returned %d (%s): %s", e.StatusCode, e.Code, e.Message)
}

// --- API methods -------------------------------------------------------------

// Register self-registers the gateway using the register token.
func (c *Client) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	var out RegisterResponse
	if err := c.do(ctx, http.MethodPost, "/api/gateways/register", false, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Heartbeat sends a heartbeat (gateway auth).
func (c *Client) Heartbeat(ctx context.Context, req HeartbeatRequest) (*HeartbeatResponse, error) {
	id, _ := c.creds()
	var out HeartbeatResponse
	if err := c.do(ctx, http.MethodPost, "/api/gateways/"+id+"/heartbeat", true, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetConfig pulls the latest tunnel config (gateway auth).
func (c *Client) GetConfig(ctx context.Context) (*ConfigResponse, error) {
	id, _ := c.creds()
	var out ConfigResponse
	if err := c.do(ctx, http.MethodGet, "/api/gateways/"+id+"/config", true, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReportHealth uploads health-check results (gateway auth).
func (c *Client) ReportHealth(ctx context.Context, req HealthReportRequest) error {
	id, _ := c.creds()
	return c.do(ctx, http.MethodPost, "/api/gateways/"+id+"/health", true, req, nil)
}

// Publish forwards a publish request to the gateway-scoped publish endpoint
// using the gateway secret. The server forces the gateway_id to the
// authenticated gateway, so the agent can only publish to itself. This avoids
// putting the admin token on the internal jump host. See docs/AI_PUBLISH.md.
func (c *Client) Publish(ctx context.Context, req PublishRequest) (*PublishResponse, error) {
	id, _ := c.creds()
	var out PublishResponse
	if err := c.do(ctx, http.MethodPost, "/api/gateways/"+id+"/publish", true, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// --- transport ---------------------------------------------------------------

// do performs a request, using the gateway secret as the bearer token when auth
// is true.
func (c *Client) do(ctx context.Context, method, path string, auth bool, body, out any) error {
	token := ""
	if auth {
		_, token = c.creds()
	}
	return c.doWithToken(ctx, method, path, token, body, out)
}

func (c *Client) doWithToken(ctx context.Context, method, path, token string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode >= 400 {
		return parseAPIError(resp.StatusCode, raw)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func parseAPIError(status int, raw []byte) *APIError {
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &env); err == nil && env.Error.Message != "" {
		return &APIError{StatusCode: status, Code: env.Error.Code, Message: env.Error.Message}
	}
	return &APIError{StatusCode: status, Code: "unknown", Message: strings.TrimSpace(string(raw))}
}
