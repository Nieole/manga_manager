package logger

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Init 配置全局结构化日志与双路输出（Stdout + 物理日志文件）
func Init(logDir string) error {
	// 如果配置了目录，则试图建立文件写入管道
	var writers []io.Writer
	writers = append(writers, os.Stdout)

	if logDir != "" {
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return fmt.Errorf("failed to create log directory %q: %w", logDir, err)
		}

		logFile := filepath.Join(logDir, "manga_manager.log")

		// 引入 Lumberjack 自动滚动截断记录器
		fileWriter := &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    10,   // 每个日志文件最大 10 MB
			MaxBackups: 5,    // 最多保留 5 个旧的文件备份
			MaxAge:     28,   // 旧账最多保留 28 天
			Compress:   true, // 是否将旧的轮转文件使用 gzip 开启无损压缩
		}

		writers = append(writers, fileWriter)
	}

	// 利用 io.MultiWriter 合成一条同时轰击控制台和磁盘的双头水管
	multiLog := io.MultiWriter(writers...)

	// 配置 slog 输出为可读性较好的 Text 格式（也可使用 JSONHandler）
	// 加入 Time 戳，设定输出级别，并将 Source 指针关掉以保持日志不要太冗长
	opts := &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: false,
	}
	handler := slog.NewTextHandler(multiLog, opts)
	logger := slog.New(handler)

	// 全局接管：覆盖标准库 slog 以及古板裸 log 包默认行为
	slog.SetDefault(logger)
	log.SetOutput(multiLog)

	// 调整古板 log 的前缀使其向后兼容我们现在的结构化版式，方便一些顽固三方库的归口输出
	log.SetPrefix("[LEGACY] ")
	log.SetFlags(log.Ldate | log.Ltime | log.Lmsgprefix)

	slog.Info("Logger initialized successfully", "log_dir", logDir)
	return nil
}
