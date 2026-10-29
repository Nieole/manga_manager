package maintenance_test

import (
	"manga-manager/internal/config"
	"manga-manager/internal/task"
	"os"
	"path/filepath"
	"testing"
)

func TestQueuedThumbnailRebuildKeepsItsOriginalCacheProtection(t *testing.T) {
	var body func()
	r := newRig(t, func(fn func()) { body = fn })
	before := r.cfg.Snapshot()
	pageDir := config.PageCacheDir(before)
	if err := os.MkdirAll(pageDir, 0755); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(pageDir, "reader-page.bin")
	if err := os.WriteFile(page, []byte("cached reader page"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := r.service.RebuildThumbnails(task.TriggerManual); err != nil {
		t.Fatal(err)
	}
	next := config.CloneConfig(before)
	next.Cache.Dir = t.TempDir()
	r.cfg.Replace(&next)
	body()
	data, err := os.ReadFile(page)
	if err != nil || string(data) != "cached reader page" {
		t.Fatalf("hot update changed cleanup intent: data=%q err=%v", data, err)
	}
}
