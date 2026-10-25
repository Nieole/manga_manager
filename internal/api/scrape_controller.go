package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"manga-manager/internal/metadata"
	"manga-manager/internal/proposal"
	"manga-manager/internal/task"
)

// 以下 helper 用常量格式串按 locale 生成带占位的刮削响应文案（默认中文，en-US 输出英文），
// 满足 go vet 的常量格式检查（若把 %s/%v 格式串入表再 Sprintf，vet 会报 non-constant format string）。
func scrapeNotFoundMsg(locale, providerName string) string {
	if locale == "en-US" {
		return fmt.Sprintf("No matching entry found on %s", providerName)
	}
	return fmt.Sprintf("未在 %s 上找到匹配的条目", providerName)
}

func scrapeSearchFailedMsg(locale, providerName string, err error) string {
	if locale == "en-US" {
		return fmt.Sprintf("%s search failed: %v", providerName, err)
	}
	return fmt.Sprintf("%s 搜索失败: %v", providerName, err)
}

func scrapeFailedMsg(locale, providerName string, err error) string {
	if locale == "en-US" {
		return fmt.Sprintf("%s scrape failed: %v", providerName, err)
	}
	return fmt.Sprintf("%s 刮削失败: %v", providerName, err)
}

// scrapeResultSourceURL 给出一条搜索结果的来源链接：数据源自己给了就用它，
// 否则只有 Bangumi 能从条目 id 拼出可用的外链。
func scrapeResultSourceURL(providerName string, result *metadata.SeriesMetadata) string {
	if result == nil {
		return ""
	}
	if strings.TrimSpace(result.SourceURL) != "" {
		return strings.TrimSpace(result.SourceURL)
	}
	if result.SourceID > 0 && strings.EqualFold(providerName, "bangumi") {
		return fmt.Sprintf("https://bgm.tv/subject/%d", result.SourceID)
	}
	return ""
}

// getProvider 根据名称返回对应的 Provider 实例
func (c *Controller) getProvider(name string) metadata.Provider {
	if c.providerFactory != nil {
		return c.providerFactory(name)
	}
	return metadata.NewConfiguredProvider(c.currentConfig(), name)
}

// availableProviders 返回可用的 provider 列表供前端展示。
// AniList / MangaDex 免密钥恒可用；MyAnimeList / Comic Vine 仅在配置了对应凭据时出现（否则源不可用，避免误选）。
func (c *Controller) listProviders(w http.ResponseWriter, r *http.Request) {
	providers := []map[string]string{
		{"id": "bangumi", "name": "Bangumi", "description": "从 Bangumi 番组计划获取漫画元数据"},
		{"id": "anilist", "name": "AniList", "description": "从 AniList 获取漫画元数据（英文/罗马音/原名、评分、标签、作者）"},
		{"id": "mangadex", "name": "MangaDex", "description": "从 MangaDex 获取漫画元数据（多语言标题、标签、作者、封面）"},
	}
	cfg := c.currentConfig()
	if strings.TrimSpace(cfg.Scrapers.MALClientID) != "" {
		providers = append(providers, map[string]string{"id": "myanimelist", "name": "MyAnimeList", "description": "从 MyAnimeList 获取漫画元数据（需配置 Client ID）"})
	}
	if strings.TrimSpace(cfg.Scrapers.ComicVineAPIKey) != "" {
		providers = append(providers, map[string]string{"id": "comicvine", "name": "Comic Vine", "description": "从 Comic Vine 获取欧美 comics 元数据（需配置 API Key）"})
	}
	providers = append(providers, map[string]string{"id": "llm", "name": "AI/LLM 解析", "description": "通过配置的大语言模型(如 Ollama, LM Studio, OpenAI)推理生成元数据"})
	jsonResponse(w, http.StatusOK, providers)
}

