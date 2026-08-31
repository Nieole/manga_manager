package api

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"manga-manager/internal/logger"
)

// maxLogQueryLimit 限制单次日志查询返回条数上限，避免 ?limit=极大值 时按该值预分配打爆内存。
const maxLogQueryLimit = 2000

const (
	// maxLogLineBytes 是单行日志读进内存的上限，超出的部分丢弃并加标记。
	maxLogLineBytes = 16 * 1024
	// logReadBufferBytes 是逐行读取的缓冲区大小，只影响读取批次，不构成行长上限。
	logReadBufferBytes = 64 * 1024
	// logLineTruncatedMarker 让被截断的行在页面上可辨认，而不是静默变短。
	logLineTruncatedMarker = "…(line truncated by viewer)"
)

// 日志行解析用的正则预编译为包级变量，避免每行、每次请求重复编译。
var (
	logTimeRe    = regexp.MustCompile(`time=([^ ]+)`)
	logLevelRe   = regexp.MustCompile(`level=([^ ]+)`)
	logMsgReQ    = regexp.MustCompile(`msg="([^"]+)"`)
	logMsgReBare = regexp.MustCompile(`msg=([^ ]+)`)
)

// LogEntry 代表一行日志解析后的结构
type LogEntry struct {
	Time  string `json:"time"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Raw   string `json:"raw"`
}

type LogSummary struct {
	Total   int            `json:"total"`
	ByLevel map[string]int `json:"by_level"`
}

type LogsResponse struct {
	Items   []LogEntry `json:"items"`
	Summary LogSummary `json:"summary"`
}

// getSystemLogs 提供分页或者只取最新的错误日志
// 此处我们实现一个基本逻辑：从 data/manga_manager.log 倒序读取包含 level=ERROR 的行
func (c *Controller) getSystemLogs(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 100 // 默认返回最新的 100 条错误
	if l, err := fmt.Sscanf(limitStr, "%d", &limit); err != nil || l != 1 || limit <= 0 {
		limit = 100
	}
	if limit > maxLogQueryLimit {
		limit = maxLogQueryLimit
	}

	filterLevel := r.URL.Query().Get("level")
	if filterLevel == "" {
		filterLevel = "ERROR"
	}

	searchQuery := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	taskKeyFilter := strings.TrimSpace(r.URL.Query().Get("task_key"))
	taskKeyNeedle := ""
	if taskKeyFilter != "" {
		taskKeyNeedle = "task_key=" + taskKeyFilter
	}

	// 优先使用 logger 实际写入的日志文件路径，避免查看侧与写入侧依据不同来源推导而分叉。
	// 仅当 logger 未初始化文件日志（如测试环境）时，回退到按数据目录推导。
	logFilePath := logger.LogFilePath()
	if logFilePath == "" {
		logFilePath = filepath.Join(filepath.Dir(c.currentConfig().Database.Path), "manga_manager.log")
	}

	file, err := os.Open(logFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			jsonResponse(w, http.StatusOK, []LogEntry{})
			return
		}
		jsonError(w, http.StatusInternalServerError, "Failed to open log file")
		return
	}
	defer file.Close()

	// 由于日志文件可能很大，我们简单暴力点：扫描一遍，存下符合条件的行。
	// 对于非常巨大的日志可能需要特殊倒序读取库，此处假定只读当前 .log 并收集
	matchedLogs := make([]LogEntry, 0, limit)
	summary := LogSummary{
		ByLevel: map[string]int{
			"DEBUG": 0,
			"ERROR": 0,
			"WARN":  0,
			"INFO":  0,
		},
	}
	collect := func(line string) {
		entry := parseLogLine(line)
		level := strings.ToUpper(entry.Level)
		if _, ok := summary.ByLevel[level]; ok {
			summary.ByLevel[level]++
		}

		if filterLevel != "ALL" && level != strings.ToUpper(filterLevel) {
			return
		}
		if searchQuery != "" {
			raw := strings.ToLower(entry.Raw)
			msg := strings.ToLower(entry.Msg)
			if !strings.Contains(raw, searchQuery) && !strings.Contains(msg, searchQuery) {
				return
			}
		}
		if taskKeyNeedle != "" && !strings.Contains(entry.Raw, taskKeyNeedle) {
			return
		}

		summary.Total++
		if len(matchedLogs) == limit {
			copy(matchedLogs, matchedLogs[1:])
			matchedLogs[len(matchedLogs)-1] = entry
			return
		}
		matchedLogs = append(matchedLogs, entry)
	}

	if err := forEachLogLine(file, collect); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to read log file")
		return
	}

	// 倒序排列：最新的在前面
	for i, j := 0, len(matchedLogs)-1; i < j; i, j = i+1, j-1 {
		matchedLogs[i], matchedLogs[j] = matchedLogs[j], matchedLogs[i]
	}

	jsonResponse(w, http.StatusOK, LogsResponse{
		Items:   matchedLogs,
		Summary: summary,
	})
}

// forEachLogLine 逐行回调日志内容；超过 maxLogLineBytes 的行截断并标注后，继续读余下的行。
//
// 不用 bufio.Scanner：它遇到超长行会中止整趟扫描并把错误交给调用方，一行落盘的上游整页错误页
// 就足以让日志端点在轮转前一直回 500——恰好是最需要看日志的时候。调大上限只是把悬崖挪远，
// 而且修不了已经躺在文件里的那些行；所以这里选就地降级：坏的那一行降级，别的行照常读出来。
func forEachLogLine(r io.Reader, visit func(line string)) error {
	reader := bufio.NewReaderSize(r, logReadBufferBytes)
	var line []byte
	truncated := false

	appendCapped := func(chunk []byte) {
		if room := maxLogLineBytes - len(line); len(chunk) > room {
			chunk = chunk[:room]
			truncated = true
		}
		line = append(line, chunk...)
	}
	emit := func() {
		text := string(line)
		if truncated {
			text = string(trimPartialRune(line)) + logLineTruncatedMarker
		}
		visit(text)
		line = line[:0]
		truncated = false
	}

	for {
		chunk, err := reader.ReadSlice('\n')
		switch {
		case err == nil:
			appendCapped(bytes.TrimSuffix(bytes.TrimSuffix(chunk, []byte("\n")), []byte("\r")))
			emit()
		case errors.Is(err, bufio.ErrBufferFull):
			appendCapped(chunk)
		case errors.Is(err, io.EOF):
			// 末行没有换行符时也要交出去。
			if len(chunk) > 0 || len(line) > 0 {
				appendCapped(chunk)
				emit()
			}
			return nil
		default:
			return err
		}
	}
}

// trimPartialRune 去掉按字节截断后残留的半个 UTF-8 字符，最多回退 3 字节。
func trimPartialRune(b []byte) []byte {
	for i := 0; i < utf8.UTFMax-1 && len(b) > 0; i++ {
		if r, size := utf8.DecodeLastRune(b); r != utf8.RuneError || size > 1 {
			break
		}
		b = b[:len(b)-1]
	}
	return b
}

// 简单的 text handler parser
func parseLogLine(line string) LogEntry {
	entry := LogEntry{Raw: line}

	if m := logTimeRe.FindStringSubmatch(line); len(m) > 1 {
		entry.Time = m[1]
	}
	if m := logLevelRe.FindStringSubmatch(line); len(m) > 1 {
		entry.Level = m[1]
	} else {
		entry.Level = "UNKNOWN"
	}

	if m := logMsgReQ.FindStringSubmatch(line); len(m) > 1 {
		entry.Msg = m[1]
	} else if m2 := logMsgReBare.FindStringSubmatch(line); len(m2) > 1 {
		// 没用引号或非标准格式时的回退
		entry.Msg = m2[1]
	}

	return entry
}
