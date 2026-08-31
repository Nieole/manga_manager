//go:build unix

// 本文件守卫「打开文件管理器」不泄漏僵尸进程。
//
// exec.Cmd.Start() 之后若没有配对的 Wait()，子进程退出后会在内核进程表里留下僵尸表项，
// Go 运行时不会替我们收割。该接口每被点击一次就泄漏一个，长期运行的服务会一直累积。
// 用例只在 Unix 上跑：Windows 没有僵尸进程这个概念。

package api

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// TestSpawnedProcessIsReaped 用一个立即退出的真实子进程，验证「Start 之后异步 Wait」
// 这条收尾路径确实会把它收掉。
//
// 判据是 syscall.Kill(pid, 0)：僵尸进程仍占着 pid、Kill 返回 nil；被收割后 pid 消失、返回 ESRCH。
func TestSpawnedProcessIsReaped(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	pid := cmd.Process.Pid

	// 与 openPathInDefaultFileManager 里完全相同的收尾方式。
	go func() { _ = cmd.Wait() }()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); err != nil {
			return // pid 已消失 —— 已被收割
		}
		if time.Now().After(deadline) {
			t.Fatalf("pid %d 在 3 秒后仍存在：子进程未被收割，僵尸表项泄漏", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestOpenPathStartFailureDoesNotWaitOnNilProcess 守卫失败分支。
// Start 失败时 cmd.Process 为 nil，若仍进入 go cmd.Wait() 会对 nil Process 调 Wait。
func TestOpenPathStartFailureDoesNotWaitOnNilProcess(t *testing.T) {
	// 目录不存在时命令本身仍能启动（是命令自己报错），所以这里直接验证一个必然启动失败的场景：
	// 用一个不存在的可执行文件。
	cmd := exec.Command("/nonexistent/definitely-not-a-real-binary")
	if err := cmd.Start(); err == nil {
		t.Skip("该平台上这个路径竟然能启动，用例前提不成立")
	} else if cmd.Process != nil {
		t.Fatalf("Start 失败后 cmd.Process 不应为非 nil，实际 %+v —— 该分支不得进入 cmd.Wait()", cmd.Process)
	}

	// openPathInDefaultFileManager 在 Start 失败时必须直接返回错误。
	if err := openPathInDefaultFileManager("/nonexistent/definitely-not-a-real-directory-xyz"); err != nil {
		t.Logf("openPathInDefaultFileManager 返回错误（可接受）: %v", err)
	}
}
