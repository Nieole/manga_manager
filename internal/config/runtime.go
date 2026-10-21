package config

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// Manager provides synchronized access to the live runtime config.
type Manager struct {
	updateMu    sync.Mutex
	afterUpdate func()
	versionKey  string
	mu          sync.RWMutex
	cfg         Config
	writeFile   FileWriter
}

// FileWriter is the atomic configuration-file persistence boundary.
type FileWriter func(string, []byte, os.FileMode) error

// SetAfterUpdate registers the application notification run after a successful
// Save or Reload, while the update boundary is still held. It must not update Manager.
func (m *Manager) SetAfterUpdate(after func()) {
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	m.afterUpdate = after
}

func NewManager(cfg *Config, writers ...FileWriter) *Manager {
	m := &Manager{versionKey: rand.Text(), writeFile: AtomicWriteFile}
	if len(writers) > 0 && writers[0] != nil {
		m.writeFile = writers[0]
	}
	if cfg != nil {
		m.cfg = CloneConfig(*cfg)
	}
	return m
}

// Version returns an opaque expected-state token without exposing configuration secrets.
// Tokens are scoped to this Manager's lifetime; a server restart invalidates old tokens.
func (m *Manager) Version(cfg Config) string {
	data, _ := yaml.Marshal(cfg)
	digest := hmac.New(sha256.New, []byte(m.versionKey))
	_, _ = digest.Write(data)
	return `"` + hex.EncodeToString(digest.Sum(nil)) + `"`
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
	m.updateMu.Lock()
	defer m.updateMu.Unlock()
	m.replace(cfg)
}

func (m *Manager) replace(cfg *Config) {
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
	cfg.Tasks.ResumeAfterRestart = cloneBool(cfg.Tasks.ResumeAfterRestart)
	cfg.Server.AllowedOrigins = cloneStrings(cfg.Server.AllowedOrigins)
	cfg.Server.TrustedProxies = cloneStrings(cfg.Server.TrustedProxies)
	cfg.Library.Paths = cloneStrings(cfg.Library.Paths)
	cfg.Library.StoragePolicies = cloneStoragePolicies(cfg.Library.StoragePolicies)
	return cfg
}

func cloneBool(src *bool) *bool {
	if src == nil {
		return nil
	}
	value := *src
	return &value
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
