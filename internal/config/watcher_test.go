// 业务说明：本文件守卫配置热重载的四条性质：去抖、值域门禁、缺文件时不覆盖、可被停掉。
//
// 这条链路的每种失效都是静默的：坏值直接生效、整份配置被默认值覆盖、停机期间还在改全局资源。
// 用例直接向事件循环注入 fsnotify 事件，因而不依赖真实文件系统的事件时序（那在 CI 上不稳）。

package config

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

const testDebounce = 60 * time.Millisecond

// newTestWatcher 构造一个不接真实 fsnotify、由测试自己投递事件的 Watcher。
// apply 的调用发生在监听 goroutine 上，故计数器用 atomic（用例要过 -race）。
func newTestWatcher(t *testing.T, path string, mgr *Manager) (*Watcher, chan fsnotify.Event, *atomic.Int32) {
	t.Helper()
	var applyCalls atomic.Int32
	w := &Watcher{
		path:     path,
		manager:  mgr,
		apply:    func(*Config) error { applyCalls.Add(1); return nil },
		debounce: testDebounce,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	events := make(chan fsnotify.Event, 16)
	errs := make(chan error, 1)
	go w.run(events, errs)
	t.Cleanup(w.Stop)
	return w, events, &applyCalls
}

// writeTestConfig 落盘一份**取值合法**的配置。基线复用 validBaseConfig：
// 从零值 Config 起手写出来的 YAML 会缺 llm.model 之类的必填项，那样的夹具会让
// 「值域门禁」用例恒绿、「去抖」用例恒红——测的是夹具而不是代码。
func writeTestConfig(t *testing.T, path string, mutate func(*Config)) {
	t.Helper()
	cfg := validBaseConfig(t)
	NormalizeConfig(cfg)
	if mutate != nil {
		mutate(cfg)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, ConfigFilePerm); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestWatcherDebouncesEventBurst(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeTestConfig(t, path, nil)
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	mgr := NewManager(cfg)

	_, events, applyCalls := newTestWatcher(t, path, mgr)

	// 改成一个与当前不同的值，让重载确实有事可做。
	writeTestConfig(t, path, func(c *Config) { c.Scanner.ArchivePoolSize = 9 })

	// 一次保存在目录监听下会产生 Create / Rename / Write 多个事件；本项目的原子写也是如此。
	for _, op := range []fsnotify.Op{fsnotify.Create, fsnotify.Rename, fsnotify.Write, fsnotify.Write} {
		events <- fsnotify.Event{Name: path, Op: op}
	}

	// 断言的是**时序**而不是次数。次数在这里不能证明去抖：内容相等检查本身就会把后续几次
	// 重载压成空操作，去掉去抖后 apply 次数一样是 1。真正只有去抖能保证的性质是
	// 「突发事件期间不立刻动手」——省掉的是那几次读盘 + YAML 解析。
	time.Sleep(testDebounce / 3)
	if got := applyCalls.Load(); got != 0 {
		t.Fatalf("去抖窗口内就已经 apply 了 %d 次 —— 突发事件没有被合并", got)
	}

	time.Sleep(6 * testDebounce)
	if got := applyCalls.Load(); got != 1 {
		t.Fatalf("apply 被调用了 %d 次, want 1", got)
	}
	if got := mgr.Snapshot().Scanner.ArchivePoolSize; got != 9 {
		t.Fatalf("ArchivePoolSize = %d, want 9", got)
	}
}

func TestWatcherRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		// 这几个都能穿过 NormalizeConfig（它只兜零值），此前会被原样装进运行中的进程。
		{"端口越界", func(c *Config) { c.Server.Port = 70000 }},
		{"归档池为负", func(c *Config) { c.Scanner.ArchivePoolSize = -3 }},
		{"缩略图格式非法", func(c *Config) { c.Scanner.ThumbnailFormat = "gif" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			writeTestConfig(t, path, nil)
			cfg, err := LoadConfigFile(path)
			if err != nil {
				t.Fatalf("LoadConfigFile: %v", err)
			}
			mgr := NewManager(cfg)
			before := mgr.Snapshot()

			_, events, applyCalls := newTestWatcher(t, path, mgr)

			writeTestConfig(t, path, tc.mutate)
			events <- fsnotify.Event{Name: path, Op: fsnotify.Write}
			time.Sleep(6 * testDebounce)

			if got := applyCalls.Load(); got != 0 {
				t.Fatalf("非法配置仍触发了 %d 次 apply —— 坏值会立刻在运行中的进程里生效", got)
			}
			if after := mgr.Snapshot(); after.Server.Port != before.Server.Port ||
				after.Scanner.ArchivePoolSize != before.Scanner.ArchivePoolSize ||
				after.Scanner.ThumbnailFormat != before.Scanner.ThumbnailFormat {
				t.Fatalf("非法配置被装进了内存：%+v", after.Scanner)
			}
		})
	}
}