func (c *Controller) searchMetadata(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		jsonError(w, http.StatusBadRequest, "Missing query parameter 'q'")
		return
	}

	providerName := r.URL.Query().Get("provider")
	provider := c.getProvider(providerName)

	result, err := provider.FetchSeriesMetadata(requestContextWithLocale(r), query)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, scrapeSearchFailedMsg(requestLocale(r), provider.Name(), err))
		return
	}

	if result == nil {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"found": false, "message": scrapeNotFoundMsg(requestLocale(r), provider.Name())})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"found":      true,
		"provider":   provider.Name(),
		"title":      result.Title,
		"summary":    result.Summary,
		"publisher":  result.Publisher,
		"cover_url":  result.CoverURL,
		"rating":     result.Rating,
		"tags":       result.Tags,
		"source_id":  result.SourceID,
		"source_url": scrapeResultSourceURL(provider.Name(), result),
		"confidence": result.Confidence,
	})
}

func (c *Controller) scrapeSearchMetadata(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	providerName := r.URL.Query().Get("provider")
	provider := c.getProvider(providerName)

	// 优先从查询参数获取 q，若无则按系列标题搜索
	searchTitle := r.URL.Query().Get("q")
	if searchTitle == "" {
		series, err := c.store.GetSeries(r.Context(), seriesID)
		if err != nil {
			jsonError(w, http.StatusNotFound, "Series not found")
			return
		}

		searchTitle = series.Name
		if series.Title.Valid && series.Title.String != "" {
			searchTitle = series.Title.String
		}
	}

	// 这两个值会原样透传给外部元数据源（Bangumi/AniList/ComicVine…）。
	// 旧写法用 fmt.Sscanf 且完全不校验：limit=99999999 会直接打到上游，既浪费我们的
	// 配额也可能触发对方限流封禁；解析失败时 Sscanf 还会让变量保持默认值而不报错。
	// 上限取 50 与各 provider 单页返回量对齐。
	limit := queryLimit(r, "limit", 20, maxScrapeSearchLimit)
	offset := queryOffset(r, "offset")
	if offset > maxScrapeSearchOffset {
		offset = maxScrapeSearchOffset
	}

	results, total, err := provider.SearchMetadata(requestContextWithLocale(r), searchTitle, limit, offset)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, scrapeSearchFailedMsg(requestLocale(r), provider.Name(), err))
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"results":  results,
		"provider": provider.Name(),
		"limit":    limit,
		"offset":   offset,
		"total":    total,
	})
}

func (c *Controller) applyScrapedMetadata(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	var result metadata.SeriesMetadata
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid metadata payload")
		return
	}

	// 从路径参数或请求体获取 provider 用于记录来源链接
	providerName := r.URL.Query().Get("provider")

	series, err := c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	// force=1 让用户能把「拒绝过的提案」重新加回队列。没有这个出口，一旦误拒就是死胡同：
	// 之后每次刮削都会被去重挡下，同一份数据再也进不来。
	forced := isTruthyParam(r.URL.Query().Get("force"))
	queued, err := c.proposals.Queue(r.Context(), series, &result, providerName, series.Name,
		proposal.QueueOptions{IgnoreRejected: forced})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to queue metadata review")
		return
	}

	if queued.Status != proposal.QueueQueued {
		outcome, message := queueOutcomeForStatus(queued.Status)
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"queued":  false,
			"outcome": outcome,
			"message": message,
		})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"queued":      true,
		"outcome":     "queued",
		"review_id":   queued.Proposal.ID,
		"field_count": len(queued.Fields),
		"series":      series,
	})
}

// queueOutcomeForStatus 把一次入队的**非新建**结局折成给前端的 outcome 与文案。
// 新建成功不经这里——调用方各自组自己的成功响应。
//
// 表必须穷举 proposal.QueueStatus 上除「已入队」外的每一个取值：漏掉的会回落成空 outcome，
// 用户拿到的是一条 200 却与实情无关的兜底提示。
func queueOutcomeForStatus(status proposal.QueueStatus) (outcome, message string) {
	switch status {
	case proposal.QueueReusedExisting:
		return "duplicate_ignored", "待审核队列中已存在完全相同的记录，已为您忽略"
	case proposal.QueueNoChanges:
		return "no_changes", "所有数据与当前信息完全一致，无需更新"
	case proposal.QueueAllFieldsLocked:
		return "all_locked", "有差异的字段都已被锁定，解锁后再试"
	case proposal.QueueRejectedBefore:
		return "rejected_before", "这份提案此前已被拒绝，已为您忽略；如需重新加入队列请使用强制刮削"
	default:
		return "", ""
	}
}

