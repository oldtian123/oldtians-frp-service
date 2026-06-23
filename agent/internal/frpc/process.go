package frpc

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Status values reported to the heartbeat.
const (
	StatusRunning = "running"
	StatusStopped = "stopped"
)

// Manager owns the frpc child process and the on-disk frpc.toml.
//
// The current implementation runs frpc as a child process and "reloads" by
// restarting it. The public interface (Apply / EnsureRunning / Status / Stop)
// is intentionally narrow so it can later be swapped for frp's hot-reload admin
// API without touching callers.
type Manager struct {
	binPath    string
	configPath string
	workDir    string
	logger     *log.Logger

	mu      sync.Mutex
	current *managedProc
}

type managedProc struct {
	cmd  *exec.Cmd
	done chan struct{}
}

// NewManager builds a frpc process Manager.
func NewManager(binPath, configPath, workDir string, logger *log.Logger) *Manager {
	if logger == nil {
		logger = log.Default()
	}
	return &Manager{binPath: binPath, configPath: configPath, workDir: workDir, logger: logger}
}

// ConfigPath returns the managed frpc.toml path.
func (m *Manager) ConfigPath() string { return m.configPath }

// Apply writes the new config (validating it first) and reloads frpc.
func (m *Manager) Apply(content string) error {
	if err := m.WriteConfig(content); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reloadLocked()
}

// WriteConfig atomically writes frpc.toml after a best-effort validation.
func (m *Manager) WriteConfig(content string) error {
	if m.workDir != "" {
		if err := os.MkdirAll(m.workDir, 0o755); err != nil {
			return fmt.Errorf("create work dir %q: %w", m.workDir, err)
		}
	}
	tmp := m.configPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write temp frpc config: %w", err)
	}
	if err := m.validate(tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, m.configPath); err != nil {
		return fmt.Errorf("install frpc config: %w", err)
	}
	return nil
}

// validate runs `frpc verify -c <tmp>` when supported. Older frp releases without
// the verify subcommand, or a missing binary, skip validation (non-fatal) so the
// agent still installs the config; EnsureRunning surfaces a missing binary.
func (m *Manager) validate(path string) error {
	cmd := exec.Command(m.binPath, "verify", "-c", path)
	cmd.Dir = m.workDir
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if _, ok := err.(*exec.Error); ok {
		// binary missing / not executable -> cannot validate, skip.
		return nil
	}
	s := strings.ToLower(string(out))
	if strings.Contains(s, "unknown command") || strings.Contains(s, "unknown shorthand") {
		return nil // verify subcommand unsupported on this frp version
	}
	return fmt.Errorf("frpc config validation failed: %s", strings.TrimSpace(string(out)))
}

// EnsureRunning starts frpc if it is not currently running.
func (m *Manager) EnsureRunning() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isRunningLocked() {
		return nil
	}
	return m.startLocked()
}

// Status reports whether frpc is currently running.
func (m *Manager) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isRunningLocked() {
		return StatusRunning
	}
	return StatusStopped
}

// Stop kills the frpc process (used on agent shutdown).
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
}

// reloadLocked restarts frpc to pick up a new config.
func (m *Manager) reloadLocked() error {
	m.stopLocked()
	return m.startLocked()
}

func (m *Manager) startLocked() error {
	if _, err := os.Stat(m.binPath); err != nil {
		return fmt.Errorf("frpc binary not found at %q: %w", m.binPath, err)
	}
	cmd := exec.Command(m.binPath, "-c", m.configPath)
	cmd.Dir = m.workDir
	cmd.Stdout = &logWriter{logger: m.logger, prefix: "[frpc] "}
	cmd.Stderr = &logWriter{logger: m.logger, prefix: "[frpc] "}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start frpc: %w", err)
	}
	mp := &managedProc{cmd: cmd, done: make(chan struct{})}
	m.current = mp
	go func() {
		werr := cmd.Wait()
		close(mp.done)
		if werr != nil {
			m.logger.Printf("frpc process exited: %v", werr)
		} else {
			m.logger.Printf("frpc process exited cleanly")
		}
	}()
	m.logger.Printf("frpc started (pid %d, config %s)", cmd.Process.Pid, m.configPath)
	return nil
}

func (m *Manager) stopLocked() {
	if m.current == nil {
		return
	}
	mp := m.current
	if !m.isRunningLocked() {
		m.current = nil
		return
	}
	if mp.cmd.Process != nil {
		_ = mp.cmd.Process.Kill()
	}
	select {
	case <-mp.done:
	case <-time.After(5 * time.Second):
		m.logger.Printf("frpc did not exit within 5s after kill")
	}
	m.current = nil
}

func (m *Manager) isRunningLocked() bool {
	if m.current == nil {
		return false
	}
	select {
	case <-m.current.done:
		return false
	default:
		return true
	}
}

// logWriter forwards a child process's output to the agent logger, line by line.
type logWriter struct {
	logger *log.Logger
	prefix string
}

func (w *logWriter) Write(p []byte) (int, error) {
	text := strings.TrimRight(string(p), "\n")
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			w.logger.Printf("%s%s", w.prefix, line)
		}
	}
	return len(p), nil
}