// TestWatcherDoesNotRecreateMissingConfig 是本项最危险的一条。
//
// 编辑器保存常常是「先把 config.yaml rename 走、再写新文件」。事件若落在文件缺失的窗口，
// 用 LoadConfig 会走 createDefaultConfig —— 把用户的整份配置**覆盖成默认值**，
// 同时把内存配置也清成默认。一次编辑保存换来整份配置丢失。
func TestWatcherDoesNotRecreateMissingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeTestConfig(t, path, func(c *Config) { c.Scanner.ArchivePoolSize = 7 })
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	mgr := NewManager(cfg)

	_, events, applyCalls := newTestWatcher(t, path, mgr)

	// 模拟「rename 走了、还没写回」的中间态。
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	events <- fsnotify.Event{Name: path, Op: fsnotify.Rename}
	time.Sleep(6 * testDebounce)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("热重载把配置文件重新生成了出来（err=%v）—— 用户的配置已被默认值覆盖", err)
	}
	if got := applyCalls.Load(); got != 0 {
		t.Fatalf("文件缺失时仍触发了 %d 次 apply", got)
	}
	if got := mgr.Snapshot().Scanner.ArchivePoolSize; got != 7 {
		t.Fatalf("内存配置被改成了 %d，应保留原值 7", got)
	}
}

// TestWatcherSkipsIdenticalContent 覆盖自写回环：
// 设置页保存走的也是 AtomicWriteFile，必然回弹一次事件，不该因此重建派生资源。
func TestWatcherSkipsIdenticalContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeTestConfig(t, path, nil)
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	mgr := NewManager(cfg)

	_, events, applyCalls := newTestWatcher(t, path, mgr)
	events <- fsnotify.Event{Name: path, Op: fsnotify.Write}
	time.Sleep(6 * testDebounce)

	if got := applyCalls.Load(); got != 0 {
		t.Fatalf("内容未变却触发了 %d 次 apply —— 每次 UI 保存都会白重建一遍归档池与 AI 信号量", got)
	}
}

// TestWatcherStopEndsEventLoop 守卫「监听器真的能被停掉」。
// 此前它是裸 goroutine，进程活着就永远停不掉——停机排空期间仍可能落地一次重载。
func TestWatcherStopEndsEventLoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeTestConfig(t, path, nil)
	cfg, err := LoadConfigFile(path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	mgr := NewManager(cfg)

	w, events, applyCalls := newTestWatcher(t, path, mgr)

	stopped := make(chan struct{})
	go func() { w.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop 没有返回：事件循环停不下来")
	}

	// 停止之后投递的事件不该再引发重载。
	writeTestConfig(t, path, func(c *Config) { c.Scanner.ArchivePoolSize = 11 })
	events <- fsnotify.Event{Name: path, Op: fsnotify.Write}
	time.Sleep(6 * testDebounce)

	if got := applyCalls.Load(); got != 0 {
		t.Fatalf("Stop 之后仍发生了 %d 次 apply", got)
	}
	if got := mgr.Snapshot().Scanner.ArchivePoolSize; got == 11 {
		t.Fatal("Stop 之后配置仍被热重载改写了")
	}
}

// TestStopIsSafeOnNilWatcher 覆盖 StartWatcher 失败时调用方拿到 nil 的情形。
func TestStopIsSafeOnNilWatcher(t *testing.T) {
	var w *Watcher
	w.Stop()
}
