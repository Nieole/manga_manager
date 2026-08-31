// 本文件守卫磁盘页缓存清道夫的两条性质：
// 未超标时不收集任何条目（早退），超标时只保留「最旧的那一批」候选而非全部文件。
//
// 早退不是可选优化：2 GiB 上限、每页几十到几百 KB，稳态下单目录可能有几千到几万个条目，
// 未超标时若仍全量列出，这份开销纯属白付。

package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedCacheFiles 写入 n 个文件，mtime 依次递增（i 越大越新）。
func seedCacheFiles(t *testing.T, dir string, n int, size int) []string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	base := time.Now().Add(-time.Duration(n) * time.Minute)
	paths := make([]string, 0, n)
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, "page"+itoaTest(i)+".webp")
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		mod := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		paths = append(paths, p)
	}
	return paths
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestCollectOldestFilesKeepsOnlyWhatIsNeeded 是核心：
// 候选集的规模应当由「要删多少」决定，而不是由目录里有多少文件决定。
func TestCollectOldestFilesKeepsOnlyWhatIsNeeded(t *testing.T) {
	dir := t.TempDir()
	const fileCount = 200
	const fileSize = 1 << 10 // 1 KiB
	seedCacheFiles(t, dir, fileCount, fileSize)

	// 只需要删 5 KiB —— 候选不该是全部 200 个文件。
	const need = 5 << 10
	victims := collectOldestFiles(dir, need)

	if len(victims) == 0 {
		t.Fatal("一个候选都没收集到")
	}
	if len(victims) >= fileCount {
		t.Fatalf("收集了 %d 个候选（目录共 %d 个）—— 候选规模没有被 need 约束住",
			len(victims), fileCount)
	}

	var held int64
	for _, v := range victims {
		held += v.size
	}
	if held < need {
		t.Fatalf("候选累计 %d 字节，不足以删够 %d 字节", held, need)
	}

	// 必须是**最旧**的那一批，且按从旧到新排列。
	for i := 1; i < len(victims); i++ {
		if victims[i].mod.Before(victims[i-1].mod) {
			t.Fatalf("候选未按从旧到新排列：第 %d 个比第 %d 个还旧", i, i-1)
		}
	}
	// 第一个候选应当就是最旧的那个文件（page0）。
	if filepath.Base(victims[0].path) != "page0.webp" {
		t.Fatalf("最旧的候选是 %q, want page0.webp —— 淘汰顺序不对",
			filepath.Base(victims[0].path))
	}
}

// TestEnforcePageCacheBudgetEvictsOldestFirst 走完整路径，验证淘汰行为与低水位。
func TestEnforcePageCacheBudgetEvictsOldestFirst(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	cfg := controller.config.Snapshot()
	cfg.Cache.PageDiskCacheEnabled = true
	cfg.Cache.PageDiskCacheMaxBytes = 10 << 10 // 10 KiB
	controller.config.Replace(&cfg)

	dir := controller.processedImageCacheDir()
	const fileCount = 30
	const fileSize = 1 << 10
	paths := seedCacheFiles(t, dir, fileCount, fileSize)

	controller.enforcePageCacheBudget()

	var remaining int64
	survivors := 0
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			remaining += info.Size()
			survivors++
		}
	}
	if remaining > cfg.Cache.PageDiskCacheMaxBytes {
		t.Fatalf("淘汰后仍有 %d 字节，超过上限 %d", remaining, cfg.Cache.PageDiskCacheMaxBytes)
	}
	if survivors == 0 {
		t.Fatal("全被删光了 —— 淘汰过度")
	}
	// 最旧的必然先被删，最新的必然还在。
	if _, err := os.Stat(paths[0]); err == nil {
		t.Fatal("最旧的文件还在 —— 淘汰顺序不对")
	}
	if _, err := os.Stat(paths[fileCount-1]); err != nil {
		t.Fatalf("最新的文件被删了：%v", err)
	}
}

// TestEnforcePageCacheBudgetSkipsWhenUnderLimit 守卫早退：未超标时什么都不该动。
func TestEnforcePageCacheBudgetSkipsWhenUnderLimit(t *testing.T) {
	controller, _, _, _ := newTestController(t)

	cfg := controller.config.Snapshot()
	cfg.Cache.PageDiskCacheEnabled = true
	cfg.Cache.PageDiskCacheMaxBytes = 1 << 20 // 1 MiB，远大于我们写入的量
	controller.config.Replace(&cfg)

	dir := controller.processedImageCacheDir()
	paths := seedCacheFiles(t, dir, 20, 1<<10)

	controller.enforcePageCacheBudget()

	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("未超标却删了文件 %s：%v", filepath.Base(p), err)
		}
	}
}
