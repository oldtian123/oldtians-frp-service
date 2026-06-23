// Package config loads and validates the control-api configuration.
//
// Configuration is read from a YAML file (see config.example.yaml) and then
// selected fields may be overridden by environment variables. Secrets are never
// printed to logs by this package.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration object for the control-api server.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	FRP      FRPConfig      `yaml:"frp"`
	Tunnel   TunnelConfig   `yaml:"tunnel"`
}

// ServerConfig controls the HTTP listener and the public base URL of the API.
type ServerConfig struct {
	ListenAddr    string `yaml:"listen_addr"`
	ListenPort    int    `yaml:"listen_port"`
	PublicBaseURL string `yaml:"public_base_url"`
}

// DatabaseConfig holds the SQLite database location.
type DatabaseConfig struct {
	Path string `yaml:"path"`
}

// AuthConfig holds the static tokens used to protect the API surfaces.
//
//	AdminToken    - protects management + dashboard APIs.
//	RegisterToken - allows a gateway-agent to self-register.
type AuthConfig struct {
	AdminToken    string `yaml:"admin_token"`
	RegisterToken string `yaml:"register_token"`
}

// FRPConfig describes the frps server the agents should connect to, plus the
// HTTP domain suffix used when generating tunnel domains.
type FRPConfig struct {
	ServerAddr       string `yaml:"server_addr"`
	ServerPort       int    `yaml:"server_port"`
	AuthToken        string `yaml:"auth_token"`
	HTTPDomainSuffix string `yaml:"http_domain_suffix"`
	DefaultHTTPPort  int    `yaml:"default_http_port"`
}

// TunnelConfig holds defaults and the allocatable TCP remote-port range.
type TunnelConfig struct {
	DefaultTTLHours int `yaml:"default_ttl_hours"`
	TCPPortMin      int `yaml:"tcp_port_min"`
	TCPPortMax      int `yaml:"tcp_port_max"`
}

// Load reads the YAML config file, applies environment overrides, fills in
// defaults, validates required fields and ensures the database directory exists.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config file %q: %w", path, err)
	}

	cfg.applyEnvOverrides()
	cfg.applyDefaults()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	if err := cfg.ensureDatabaseDir(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// applyEnvOverrides allows the most security-sensitive / deployment-sensitive
// fields to be supplied via environment variables. This is convenient for
// Docker / 1Panel deployments where secrets live outside the YAML file.
func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("OFS_LISTEN_ADDR"); v != "" {
		c.Server.ListenAddr = v
	}
	if v := os.Getenv("OFS_LISTEN_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.Server.ListenPort = p
		}
	}
	if v := os.Getenv("OFS_PUBLIC_BASE_URL"); v != "" {
		c.Server.PublicBaseURL = v
	}
	if v := os.Getenv("OFS_DATABASE_PATH"); v != "" {
		c.Database.Path = v
	}
	if v := os.Getenv("OFS_ADMIN_TOKEN"); v != "" {
		c.Auth.AdminToken = v
	}
	if v := os.Getenv("OFS_REGISTER_TOKEN"); v != "" {
		c.Auth.RegisterToken = v
	}
	if v := os.Getenv("OFS_FRP_SERVER_ADDR"); v != "" {
		c.FRP.ServerAddr = v
	}
	if v := os.Getenv("OFS_FRP_SERVER_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.FRP.ServerPort = p
		}
	}
	if v := os.Getenv("OFS_FRP_AUTH_TOKEN"); v != "" {
		c.FRP.AuthToken = v
	}
	if v := os.Getenv("OFS_FRP_HTTP_DOMAIN_SUFFIX"); v != "" {
		c.FRP.HTTPDomainSuffix = v
	}
}

// applyDefaults fills in sensible defaults for any optional fields left empty.
func (c *Config) applyDefaults() {
	if c.Server.ListenAddr == "" {
		c.Server.ListenAddr = "0.0.0.0"
	}
	if c.Server.ListenPort == 0 {
		c.Server.ListenPort = 8088
	}
	if c.Database.Path == "" {
		c.Database.Path = "./data/oldtians-frp-service.db"
	}
	if c.FRP.ServerPort == 0 {
		c.FRP.ServerPort = 7000
	}
	if c.FRP.DefaultHTTPPort == 0 {
		c.FRP.DefaultHTTPPort = 8080
	}
	if c.Tunnel.DefaultTTLHours == 0 {
		c.Tunnel.DefaultTTLHours = 24
	}
	if c.Tunnel.TCPPortMin == 0 {
		c.Tunnel.TCPPortMin = 22000
	}
	if c.Tunnel.TCPPortMax == 0 {
		c.Tunnel.TCPPortMax = 22999
	}
}

// validate ensures required configuration is present and internally consistent.
func (c *Config) validate() error {
	var missing []string
	if c.Auth.AdminToken == "" {
		missing = append(missing, "auth.admin_token")
	}
	if c.Auth.RegisterToken == "" {
		missing = append(missing, "auth.register_token")
	}
	if c.FRP.ServerAddr == "" {
		missing = append(missing, "frp.server_addr")
	}
	if c.FRP.AuthToken == "" {
		missing = append(missing, "frp.auth_token")
	}
	if c.FRP.HTTPDomainSuffix == "" {
		missing = append(missing, "frp.http_domain_suffix")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config fields: %v", missing)
	}

	if c.Tunnel.TCPPortMin <= 0 || c.Tunnel.TCPPortMax <= 0 || c.Tunnel.TCPPortMin > c.Tunnel.TCPPortMax {
		return errors.New("invalid tunnel TCP port range: tcp_port_min must be > 0 and <= tcp_port_max")
	}
	return nil
}

// ensureDatabaseDir creates the parent directory of the SQLite database file
// when it does not yet exist, so the first boot succeeds on a fresh host.
func (c *Config) ensureDatabaseDir() error {
	dir := filepath.Dir(c.Database.Path)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create database directory %q: %w", dir, err)
	}
	return nil
}
