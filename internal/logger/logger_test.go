// 本文件是业务回归测试，属于日志基础设施，负责统一后端运行日志的格式、级别和输出位置。
// 它通过自动化断言保护对应业务场景在扫描、读取、展示或配置变更后仍保持兼容。
// 维护时应让用例名称、测试数据和断言结果直接反映真实用户流程，而不是只覆盖实现细节。

package logger

import (
	"errors"
	"log"
	"log/slog"
	"os"
	"testing"
)

func TestSetLevelAndCurrentLevel(t *testing.T) {
	levels := []string{"debug", "info", "warn", "error"}

	for _, level := range levels {
		if err := SetLevel(level); err != nil {
			t.Fatalf("SetLevel(%q) returned error: %v", level, err)
		}
		if got := CurrentLevel(); got != level {
			t.Fatalf("expected CurrentLevel %q, got %q", level, got)
		}
	}
}

func TestSetLevelRejectsInvalidValue(t *testing.T) {
	if err := SetLevel("verbose"); err == nil {
		t.Fatal("expected invalid log level to return an error")
	}
}

// TestCloseReleasesLogFile 守 Init 打开的文件句柄真有人松开：Close 之后这个文件删得掉，
// 而且迟到的日志不会把它重新打开。
//
// 两条在 POSIX 上都看不出破绽——删掉的文件照样写得进去、句柄留着也没人管；Windows 上则是
// 「The process cannot access the file because it is being used by another process.」
func TestCloseReleasesLogFile(t *testing.T) {
	initFileLoggingInTempDir(t)

	path := LogFilePath()
	if path == "" {
		t.Fatal("Init 之后拿不到日志文件路径")
	}
	// lumberjack 到第一次写才真的打开文件，先写一行，守的才是「打开着的句柄」。
	slog.Info("close 之前的一行")

	if err := Close(); err != nil {
		t.Fatalf("Close 报错: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("Close 之后日志文件仍删不掉: %v", err)
	}

	slog.Info("close 之后的一行")
	log.Print("close 之后走标准库那一路的一行")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Close 之后仍有人把日志文件重新打开了: %v", err)
	}
	if got := LogFilePath(); got != "" {
		t.Fatalf("Close 之后 LogFilePath 仍是 %q，查看接口会去读一个不再写入的路径", got)
	}
}

// initFileLoggingInTempDir 在一个临时目录上装好文件日志，路径经 LogFilePath 取。
//
// 顺序是这个 helper 存在的理由：t.Cleanup 后进先出，删目录在 Init 之前登记，关文件就必须登记得更晚。
// 反过来的话 Windows 上用例本身通过、清理阶段红在「文件正被另一进程使用」。
func initFileLoggingInTempDir(t *testing.T) {
	t.Helper()

	restoreLoggerGlobals(t)

	dir := t.TempDir()
	if err := Init(dir, "info"); err != nil {
		t.Fatalf("Init 失败: %v", err)
	}
	t.Cleanup(func() {
		if err := Close(); err != nil {
			t.Errorf("关闭日志文件失败: %v", err)
		}
	})
}

// restoreLoggerGlobals 还原 Init 改掉的进程级状态：slog 默认 logger、级别、包级日志文件路径，
// 以及标准库 log 的输出、前缀与标志。不还原的话，之后的用例读写的是一个已被删掉的临时目录。
func restoreLoggerGlobals(t *testing.T) {
	t.Helper()

	previousLogger := slog.Default()
	previousLevel := levelVar.Level()
	previousPath := LogFilePath()
	previousOutput := log.Writer()
	previousPrefix := log.Prefix()
	previousFlags := log.Flags()

	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
		levelVar.Set(previousLevel)
		fileMu.Lock()
		logFilePath = previousPath
		fileMu.Unlock()
		log.SetOutput(previousOutput)
		log.SetPrefix(previousPrefix)
		log.SetFlags(previousFlags)
	})
}
