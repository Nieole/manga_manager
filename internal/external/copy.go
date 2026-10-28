package external

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"manga-manager/internal/diskwork"
	"manga-manager/internal/storageio"
	"manga-manager/internal/taskcontrol"
)

const transferCopyBufferSize = 1024 * 1024

var transferCopyBufferPool = sync.Pool{New: func() any { buf := make([]byte, transferCopyBufferSize); return &buf }}

// ErrCopyChanged means source, staging or destination no longer matches this copy.
var ErrCopyChanged = errors.New("copy cannot continue safely because file facts changed")

// CopyFile stages a missing destination atomically and preserves an existing file.
func CopyFile(src, dst string, createdDirs map[string]struct{}) (bool, error) {
	return copyFile(context.Background(), src, dst, createdDirs, nil, nil, nil)
}

func copyFile(ctx context.Context, src, dst string, createdDirs map[string]struct{}, work TransferWork, progress func(int64, int64), validate func() error) (bool, error) {
	if err := taskcontrol.Wait(ctx); err != nil {
		return false, err
	}
	if createdDirs == nil {
		createdDirs = make(map[string]struct{})
	}
	parentDir := filepath.Dir(dst)
	if _, ok := createdDirs[parentDir]; !ok {
		if err := os.MkdirAll(parentDir, 0755); err != nil {
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
	if validate != nil && !info.Mode().IsRegular() {
		return false, ErrCopyChanged
	}
	physicalParent, err := filepath.EvalSymlinks(parentDir)
	if err != nil {
		return false, err
	}
	// A fixed-length staging name also fits destinations with long comic names.
	// The physical parent keeps cleanup attached to this copy if an alias changes.
	targetFile, err := os.CreateTemp(physicalParent, ".mm-transfer-*.tmp")
	if err != nil {
		return false, err
	}
	tmpName := targetFile.Name()
	tmpInfo, err := targetFile.Stat()
	if err != nil {
		_ = targetFile.Close()
		_ = os.Remove(tmpName)
		return false, err
	}
	defer func() {
		_ = targetFile.Close()
		if current, err := os.Stat(tmpName); err == nil && os.SameFile(tmpInfo, current) {
			_ = os.Remove(tmpName)
		}
	}()
	bufPtr := transferCopyBufferPool.Get().(*[]byte)
	defer transferCopyBufferPool.Put(bufPtr)
	var copied int64
	stagedTime := tmpInfo.ModTime()
	check := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			current, err := os.Stat(src)
			if err != nil {
				return errors.Join(ErrCopyChanged, err)
			}
			opened, err := sourceFile.Stat()
			if err != nil {
				return errors.Join(ErrCopyChanged, err)
			}
			if !os.SameFile(info, current) || !os.SameFile(info, opened) || current.Size() != info.Size() || !current.ModTime().Equal(info.ModTime()) || opened.Size() != info.Size() || !opened.ModTime().Equal(info.ModTime()) {
				return ErrCopyChanged
			}
		}
		current, err := targetFile.Stat()
		if err != nil {
			return errors.Join(ErrCopyChanged, err)
		}
		named, err := os.Stat(tmpName)
		if err != nil {
			return errors.Join(ErrCopyChanged, err)
		}
		if !os.SameFile(tmpInfo, named) || !os.SameFile(tmpInfo, current) || current.Size() != copied || !current.ModTime().Equal(stagedTime) {
			return ErrCopyChanged
		}
		parent, err := filepath.EvalSymlinks(parentDir)
		if err != nil || !sameBoundary(parent, physicalParent) {
			return errors.Join(ErrCopyChanged, err)
		}
		if validate != nil {
			return validate()
		}
		return nil
	}
	do := func(path string, fn func() error) error {
		operation := func() error {
			if err := check(); err != nil {
				return err
			}
			return fn()
		}
		if work != nil {
			return work.Disk(ctx, diskwork.Work{Kind: storageio.WorkKindTransferCopy, Path: path}, operation)
		}
		if err := taskcontrol.Wait(ctx); err != nil {
			return err
		}
		return operation()
	}
	for {
		var n int
		var readErr error
		if err := do(src, func() error { n, readErr = sourceFile.Read(*bufPtr); return nil }); err != nil {
			return false, err
		}
		if n > 0 {
			if err := do(dst, func() error {
				written, err := targetFile.Write((*bufPtr)[:n])
				if err != nil {
					return err
				}
				if written != n {
					return io.ErrShortWrite
				}
				copied += int64(written)
				state, err := targetFile.Stat()
				if err != nil {
					return err
				}
				stagedTime = state.ModTime()
				return nil
			}); err != nil {
				return false, err
			}
			if progress != nil {
				progress(copied, info.Size())
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				return false, readErr
			}
			break
		}
	}
	committed := false
	err = do(dst, func() error {
		if err := targetFile.Sync(); err != nil {
			return err
		}
		if err := targetFile.Close(); err != nil {
			return err
		}
		// FAT/exFAT may reject chmod; file contents and publication remain valid.
		_ = os.Chmod(tmpName, 0644)
		if err := os.Chtimes(tmpName, info.ModTime(), info.ModTime()); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := publishMissing(tmpName, dst); err != nil {
			return err
		}
		committed = true
		return nil
	})
	if errors.Is(err, os.ErrExist) {
		return true, nil
	}
	if err == nil && !committed {
		return false, ErrCopyChanged
	}
	return false, err
}
