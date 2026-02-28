package parser

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// OpenArchive 根据后缀自动分发归档解压驱动
func OpenArchive(path string) (Archive, error) {
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".cbz", ".zip":
		return OpenZip(path)
	case ".cbr", ".rar":
		return OpenRar(path)
	default:
		return nil, fmt.Errorf("unsupported archive format: %s", ext)
	}
}

var chunkRegexp = regexp.MustCompile(`(\d+|\D+)`)

// naturalCompare 模拟文件管理器的自然排序算法，比如 1.jpg 排在 10.jpg 之前。
// naturalCompare 模拟文件管理器的自然排序算法，并针对漫画归档优化（优先浅层目录，优先 Cover 关键字）。
func naturalCompare(a, b string) bool {
	// 统一使用通用斜杠进行路径处理
	ap := filepath.ToSlash(a)
	bp := filepath.ToSlash(b)

	// 维度 1: 漫画封面关键字启发式识别 (跨目录层级)
	// 即使封面在子目录中 (如 Cover/01.jpg)，也应优先于根目录的非封面图 (如 Ad.jpg)
	coverKeywords := []string{"cover", "folder", "poster", "front", "fc", "封面", "封面一", "index"}
	excludeKeywords := []string{"back", "rear", "bc", "封底", "广告", "ad"}

	isCover := func(path string) bool {
		lowPath := strings.ToLower(filepath.ToSlash(path))
		base := filepath.Base(lowPath)

		// 如果文件名包含任何排除关键字，则不视为封面
		for _, ex := range excludeKeywords {
			if strings.Contains(base, ex) {
				return false
			}
		}

		// 1. 优先检查文件名是否包含关键标识
		for _, kw := range coverKeywords {
			if strings.Contains(base, kw) {
				return true
			}
		}

		// 2. 检查父目录是否包含关键标识 (如 "Cover/01.jpg")
		dir := filepath.Dir(lowPath)
		if dir != "." && dir != "/" {
			dirBase := filepath.Base(dir)
			for _, kw := range coverKeywords {
				if strings.Contains(dirBase, kw) || dirBase == "scans" {
					return true
				}
			}
		}
		return false
	}

	isACover := isCover(ap)
	isBCover := isCover(bp)

	if isACover && !isBCover {
		return true
	}
	if !isACover && isBCover {
		return false
	}

	// 维度 2: 路径深度优先 (同为封面或同非封面时，浅目录优先)
	aDepth := strings.Count(ap, "/")
	bDepth := strings.Count(bp, "/")
	if aDepth != bDepth {
		return aDepth < bDepth
	}

	// 维度 3: 自然序分块比较 (处理 1.jpg vs 10.jpg)
	aChunks := chunkRegexp.FindAllString(strings.ToLower(a), -1)
	bChunks := chunkRegexp.FindAllString(strings.ToLower(b), -1)

	n := len(aChunks)
	if len(bChunks) < n {
		n = len(bChunks)
	}

	for i := 0; i < n; i++ {
		aChunk := aChunks[i]
		bChunk := bChunks[i]

		if aChunk == bChunk {
			continue
		}

		// 检查是否都是数字
		aNum, aErr := strconv.Atoi(aChunk)
		bNum, bErr := strconv.Atoi(bChunk)

		if aErr == nil && bErr == nil {
			if aNum != bNum {
				return aNum < bNum
			}
			// 数值相同时，前导零更多的 (更长的) 排在前面，例如 01.jpg 优于 1.jpg
			if len(aChunk) != len(bChunk) {
				return len(aChunk) > len(bChunk)
			}
		}

		// 否则普通字符串比较
		return aChunk < bChunk
	}

	return len(aChunks) < len(bChunks)
}
