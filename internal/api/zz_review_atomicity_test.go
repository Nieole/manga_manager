//go:build unix

package api

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCopyFileToExternalLibraryNeverExposesPartialDestination(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "source.cbz")
	dst := filepath.Join(dstDir, "series", "target.cbz")

	if err := syscall.Mkfifo(src, 0o600); err != nil {
		t.Fatalf("mkfifo failed: %v", err)
	}

	payload := bytes.Repeat([]byte("manga-manager-atomic-transfer"), 72000) // ~2 MiB

	done := make(chan struct{})
	var copyErr error
	go func() {
		defer close(done)
		_, copyErr = copyFileToExternalLibrary(src, dst, nil)
	}()

	w, err := os.OpenFile(src, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open fifo for write failed: %v", err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("write payload failed: %v", err)
	}

	if _, statErr := os.Stat(dst); !errors.Is(statErr, os.ErrNotExist) {
		info, _ := os.Stat(dst)
		size := int64(-1)
		if info != nil {
			size = info.Size()
		}
		w.Close()
		<-done
		t.Fatalf("传输中途最终路径已可见，非原子落盘: stat err=%v size=%d 完整应为 %d", statErr, size, len(payload))
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close fifo failed: %v", err)
	}
	<-done
	if copyErr != nil {
		t.Fatalf("copyFileToExternalLibrary failed: %v", copyErr)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read destination failed: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("destination content mismatch: got %d bytes want %d", len(got), len(payload))
	}
	entries, err := os.ReadDir(filepath.Dir(dst))
	if err != nil {
		t.Fatalf("read dir failed: %v", err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("目标目录残留文件: %v", names)
	}
}
