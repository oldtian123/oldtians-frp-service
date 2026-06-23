-- Initial schema for Oldtian's FRP Service control-api (SQLite).
--
-- NOTE: This file is the human-readable / manual-migration copy. The control-api
-- binary embeds an identical copy at server/internal/database/schema.sql and
-- applies it automatically on startup (CREATE TABLE IF NOT EXISTS is idempotent).
-- Keep both files in sync whenever the schema changes.

CREATE TABLE IF NOT EXISTS gateways (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  secret_hash TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'offline',
  hostname TEXT,
  os TEXT,
  arch TEXT,
  lan_ip TEXT,
  public_ip TEXT,
  agent_version TEXT,
  current_config_version INTEGER DEFAULT 0,
  last_seen_at DATETIME,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS tunnels (
  id TEXT PRIMARY KEY,
  gateway_id TEXT NOT NULL,
  name TEXT NOT NULL,
  type TEXT NOT NULL,
  target_host TEXT NOT NULL,
  target_port INTEGER NOT NULL,
  domain TEXT,
  remote_port INTEGER,
  enabled BOOLEAN DEFAULT 1,
  auth_mode TEXT DEFAULT 'none',
  ttl_seconds INTEGER,
  expires_at DATETIME,
  source TEXT,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tunnels_gateway_id ON tunnels (gateway_id);

CREATE TABLE IF NOT EXISTS config_versions (
  gateway_id TEXT PRIMARY KEY,
  version INTEGER NOT NULL DEFAULT 1,
  updated_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS health_checks (
  tunnel_id TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  latency_ms INTEGER,
  message TEXT,
  checked_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS event_logs (
  id TEXT PRIMARY KEY,
  gateway_id TEXT,
  tunnel_id TEXT,
  level TEXT NOT NULL,
  event_type TEXT NOT NULL,
  message TEXT,
  created_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_event_logs_created_at ON event_logs (created_at);
