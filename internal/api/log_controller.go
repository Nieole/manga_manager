package api

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// LogEntry 代表一行日志解析后的结构
type LogEntry struct {
	Time  string `json:"time"`
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Raw   string `json:"raw"`
}

// getSystemLogs 提供分页或者只取最新的错误日志
// 此处我们实现一个基本逻辑：从 data/manga_manager.log 倒序读取包含 level=ERROR 的行
func (c *Controller) getSystemLogs(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	limit := 100 // 默认返回最新的 100 条错误
	if l, err := fmt.Sscanf(limitStr, "%d", &limit); err != nil || l != 1 || limit <= 0 {
		limit = 100
	}

	filterLevel := r.URL.Query().Get("level")
	if filterLevel == "" {
		filterLevel = "ERROR"
	}

	// manga_manager.log 的路径
	// Controller 里可以直接通过配置或者数据路径推导
	// 例如：Controller 中的 watcher 扫描器之类的依赖。为了简便，我们从存库路径推导
	logFilePath := filepath.Join(filepath.Dir(c.config.Database.Path), "manga_manager.log")

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
	var matchedLogs []LogEntry
	scanner := bufio.NewScanner(file)

	// 简单正则提取 level=ERROR
	// 现有的 slog.TextHandler 格式大概是: time=2023-10-10T... level=ERROR msg="something"
	levelPattern := fmt.Sprintf(`level=%s`, filterLevel)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, levelPattern) || (filterLevel == "ALL") {
			entry := parseLogLine(line)
			matchedLogs = append(matchedLogs, entry)
		}
	}

	if err := scanner.Err(); err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to read log file")
		return
	}

	// 倒序排列：最新的在前面
	for i, j := 0, len(matchedLogs)-1; i < j; i, j = i+1, j-1 {
		matchedLogs[i], matchedLogs[j] = matchedLogs[j], matchedLogs[i]
	}

	// 截断到 limit
	if len(matchedLogs) > limit {
		matchedLogs = matchedLogs[:limit]
	}

	jsonResponse(w, http.StatusOK, matchedLogs)
}

// 简单的 text handler parser
func parseLogLine(line string) LogEntry {
	entry := LogEntry{Raw: line}

	// 尝试提取 time=... level=... msg=...
	timeRe := regexp.MustCompile(`time=([^ ]+)`)
	levelRe := regexp.MustCompile(`level=([^ ]+)`)
	msgRe := regexp.MustCompile(`msg="([^"]+)"`)

	if m := timeRe.FindStringSubmatch(line); len(m) > 1 {
		entry.Time = m[1]
	}
	if m := levelRe.FindStringSubmatch(line); len(m) > 1 {
		entry.Level = m[1]
	} else {
		entry.Level = "UNKNOWN"
	}

	if m := msgRe.FindStringSubmatch(line); len(m) > 1 {
		entry.Msg = m[1]
	} else {
		// 如果没用引号，或者不是标准格式
		msgRe2 := regexp.MustCompile(`msg=([^ ]+)`)
		if m2 := msgRe2.FindStringSubmatch(line); len(m2) > 1 {
			entry.Msg = m2[1]
		}
	}

	return entry
}
