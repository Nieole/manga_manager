// cache.dir 下的目录划分。缩略图与页图磁盘缓存共用一个根，谁在哪、谁不该被谁清掉，
// 只在本文件回答一次——各清理路径自己记一份，漏一个就是一次误删。

package config

import (
	"path/filepath"
	"strings"
)

// ThumbnailDir 是封面缩略图的根目录，缩略图按哈希前两位分子目录存放其中。
// 配了 cache.dir 时它就是 cache.dir 本身。
func ThumbnailDir(cfg Config) string {
	if cfg.Cache.Dir != "" {
		return cfg.Cache.Dir
	}
	return filepath.Join(".", "data", "thumbnails")
}

// PageCacheDir 是页图磁盘缓存的目录。配了 cache.dir 时它落在 <cache.dir>/pages/，
// 也就是**嵌在 ThumbnailDir 内部**——清缩略图的代码必须先问过 NonThumbnailDirs。
func PageCacheDir(cfg Config) string {
	if cfg.Cache.Dir != "" {
		return filepath.Join(cfg.Cache.Dir, "pages")
	}
	return filepath.Join(".", "data", "page-cache")
}

// NonThumbnailDirs 返回落在 ThumbnailDir 内部、但不属于缩略图的目录（已 Clean）。
//
// 清缩略图的每条路径都必须跳过它们，连同整棵子树：缩略图清理按「不在 cover_path 集合里
// 就删」逐文件判定，页图缓存文件一条也不在那个集合里，走进去就是全删，收尾还会把空目录
// 一并移除。用户只想清没人引用的封面，代价却是之后每一页都要重新解码转码。
//
// 新增 cache.dir 下的缓存子目录时，把它加进这里，各清理路径无须改动。
func NonThumbnailDirs(cfg Config) []string {
	thumbDir := filepath.Clean(ThumbnailDir(cfg))
	var dirs []string
	for _, candidate := range []string{PageCacheDir(cfg)} {
		clean := filepath.Clean(candidate)
		if clean != thumbDir && strings.HasPrefix(clean, thumbDir+string(filepath.Separator)) {
			dirs = append(dirs, clean)
		}
	}
	return dirs
}
