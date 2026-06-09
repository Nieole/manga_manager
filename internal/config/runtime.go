// 业务说明：本文件是业务实现，属于运行时配置管理层，负责读取、归一化和持久化漫画库、扫描、元数据、AI 和服务端选项。
// 它是后端各服务共享配置的来源，影响扫描路径、外部库、图片缓存和前端设置页展示。
// 维护时应避免直接修改配置副本，新增字段需要兼顾默认值、兼容迁移和前端表单含义。

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
