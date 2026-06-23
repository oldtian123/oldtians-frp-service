// Package tunnel owns tunnel validation, creation, mutation and the derived
// "view" (public URL / target / health) used by the management + panel APIs.
//
// The Service type holds all business rules so that both the management API and
// the publish API create tunnels through a single, consistent code path.
package tunnel

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/oldtian123/oldtians-frp-service/server/internal/config"
	"github.com/oldtian123/oldtians-frp-service/server/internal/database"
	"github.com/oldtian123/oldtians-frp-service/server/internal/eventlog"
)

// Service encapsulates tunnel business logic.
type Service struct {
	store  *database.Store
	events *eventlog.Logger
	cfg    *config.Config
}

// NewService constructs a tunnel Service.
func NewService(store *database.Store, events *eventlog.Logger, cfg *config.Config) *Service {
	return &Service{store: store, events: events, cfg: cfg}
}

// ServiceError is a typed error carrying an HTTP-mappable code.
type ServiceError struct {
	Code    string // bad_request | conflict | not_found | internal_error
	Message string
}

func (e *ServiceError) Error() string { return e.Message }

func badRequest(format string, a ...any) *ServiceError {
	return &ServiceError{Code: "bad_request", Message: fmt.Sprintf(format, a...)}
}
func conflict(format string, a ...any) *ServiceError {
	return &ServiceError{Code: "conflict", Message: fmt.Sprintf(format, a...)}
}
func notFound(format string, a ...any) *ServiceError {
	return &ServiceError{Code: "not_found", Message: fmt.Sprintf(format, a...)}
}
func internal(format string, a ...any) *ServiceError {
	return &ServiceError{Code: "internal_error", Message: fmt.Sprintf(format, a...)}
}

// CreateInput is the normalised input for creating a tunnel.
type CreateInput struct {
	GatewayID  string
	Name       string
	Type       string
	TargetHost string
	TargetPort int
	Domain     string
	RemotePort int
	Enabled    *bool // nil => true
	Source     string
	TTLSeconds int64
	ExpiresAt  *time.Time
}

var (
	nameRe     = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	hostnameRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)
)

// Create validates the input, allocates a TCP port if needed, persists the
// tunnel, bumps the gateway config version and records an event.
func (s *Service) Create(in CreateInput) (*database.Tunnel, error) {
	// gateway must exist
	exists, err := s.store.GatewayIDExists(in.GatewayID)
	if err != nil {
		return nil, internal("failed to check gateway: %v", err)
	}
	if !exists {
		return nil, notFound("gateway %q not found", in.GatewayID)
	}

	in.Type = strings.ToLower(strings.TrimSpace(in.Type))
	if in.Type != database.TunnelTypeHTTP && in.Type != database.TunnelTypeTCP {
		return nil, badRequest("type must be 'http' or 'tcp'")
	}

	in.Name = strings.TrimSpace(in.Name)
	if !nameRe.MatchString(in.Name) {
		return nil, badRequest("name must contain only letters, digits, '-' or '_'")
	}

	if !validHost(in.TargetHost) {
		return nil, badRequest("target_host %q is not a valid IP or hostname", in.TargetHost)
	}
	if in.TargetPort < 1 || in.TargetPort > 65535 {
		return nil, badRequest("target_port must be between 1 and 65535")
	}

	// name must be unique within the gateway
	if dup, err := s.store.TunnelNameExists(in.GatewayID, in.Name); err != nil {
		return nil, internal("failed to check name: %v", err)
	} else if dup {
		return nil, conflict("tunnel name %q already exists on this gateway", in.Name)
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}

	t := &database.Tunnel{
		ID:         database.NewID(),
		GatewayID:  in.GatewayID,
		Name:       in.Name,
		Type:       in.Type,
		TargetHost: in.TargetHost,
		TargetPort: in.TargetPort,
		Enabled:    enabled,
		AuthMode:   "none",
		Source:     in.Source,
		TTLSeconds: in.TTLSeconds,
		ExpiresAt:  in.ExpiresAt,
	}

	switch in.Type {
	case database.TunnelTypeHTTP:
		if in.Domain == "" {
			return nil, badRequest("http tunnel requires a domain")
		}
		if !validDomain(in.Domain) {
			return nil, badRequest("domain %q is not valid", in.Domain)
		}
		if dup, err := s.store.DomainExists(in.Domain); err != nil {
			return nil, internal("failed to check domain: %v", err)
		} else if dup {
			return nil, conflict("domain %q is already in use", in.Domain)
		}
		t.Domain = in.Domain

	case database.TunnelTypeTCP:
		if in.RemotePort != 0 {
			if in.RemotePort < 1 || in.RemotePort > 65535 {
				return nil, badRequest("remote_port must be between 1 and 65535")
			}
			if dup, err := s.store.RemotePortExists(in.RemotePort); err != nil {
				return nil, internal("failed to check remote_port: %v", err)
			} else if dup {
				return nil, conflict("remote_port %d is already in use", in.RemotePort)
			}
			t.RemotePort = in.RemotePort
		} else {
			port, err := s.allocateRemotePort()
			if err != nil {
				return nil, err
			}
			t.RemotePort = port
		}
	}

	if err := s.store.CreateTunnel(t); err != nil {
		return nil, internal("failed to create tunnel: %v", err)
	}
	if _, err := s.store.BumpConfigVersion(in.GatewayID); err != nil {
		return nil, internal("failed to bump config version: %v", err)
	}
	s.events.Info("tunnel.create", fmt.Sprintf("tunnel %q (%s) created", t.Name, t.Type),
		eventlog.WithGateway(in.GatewayID), eventlog.WithTunnel(t.ID))

	return t, nil
}

