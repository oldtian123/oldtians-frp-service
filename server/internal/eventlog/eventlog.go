// Package eventlog provides a small helper around the event_logs table so that
// important operations (registrations, tunnel changes, reload results, health
// transitions) are recorded consistently. Secrets must never be passed in.
package eventlog

import (
	"log"

	"github.com/oldtian123/oldtians-frp-service/server/internal/database"
)

// Levels used across the codebase.
const (
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// Logger writes structured events to the database and mirrors them to stdout.
type Logger struct {
	store *database.Store
}

// New returns a Logger backed by the given store.
func New(store *database.Store) *Logger {
	return &Logger{store: store}
}

// Entry describes a single event to record. GatewayID / TunnelID are optional.
type Entry struct {
	GatewayID string
	TunnelID  string
	Level     string
	EventType string
	Message   string
}

// Write persists the event. Failures to persist are logged but never returned,
// since event logging must not break the primary request flow.
func (l *Logger) Write(e Entry) {
	if e.Level == "" {
		e.Level = LevelInfo
	}
	rec := &database.EventLog{
		ID:        database.NewID(),
		GatewayID: e.GatewayID,
		TunnelID:  e.TunnelID,
		Level:     e.Level,
		EventType: e.EventType,
		Message:   e.Message,
	}
	if err := l.store.CreateEventLog(rec); err != nil {
		log.Printf("eventlog: failed to persist event %q: %v", e.EventType, err)
	}
}

// Info records an informational event.
func (l *Logger) Info(eventType, message string, opts ...Option) {
	l.write(LevelInfo, eventType, message, opts...)
}

// Warn records a warning event.
func (l *Logger) Warn(eventType, message string, opts ...Option) {
	l.write(LevelWarn, eventType, message, opts...)
}

// Error records an error event.
func (l *Logger) Error(eventType, message string, opts ...Option) {
	l.write(LevelError, eventType, message, opts...)
}

func (l *Logger) write(level, eventType, message string, opts ...Option) {
	e := Entry{Level: level, EventType: eventType, Message: message}
	for _, o := range opts {
		o(&e)
	}
	l.Write(e)
}

// Option mutates an Entry; used to attach a gateway or tunnel id fluently.
type Option func(*Entry)

// WithGateway attaches a gateway id to the event.
func WithGateway(id string) Option { return func(e *Entry) { e.GatewayID = id } }

// WithTunnel attaches a tunnel id to the event.
func WithTunnel(id string) Option { return func(e *Entry) { e.TunnelID = id } }
