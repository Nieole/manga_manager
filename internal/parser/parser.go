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
	// 维度 1: 路径深度优先 (浅目录文件优先作为封面)
	aDepth := strings.Count(a, "/")
	bDepth := strings.Count(b, "/")
	if aDepth != bDepth {
		return aDepth < bDepth
	}

	// 维度 2: 漫画封面关键字启发式识别 (针对同一级目录)
	aBase := filepath.Base(a)
	bBase := filepath.Base(b)
	aLow := strings.ToLower(aBase)
	bLow := strings.ToLower(bBase)

	isACover := strings.Contains(aLow, "cover") || strings.Contains(aLow, "folder") || strings.Contains(aLow, "poster") || strings.Contains(aLow, "p000")
	isBCover := strings.Contains(bLow, "cover") || strings.Contains(bLow, "folder") || strings.Contains(bLow, "poster") || strings.Contains(bLow, "p000")

	if isACover && !isBCover {
		return true
	}
	if !isACover && isBCover {
		return false
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
