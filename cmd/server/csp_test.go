// 本文件守卫 Content-Security-Policy 及其成立前提。
//
// CSP 的失效方式和别的安全头不同：它不会报错，只会**静默地不生效**（放太宽）
// 或**静默地打掉功能**（收太紧，浏览器控制台之外无迹象）。所以这里同时钉住两侧：
// 策略本身不许被图省事放宽，以及 script-src 'self' 所依赖的「index.html 无内联脚本」这个前提。

package main

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"manga-manager/web"
)

// parseCSP 把策略串解析成「指令 -> source 集合」。
// 按解析结果比对而非子串匹配：子串断言既挡不住多出来的 source，又会因重排而误报。
func parseCSP(header string) map[string][]string {
	out := make(map[string][]string)
	for _, raw := range strings.Split(header, ";") {
		fields := strings.Fields(raw)
		if len(fields) == 0 {
			continue
		}
		sources := append([]string(nil), fields[1:]...)
		sort.Strings(sources)
		out[fields[0]] = sources
	}
	return out
}

func TestSecurityHeadersContentSecurityPolicy(t *testing.T) {
	rec := httptest.NewRecorder()
	securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	got := parseCSP(rec.Header().Get("Content-Security-Policy"))
	if len(got) == 0 {
		t.Fatal("响应里没有 Content-Security-Policy")
	}

	want := map[string][]string{
		"default-src":     {"'self'"},
		"base-uri":        {"'none'"},
		"object-src":      {"'none'"},
		"frame-ancestors": {"'none'"},
		"form-action":     {"'self'"},
		"script-src":      {"'self'"},
		"style-src":       {"'self'", "'unsafe-inline'"},
		"img-src":         {"'self'", "blob:", "data:", "https:"},
		"connect-src":     {"'self'"},
		"worker-src":      {"'self'"},
		"manifest-src":    {"'self'"},
	}
	for directive, wantSources := range want {
		gotSources, ok := got[directive]
		if !ok {
			t.Errorf("CSP 缺少指令 %s", directive)
			continue
		}
		sort.Strings(wantSources)
		if strings.Join(gotSources, " ") != strings.Join(wantSources, " ") {
			t.Errorf("%s = %v, want %v", directive, gotSources, wantSources)
		}
	}
	for directive := range got {
		if _, expected := want[directive]; !expected {
			t.Errorf("CSP 多出未预期的指令 %s（新增放宽请连同理由一起写进 contentSecurityPolicy 的注释）", directive)
		}
	}

	// 真正的门禁：防止有人为了让某个功能跑起来而图省事放开。
	// style-src 是唯一有据可依的例外（comimi 在运行时注入 <style>，hash 不可维护）。
	for directive, sources := range got {
		for _, src := range sources {
			switch src {
			case "'unsafe-inline'":
				if directive != "style-src" {
					t.Errorf("%s 出现 'unsafe-inline'：只有 style-src 有正当理由，其余指令放开等于让 CSP 失去意义", directive)
				}
			case "'unsafe-eval'", "*":
				t.Errorf("%s 出现 %s，CSP 对该指令等同于没有", directive, src)
			}
		}
	}
}

// TestServiceWorkerRegistrationIsExternalScript 守卫 script-src 'self' 的前提。
//
// 若 SW 注册脚本被挪回 index.html 内联，CSP 会静默拦掉它：SW 永不注册、
// sw.js 的离线兜底与 PWA 安装全部失效，而页面看上去一切正常。
// 同样地，dist 里缺 register-sw.js 时，main.go 的 SPA 回落会把它当路由返回 index.html（text/html），
// 浏览器在 nosniff 下拒绝按脚本执行 —— 也是静默失效。
func TestServiceWorkerRegistrationIsExternalScript(t *testing.T) {
	indexHTML, err := web.FS.ReadFile("dist/index.html")
	if err != nil {
		t.Fatalf("读取 dist/index.html 失败（需先 npm run build）：%v", err)
	}
	html := string(indexHTML)

	if !strings.Contains(html, `src="/register-sw.js"`) {
		t.Error(`dist/index.html 未引用 /register-sw.js —— Service Worker 不会被注册`)
	}
	// 检测「有开标签但紧跟着不是 src=」的内联脚本。
	for _, chunk := range strings.Split(html, "<script")[1:] {
		head := chunk
		if i := strings.Index(chunk, ">"); i >= 0 {
			head = chunk[:i]
		}
		if !strings.Contains(head, "src=") {
			t.Errorf("dist/index.html 含内联 <script（属性段 %q）：CSP 的 script-src 'self' 会静默拦掉它", head)
		}
	}

	registerSW, err := web.FS.ReadFile("dist/register-sw.js")
	if err != nil {
		t.Fatalf("dist/register-sw.js 缺失：%v（该文件须放在 web/public/ 下才会被原样复制到 dist 根）", err)
	}
	if !strings.Contains(string(registerSW), "serviceWorker.register") {
		t.Error("dist/register-sw.js 里没有 serviceWorker.register，SW 注册链路已断")
	}
}
