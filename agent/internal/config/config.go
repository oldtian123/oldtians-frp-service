// Package config loads the ofs-gateway-agent YAML configuration and the
// persistent state file (gateway_id / gateway_secret / current_config_version).
//
// Secrets are never logged by this package.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the agent's static configuration (see config.example.yaml).
type Config struct {
	ServerURL     string        `yaml:"server_url"`
	RegisterToken string        `yaml:"register_token"`
	Gateway       GatewayConfig `yaml:"gateway"`
	Agent         AgentConfig   `yaml:"agent"`
	Frpc          FrpcConfig    `yaml:"frpc"`
}

// GatewayConfig holds the human-friendly gateway name used at registration.
type GatewayConfig struct {
	Name string `yaml:"name"`
}

// AgentConfig holds the local API listener and the loop intervals.
type AgentConfig struct {
	ListenAddr                 string `yaml:"listen_addr"`
	ListenPort                 int    `yaml:"listen_port"`
	HeartbeatIntervalSeconds   int    `yaml:"heartbeat_interval_seconds"`
	SyncIntervalSeconds        int    `yaml:"sync_interval_seconds"`
	HealthCheckIntervalSeconds int    `yaml:"health_check_interval_seconds"`
	// StatePath is where gateway_id / secret / config version are persisted.
	StatePath string `yaml:"state_path"`
}

// FrpcConfig points at the frpc binary, the config it manages and its work dir.
type FrpcConfig struct {
	BinPath    string `yaml:"bin_path"`
	ConfigPath string `yaml:"config_path"`
	WorkDir    string `yaml:"work_dir"`
}

// Load reads, env-overrides, defaults and validates the agent config.
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
	return &cfg, nil
}

func (c *Config) applyEnvOverrides() {
	if v := os.Getenv("OFS_SERVER_URL"); v != "" {
		c.ServerURL = v
	}
	if v := os.Getenv("OFS_REGISTER_TOKEN"); v != "" {
		c.RegisterToken = v
	}
}

func (c *Config) applyDefaults() {
	if c.Agent.ListenAddr == "" {
		c.Agent.ListenAddr = "127.0.0.1"
	}
	if c.Agent.ListenPort == 0 {
		c.Agent.ListenPort = 8899
	}
	if c.Agent.HeartbeatIntervalSeconds == 0 {
		c.Agent.HeartbeatIntervalSeconds = 10
	}
	if c.Agent.SyncIntervalSeconds == 0 {
		c.Agent.SyncIntervalSeconds = 10
	}
	if c.Agent.HealthCheckIntervalSeconds == 0 {
		c.Agent.HealthCheckIntervalSeconds = 15
	}
	if c.Frpc.BinPath == "" {
		c.Frpc.BinPath = "/usr/local/bin/frpc"
	}
	if c.Frpc.WorkDir == "" {
		c.Frpc.WorkDir = "/etc/oldtians-frp-service"
	}
	if c.Frpc.ConfigPath == "" {
		c.Frpc.ConfigPath = filepath.Join(c.Frpc.WorkDir, "frpc.toml")
	}
	if c.Agent.StatePath == "" {
		c.Agent.StatePath = filepath.Join(c.Frpc.WorkDir, "state.yaml")
	}
	if c.Gateway.Name == "" {
		if h, err := os.Hostname(); err == nil {
			c.Gateway.Name = h
		} else {
			c.Gateway.Name = "home-gateway"
		}
	}
}

func (c *Config) validate() error {
	var missing []string
	if c.ServerURL == "" {
		missing = append(missing, "server_url")
	}
	if c.RegisterToken == "" {
		missing = append(missing, "register_token")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config fields: %v", missing)
	}
	return nil
}

// HeartbeatInterval / SyncInterval / HealthCheckInterval expose typed durations.
func (c *Config) HeartbeatInterval() int   { return c.Agent.HeartbeatIntervalSeconds }
func (c *Config) SyncInterval() int        { return c.Agent.SyncIntervalSeconds }
func (c *Config) HealthCheckInterval() int { return c.Agent.HealthCheckIntervalSeconds }
