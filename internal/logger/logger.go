package logger

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

var levelVar = &slog.LevelVar{}

// fileMu 守下面两个包级变量：Init 在启动时写、Close 在停机时清，两者未必来自同一个 goroutine。
var fileMu sync.Mutex

// currentSink 是当前这一路文件日志的写入端，Init 装上、Close 摘掉。nil 表示未启用文件日志。
var currentSink *fileSink

// logFilePath 记录 Init 时实际使用的日志文件绝对/相对路径，供查看接口读取同一文件，
// 避免日志写入路径与查看路径依据不同来源推导而分叉。空串表示未启用文件日志。
var logFilePath string

// LogFilePath 返回当前日志文件的实际路径（Init 时确定）。空串表示只输出到 stdout。
func LogFilePath() string {
	fileMu.Lock()
	defer fileMu.Unlock()
	return logFilePath
}

// fileSink 是日志文件这一路的写入端：把 lumberjack 关在一道可以合上的门后面。
//
// 不把 lumberjack 直接交给 io.MultiWriter，是因为它的 Write 在文件没打开时会自己打开——
// 那样 Close 之后任何一行迟到的日志都足以把文件重新占住。合上之后本层直接丢弃写入，
// 日志仍经 stdout 那一路出去。
type fileSink struct {
	mu     sync.Mutex
	writer *lumberjack.Logger
}

func (s *fileSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer == nil {
		return len(p), nil
	}
	return s.writer.Write(p)
}

// Close 合上这道门：先关文件，再把内部指针清掉，此后经本层的写入一律落空。
func (s *fileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer == nil {
		return nil
	}
	err := s.writer.Close()
	s.writer = nil
	return err
}

// Init 配置全局结构化日志与双路输出（Stdout + 物理日志文件）
func Init(logDir, level string) error {
	// 如果配置了目录，则试图建立文件写入管道
	var writers []io.Writer
	writers = append(writers, os.Stdout)

	var sink *fileSink
	logFile := ""

	if logDir != "" {
		if err := os.MkdirAll(logDir, 0755); err != nil {
			return fmt.Errorf("failed to create log directory %q: %w", logDir, err)
		}

		logFile = filepath.Join(logDir, "manga_manager.log")

		// 引入 Lumberjack 自动滚动截断记录器
		sink = &fileSink{writer: &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    10,   // 每个日志文件最大 10 MB
			MaxBackups: 5,    // 最多保留 5 个旧的文件备份
			MaxAge:     28,   // 旧账最多保留 28 天
			Compress:   true, // 是否将旧的轮转文件使用 gzip 开启无损压缩
		}}

		writers = append(writers, sink)
	}

	// 利用 io.MultiWriter 合成一条同时轰击控制台和磁盘的双头水管
	multiLog := io.MultiWriter(writers...)

	// 级别解析放在装上新 sink 之前：参数不合法时这次 Init 整个不生效，
	// 而不是把日志改道到一个随后并不会被写入的文件。
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
	handler := NewContextHandler(slog.NewTextHandler(multiLog, opts))
	logger := slog.New(handler)

	installSink(sink, logFile)

	// 全局接管：覆盖标准库 slog 以及古板裸 log 包默认行为
	slog.SetDefault(logger)
	log.SetOutput(multiLog)

	// 调整古板 log 的前缀使其向后兼容我们现在的结构化版式，方便一些顽固三方库的归口输出
	log.SetPrefix("[LEGACY] ")
	log.SetFlags(log.Ldate | log.Ltime | log.Lmsgprefix)

	slog.Info("Logger initialized successfully", "log_dir", logDir, "level", CurrentLevel())
	return nil
}

// installSink 换上这一路文件日志，并关掉上一路：Init 被调用两次时，第一次那个句柄同样有人负责。
func installSink(sink *fileSink, path string) {
	fileMu.Lock()
	previous := currentSink
	currentSink = sink
	logFilePath = path
	fileMu.Unlock()

	if previous != nil {
		_ = previous.Close()
	}
}

// Close 关掉 Init 打开的日志文件，此后日志只走 stdout。重复调用无副作用。
//
// 谁在什么时候调用它：进程侧是 cmd/server 的停机路径（走 os.Exit 的致命分支会跳过它，那时句柄由
// 进程退出交还给 OS）；测试侧是每个调过 Init 的用例——必须排在 t.TempDir 删目录之前，Windows
// 拒绝删除仍被打开的文件，少了这一步用例本身通过、清理阶段报「文件正被另一进程使用」。
//
// 返回之后进程内没有任何一条路径能把这个文件重新打开：写入端只剩 fileSink 一个，而它已经合上。
func Close() error {
	fileMu.Lock()
	sink := currentSink
	currentSink = nil
	logFilePath = ""
	fileMu.Unlock()

	if sink == nil {
		return nil
	}
	return sink.Close()
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