// isTruthyParam 判定查询参数是否表示「是」。
func isTruthyParam(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func (c *Controller) scrapeSeriesMetadata(w http.ResponseWriter, r *http.Request) {
	seriesID, err := parseID(r, "seriesId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid series ID")
		return
	}

	// 从请求体解析 provider 参数
	var reqBody struct {
		Provider string `json:"provider"`
	}
	_ = json.NewDecoder(r.Body).Decode(&reqBody)

	provider := c.getProvider(reqBody.Provider)

	series, err := c.store.GetSeries(r.Context(), seriesID)
	if err != nil {
		jsonError(w, http.StatusNotFound, "Series not found")
		return
	}

	// 用系列的 title（若有）或 name 作为搜索关键词
	searchTitle := series.Name
	if series.Title.Valid && series.Title.String != "" {
		searchTitle = series.Title.String
	}

	result, err := provider.FetchSeriesMetadata(requestContextWithLocale(r), searchTitle)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, scrapeFailedMsg(requestLocale(r), provider.Name(), err))
		return
	}

	if result == nil {
		// outcome 是与前端约定的稳定结果码：前端据此决定提示级别并本地化文案，不再靠解析中文 message。
		// message 仍返回作为老客户端/未映射时的兜底文本。
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"scraped": false,
			"outcome": "not_found",
			"message": fmt.Sprintf("在 %s 上未找到与『%s』匹配的条目", provider.Name(), searchTitle),
		})
		return
	}

	queued, err := c.proposals.Queue(r.Context(), series, result, provider.Name(), searchTitle,
		proposal.QueueOptions{IgnoreRejected: isTruthyParam(r.URL.Query().Get("force"))})
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "Failed to save scraped metadata")
		return
	}

	if queued.Status != proposal.QueueQueued {
		outcome, message := queueOutcomeForStatus(queued.Status)
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"scraped": false,
			"outcome": outcome,
			"message": fmt.Sprintf("从 %s 找到条目，但%s", provider.Name(), message),
		})
		return
	}

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"scraped":     true,
		"outcome":     "queued",
		"provider":    provider.Name(),
		"message":     fmt.Sprintf("已将 %s 的『%s』加入审阅队列", provider.Name(), result.Title),
		"series":      series,
		"metadata":    result,
		"review_id":   queued.Proposal.ID,
		"field_count": len(queued.Fields),
	})
}

func (c *Controller) batchScrapeAllSeries(w http.ResponseWriter, r *http.Request) {
	ctx := requestContextWithLocale(r)

	var reqBody struct {
		Provider string `json:"provider"`
	}
	_ = json.NewDecoder(r.Body).Decode(&reqBody)

	if _, err := c.intelligence.ScrapeAll(ctx, reqBody.Provider, task.TriggerManual); err != nil {
		writeTaskLaunchError(w, err, "A batch scrape task is already running", "Failed to list libraries")
		return
	}

	provider := c.getProvider(reqBody.Provider)

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":  fmt.Sprintf("批量刮削(%s)已异步启动，任务已加入后台队列", provider.Name()),
		"provider": provider.Name(),
	})
}

func (c *Controller) scrapeLibrary(w http.ResponseWriter, r *http.Request) {
	ctx := requestContextWithLocale(r)
	libraryID, err := parseID(r, "libraryId")
	if err != nil {
		jsonError(w, http.StatusBadRequest, "Invalid library ID")
		return
	}

	var reqBody struct {
		Provider string `json:"provider"`
	}
	_ = json.NewDecoder(r.Body).Decode(&reqBody)

	if _, err := c.intelligence.ScrapeLibrary(ctx, libraryID, reqBody.Provider, task.TriggerManual); err != nil {
		writeTaskLaunchError(w, err, "A library scrape task is already running", "Failed to list series in library")
		return
	}

	provider := c.getProvider(reqBody.Provider)

	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message":  fmt.Sprintf("资源库批量刮削(%s)已异步启动，任务已加入后台队列", provider.Name()),
		"provider": provider.Name(),
	})
}
