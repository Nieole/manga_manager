// 业务说明：本文件是业务实现，属于后端 API 层的国际化支撑，为直接由 HTTP 响应下发、
// 前端无法二次翻译的用户可见文案（鉴权错误、资料库校验等）按 locale 选择中/英文本。
// 与 opds_controller.go 的 opdsText 同一思路：这类响应前端只能原样展示（校验消息随结果结构下发、
// 错误 toast 直接显示后端 error 字段），故必须后端按 Accept-Language / X-App-Locale 本地化。
// 维护时应保证 zh-CN 与 en-US 两张表 key 一致，新增文案须两处同步。

package api

// apiMessages 按 locale 提供直接经 HTTP 响应下发的用户可见文案。
var apiMessages = map[string]map[string]string{
	"zh-CN": {
		"auth.token_required":              "需要有效的访问令牌",
		"library.validation.name_required": "名称不能为空。",
		"library.validation.path_required": "路径不能为空。",
		"library.validation.path_missing":  "路径不存在或不可访问。",
		"library.validation.path_not_dir":  "这里只能选择目录。",
		"library.validation.interval_min":  "扫描间隔至少为 1 分钟。",
		"library.validation.formats_empty": "至少保留一个受支持的扫描格式。",
		"library.validation.path_in_use":   "这个目录已经被其他资源库使用。",
	},
	"en-US": {
		"auth.token_required":              "A valid access token is required",
		"library.validation.name_required": "Name cannot be empty.",
		"library.validation.path_required": "Path cannot be empty.",
		"library.validation.path_missing":  "Path does not exist or is not accessible.",
		"library.validation.path_not_dir":  "Only a directory can be selected here.",
		"library.validation.interval_min":  "Scan interval must be at least 1 minute.",
		"library.validation.formats_empty": "Keep at least one supported scan format.",
		"library.validation.path_in_use":   "This directory is already used by another library.",
	},
}

// apiText 返回给定 locale 的 API 响应文案；未知 locale/key 回退 zh-CN，再回退 key 本身。
// 语义与 opdsText 一致。
func apiText(locale, key string) string {
	if m, ok := apiMessages[locale]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	if s, ok := apiMessages["zh-CN"][key]; ok {
		return s
	}
	return key
}
