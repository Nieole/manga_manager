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

		"koreader.validation.username_required": "用户名不能为空。",
		"koreader.validation.base_path_slash":   "同步路径必须以 / 开头。",
		"koreader.validation.match_mode":        "匹配模式必须是 binary_hash 或 file_path。",
		"koreader.account.username_taken":       "KOReader 用户名已存在",
		"koreader.account.deleted":              "KOReader 账号已删除",
		"koreader.progress.reset":               "KOReader 进度记录已重置",
		"koreader.task.index_rebuild_started":   "KOReader 索引重建已启动",
		"koreader.task.match_apply_started":     "KOReader 匹配规则应用任务已启动",
		"koreader.task.reconcile_started":       "未匹配同步记录重关联已启动",

		"maintenance.search_index_rebuilt":          "搜索索引已在线重建，并已触发全库重新建立索引。",
		"maintenance.thumbnails_rebuilding":         "当前的所有缩略图缓存已彻底撕毁，后台已发起全量静默遍历来重制封面。",
		"maintenance.cover_cleanup_started":         "已在后台启动无效封面资源清理任务。",
		"maintenance.file_identity_rebuild_started": "文件身份索引重建已启动",
		"config.saved":                          "配置已成功保存。大部分设定会立刻生效。",
		"recommendations.ai_grouping_submitted": "AI 分组审核任务已提交至后台",

		"comicinfo.write.unsupported": "该格式不支持写入元数据（仅 cbz/zip 可写，rar/cbr 会跳过）。",
		"comicinfo.write.failed":      "写入 ComicInfo 到归档失败。",

		"tag.rename.conflict": "已存在同名标签，请改用「合并」。",
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

		"koreader.validation.username_required": "Username cannot be empty.",
		"koreader.validation.base_path_slash":   "The sync path must start with /.",
		"koreader.validation.match_mode":        "Match mode must be binary_hash or file_path.",
		"koreader.account.username_taken":       "KOReader username already exists",
		"koreader.account.deleted":              "KOReader account deleted",
		"koreader.progress.reset":               "KOReader progress record reset",
		"koreader.task.index_rebuild_started":   "KOReader index rebuild started",
		"koreader.task.match_apply_started":     "KOReader match-rule apply task started",
		"koreader.task.reconcile_started":       "Reconcile of unmatched sync records started",

		"maintenance.search_index_rebuilt":          "Search index rebuilt online; a full re-index has been triggered.",
		"maintenance.thumbnails_rebuilding":         "All thumbnail caches were cleared; a full background sweep is regenerating covers.",
		"maintenance.cover_cleanup_started":         "Started a background task to clean up orphaned cover assets.",
		"maintenance.file_identity_rebuild_started": "File identity index rebuild started",
		"config.saved":                          "Configuration saved. Most settings take effect immediately.",
		"recommendations.ai_grouping_submitted": "AI grouping review task submitted to the background",

		"comicinfo.write.unsupported": "This format cannot embed metadata (only cbz/zip are writable; rar/cbr are skipped).",
		"comicinfo.write.failed":      "Failed to write ComicInfo into the archive.",

		"tag.rename.conflict": "A tag with that name already exists — use Merge instead.",
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
