package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"manga-manager/internal/application/configuration"
	"manga-manager/internal/config"
	"manga-manager/internal/taskcontrol"

	"github.com/go-chi/chi/v5"
)

type TaskRuntime struct {
	Context   context.Context
	Cancel    context.CancelFunc
	PauseGate *taskcontrol.PauseGate
	StartedAt time.Time
}

type SystemCapabilitiesResponse struct {
	SupportedScanFormats     []string `json:"supported_scan_formats"`
	SupportedScanProfiles    []string `json:"supported_scan_profiles"`
	SupportedLogLevels       []string `json:"supported_log_levels"`
	SupportedStorageProfiles []string `json:"supported_storage_profiles"`
	DefaultScanFormats       string   `json:"default_scan_formats"`
	DefaultScanInterval      int      `json:"default_scan_interval"`
	SupportedLLMProviders    []string `json:"supported_llm_providers"`
	SupportedLLMAPIModes     []string `json:"supported_llm_api_modes"`
}

type SystemConfigResponse struct {
	Config       config.Config              `json:"config"`
	Validation   config.ValidationResult    `json:"validation"`
	Capabilities SystemCapabilitiesResponse `json:"capabilities"`
	FieldEffects []config.FieldEffect       `json:"field_effects"`
	Publication  configuration.Publication  `json:"publication"`
}

type SearchResult struct {
	Hits     []*SearchHit `json:"hits"`
	Total    uint64       `json:"total_hits"`
	MaxScore float64      `json:"max_score"`
}

type SearchHit struct {
	ID     string                 `json:"id"`
	Score  float64                `json:"score"`
	Fields map[string]interface{} `json:"fields,omitempty"`
}

func constantTimeTokenMatch(provided, expected string) bool {
	if provided == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

// validateOutboundLLMTarget 对 test-llm 的出站目标做 SSRF 加固：仅允许 http/https scheme，
// 拒绝 file://、gopher:// 等危险协议。由于本服务默认支持本机 Ollama（localhost），此处不封锁
// 私有网段/回环地址，未鉴权部署时应配合 server.auth 开启，或置于受信内网/反向代理之后。
func validateOutboundLLMTarget(baseURL, endpoint string) error {
	target := strings.TrimSpace(baseURL)
	if target == "" {
		target = strings.TrimSpace(endpoint)
	}
	if target == "" {
		return nil
	}
	u, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("无效的目标地址: %v", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("不支持的目标协议 %q，仅允许 http/https", u.Scheme)
	}
}

func openPathInDefaultFileManager(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("explorer.exe", path)
	case "linux":
		cmd = exec.Command("xdg-open", path)
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	if err := cmd.Start(); err != nil {
		return err
	}
	// 必须有配对的 Wait：Start 之后不 Wait，子进程退出后会在 Unix 内核里留下僵尸表项，
	// 而 Go 运行时不会替我们收割——每点一次「在文件管理器中打开」就泄漏一个。
	//
	// 但**不能同步等**：这些命令拉起的是长期存活的 GUI 进程（Finder / Explorer / 文件管理器），
	// 同步 Wait 会把 HTTP 请求一直挂到用户关掉那个窗口为止。
	//
	// 也刻意不走 runBackground：那会把这个 goroutine 计入应用后台，于是优雅停机要等
	// 用户关闭文件管理器才能完成。这个 goroutine 的唯一职责就是收尸，进程退出时随之消失即可。
	//
	// （对应地也不用 exec.CommandContext + 超时兜底：那会在超时后杀掉用户正开着的文件管理器。）
	go func() {
		_ = cmd.Wait() // explorer.exe 惯例返回非零退出码，与成功与否无关，丢弃即可
	}()
	return nil
}

// eventPrefix 截取事件前缀用于日志，避免输出整段 JSON
func eventPrefix(event string) string {
	if i := strings.IndexByte(event, ':'); i >= 0 {
		return event[:i]
	}
	if len(event) > 32 {
		return event[:32]
	}
	return event
}

// jsonResponse 是 JSON API 的唯一出口，也是宿主机绝对路径按角色净化的唯一落点：
// 非管理员的响应一律先过 redactHostPaths，新增端点因此默认就是安全的，无需逐个记得裁。
func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	if !responseKeepsHostPaths(w) {
		data = redactHostPaths(data)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// 响应头已 WriteHeader 发出，此时若编码/写出失败（多为客户端已断开）已无可挽救的动作，显式忽略。
	_ = json.NewEncoder(w).Encode(data)
}

func jsonError(w http.ResponseWriter, status int, message string) {
	jsonResponse(w, status, map[string]string{"error": message})
}

func parseID(r *http.Request, param string) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, param), 10, 64)
}

type BulkUpdateSeriesRequest struct {
	SeriesIDs  []int64 `json:"series_ids"`
	IsFavorite *bool   `json:"is_favorite"`
}