// allocateRemotePort returns the first free port in the configured TCP range.
func (s *Service) allocateRemotePort() (int, error) {
	min, max := s.cfg.Tunnel.TCPPortMin, s.cfg.Tunnel.TCPPortMax
	for p := min; p <= max; p++ {
		used, err := s.store.RemotePortExists(p)
		if err != nil {
			return 0, internal("failed to scan ports: %v", err)
		}
		if !used {
			return p, nil
		}
	}
	return 0, conflict("no free TCP remote port in range %d-%d", min, max)
}

// UpdateInput carries the mutable tunnel fields; nil pointers are left unchanged.
type UpdateInput struct {
	Enabled    *bool
	TargetHost *string
	TargetPort *int
	Domain     *string
	RemotePort *int
}

// Update validates and applies a partial update, then bumps the config version.
func (s *Service) Update(id string, in UpdateInput) (*database.Tunnel, error) {
	existing, err := s.store.GetTunnel(id)
	if err != nil {
		return nil, notFound("tunnel %q not found", id)
	}

	if in.TargetHost != nil && !validHost(*in.TargetHost) {
		return nil, badRequest("target_host %q is not valid", *in.TargetHost)
	}
	if in.TargetPort != nil && (*in.TargetPort < 1 || *in.TargetPort > 65535) {
		return nil, badRequest("target_port must be between 1 and 65535")
	}
	if in.Domain != nil && *in.Domain != "" {
		if !validDomain(*in.Domain) {
			return nil, badRequest("domain %q is not valid", *in.Domain)
		}
		if *in.Domain != existing.Domain {
			if dup, err := s.store.DomainExists(*in.Domain); err != nil {
				return nil, internal("failed to check domain: %v", err)
			} else if dup {
				return nil, conflict("domain %q is already in use", *in.Domain)
			}
		}
	}
	if in.RemotePort != nil && *in.RemotePort != 0 {
		if *in.RemotePort < 1 || *in.RemotePort > 65535 {
			return nil, badRequest("remote_port must be between 1 and 65535")
		}
		if *in.RemotePort != existing.RemotePort {
			if dup, err := s.store.RemotePortExists(*in.RemotePort); err != nil {
				return nil, internal("failed to check remote_port: %v", err)
			} else if dup {
				return nil, conflict("remote_port %d is already in use", *in.RemotePort)
			}
		}
	}

	updated, err := s.store.UpdateTunnel(id, database.TunnelUpdate{
		Enabled:    in.Enabled,
		TargetHost: in.TargetHost,
		TargetPort: in.TargetPort,
		Domain:     in.Domain,
		RemotePort: in.RemotePort,
	})
	if err != nil {
		return nil, internal("failed to update tunnel: %v", err)
	}
	if _, err := s.store.BumpConfigVersion(updated.GatewayID); err != nil {
		return nil, internal("failed to bump config version: %v", err)
	}
	s.events.Info("tunnel.update", fmt.Sprintf("tunnel %q updated", updated.Name),
		eventlog.WithGateway(updated.GatewayID), eventlog.WithTunnel(updated.ID))

	return updated, nil
}

// Delete removes a tunnel (and its health row) and bumps the config version.
func (s *Service) Delete(id string) error {
	t, err := s.store.GetTunnel(id)
	if err != nil {
		return notFound("tunnel %q not found", id)
	}
	if err := s.store.DeleteTunnel(id); err != nil {
		return internal("failed to delete tunnel: %v", err)
	}
	if _, err := s.store.BumpConfigVersion(t.GatewayID); err != nil {
		return internal("failed to bump config version: %v", err)
	}
	s.events.Info("tunnel.delete", fmt.Sprintf("tunnel %q deleted", t.Name),
		eventlog.WithGateway(t.GatewayID), eventlog.WithTunnel(t.ID))
	return nil
}

// PublicURL returns the externally reachable URL for a tunnel.
func (s *Service) PublicURL(t *database.Tunnel) string {
	switch t.Type {
	case database.TunnelTypeHTTP:
		return "https://" + t.Domain
	case database.TunnelTypeTCP:
		return fmt.Sprintf("%s:%d", s.cfg.FRP.ServerAddr, t.RemotePort)
	default:
		return ""
	}
}

// --- validation helpers ------------------------------------------------------

func validHost(h string) bool {
	h = strings.TrimSpace(h)
	if h == "" {
		return false
	}
	if net.ParseIP(h) != nil {
		return true
	}
	return hostnameRe.MatchString(h)
}

func validDomain(d string) bool {
	d = strings.TrimSpace(d)
	if !strings.Contains(d, ".") {
		return false
	}
	return hostnameRe.MatchString(d)
}
