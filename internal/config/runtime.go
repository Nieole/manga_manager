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
		m.cfg = CloneConfig(*cfg)
	}
	return m
}

// Snapshot 返回配置的**深拷贝**。
//
// Config 的值拷贝只复制切片头，底层数组仍与 Manager 内部共享。这在本仓库不是理论问题：
// ResolveStoragePolicy 按值收 Config，却对 cfg.Library.StoragePolicies[i] 做就地归一化——
// 那次写入会直接落到 Manager 持有的那个数组上。扫描时每个 worker 对每个文件都会调它一次，
// 于是「读一份配置」这件看起来纯粹的事，实际是多个 goroutine 并发写同一段内存。
//
// 深拷贝的成本只是几个小切片的复制，换来的是调用方拿到真正独立的副本。
func (m *Manager) Snapshot() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return CloneConfig(m.cfg)
}

func (m *Manager) Replace(cfg *Config) {
	if cfg == nil {
		return
	}
	m.mu.Lock()
	// 存入时也复制一份：否则调用方手里那个 *Config 在 Replace 之后仍能改动 Manager 的状态。
	m.cfg = CloneConfig(*cfg)
	m.mu.Unlock()
}

// CloneConfig 返回 cfg 的深拷贝，逐一复制其中的引用类型字段。
//
// 新增切片 / map / 指针字段时**必须**在这里补上对应的复制，
// 否则又会退回「值拷贝看着独立、底层却共享」的状态。
func CloneConfig(cfg Config) Config {
	cfg.Server.AllowedOrigins = cloneStrings(cfg.Server.AllowedOrigins)
	cfg.Server.TrustedProxies = cloneStrings(cfg.Server.TrustedProxies)
	cfg.Library.Paths = cloneStrings(cfg.Library.Paths)
	cfg.Library.StoragePolicies = cloneStoragePolicies(cfg.Library.StoragePolicies)
	return cfg
}

func cloneStrings(src []string) []string {
	if src == nil {
		return nil
	}
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

func cloneStoragePolicies(src []LibraryStoragePolicy) []LibraryStoragePolicy {
	if src == nil {
		return nil
	}
	dst := make([]LibraryStoragePolicy, len(src))
	copy(dst, src)
	return dst
}
