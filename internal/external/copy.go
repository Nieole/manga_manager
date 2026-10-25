package external

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const transferCopyBufferSize = 1024 * 1024

var transferCopyBufferPool = sync.Pool{New: func() any { buf := make([]byte, transferCopyBufferSize); return &buf }}

// CopyFile stages a missing destination atomically and preserves an existing file.
func CopyFile(src, dst string, createdDirs map[string]struct{}) (bool, error) {
	if createdDirs == nil {
		createdDirs = make(map[string]struct{})
	}
	parentDir := filepath.Dir(dst)
	if _, ok := createdDirs[parentDir]; !ok {
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			return false, err
		}
		createdDirs[parentDir] = struct{}{}
	}
	if _, err := os.Stat(dst); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}

	sourceFile, err := os.Open(src)
	if err != nil {
		return false, err
	}
	defer sourceFile.Close()

	info, err := sourceFile.Stat()
	if err != nil {
		return false, err
	}

	// 临时文件名与源文件名无关，是刻意的：漫画文件名最容易逼近 NAME_MAX（255 字节，
	// 中文名 UTF-8 每字 3 字节只够约 85 字），把原名嵌进 pattern 再加随机后缀会让一批
	// 今天能正常传输的书直接报 "file name too long"。与 comicinfo 写回同款定长 pattern。
	targetFile, err := os.CreateTemp(parentDir, ".mm-transfer-*.tmp")
	if err != nil {
		return false, err
	}
	tmpName := targetFile.Name()
	committed := false
	defer func() {
		if committed {
			return
		}
		_ = targetFile.Close()
		_ = os.Remove(tmpName)
	}()

	bufPtr := transferCopyBufferPool.Get().(*[]byte)
	defer transferCopyBufferPool.Put(bufPtr)

	if _, err := io.CopyBuffer(targetFile, sourceFile, *bufPtr); err != nil {
		return false, err
	}
	// Sync 之后再 rename：外接盘掉电时，只有落盘的数据才谈得上「原子替换」。
	if err := targetFile.Sync(); err != nil {
		return false, err
	}
	if err := targetFile.Close(); err != nil {
		return false, err
	}
	// os.CreateTemp 建的是 0600，尽力放宽到 0644。用 _ = 忽略错误是有意的：
	// 目的地常是 exFAT/FAT32/网络挂载，chmod 在那里要么是 no-op 要么直接 ENOTSUP，
	// 为一个在该文件系统上根本没有意义的权限位让整本书传输失败不划算。
	_ = os.Chmod(tmpName, 0o644)
	// mtime 在 rename 之前设：os.Rename 不改 mtime，而 rename 之后目标已对外可见，
	// 此时再改属性等于让别人看到一个属性还没定型的文件。
	if err := os.Chtimes(tmpName, info.ModTime(), info.ModTime()); err != nil {
		return false, err
	}
	// rename 前再确认一次目标不存在。开头那次 Stat 到这里隔了整个拷贝过程（可能几分钟），
	// 期间别的传输任务完全可能已经把同一本书放好了；rename 会无声覆盖，那才是真的丢数据。
	if _, err := os.Stat(dst); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return false, err
	}
	committed = true
	return false, nil
}
