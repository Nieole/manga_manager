//go:build unix

// 本文件守卫「外部库传输不会在目标路径上留下半截文件」。
//
// 传输目的地通常是 USB/SD 外接盘，单本几百 MB、整个系列跑几十分钟，进程随时可能被强杀
// 或外接盘中途掉线：必须先写临时路径再原子改名，直接写最终路径会留下截断的 .cbz。
// copyFileToExternalLibrary 开头的 os.Stat 与 external/manager.go 按扩展名列举归档，
// 这两处「已存在」判定都只看路径不看内容，坏书会被永远当成「已经有了」而跳过重传。

package api

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestCopyFileToExternalLibraryNeverExposesPartialDestination 用 FIFO 造出一个
// 确定性的「拷贝进行中」观测点：写端不关，读端就永远等不到 EOF，拷贝必然卡在中途。
func TestCopyFileToExternalLibraryNeverExposesPartialDestination(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	// 后缀必须是受支持的归档扩展名：这正是「坏文件会被当成已存在的书」的前提。
	src := filepath.Join(srcDir, "Volume01.cbz")
	dst := filepath.Join(dstDir, "Volume01.cbz")

	if err := syscall.Mkfifo(src, 0o644); err != nil {
		t.Skipf("建不了 FIFO：%v", err)
	}

	// 先起拷贝协程（它会打开 FIFO 的读端），再开写端：
	// FIFO 的 O_WRONLY 会一直阻塞到有读端为止，顺序反了这里就永远回不来。
	done := make(chan error, 1)
	go func() {
		_, copyErr := copyFileToExternalLibrary(src, dst, nil)
		done <- copyErr
	}()

	writer, err := os.OpenFile(src, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("打开 FIFO 写端: %v", err)
	}
	if _, err := writer.Write(make([]byte, 2<<20)); err != nil {
		t.Fatalf("写入 FIFO: %v", err)
	}

	// 给拷贝协程一点时间把这批数据搬过去。
	time.Sleep(300 * time.Millisecond)

	// 核心断言：此刻最终路径上**什么都不该有**。
	//
	// 只看「是否可见」，不看大小——FIFO 的读端可能已经把 2 MiB 全消费并写出去了，
	// 只是还没收到 EOF，所以中途大小甚至可能等于完整体积，拿它做断言必然 flaky。
	_, statErr := os.Stat(dst)
	if !errors.Is(statErr, os.ErrNotExist) {
		// 先收尾再报错：否则拷贝协程会永久阻塞在 FIFO 读上并持有 fd，
		// 而 t.TempDir 的 cleanup 正要 RemoveAll 掉它在写的目录。
		size := int64(-1)
		if info, err := os.Stat(dst); err == nil {
			size = info.Size()
		}
		_ = writer.Close()
		<-done
		t.Fatalf("传输中途最终路径已可见（size=%d）—— 进程此刻被杀就会在用户盘上留下一本"+
			"打不开的坏书，而且因为路径已存在，重传会被当成「已经有了」永远跳过", size)
	}

	// 收尾：关掉写端让拷贝正常结束，确认最终文件是完整的。
	if err := writer.Close(); err != nil {
		t.Fatalf("关闭 FIFO 写端: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("copyFileToExternalLibrary: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("拷贝完成后目标不存在: %v", err)
	}
	if info.Size() != 2<<20 {
		t.Fatalf("目标大小 %d，期望 %d —— 提交出去的必须是完整文件", info.Size(), 2<<20)
	}
}

// TestCopyFileToExternalLibraryCleansUpTemporaryFileOnFailure：失败时不留垃圾。
//
// 临时文件若残留在外部库目录里，用户会在自己的盘上看到一堆莫名其妙的 .tmp。
func TestCopyFileToExternalLibraryCleansUpTemporaryFileOnFailure(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	src := filepath.Join(srcDir, "Volume01.cbz")
	dst := filepath.Join(dstDir, "Volume01.cbz")

	if err := syscall.Mkfifo(src, 0o644); err != nil {
		t.Skipf("建不了 FIFO：%v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, copyErr := copyFileToExternalLibrary(src, dst, nil)
		done <- copyErr
	}()

	writer, err := os.OpenFile(src, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("打开 FIFO 写端: %v", err)
	}
	if _, err := writer.Write(make([]byte, 1<<20)); err != nil {
		t.Fatalf("写入 FIFO: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	_ = writer.Close()
	<-done

	entries, err := os.ReadDir(dstDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("外部库目录里残留了临时文件 %q —— 用户会在自己的盘上看到一堆垃圾", e.Name())
		}
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("正常结束后目标应当存在: %v", err)
	}
}
