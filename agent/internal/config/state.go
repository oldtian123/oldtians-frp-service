package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// State is the persistent agent identity + applied config version. It is stored
// next to the frpc config (default /etc/oldtians-frp-service/state.yaml).
type State struct {
	GatewayID            string `yaml:"gateway_id"`
	GatewaySecret        string `yaml:"gateway_secret"`
	CurrentConfigVersion int64  `yaml:"current_config_version"`

	mu   sync.Mutex `yaml:"-"`
	path string     `yaml:"-"`
}

// LoadState reads the state file. A missing file yields an empty (unregistered)
// state, which is the normal first-boot case.
func LoadState(path string) (*State, error) {
	s := &State{path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("read state file %q: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, s); err != nil {
		return nil, fmt.Errorf("parse state file %q: %w", path, err)
	}
	s.path = path
	return s, nil
}

// Registered reports whether the agent already has credentials.
func (s *State) Registered() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.GatewayID != "" && s.GatewaySecret != ""
}

// Credentials returns the gateway id and secret.
func (s *State) Credentials() (id, secret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.GatewayID, s.GatewaySecret
}

// SetCredentials records the credentials and persists the state.
func (s *State) SetCredentials(id, secret string) error {
	s.mu.Lock()
	s.GatewayID = id
	s.GatewaySecret = secret
	s.mu.Unlock()
	return s.Save()
}

// ConfigVersion returns the currently-applied config version.
func (s *State) ConfigVersion() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.CurrentConfigVersion
}

// SetConfigVersion records the applied config version and persists the state.
func (s *State) SetConfigVersion(v int64) error {
	s.mu.Lock()
	s.CurrentConfigVersion = v
	s.mu.Unlock()
	return s.Save()
}

// Save writes the state file atomically (temp file + rename) with 0600 perms so
// the secret is not world-readable.
func (s *State) Save() error {
	s.mu.Lock()
	snapshot := State{
		GatewayID:            s.GatewayID,
		GatewaySecret:        s.GatewaySecret,
		CurrentConfigVersion: s.CurrentConfigVersion,
	}
	path := s.path
	s.mu.Unlock()

	if path == "" {
		return fmt.Errorf("state path is empty")
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create state dir %q: %w", dir, err)
		}
	}
	raw, err := yaml.Marshal(&snapshot)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}
