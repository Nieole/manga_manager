// 守「cache.dir 下哪些子目录不属于缩略图」这条边界。
//
// 清缩略图的每条路径都问它，答漏一个就是一次误删：页图磁盘缓存嵌在缩略图目录内部，
// 不被点名就会被按「没人引用的封面」整棵删掉。

package config

import (
	"path/filepath"
	"testing"
)

func TestNonThumbnailDirsCoversPageCache(t *testing.T) {
	t.Run("配了 cache.dir 时页图缓存被点名", func(t *testing.T) {
		var cfg Config
		cfg.Cache.Dir = filepath.Join("var", "mm", "cache")

		dirs := NonThumbnailDirs(cfg)
		want := filepath.Clean(PageCacheDir(cfg))
		if len(dirs) != 1 || dirs[0] != want {
			t.Fatalf("NonThumbnailDirs = %v, want [%s] —— 漏掉它，清理缩略图就会删光页图磁盘缓存", dirs, want)
		}
	})

	t.Run("页图缓存确实嵌在缩略图目录内部", func(t *testing.T) {
		var cfg Config
		cfg.Cache.Dir = filepath.Join("var", "mm", "cache")

		thumbDir := filepath.Clean(ThumbnailDir(cfg))
		if got := filepath.Dir(filepath.Clean(PageCacheDir(cfg))); got != thumbDir {
			t.Fatalf("页图缓存的父目录为 %q, want %q", got, thumbDir)
		}
	})

	t.Run("没配 cache.dir 时两者互不包含", func(t *testing.T) {
		var cfg Config

		if dirs := NonThumbnailDirs(cfg); len(dirs) != 0 {
			t.Fatalf("NonThumbnailDirs = %v, want 空 —— 两个兜底目录本就分开，不该有豁免项", dirs)
		}
	})
}
