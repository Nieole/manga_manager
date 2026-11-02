package api

import "github.com/go-chi/chi/v5"

func (c *Host) SetupRoutes(r chi.Router) {
	f, admin := c.families, c.administration
	r.Route("/api", func(r chi.Router) {
		// 多用户会话鉴权（Cookie session + CSRF + 角色）。必须在挂载任何子路由之前 Use，
		// 中间件内部会放行公开鉴权端点（/api/auth/status|setup|login）与阅读协议前缀 /api/mihon/，
		// 首次建管理员前，其余管理端点也必须拒绝访问。
		r.Use(admin.authGate)
		admin.mount(r)
		c.reader.setupRootRoutes(r)
		c.reader.setupMihonRoutes(r)
		c.organizationHTTP.setupRootRoutes(r)

		r.Get("/events", c.events.serveEvents)
		r.Get("/libraries", f.library.getLibraries)
		r.Post("/libraries", f.library.createLibrary)
		r.Put("/libraries/{libraryId}", f.library.updateLibrary)
		r.Post("/libraries/{libraryId}/relocation-plans", f.relocation.createRelocationPlan)
		r.Get("/libraries/{libraryId}/relocation-restoration", f.relocation.getRelocationRestoration)
		r.Post("/libraries/{libraryId}/relocation-restoration/checks", f.relocation.createRestorationCheck)
		r.Post("/libraries/{libraryId}/relocation-restoration/release", f.relocation.releaseRelocationProtection)
		r.Get("/libraries/{libraryId}/relocation-source-roots", f.relocation.relocationSourceRoots)
		r.Get("/libraries/{libraryId}/relocation-plans/latest", f.relocation.latestRelocationPlan)
		r.Get("/relocation-plans/{planId}", f.relocation.getRelocationPlan)
		r.Get("/relocation-plans/{planId}/details", f.relocation.relocationDetails)
		r.Post("/relocation-plans/{planId}/commit", f.relocation.commitRelocationPlan)
		r.Get("/relocation-operations/{operationId}", f.relocation.getRelocationOperation)
		r.Post("/libraries/{libraryId}/scan", f.library.scanLibrary)
		r.Post("/libraries/{libraryId}/external-libraries/session", f.disk.createExternalLibrarySession)
		r.Get("/libraries/{libraryId}/external-libraries/session/{sessionId}", f.disk.getExternalLibrarySession)
		r.Get("/libraries/{libraryId}/external-libraries/session/{sessionId}/series", f.disk.getExternalLibrarySeries)
		r.Post("/libraries/{libraryId}/external-libraries/session/{sessionId}/transfer", f.disk.transferToExternalLibrary)
		r.Post("/libraries/{libraryId}/scrape", f.maintenance.scrapeLibrary)
		r.Post("/libraries/{libraryId}/ai-grouping", f.maintenance.aiGroupingLibrary)
		r.Post("/libraries/{libraryId}/cleanup", f.library.cleanupLibrary)
		r.Delete("/libraries/{libraryId}", f.library.deleteLibrary)
		r.Get("/metadata/search", c.providers.searchMetadata)
		r.Get("/metadata/providers", c.providers.listProviders)

		r.Route("/series", func(r chi.Router) {
			c.reader.setupSeriesRoutes(r)
			r.Post("/bulk-update", c.catalogue.bulkUpdateSeries)
			c.organizationHTTP.setupSeriesRoutes(r)
			r.Post("/{seriesId}/open-dir", c.catalogue.openSeriesDirectory)
			r.Post("/{seriesId}/rescan", f.library.scanSeries)
			r.Post("/{seriesId}/scrape", c.providers.scrapeSeriesMetadata)
			r.Get("/{seriesId}/scrape-search", c.providers.scrapeSearchMetadata)
			r.Post("/{seriesId}/scrape-apply", c.providers.applyScrapedMetadata)

			r.Put("/{seriesId}/custom-fields", c.catalogue.replaceSeriesCustomFields)

			r.Get("/{seriesId}/comicinfo.zip", f.disk.exportSeriesComicInfoArchive)
			r.Post("/{seriesId}/comicinfo", f.disk.writeSeriesComicInfo)
		})

		r.Route("/books", func(r chi.Router) {
			c.reader.setupBooksRoutes(r)
			r.Get("/duplicates", f.removal.getDuplicateBooks)
			r.Post("/remove", f.removal.removeBooks)

			r.Get("/{bookId}/comicinfo.xml", f.disk.exportBookComicInfo)
			r.Post("/{bookId}/comicinfo", f.disk.writeBookComicInfo)
			r.Post("/{bookId}/cover", c.covers.setBookCoverFromPage)
			r.Post("/{bookId}/cover/upload", c.covers.uploadBookCover)
			r.Get("/{bookId}/file", c.content.serveBookFile)

		})

		r.Route("/tags", func(r chi.Router) {
			c.reader.setupTagsRoutes(r)

			r.Patch("/{tagId}", c.catalogue.renameTag)
			r.Post("/{tagId}/merge", c.catalogue.mergeTag)
			r.Delete("/{tagId}", c.catalogue.deleteTag)
		})

		r.Route("/authors", func(r chi.Router) {
			c.reader.setupAuthorsRoutes(r)

		})

		r.Get("/system/storage-io", f.storage.getStorageIODiagnostics)
		r.Get("/system/page-cache", c.content.getPageCacheStats)
		r.Delete("/system/page-cache", c.content.clearPageCache)
		r.Get("/system/tasks", f.runs.listTasks)
		r.Delete("/system/tasks", f.runs.clearTasks)
		r.Get("/system/tasks/live", f.runs.getTaskCenterLive)
		r.Get("/system/tasks/summary", f.runs.listTaskSummaries)
		r.Post("/system/tasks/pause-all", f.runs.pauseAllTasks)
		r.Post("/system/tasks/resume-all", f.runs.resumeAllTasks)
		// 重试与禁用作用在**任务**上（前者再发起一次同一件事，后者关掉它的自动发起），
		// 三条一律按**任务 id** 寻址（ADR 0007：**任务键**退出寻址）——禁用是身份上的长期属性，
		// 而外部库那两类的键带着会话 id，同一身份的两次运行键并不相同。
		// 暂停 / 恢复 / 取消作用在**运行**上，按运行 id 寻址——同一个任务此刻可以有两条仍会变化的
		// 运行（一条在跑、一条排队），按任务寻址答不出用户按的是哪一条。
		r.Post("/system/tasks/{taskID}/retry", f.runs.retryTask)
		r.Post("/system/tasks/{taskID}/disable", f.runs.disableTask)
		r.Post("/system/tasks/{taskID}/enable", f.runs.enableTask)
		r.Post("/system/runs/{runID}/pause", f.runs.pauseRun)
		r.Post("/system/runs/{runID}/resume", f.runs.resumeRun)
		r.Post("/system/runs/{runID}/cancel", f.runs.cancelRun)
		// 事件流与**采样**都按**运行**寻址，且**不进推送通道**：详情页打开时才来问这两条。
		r.Get("/system/runs/{runID}/events", f.runs.getRunEvents)
		r.Get("/system/runs/{runID}/samples", f.runs.getRunSamples)
		r.Post("/system/koreader/apply-matching", f.maintenance.applyKOReaderMatching)
		r.Post("/system/koreader/rebuild-hashes", f.maintenance.rebuildKOReaderHashes)
		r.Post("/system/koreader/reconcile", f.maintenance.reconcileKOReaderProgress)
		r.Post("/system/rebuild-index", f.maintenance.rebuildIndex)
		r.Post("/system/rebuild-initials", f.maintenance.rebuildInitials)
		r.Post("/system/rebuild-thumbnails", f.maintenance.rebuildThumbnails)
		r.Post("/system/cleanup-thumbnails", f.maintenance.cleanupThumbnails)
		r.Post("/system/rebuild-file-identities", f.maintenance.rebuildFileIdentities)
		r.Post("/system/batch-scrape", f.maintenance.batchScrapeAllSeries)

		r.Route("/pages", func(r chi.Router) {
			r.Get("/{bookId}", c.content.getPagesByBook)
			r.Get("/{bookId}/{pageNumber}", c.content.servePageImage)
		})

		r.Route("/covers", func(r chi.Router) {
			r.Get("/{bookId}", c.content.serveCoverImage)
		})

		// 通用静态直接下发，适配首卷封面作为系列代表图（支持二级哈希子目录）
		r.Get("/thumbnails/*", c.content.serveThumbnailImage)
	})
}
