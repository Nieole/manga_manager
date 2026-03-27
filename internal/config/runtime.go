package config

import "sync"

// Manager provides synchronized access to the live runtime config.
type Manager struct {
	mu  sync.RWMutex
	cfg Config
}

func NewManager(cfg *Config) *Manager {
	m := &Manager{}
	if cfg != nil {
		m.cfg = *cfg
	}
	return m
}

func (m *Manager) Snapshot() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

func (m *Manager) Replace(cfg *Config) {
	if cfg == nil {
		return
	}
	m.mu.Lock()
	m.cfg = *cfg
	m.mu.Unlock()
}
