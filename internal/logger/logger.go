// 业务说明：本文件是业务实现，属于日志基础设施，负责统一后端运行日志的格式、级别和输出位置。
// 它支撑扫描排障、图片缓存诊断、API 错误定位和长任务进度追踪。
// 维护时应保证日志足够定位业务问题，同时避免输出敏感路径或造成高频噪声。

package logger

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

var levelVar = &slog.LevelVar{}

// logFilePath 记录 Init 时实际使用的日志文件绝对/相对路径，供查看接口读取同一文件，
// 避免日志写入路径与查看路径依据不同来源推导而分叉。空串表示未启用文件日志。
var logFilePath string

// LogFilePath 返回当前日志文件的实际路径（Init 时确定）。空串表示只输出到 stdout。
func LogFilePath() string {
	return logFilePath
}

// Init 配置全局结构化日志与双路输出（Stdout + 物理日志文件）
func Init(logDir, level string) error {
	// 如果配置了目录，则试图建立文件写入管道
	var writers []io.Writer
	writers = append(writers, os.Stdout)

	if logDir != "" {
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return fmt.Errorf("failed to create log directory %q: %w", logDir, err)
		}

		logFile := filepath.Join(logDir, "manga_manager.log")
		logFilePath = logFile

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

	slogLevel, err := parseLevel(level)
	if err != nil {
		return err
	}
	levelVar.Set(slogLevel)

	// 配置 slog 输出为可读性较好的 Text 格式（也可使用 JSONHandler）
	// 加入 Time 戳，设定输出级别，并将 Source 指针关掉以保持日志不要太冗长
	opts := &slog.HandlerOptions{
		Level:     levelVar,
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

	slog.Info("Logger initialized successfully", "log_dir", logDir, "level", CurrentLevel())
	return nil
}

func SetLevel(level string) error {
	slogLevel, err := parseLevel(level)
	if err != nil {
		return err
	}
	levelVar.Set(slogLevel)
	slog.Info("Logger level updated", "level", CurrentLevel())
	return nil
}

func CurrentLevel() string {
	switch levelVar.Level() {
	case slog.LevelDebug:
		return "debug"
	case slog.LevelWarn:
		return "warn"
	case slog.LevelError:
		return "error"
	default:
		return "info"
	}
}

func parseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unsupported log level %q", level)
	}
}
