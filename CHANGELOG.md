# Changelog

本文件记录项目各版本的功能新增、改动与修复。

---

### 📌 增量记录 — 2026-07-16（P0 安全与可靠性速修 · 第 1 批）

> 以一个 9 维度并行深读代码库的分析 workflow（架构/数据层/性能/安全/前端/阅读器·PWA/协议·竞品/测试·CI/产品）绘制优化地图后，落实其中「几行级、可实证、影响多用户/公网/离线可靠性」的一批 P0 速修，均带验证。

#### 安全
- **收敛「未初始化直通」窗口**：此前 `authGate` 在「站点尚无任何账户」时对**全部** `/api` 端点直通，默认 `0.0.0.0` 监听下，任何网络客户端可在首个管理员创建前枚举文件系统、读配置、触发扫描 / SSRF，甚至抢先 `POST /api/auth/setup` 成为永久管理员。改为该窗口仅放行公开鉴权端点（`/api/auth/status|setup|login`，及自带鉴权的 `/api/mihon/` 前缀），其余端点直接 `401`，把 setup 前的攻击面收敛到只剩公开端点。
- **登录与协议 Basic 鉴权限流**：新增自包含、零依赖、并发安全的失败尝试限流器 `internal/api/login_rate_limiter.go`（按 key 统计失败、指数退避锁定、内存有界）。`/api/auth/login` 按「来源 IP + 目标用户名」双键限流（15 分钟窗口 5 次失败→锁定，基础 1 分钟、指数退避、封顶 15 分钟），挡单机横扫多账户与分布式打单账户；OPDS/Mihon 的 HTTP Basic 按 IP 限流（5 分钟 10 次失败→锁定，封顶 10 分钟），**锁定期内直接 `429 + Retry-After` 而不再跑 bcrypt**，兼作 bcrypt CPU-DoS 防护。新增本地化文案 `auth.too_many_attempts`（中/英）。

#### 修复
- **写事务防死锁**：SQLite DSN 追加 `_txlock=immediate`（modernc.org/sqlite v1.51 支持），让 `ExecTx` 等写事务以 `BEGIN IMMEDIATE` 开始即取写锁。此前连接池有 `NumCPU*2`(≥8) 条可写连接却用默认 deferred 事务，两条连接各自 `BEGIN`→读→升级为写会撞上 `SQLITE_BUSY_SNAPSHOT`——`busy_timeout` 对这种快照升级冲突**无法重试**、会立刻抛 `database is locked`（扫描 + KOReader 同步 + 多用户进度并发写时间歇触发）。immediate 后 `busy_timeout` 可干净地串行化写者；已确认 store 内 `BeginTx` 仅用于写路径，只读查询走 sqlc 直查 + WAL 并发不受影响。
- **扫描完成回调不再被覆盖**：`main.go` 移除对 `SetBatchCallback` 的二次注册。此前它用一个只发 `"refresh"` 的闭包覆盖了 `NewController` 内注册的富回调 `handleScannerBatchEvent`（`SetBatchCallback` 为单字段覆盖语义），导致手动 / watch 扫描完成后 dashboard 统计缓存不再失效 / 预热（前端读到陈旧统计）、SSE 事件名恒为 `refresh` 丢失语义。
- **整卷下载 MIME 不再谎报支持**：`bookDownloadContentType` 删除 `cb7/7z`、`pdf` 分支。扫描器只摄入 `zip/cbz/rar/cbr`（`config.SupportedScanFormats`），这些分支是死代码，只会让 OPDS acquisition 链接向客户端宣告库里不可能存在、解析层也无法分页的「伪支持」下载类型；未知扩展名统一回退 `application/octet-stream`。
- **离线读图与画质参数解耦**：Service Worker `offlineFallback` 对 `/api/pages/` 请求增加 `cache.match(request, { ignoreSearch: true })` 回退。此前离线缓存按下载那一刻的画质 / 格式参数存页 URL，离线阅读用「当前偏好」重建的 URL（含不同 query）按完整 URL 匹配 Cache Storage，用户下载后改过画质即 miss → 翻页转圈 / 条漫图裂且无提示。页图路径已含页号、内容寻址，忽略 query 即可按页命中已缓存字节。
- **Service Worker 更新策略**：`index.html` 注册后监听 `controllerchange`，在「页面一开始就被某个 SW 控制」且新版本接管时自动 `location.reload()` 一次（首次安装不重载、`refreshing` 标志防循环）；配合 sw.js 既有的 `skipWaiting + clients.claim`，部署新版本后自动拿到新壳，避免继续跑引用已删 hash chunk 的旧壳导致懒加载白屏。`CACHE_NAME` v2→v3，`activate` 顺带清理累积的旧静态缓存。

#### 验证
- 新增 `TestAttemptLimiterLockoutAndReset`（限流器达阈锁定 / 成功清零 / 窗口外重置 / 指数退避递增，注入时钟）、`TestLoginRateLimiting`（真实路由：连续失败达阈后 `429 + Retry-After`，锁定期内正确密码仍被拦截）；更新 `TestSetupRoutesEnforcesSession` 断言「setup 窗口非公开端点即 401、公开端点仍可达」的新行为。
- `go build ./...`、`go vet ./...`、`gofmt` 全绿；全量 `go test ./...`（含 api/database/scanner 等）全部包 `ok`。前端 `eslint`（0）、`npm run build`（dist 重建，改动已进入 `dist/sw.js` + `dist/index.html`）、`vitest`（205）全绿；i18n 两语言 key 对齐。未改 SQL/schema 与 tsgen 目标结构体，无 sqlc / tsgen 生成漂移。

---

### 📌 增量记录 — 2026-07-06（资源库全景大数据量专项性能优化 · 第二批 Phase 2b · 逐项推进）

> 承接第一批。对 Phase 2b 的 9 项逐项推进：实现 5 项（均带测试），另 4 项经实证/风险评估暂缓（附理由，非盲目实现风险改动）。

#### 优化（已实现）
- **① 库内关键字走 series_search_fts**：`buildSeriesSearchQuery` 的关键字筛选，>=3 rune 时改用 `s.id IN (SELECT rowid FROM series_search_fts WHERE MATCH ?)`（trigram 子串、大小写不敏感、覆盖 name+title，与 instr 等价），取代对 100k 系列逐行 lower()+instr 的双重全表扫；<3 rune（CJK 常态）保留 instr 回退。
- **② 游标分页扩到 books/volumes/pages**：`supportsCursor` 及游标 encode/seek 增加这三个 NOT NULL 整数列（方向匹配的 *_desc 索引 + (name,id) tie-break 保证 keyset 稳定），前滚可跳过 COUNT 与深 OFFSET；rating 可空、read 每用户派生，故不纳入。前端 `SUPPORTS_CURSOR_FIELDS` 同步。
- **⑤ 扫描器读模型刷新节流**：扫描 flush 不再每批对每个 touched 系列全量 `UpdateSeriesStatistics + RefreshSeriesStats`（跨多批的大系列被重扫成 O(K²/batch)）；改为累积到 `dirtySeries`，由 10s ticker 与扫描末尾兜底节流刷新，使任一系列两次刷新至少间隔一个 tick，同时保留扫描中每 ~10s 的增量 UX。
- **⑦ 库级关系图谱（"资源库全景图谱"）上限**：后端 `getLibraryFranchiseGraph` 关系边硬上限 4000（+日志），防止巨型 payload；前端图谱按节点度数保留 top-200、过滤悬挂边、显示截断提示（双语 i18n），把 O(N²)×320 tick 力导向布局的 N 限死（取代把无界图挪 Web Worker——>几百节点的图本就不可读）。
- **⑨ 前端选择态 Set**：`useLibrarySelection` 内部改 `Set<number>` 并导出 `selectedSet`，`LibraryGrid` 逐卡判断从 `number[].includes`（大选择集 O(n²) 渲染）改为 `Set.has` O(1)。

#### 说明（经评估暂缓，附理由）
- **③ per-user 读排序改写**：EXPLAIN 实证 per-user readState 浏览与全局路径同为 `series` 驱动 + `ss` 主键点查 + 残差筛选，**非 per-user 独有退化**；真正修复需物化离散 `read_state` 列 + 反规范化 `read_pages`/`library_id`，写路径成本大且 `unread`（行不存在）无法索引，ROI 不足。
- **④ 进度写增量 delta**：与其共享的扫描期 O(K²) 放大已由 ⑤ 修复；阅读期单次翻页 O(K) 全系列聚合对现实 K（数百~数千）是亚毫秒级；增量 delta 需精确复刻 clamp/sentinel 语义 + reconcile 兜底，热阅读路径数据损坏风险远大于边际收益。
- **⑥ 前端网格虚拟化**：react-virtuoso 已可用，但 VirtuosoGrid 集成主 UI 有 scrape 弹层裁剪 / 响应式列测量 / window-scroll + 无限加载 / 滚动恢复等风险，项目 node 环境测试无法覆盖交互；且默认分页模式已把 DOM 限在 ≤100 卡。留待可浏览器 QA 的专门周期。
- **⑧ 系列详情 /context 分页**：契约被 franchise view、selection(allBookIds)、续读、批量操作复用（均假设整本书列表在客户端），服务端分页会破坏这些消费方，需先做兼容层，高风险。

#### 验证
- 新增 `TestSeriesListKeywordFTS`（关键字 FTS 命中 + <3 回退 + EXPLAIN）、`TestSearchSeriesCursorNumericSortsMatchOffset`（游标 vs OFFSET 全序列对拍，含平局/升降序）、`TestScanLibraryDeferredRefreshPopulatesStats`（扫描末尾节流刷新后 book_count/total_pages 最终一致）；前端 `libraryFilterParams.test.ts` 游标字段断言同步。
- `go vet ./...`、`go test ./...`（含 database/api/scanner）全绿；前端 `tsc`、`eslint`、`vitest`（205）全绿，i18n 两语言 key 对齐；`cmd/tsgen` 无契约漂移。

---

### 📌 增量记录 — 2026-07-06（资源库全景大数据量专项性能优化 · 第一批）

> 先以 9 路并行深读 + 综合排序的分析 workflow 绘制「资源库全景」性能地图（浏览/搜索/筛选/分页/聚合/关系图谱在 ~100k 系列 / 1M 图书下的瓶颈），确认默认无筛选/全局路径已达标，瓶颈集中在三条未拉齐到默认路径的支线。本批落实其中零/低风险、可实证的结构性快赢，均带 EXPLAIN / 基准 / 单测守护。

#### 优化
- **修复默认降序排序整库 filesort（本批最大发现，cmd/queryplan 简化查询漏掉的问题）**：真实 `buildSeriesSearchQuery` 的列表 ORDER BY 是**混合方向**（`<col> DESC, s.name ASC, s.id ASC`），全 ASC 的复合索引无法满足，`updated_desc`/`created_desc`/`rating_desc`/`books_desc`/`pages_desc`/`favorite_desc` 均对整库 filesort（TEMP B-TREE）。新增 7 个**方向匹配**的 `series(library_id, <col> DESC, name, id)` 复合索引，使每页从 O(N log N) 全库排序降为索引区间扫描（EXPLAIN 确认 TEMP B-TREE 消除）。
- **`/api/series/search` 的 `limit` 加硬上限 200**：此前只有下限校验，`limit=1000000` 即可让端点物化并 JSON 编码整库（~24 列含长文本 summary）→ 单请求 OOM/超时。UI 最大页 100，200 留足余量。
- **智能合集去 tags×authors 交叉 JOIN + GROUP BY**：`buildSmartCollectionBaseQuery` 无条件四路 LEFT JOIN 标签/作者再 `GROUP BY s.id`，把 `series_stats` 本要消灭的行爆炸加了回来（每系列产出 tags×authors 行、GROUP BY 使排序索引失效）。改为标签串读 `sc.tag_names_cache`、`ActiveTag`/`ActiveAuthor` 用 `EXISTS` 子查询、`COUNT(DISTINCT)`→`COUNT(*)`，与主列表口径一致，单行/系列、走排序索引。
- **关系图谱加 `idx_series_relations_target`**：`series_relations` 只有 `UNIQUE(source,target)` 前缀索引，递归 CTE 的反向边（`JOIN ... ON sr.target_series_id = c.id`）与反向关系查询（`WHERE target_series_id = ?`）此前每步全表扫；加索引后走 seek。
- **筛选列表计数省不必要的进度 JOIN**：`SearchSeriesPaged` 的 `COUNT(*)` 仅在 WHERE 引用进度来源 `ss.*`（阅读状态/进度筛选）时才 JOIN 进度表；否则省掉（`ss` 主键点查每系列至多一行、LEFT JOIN 不放大行数，结果不变）。
- **年度/月度回顾统计改 sargable 区间**：`user_stats.go` 的 `strftime()/substr()` 包裹列改为左闭右开日期区间（`date >= ? AND date < ?`、`last_read_at >= ? AND last_read_at < ?`），语义等价但让 `(user_id, date)` / `(user_id, last_read_at)` 索引 range-scan，老账号多年活动的回顾页从 O(账号总活动) 降到 O(该期活动)。
- **迁移末尾 `PRAGMA optimize`（`analysis_limit=400`）**：给面对约 20+ 个 `library_id` 前缀重叠索引的规划器提供选择性统计（sqlite_stat1），免版本 bump、成本受限。

#### 说明（经实证否决 / 归入后续）
- 分析曾建议给 `user_series_progress` 补 `(user_id, read_pages/completed_book_count)` 索引以救 UserID>0 的读状态/排序。**EXPLAIN 实证否决**：该路径由按 `library_id` 过滤的 `series` 驱动、`ss` 是按主键点查的 LEFT JOIN 右表，读状态是 join 后残差筛选，加索引规划器不会选用——故**未加**，避免无谓写放大。真正的 per-user 读排序需查询改写（物化离散 read_state 列），归入后续批次。
- 未纳入本批（体量大/涉用户可见行为，待评估）：库内关键字改走 `series_search_fts`（当前 instr 全表扫，基准证实为最慢路径）、游标分页扩到 rating/books 等以跳过 COUNT、读模型进度写增量化 delta、扫描器读模型刷新合并、前端资源库网格/系列详情虚拟化、库级关系图谱端点节点上限 + 力导向布局挪出主线程。

#### 验证
- 新增 `store_queryplan_test.go`（EXPLAIN 断言：关系图谱 target 索引命中、7 种降序排序均无 filesort、per-user 进度按主键点查不 SCAN）、智能合集 `TestSmartCollectionTagAuthorFilters`（标签/作者 EXISTS 改写命中正确、多标签不重复、tags_string 来自缓存）、`TestSearchSeriesPagedCapsLimit`（limit 压顶）、`GetUserPeriodStats` 跨期隔离断言。
- `cmd/queryplan` 期望索引同步为 `*_desc` 变体并新增反向关系用例。`go vet ./...`、`go test ./...` 全绿；`cmd/tsgen` 无契约漂移。schema 仅新增 `CREATE INDEX`（未动任何 CREATE TABLE / `sql/query.sql`），不产生 sqlc 生成漂移。

---

### 📌 增量记录 — 2026-07-05（修复：阅读器返回导致历史栈错乱）

#### 修复
- 修复导航 bug：从系列页进入阅读器后点返回，会退回到系列页，但**再点返回却回到了阅读器**而非资源库。根因是阅读器的返回按钮用 `navigate('/series/:id')`（push）又压了一个系列页，导致该系列页的浏览器回退指向了阅读器。
- 改为：从站内进入阅读器时（历史栈里阅读器之前有来源页），返回走浏览器回退 `navigate(-1)`，弹出阅读器这一条历史，正确回到来源页；仅在直达阅读器（深链/新标签/刷新页、无站内历史）时导航到系列页，并用 `replace` 覆盖，确保系列页后面不再残留阅读器。
- 决策逻辑抽成纯函数 `web/src/pages/book-reader/readerNavigation.ts::computeReaderBack`（以 `location.key !== 'default'` 判定是否有站内历史，`useRef` 在挂载时定格，不受阅读器内自动翻页 replace 影响）。

#### 验证
- 新增 `readerNavigation.test.ts`（5 例，含回归场景）；前端 `npm run test`（29 通过）、`npm run lint`（0）、`npm run build` 通过。

---

### 📌 增量记录 — 2026-07-05（深度阅读统计 · P1 第 6 项，全部按用户）

> 建立在完整多用户之上，全部按当前用户统计。**至此 10 项改进全部完成。**

#### 新增
- **每用户活动 / 连续天数 / 回顾**：新表 `user_reading_activity(user_id, book_id, date, pages_read, read_seconds)`（取代全局热力图数据；旧全局活动在首个管理员创建时迁入）。进度写入路径全局表 + 每用户表双写。新增 store：`GetUserReadingStreak`（当前/最长连续阅读天数，Go 侧计算）、`GetUserActivityHeatmap`、`GetUserPeriodStats`（年度/月度回顾：页数/时长/活跃天数/涉及本数/读完本数/最多阅读系列）。热力图端点改为按用户双路径。新端点 `/api/stats/streak`、`/api/stats/period`。
- **每本阅读时长**：新表 `user_book_reading_time(user_id, book_id, total_seconds)`（增量累加）。前端 `useReaderReadingTime` hook——按"活跃阅读"计秒（可见 + 近期有操作才计，切后台/长空闲暂停），心跳 + 切书/卸载/隐藏时经 `navigator.sendBeacon` 上报到 `POST /api/books/{id}/reading-time`（该端点 CSRF 豁免，因 beacon 无法带头，仍要求会话）。端点 `/api/stats/reading-time`（累计 + 每本排行）。
- **个人系列短评**：新表 `user_series_review(user_id, series_id, rating, review)`（与全局刮削评分区分）。端点 `GET/PUT/DELETE /api/series/{id}/review`。前端系列详情侧栏新增「短评」页（星级 + 短文本）。
- **独立「统计」页**：`web/src/pages/Stats.tsx` + 侧栏入口——连续天数、累计时长、年度/月度回顾（切年/月）、每本时长排行。

#### 质量
- 用 4 维并行 + 逐条对抗式验证的复核 workflow 审查本项改动，确认并修复 5 个真实缺陷：① `books_completed` 恒为 0（`last_read_at` 由 Go `t.String()` 落库、SQLite 日期函数解析不了 → 改用 `substr` 取年月前缀）；② 阅读时长 hook 上报失败重加会跨书错记/丢包重复计数（改为失败即丢弃）；③ reading-time 端点未校验书存在致 FK 500 日志噪声（补存在性检查、缺失即静默接受）；④ 短评面板切系列时残留旧值可致跨系列串写（加载前先清空）；⑤ `formatDuration` 会显示 "1h 60m"（先算总分钟再拆）。

#### 说明
- `books_completed` 以 `last_read_at`（服务器本地时区）落期，其余按 UTC `date`，跨零点边界有极小口径差；为软统计可接受。活动双写为两张独立表，无重复计数。

#### 验证
- 新增 `TestUserReadingStreak`、`TestUserBookReadingTime`（含 books_completed）、`TestUserSeriesReviewIsolation`；`go build`、`go vet`、`go test ./internal/...` 全绿。前端 `npm run lint`（0）、`npm run build` 通过，i18n 两语言 key 对齐。

---

### 📌 增量记录 — 2026-07-05（完整多用户 · P0 第 3 项 · 阶段3：KOReader 并入 + OPDS/Mihon 按用户鉴权）

> 承接阶段2。本阶段让三个阅读协议（OPDS/Mihon/KOReader）按站点用户鉴权，进度随之转为每用户。**至此第 3 项「完整多用户」三阶段全部完成。**

#### 新增
- **OPDS / Mihon HTTP Basic 鉴权**：新中间件 `requireBasicAuth`——用站点用户名+密码校验（bcrypt），成功则把用户写入请求上下文（`currentUserID` 取用），失败 401 + `WWW-Authenticate: Basic`。带 TTL 内存缓存已验证凭据（`basicAuthCache`），避免每个协议请求都跑 bcrypt。站点尚无账户时（首启）直通。挂到 `/opds/v1.2` 与 `/api/mihon/v1` 路由组（Mihon 仍被 authGate 放行，Basic 是其唯一鉴权）。
- **Mihon 进度按用户**：进度写入端点复用 `updateBookProgress`，经上下文用户自动写入本人 `user_book_progress`；继续阅读、系列书目响应改按当前用户取数/叠加。
- **OPDS 进度按用户**：继续阅读、系列书目 feed 的 last-read 按当前用户显示。
- **KOReader 账户并入站点用户**：`koreader_accounts` 新增 `user_id` 列（0=未关联）。现有账户在首个管理员创建时并入该管理员（`AssignOrphanKOReaderAccountsToUser`）；管理员创建的账户归创建者，设备自助注册的账户归首个管理员。KOReader 同步进度（push / pull / reconcile 三条路径）经账户 `user_id` 写入对应用户的每用户进度（未关联则回落全局），「不回退」保护按对应用户既有进度比较。为避免改动 `KOReaderAccount` 结构与其众多扫描点，账户→用户映射走独立单列查询（`GetKOReaderAccountUserID`/`SetKOReaderAccountUser`）。

#### 说明
- 首启阶段与未登录协议请求仍走旧全局路径（双路径），保持向后兼容与既有测试。活动热力图/书签等仍全局（见阶段2 说明）。

#### 验证
- 新增 `TestProtocolBasicAuth`（首启直通 → 建账户后无/错/正确凭据分别 401/401/200）；`go build`、`go vet`、`go test ./internal/...` 全绿。

---

### 📌 增量记录 — 2026-07-05（完整多用户 · P0 第 3 项 · 阶段2：每用户阅读进度）

> 承接阶段1（用户+会话鉴权）。本阶段把阅读进度从全局拆成按用户，并把旧全局进度迁移到第一个管理员。

#### 新增
- **每用户进度存储**：新表 `user_book_progress(user_id, book_id, last_read_page, last_read_at)` 取代全局 `books.last_read_page/last_read_at`；派生 `user_series_progress(user_id, series_id, read_pages, read_book_count, completed_book_count, last_read_at, last_read_book_id)`——等价旧 `series_stats` 的进度列但按用户拆分，`series_stats` 仅保留全局封面/标签缓存。手写 store（`user_progress.go`）：`SetUserBookProgress`/`ClearUserBookProgress`/`SetUserBooksReadState`（写进度并按 (user,series) 增量刷新）、`GetUserBookProgress(Map)`、`GetUserRecentReadAll/Series`、`GetUserReadBooksCount`、`MigrateGlobalProgressToUser`。
- **迁移**：首个管理员在 setup 时把旧全局进度幂等迁入其名下并回填系列聚合。
- **双路径设计**：`currentUserID(r)` 已登录返回用户 id（>0 走每用户路径），无用户返回 0（首启/单元测试/未接入会话的协议走旧全局路径，行为与既有测试完全一致）。
- **改写路径**：单本进度、KOReader 式批量同步、批量标记已读/未读全部按当前用户写入 `user_book_progress`（节流缓存按用户键控）。
- **读取路径**：书目响应（书详情/系列卷册/继续阅读）按当前用户叠加进度；库页列表/搜索（`buildSeriesSearchQuery` 引入 `sc`=封面缓存 / `ss`=进度来源 的别名拆分）、智能合集、看板「继续阅读」与已读书本数、阅读清单进度均按当前用户取数。

#### 说明
- 活动热力图（`reading_activity`）、书签（`reading_bookmarks`）、续读建议、全局搜索下拉的进度本阶段仍为全局，留待后续/第 6 项。KOReader/OPDS/Mihon 仍读全局进度，阶段3 接入按用户鉴权后转为每用户。

#### 验证
- 新增 `TestUserBookProgressIsolationAndAggregation`（两用户进度隔离、系列聚合、阅读状态按用户筛选、清除重算）、`TestMigrateGlobalProgressToUser`；`go build`、`go vet`、`go test ./internal/...` 全绿（既有全局路径测试不变，验证向后兼容）。

---

### 📌 增量记录 — 2026-07-05（完整多用户 · P0 第 3 项 · 阶段1：用户 + 会话鉴权）

> 第 3 项「完整多用户」分三阶段推进：**阶段1 用户体系与鉴权（本次）** → 阶段2 每用户阅读进度迁移 → 阶段3 KOReader 并入 + OPDS/Mihon 按用户鉴权。

#### 新增
- **站点账户体系**：新表 `users`（用户名 / bcrypt 口令 / 角色 admin·regular / 显示名 / must_change_password）与 `sessions`（服务端会话，cookie 存随机令牌、DB 存其 SHA-256，含 csrf_token 与滑动过期），均在 `schema.sql` 由 `Migrate()` 幂等建表。手写 store 方法（`users.go`）：用户增删查改、计数、首个管理员定位、会话 CRUD 与过期清理，全部登记进 `Store` 接口。
- **Cookie session + CSRF 鉴权**：新中间件 `authGate` 统一守卫 `/api` 组，取代已退役的可选共享令牌（`requireAuth`/`extractAPIToken` 删除，`Server.Auth` 配置字段仅保留兼容）。改写类请求（POST/PUT/PATCH/DELETE）需在 `X-CSRF-Token` 头回传会话绑定的 CSRF 令牌。
- **强制登录 + 首次建管理员**：站点尚无账户时 `authGate` 直通（首启无数据可护），前端进入「创建首个管理员」引导页；建成后全站强制登录。端点 `GET /api/auth/status`、`POST /api/auth/setup|login|logout`、`GET /api/auth/me`、`POST /api/auth/change-password`。
- **角色权限**：管理员全权；普通用户只读浏览 + 记录本人阅读状态（进度/书签/短评），不能改共享库元数据或访问 `/api/system`、`/api/users`。集中在 `authGate.authorize` 实施。
- **账户管理（仅管理员）**：端点 `GET/POST /api/users`、`PATCH /api/users/{id}`、`POST /api/users/{id}/password`、`DELETE /api/users/{id}`；守卫「不能删自己 / 删或降级最后一个管理员」。管理员代建账户置 `must_change_password`，用户首登强制改密。
- **前端**：axios 切到 Cookie 模式（`withCredentials` + 改写请求附 `X-CSRF-Token`，旧 `mm_token`/`X-API-Token` 退役，`withApiToken` 退化为无操作）。新增 `AuthProvider`（启动探测状态、登录/登出/改密、401 兜底回登录页）与 `AuthGate`（加载→建管理员→登录→强制改密→放行）；登录页 / 首启引导页 / 强制改密页；顶栏账户菜单（改密 + 登出）；设置页「用户管理」（仅管理员，建/删账户、改角色、重置密码）。i18n zh-CN/en-US 同步。

#### 说明
- 生产环境 SPA 与 API 同源，Cookie 自动携带；dev 经 vite 代理亦同源。会话采用服务端存储（非 JWT），改密即失效全部旧会话。
- 阅读进度本阶段仍为全局，阶段2 再拆为每用户并迁移到第一个管理员。

#### 验证
- 新增后端测试 `TestSetupRoutesEnforcesSession`、`TestAuthGateRoles`、`TestAuthSetupFlow`、`TestAuthLoginAndChangePassword`、`TestAuthUserManagementGuards`；`go build`、`go vet`、`go test ./internal/...` 全绿。前端 `npm run lint`（0）、`npm run build` 通过，i18n 两语言 key 对齐（1885/1885）。

---

### 📌 增量记录 — 2026-07-05（重复文件去重工作流 · P1 第 7 项）

#### 新增
- 整理页(`/organize`)新增「重复文件」面板：按 `file_hash` 分组列出内容完全相同的书籍，勾选后**安全移除**——只从库中删除记录，可选把源文件**移入回收站目录**，**绝不硬删源文件**，移除前二次确认。
- 后端手写 `FindDuplicateBooks`（`file_hash` 非空且出现多次，带系列名/路径/大小/页数）；端点 `GET /api/books/duplicates` 分组返回、`POST /api/books/remove`（body `{book_ids, move_to_trash}`）。移除复用既有 `DeleteBook`（删记录 + 刷新系列统计）；`move_to_trash` 把文件移到 `<数据库目录>/trash`（`rename` 失败跨盘时回退复制+删除），**移动失败即不删记录**避免孤儿。
- 前端 `DuplicatesPanel`（分组勾选、移入回收站开关、`ConfirmDialog` 二次确认）。

#### 说明
- 依赖 identity_scan / repair_scan 计算的 `file_hash`；未计算指纹的书不参与。默认不勾选任何书，避免误删。

#### 验证
- `go build`、`go vet`、`go test ./internal/api ./internal/database` 全绿；前端 `npm run lint`（0）、`npm run build` 通过。

---

### 📌 增量记录 — 2026-07-05（系列自定义字段 · P1 第 5 项之四，完成第 5 项）

#### 新增
- 系列元数据编辑弹窗新增「自定义字段」区：任意 key-value 元数据（如 ISBN、收藏位置、装帧版本），加/删行、独立保存。
- 新表 `series_custom_fields(series_id, field_key, field_value, PK(series_id,field_key), FK→series ON DELETE CASCADE)`（`schema.sql`，`Migrate()` 幂等建表，无需 sqlc）。手写 store：`ListSeriesCustomFields`、`ReplaceSeriesCustomFields`（整体替换、空 key 跳过、事务）。端点 `GET/PUT /api/series/{id}/custom-fields`。
- 前端 `SeriesCustomFieldsEditor`（自包含 GET/PUT，与主元数据保存解耦）内嵌编辑弹窗。

#### 验证
- 新增 `TestSeriesCustomFields`（替换/列出/空 key 跳过/更新值）；`go build`、`go test ./internal/api ./internal/database` 全绿；前端 `npm run lint`（0）、`npm run build` 通过。**至此第 5 项(元数据编辑：批量编辑 + 标签管理 + 自定义封面 + 自定义字段)全部完成。**

---

### 📌 增量记录 — 2026-07-05（自定义封面：设为封面 / 上传封面 · P1 第 5 项之三）

#### 新增
- 收藏者可自定义书封面：
  - **阅读器**顶栏新增「设当前页为封面」（`ImagePlus`）——把正在看的这一页设成该书封面。
  - **系列书卡「⋯」菜单**新增「上传封面」——选一张本地图片作为封面（前端就地缓存刷新即时可见）。
- 后端在扫描器封面管线上新增 `Scanner.SetBookCoverFromPage`（按页取图）与 `SetBookCoverFromImage`（上传字节），复用 `images.ProcessImage`（400px 缩略图）+ 内容 SHA1 寻址落盘（同扫描封面的目录方案，天然去重/刷新缓存）；`store.SetBookCover` **无条件**更新 `books.cover_path`（区别于只在缺失时写的 `SetBookCoverIfMissing`），随后 `RefreshSeriesStats`。
- 端点 `POST /api/books/{id}/cover`（body `{page}`）与 `POST /api/books/{id}/cover/upload`（**首个 multipart 处理器**，16 MiB 上限 + `image/*` 类型校验，错误经 `apiText` 本地化）。

#### 验证
- `go build`、`go vet`、`go test ./internal/api ./internal/scanner ./internal/database` 全绿；前端 `npm run lint`（0）、`npm run build` 通过。

---

### 📌 增量记录 — 2026-07-05（标签管理：重命名 / 合并 / 删除 · P1 第 5 项之二）

#### 新增
- 设置新增「标签管理」页(`/settings/tags`)：跨全库列出标签(带系列计数)、搜索，并支持**重命名 / 合并 / 删除**——都是影响多个系列的操作，删除走二次确认。
- 后端手写 store 方法:`RenameTag`(重名触发 UNIQUE 冲突→前端提示改用合并)、`MergeTags`(迁移 `series_tags` 关联后删源标签)、`DeleteTag`(级联清理 `series_tags`);三者均在操作后刷新受影响系列的派生统计(`tag_names_cache` 等)。端点 `PATCH /api/tags/{id}`、`POST /api/tags/{id}/merge`、`DELETE /api/tags/{id}`;重名冲突文案经 `apiText` 本地化(`tag.rename.conflict`)。
- 前端 `SettingsTagsPage`(内联改名、合并目标 datalist、`ConfirmDialog` 删除);`Settings`/`SettingsContext`/`App` 的 section 类型与路由同步新增 `tags`。

#### 验证
- 新增 `TestTagManagement`(改名生效、合并后仅剩目标且源被删、删除后系列无标签);`go build`、`go test ./internal/api ./internal/database` 全绿;前端 `npm run lint`(0)、`npm run build` 通过。

---

### 📌 增量记录 — 2026-07-05（批量编辑多个系列 · P1 第 5 项之一）

#### 新增
- 资料库多选后新增「批量编辑」：一次对多个系列**增量**改元数据 —— 添加标签 / 移除标签 / 设置状态 / 设置出版社（留空即不改）。与单系列编辑的"全量替换"不同，标签是增量增删。
- 后端新增手写事务方法 `BulkEditSeries`（`store.go`）：单事务内更新 status/publisher、UpsertTag+Link 增标签、按名解绑删标签，再逐个 `RefreshSeriesStats` 刷新派生统计（tag_names_cache 等）；`tags.series_count` 由既有触发器维护。端点 `POST /api/series/bulk-edit`（`BulkEditSeriesRequest`，字段用指针/空表达"不改"）。
- 前端新增 `BulkEditSeriesModal`（标签 chip 输入 + `/api/tags/all` 补全 datalist、状态下拉、出版社），接入 `LibrarySelectionBar` 的「批量编辑」动作，成功后刷新当前页。

#### 验证
- 新增 `TestBulkEditSeries`（加/删标签、改状态、多系列一致）；`go build`、`go test ./internal/api ./internal/database` 全绿；前端 `npm run lint`（0）、`npm run build` 通过。

---

### 📌 增量记录 — 2026-07-05（新增刮削源：AniList / MangaDex / MyAnimeList / Comic Vine · P0 第 2 项）

#### 新增
- 元数据刮削从「仅 Bangumi + LLM」扩展到 **6 个来源**，覆盖中日与欧美/多语言库：
  - **AniList**（`internal/metadata/anilist.go`，GraphQL，免密钥）：英文/罗马音/原名、简介（去 HTML）、封面、评分（0–100→0–10）、genres+高 rank 标签、staff 作者角色、卷数、状态、siteUrl。
  - **MangaDex**（`mangadex.go`，REST，免密钥）：多语言标题、简介、封面（uploads CDN）、标签、author/artist 去重、状态（原生小写直用）。
  - **MyAnimeList**（`myanimelist.go`，需 `scrapers.mal_client_id`）：标题、简介、封面、mean 评分、genres、作者、卷数、状态映射。
  - **Comic Vine**（`comicvine.go`，需 `scrapers.comicvine_api_key`）：欧美 comics 卷元数据、出版社、封面、期数、site_detail_url。
  - 四者均实现现有 `Provider` 接口（`SearchMetadata`/`FetchSeriesMetadata`），复用 429/5xx 指数退避重试；结果照旧进**元数据审核队列**，不直接改库。
- `getProvider` 注册四源；`listProviders` 动态返回：AniList/MangaDex 恒有，**MyAnimeList/Comic Vine 仅在配置了对应密钥时出现**（否则不可选）。`metadataDefaultConfidence` 为新源补默认置信度。
- 配置：`config.yaml` 新增 `scrapers.{mal_client_id, comicvine_api_key}`，两者经 `MaskSecrets`/`RestoreMaskedSecrets` 脱敏回填，绝不回显明文；`config.example.yaml` 同步。
- 前端：新增 `useScrapeProviders`（拉 `/api/metadata/providers`，全局缓存）；系列页工具条与资料库卡片的刮削菜单**改为按后端声明动态渲染**（不再硬编码 bangumi/llm）。系列页刮削判定由 `=== 'bangumi'` 改为 `!== 'llm'`，使所有外部源统一走「搜索候选→人工挑选」弹窗（后端 scrape-search/apply 本就与 provider 无关）。

#### 说明
- 侧栏「整库批量刮削」仍默认 Bangumi（批量作业不走候选挑选流程）；MAL/Comic Vine 需用户在设置里填入各自凭据后方可用。

#### 验证
- `go build`、`go vet`、`go test ./internal/api ./internal/config ./internal/metadata` 全绿（更新 `TestMetadataLookupValidationHandlers` 断言新的动态 provider 列表 + 密钥门控）；前端 `npm run lint`（0）、`npm run build` 通过。4 个 provider 文件由并行子代理生成，经 gofmt/vet 校验。

---

### 📌 增量记录 — 2026-07-05（ComicInfo 写回归档 · 收藏者体验⑥）

#### 新增
- 支持把库内元数据**写回归档内的 ComicInfo.xml**，让收藏者精心刮削/修订的元数据能烧进自己的文件、换软件也带得走：
  - 系列书卡「⋯」菜单新增「写入 ComicInfo 到文件」（单本）；系列详情工具条新增「写入 ComicInfo 到所有归档」（整系列，返回 写入/跳过/失败 计数）。
  - 均**二次确认**后触发（修改用户原始文件）。
- 后端新增 `parser.WriteComicInfoIntoArchive`：**仅 cbz/zip 可写**（rar/cbr 返回 `ErrArchiveNotWritable` 并跳过，Go 无 rar 写库）；采用「同目录临时文件 + 原子 rename 覆盖」，中途失败不损坏原文件，**不备份**（按确认策略）；替换已存在的 ComicInfo.xml，保留其余页条目原头部。Windows 下 rename 前先关闭源句柄。
- 端点 `POST /api/books/{id}/comicinfo`（单本）与 `POST /api/series/{id}/comicinfo`（整系列），复用导出路径的 `buildComicInfoForBook` 构造。错误文案经 `apiText` 本地化（`comicinfo.write.unsupported` / `comicinfo.write.failed`）。

#### 验证
- 新增 `TestWriteComicInfoIntoArchiveAddsAndReplaces`（新增/替换不重复、保留页条目、可正常重开）、`TestWriteComicInfoIntoArchiveRejectsRar`；`go build`、`go vet`、`go test ./internal/parser ./internal/api` 全绿；前端 `npm run lint`（0）、`npm run build` 通过。

---

### 📌 增量记录 — 2026-07-05（资源库筛选补充：阅读状态/评分/进度/加入时间 · 收藏者体验③）

#### 新增
- 资源库筛选条补齐此前只有智能合集才有的维度，让"我库里还有哪些没读的"能在库页一键筛：
  - **阅读状态**：未读 / 在读 / 已读完（基于 series_stats 的已读、读完卷册数与 series.book_count）。
  - **评分区间**（0–10）、**阅读进度区间**（0–100%，read_pages / total_pages）、**加入时间**（最近 7/30/90 天）。
  - 均以激活芯片展示、可单独移除，纳入"清空所有筛选"，并**同步到 URL query**（`read/rmin/rmax/pmin/pmax/days`）与本地持久化，刷新/分享可恢复。
- 后端 `SearchSeriesPaged` / `SearchSeriesCursor` 改用 `SeriesListFilters` 结构承载全部筛选（收敛多参数签名），新增阅读状态 / 评分 / 进度 / 加入天数的 WHERE 构造；筛选计数查询同步 `LEFT JOIN series_stats`。控制器 `/api/series/search` 解析新参数。
- 前端：`useLibraryFilters` 新增 `advanced` 高级筛选对象（URL 同步 + 持久化 + 重置），`useLibrarySeries` 透传查询参数，`LibraryFilterBar` 新增四组筛选控件与芯片。

#### 验证
- 新增 `TestSearchSeriesPagedAdvancedFilters`（覆盖 unread/reading/completed、评分下限、进度上/下限、加入天数）；`go build`、`go vet`、`go test ./internal/database ./internal/api` 全绿；前端 `npm run lint`（0）、`npm run build` 通过。

---

### 📌 增量记录 — 2026-07-05（翻页阅读器自由缩放 · 收藏者体验④）

#### 新增
- 翻页模式（PagedReader）新增自由缩放 + 平移，看画集 / 同人本 / 小字对话时可放大看细节：
  - **捏合**（触屏双指）、**双击**（放大 2.5x / 还原）、**Ctrl+滚轮**（桌面）三种缩放入口，范围 1x–4x。
  - 缩放态下**单指拖拽平移**；容器切到 `overflow-hidden` 由 transform 平移承载，避免与 1x 态的宽图滚动拖拽冲突。
  - 每次翻页 / 切换双页自动复位到 1x。
- 新增 `useReaderZoom` hook（`book-reader/useReaderZoom.ts`）统一接管指针：单指点按经**单/双击消歧**后，双击缩放、单击回调交还 PagedReader 处理翻页/中央切换；双指捏合缩放；缩放态拖拽平移。Ctrl+滚轮用原生非被动监听以可靠 `preventDefault`（避免触发整页缩放）。
- 条漫模式（WebtoonReader）不引入缩放，维持竖向滚动手感。
- 实现完全收在阅读器内部，`BookReader.tsx` 未改动。

#### 行为变化 / 权衡
- 为与双击缩放消歧，1x 态的**点按翻页**改为延迟 ~280ms 执行（等待可能的第二次点按）。键盘 / 中央章节导航不受影响、仍即时。若觉翻页迟滞可后续调低或改用其它缩放入口。

#### 验证
- 前端 `npm run lint`（0）、`npm run build` 通过。

---

### 📌 增量记录 — 2026-07-05（资源库三档视图切换 · 收藏者体验⑤）

#### 新增
- 资源库页新增视图密度切换（大图 / 紧凑 / 列表），偏好持久化到 `localStorage`（`manga_manager_library_view_mode`），刷新保留：
  - **大图**：现有大封面网格（`minmax(180px)`），显示简介，默认值。
  - **紧凑**：更密网格（`minmax(130px)`）、更多列、隐藏简介，适合中大库扫读。
  - **列表**：横向信息行 —— 封面缩略 + 标题 + 卷/册计数 + 总页数 + 评分 + 进度条 + 「上次读到」，大库高密度浏览最快。列表行保留收藏 / 重扫 / 刮削操作与多选。
- `LibraryHeader` 右侧新增分段切换控件（图标 `LayoutGrid` / `Grid3x3` / `LayoutList`），仅在有系列时显示。
- 新增 `ViewMode` 类型（`library/types.ts`）；`LibraryCard` 按 `viewMode` 渲染纵向卡 / 紧凑卡 / 列表行；i18n 补 `library.view.grid|compact|list`（中/英）。

#### 验证
- 前端 `npm run lint`（0）、`npm run build` 通过。

---

### 📌 增量记录 — 2026-07-05（原始归档下载入口 · 收藏者体验①）

#### 新增
- 在 Web 端露出「下载原文件」入口：后端整卷下载端点 `GET /api/books/{bookId}/file`（`serveBookFile`，带 `Content-Disposition: attachment`、正确归档 MIME 与 Range 断点续传）此前只被 OPDS 消费，前端无入口；现在
  - 系列详情的书卡「⋯」菜单新增「下载原文件」，位于「导出 ComicInfo」与「复制识别码」之间；
  - 阅读器顶栏新增下载按钮（位于书签左侧，仅当前书 ID 有效时显示）。
- 新增前端助手 `web/src/utils/download.ts`（`downloadBookFile`）：经 `withApiToken` 在启用可选管理令牌时把 token 附到 URL；不设 `download` 属性，保留服务器给出的（含中文卷名的）文件名。下载动作自包含、无需穿透父级 props。
- i18n 补 `series.book.download` / `reader.download`（中/英）。

#### 验证
- 前端 `npm run lint`（0）、`npm run build`（tsc 严格 + Vite 打包）通过。

---

### 📌 增量记录 — 2026-07-05（任务引擎状态收敛为 taskEngine · M17 保守版）

#### 重构（行为保持）
- 把散在 Controller 上的 7 个后台任务引擎状态字段(`taskMutex` / `tasks` / `taskRuntimes` / `taskSeq` / `taskPersistPending` / `taskPersistWake` / `taskRelaunchers`)收敛进一个内聚的 `taskEngine` 结构体(`controller_tasks.go`),Controller 改持 `taskEngine *taskEngine`(经 `newTaskEngine()` 构造)。任务引擎的内存状态边界从此清晰、集中一处。
- 任务方法仍是 Controller 方法、**155 个外部调用点**(scrape/koreader/maintenance/library/scan/recommendations 等)一律不动——仅把内部字段访问从 `c.taskX` 改为 `c.taskEngine.<字段>`(174 处,机械重命名)。锁语义、异步落盘 goroutine、生命周期、重试注册表逻辑一字未改,行为完全等价。
- 说明:审计 M17 原意是抽为独立 `internal/task` 包;但该引擎与 Controller 深度耦合(重试注册表回调、存储令牌、SSE 广播、生命周期),且 `TaskStatus` 类型被 api 响应与 M47 tsgen 广泛引用,全量跨包迁移 churn 极大、风险高且零用户收益。本次采用**同包状态收敛**,拿到"引擎状态内聚"的主要可维护性收益,风险可控;跨包全量迁移留作后续单独立项。

#### 验证
- `go vet`、`go build ./...`、`go test ./...`、`go test -race ./internal/api`(70s,无数据竞争)全绿。

---

### 📌 增量记录 — 2026-07-05（前后端契约类型生成管线 · M47 完整）

#### 重构 / 基础设施
- 引入 Go→TS 契约类型生成管线,以后端响应结构体为单一事实源,根治此前前端手写类型与后端各自漂移(M47 局部已修 `paused_at`、`Null*` 收敛等具体点,本次落地机制):
  - 新增 `cmd/tsgen`:反射生成器,对 `targets` 列表中的结构体(首批 `api.TaskLimits`、`api.TaskStatus`)生成 TS 接口到 `web/src/api/generated.ts`。按 json tag 命名、pointer / `omitempty` → 可选、`time.Time` → `string`、`sql.Null*` → 复用 `contracts.ts` 的单一定义(生成文件 `import` 之);遇未纳入的具名结构体显式失败,不静默产出 `unknown`。
  - 前端 `TaskCenter.tsx` 删除手写的 `TaskStatus` / `TaskLimits`,改为从 `api/generated.ts` 再导出,消费方 import 路径不变(`TaskCenter` / `BackgroundTasks` / `Layout` 等)。
  - CI 新增 drift 校验(`.github/workflows/ci.yml`,ubuntu):`go run ./cmd/tsgen` 后 `git diff --exit-code -- web/src/api/generated.ts`——改了源结构体却没重新生成即报错。`AGENTS.md` 同步说明。
- 扩展方式:后续把更多响应结构体(如 `database.Series`)纳入受管,只需加入 `cmd/tsgen` 的 `targets`。

#### 验证
- `go vet ./cmd/tsgen`、`go build ./...`;前端 `npm run build`(tsc 严格:生成类型无缝替换手写版、消费方零破裂)、`npm run lint`(0)、`npm run test`(24)通过;`go run ./cmd/tsgen` 幂等(再次生成无 diff)。

---

### 📌 增量记录 — 2026-07-05（系列详情主数据错误态 + 重试 · M48 续批）

#### 修复(用户可见)
- `useSeriesContext`(系列详情页主数据 `/api/series/{id}/context`)此前加载失败只 `console.error`、`series` 保持 null 后被传给整页 → 页面破损、无提示、无法重试。现新增 `error` 状态(成功清空、失败经 `getApiErrorMessage` 取可读消息)与 `retry`;`series-detail/index.tsx` 在"非加载中且无 series"时渲染错误 + 「重试」按钮,而非把 null 传给 Hero 等子组件。复用 `common.retry`,新增 `series.content.loadFailed` 中/英文案。
- 说明:M48 其余静默 catch 经核对多为**可接受**——`SeriesFranchiseView` / `franchise-graph` 的失败态与"无关联"同为空(可选内容)、`BackgroundTasks` 的 storageIO 为辅助诊断面板(主任务列表另有加载态处理),故不强加错误 UI。

#### 验证
- 前端 `npm run lint`(0 problems)、`npm run build`、`npm run test`(24)通过。

---

### 📌 增量记录 — 2026-07-05（Layout.tsx god-component 抽离 hooks/组件 · M49）

#### 重构（行为保持）
- 从 1252 行的 `components/Layout.tsx` 抽出 4 个自包含单元到 `components/layout/`(该目录已有 constants/types/useGlobalSearch/LibraryFormModal/SearchModal):
  - `SidebarNav.tsx`:`SidebarGroup` + `SidebarLink` 两个侧栏基础展示组件。
  - `useDirectoryBrowser.ts`:目录选择器的 `browse*` 状态与打开/导航请求逻辑。
  - `useTaskBubbles.ts`:后台任务气泡的状态、终态延时清理定时器、进度覆盖事件监听,以及 SSE 进度接入(`ingestProgress`)、手动关闭(`dismiss`)与清理已完成(`clearFinished`)。
  - `useLayoutShortcuts.ts`:全局键盘快捷键(⌘K / `/` / `?` / `[` / g 前缀跳转),用 ref 持最新回调、监听器只装一次。
- Layout 通过解构复用这些 hook 的返回值,render 引用名不变;SSE mount effect 仅保留 `refresh` 分发 + `ingestTaskProgress(progress)`,气泡定时器与覆盖监听移入 hook。`Layout.tsx` 由 1252 → 1010 行,逻辑更内聚。**行为完全不变**。

#### 验证
- 前端 `npm run lint`(0 problems)、`npm run build`(tsc 严格:全部 hook 返回值/回调类型通过)、`npm run test`(24)通过。

---

### 📌 增量记录 — 2026-07-05（Collections.tsx god-component 拆分 · L98）

#### 重构（行为保持）
- 把 753 行单文件 `pages/Collections.tsx` 拆为 `pages/collections/` 目录(与 `library/`、`series-detail/` 的文件夹约定一致):
  - `index.tsx`(orchestrator,396 行):保留全部 state 与数据加载/增删改/智能编辑/快照 handler,组合各子组件。
  - `types.ts`:5 个契约类型 + 共享 `TFunc`。
  - 展示子组件:`CollectionListPanel`(左栏 tabs+列表)、`CollectionDetailPanel`(右栏详情+系列网格)、`SmartFilterChips`、`CreateCollectionModal`/`EditCollectionModal`(`CollectionFormModals`)、`SmartEditModal`、`SnapshotModal`,均为 props 驱动的纯展示组件。
  - state 仍集中在 orchestrator、handler/接口调用一字未改——**行为完全不变**。`App.tsx` 的懒加载 import 由 `./pages/Collections` 改为 `./pages/collections`。各文件均 ≤ 120 行(orchestrator 除外)。

#### 验证
- 前端 `npm run lint`(0 problems)、`npm run build`(tsc 严格:全部 props/类型接线通过)、`npm run test`(24)通过。

---

### 📌 增量记录 — 2026-07-05（KOReader 建议文案 i18n + 删死码 · M65 续批）

#### 国际化 / 清理
- `koreader_controller.go`:设备诊断 / 冲突 / 未匹配列表的建议文案(`koreaderDeviceSuggestion` / `koreaderConflictSuggestion` / 新抽的 `koreaderUnmatchedSuggestion`)全部改为按 locale 生成中/英(含 `%d` 参数分支用常量格式串,满足 go vet);调用点由 `getKOReaderDeviceDiagnostics` / `listKOReaderUnmatched` 传入 `requestLocale(r)`。默认中文,`en-US` 输出英文。
- 顺带删除死代码 `koreaderIndexLabel`(M65 第三步移除调用后已无任何引用;索引标签实由前端 `formatKOReaderIndexLabel` 按 `match_mode` / `path_ignore_extension` 参数渲染)。
- 至此 KOReader **同步响应**的用户可见文案已全部本地化。M65 仅余写入 `koreader_sync_events` 的持久化诊断 `Message`(账号创建/轮换/启停用、进度回退等),须"写码 + 读时渲染"(涉及读路径改动),列为独立架构切片。

#### 验证
- `go vet`、`go test ./internal/api` 通过(无测试断言这些字面量)。

---

### 📌 增量记录 — 2026-07-05（刮削响应参数化文案 i18n · M65 续批）

#### 国际化
- `scrape_controller.go`:新增 3 个 locale-aware 格式 helper(`scrapeNotFoundMsg` / `scrapeSearchFailedMsg` / `scrapeFailedMsg`,常量格式串按 locale 选中/英,满足 go vet 非常量格式检查),替换 4 处 `fmt.Sprintf` 响应文案(`未在 %s 找到`、`%s 搜索失败`×2、`%s 刮削失败`)。顺带统一此前一处已英文、另一处为中文的"搜索失败"不一致。默认(无 Accept-Language)仍中文,`en-US` 输出英文。
- 至此后端**同步 HTTP 响应**的用户可见中文基本收口(校验 / toast / 错误)。剩余仅:①koreader 设备/冲突/未匹配**建议 helper**(带 `%d`,需同法加 locale 格式串);②写入 `koreader_sync_events` 的**持久化诊断文案**(须写码 + 读时渲染,属架构改动)。二者列为 M65 收尾切片。

#### 验证
- `go vet`、`go test ./internal/api` 通过(无测试断言这些字面量)。

---

### 📌 增量记录 — 2026-07-05（维护/系统配置/推荐 静态响应 i18n · M65 续批）

#### 国际化
- 沿用 `apiText`,迁移 6 处静态 HTTP 响应中文:`controller_maintenance.go` 的搜索索引重建 / 缩略图重制 / 无效封面清理 / 文件身份重建 4 处 toast、`controller_system_config.go` 的"配置已保存"、`controller_recommendations.go` 的"AI 分组审核已提交"。中/英各加 6 条。
- 现状核对:`controller.go` 已无用户可见中文响应串。剩余待后续切片的均为**参数化/持久化**类:①`scrape_controller.go` 的 `未在 %s 找到 / %s 搜索失败 / %s 刮削失败`(带 provider 名与 err,须 locale-aware 格式串)——另"数据完全一致 / 已忽略重复"两条是 M65 第二步 `outcome` 码的**有意兜底**(前端已按码本地化,不动);②koreader 建议 helper(带参)与 `koreader_sync_events` 持久化诊断文案(须写码 / 读时渲染)。

#### 验证
- `go vet`、`go test ./internal/api`(`TestAPITextLocalization` 的中/英表 key 一致性覆盖新增键)通过。

---

### 📌 增量记录 — 2026-07-05（config/日志路径可配置、与 cwd 解耦 · L110）

#### 修复/增强(部署)
- 服务器的配置文件路径与日志目录此前硬编码相对 cwd(`config.LoadConfig("config.yaml")`、`logger.Init("data", …)`、`watchConfig`/`NewController` 均传字面量 `"config.yaml"`),从别的工作目录启动会找不到配置(转而在当前目录生成一份默认配置)、日志写到当前目录 `./data`。现:
  - 新增 `-config` 命令行参数(环境变量 `MANGA_MANAGER_CONFIG`,默认 `config.yaml`)覆盖配置文件路径,统一用于 `LoadConfig` / 热重载监听 / `NewController` 持久化。
  - 新增 `-data-dir` 命令行参数(环境变量 `MANGA_MANAGER_DATA_DIR`,默认 `data`)覆盖日志目录。
  - 二者启动时经 `filepath.Abs` 解析为绝对路径并记入启动日志,使配置/日志位置与进程 cwd 解耦(flag 优先于 env、env 优先于默认)。
- 数据库与缓存目录本就可经 config 的 `database.path` / `cache.dir` 指定绝对路径,故本项聚焦此前完全不可配的两处;`AGENTS.md` 同步说明。

#### 验证
- `go vet`、`go build ./...`、`go test ./cmd/server`(新增 `TestEnvOrDefault`、`TestAbsOrSelf`)通过。

---

### 📌 增量记录 — 2026-07-05（KOReader 静态响应文案 i18n · M65 续批）

#### 国际化
- 沿用 `apiText` 机制,迁移 `koreader_controller.go` 的 9 处静态 HTTP 响应中文文案:账号用户名校验(用户名不能为空)、用户名已存在、账号已删除、进度记录已重置、设置校验(同步路径须以 / 开头 / 匹配模式须为 binary_hash 或 file_path)、以及索引重建 / 匹配应用 / 重关联三处任务启动 toast。均改由 `apiText(requestLocale(r), key)` 按语言选择,`apiMessages` 中/英各加 9 条。
- 暂缓(需另做,列入 M65 后续切片):①KOReader 设备/冲突/未匹配的**建议文案**(`koreaderDeviceSuggestion`/`koreaderConflictSuggestion`/未匹配 suggestion)——含 `%d` 参数、须 locale-aware 格式串;②写入 `koreader_sync_events` 的诊断 `Message`(账号创建/轮换/启停用等)——持久化文案,须"写码 + 读时渲染",属架构改动。

#### 验证
- `go vet`、`go test ./internal/api`(`TestAPITextLocalization` 的中/英表 key 一致性覆盖新增 9 键)通过。

---

### 📌 增量记录 — 2026-07-05（清理 server.error.* 死键）

#### 清理
- 删除 `web/src/i18n/locales` 中 14 条 `server.error.*` 文案(中/英各 14 条):后端从未发出这些码、前端也无任何(含动态 `t(\`server.error.${x}\`)`)消费,是未接线的死键(疑为早期设想"服务端错误码由前端渲染"机制的残留;实际 M65 采用后端 `apiText` 按 locale 选文案,不走前端码)。删除前已全库确认零引用。

#### 验证
- `npm run lint`(0 problems)、`npm run build` 通过;中/英 locale key 集校验一致(1739=1739,0 不匹配)。

---

### 📌 增量记录 — 2026-07-04（主读路径错误态 + 重试 · M48 局部）

#### 修复(用户可见)
- 消除两条主数据读取路径的静默失败(此前 `catch` 只 `console.error`,请求失败后用户只见空白、无从得知也无法重试):
  - 资料库主系列列表(`useLibrarySeries`):新增 `error` 状态(成功清空、失败经 `getApiErrorMessage` 取可读消息)与 `retry`,`library/index.tsx` 在列表上方渲染错误条 + 「重试」按钮(非静默重取当前页)。
  - 侧栏资源库列表(`Layout.tsx` `fetchLibraries`):新增 `librariesError` 标记,加载失败且列表为空时侧栏渲染「加载资源库失败 · 重试」入口(点击重新拉取),替代原先失败后侧栏空白。
- 复用既有 `common.retry`;新增 `library.loadSeriesFailed`、`layout.sidebar.loadFailed` 中/英文案。
- 说明:M48 全量(前端约 58 处 swallow-only catch)多为有意的静默回退(Promise.all 兜底默认值、阅读器预取/预热、best-effort 进度同步等);本批聚焦"主读路径失败即空白"的高影响项,其余按需后续处理。基础设施(ToastProvider / ErrorBoundary / getApiErrorMessage)此前已就位。

#### 验证
- 前端 `npm run lint`(0 problems)、`npm run build`、`npm run test`(24 例)通过。

---

### 📌 增量记录 — 2026-07-04（前后端契约类型局部收敛 · M47 局部）

#### 修复/重构（前端类型）
- 修 `TaskStatus` 缺字段:前端手写的 `TaskStatus`(`components/tasks/TaskCenter.tsx`,被 `BackgroundTasks` 复用)缺 Go `TaskStatus.PausedAt`(`json:"paused_at"`)对应的 `paused_at?`,补齐,消除该字段的静默漂移。
- `Null*` 契约原语单一来源化:`NullString/NullInt64/NullTime/NullFloat64`(镜像 Go `sql.Null*` 的 JSON 形状)此前在 `library/types.ts` 与 `series-detail/types.ts` 各自重复声明、易与后端各自漂移。抽到 `web/src/api/contracts.ts` 单一来源,两处 `types.ts` 改为再导出(re-export),既有 import 路径与消费方零改动。
- 澄清两个同名 `Series` 的分歧:核对确认 `library/types.ts` 的 `Series`(列表/卡片聚合视图,基于 series_stats)与 `series-detail/types.ts` 的 `Series`(`GET /api/series/{id}` 直接返回的 `database.Series` 行)是**不同接口的不同 DTO**,共享字段类型一致、无运行时错位;为二者加区分注释,防止误当同一类型或误导入。
- 说明:引入 Go→TS 类型生成管线(tygo/openapi)以根治手写漂移属 M47 完整版,面较大,留作后续单独立项。

#### 验证
- 前端 `npm run lint`(0 problems)、`npm run build`、`npm run test`(24 例)通过。

---

### 📌 增量记录 — 2026-07-04（HTTP 响应 i18n 补切:任务控制消息 + 鉴权/资料库校验 · M65 残留）

#### 国际化（前后端一起改）
- 任务控制消息迁移为消息码:`pauseTask`/`resumeTask`/`cancelTask` 三处此前直接 `task.Message = 中文`,改用现成的 `applyTaskMessage(&task, "", code, nil)` 发 `task.msg.control.paused`/`resumed`/`cancelling`,与已完成的任务消息 i18n 体系一致(前端 `getTaskMessage` 按码渲染)。`zh-CN`/`en-US` 各补 3 条,全库 `task.msg.*` 码 102→105,Go/zh/en 三方一致(105=105=105)。
- 新增后端 HTTP 响应文案本地化机制 `apiText(locale, key)` + `apiMessages` 中/英表(`internal/api/messages.go`),与 OPDS 的 `opdsText` 同思路:这类文案随响应结构或 `error` 字段直接下发、前端只能原样展示,故必须后端按 `requestLocale(r)`(X-App-Locale / Accept-Language)选择。首批迁移 8 条高频文案:鉴权 401(`需要有效的访问令牌`)与资料库新增/编辑校验的 7 条 `ValidationIssue.Message`(名称/路径/间隔/格式/目录占用);`validateLibraryRequest` 增 `locale` 形参,由 `createLibrary`/`updateLibrary` 用 `requestLocale(r)` 传入。
- 说明:`koreader_controller.go`(~29 条建议/校验/成功文案)、`controller_maintenance.go`/`controller_system_config.go` 的运维成功 toast 等仍为中文,属 M65 后续批次;另发现 `web/src/i18n/locales` 的 `server.error.*` 一批 key **后端从未发出、前端从未消费**(死键),留作后续清理。

#### 验证
- `go vet`、`go test ./...`(新增 `TestAPITextLocalization` 表一致性+回退、`TestValidateLibraryRequestLocalized` 校验消息中/英切换且英文无中文标点;更新 `TestCancelTaskRequestsRunningCancellation` 断言改为消息码)全绿;前端 `npm run lint` + `npm run build` 通过。

---

### 📌 增量记录 — 2026-07-04（config.yaml 取消跟踪 + 提供模板 · M15）

#### 修复(仓库卫生 / 安全)
- `config.yaml` 此前**既被 git 跟踪又在 `.gitignore` 中**——由于已被跟踪,`.gitignore` 对其失效,用户以为被忽略、实则任何本地改动(含填入 `llm.api_key` 等真实密钥)仍会被提交,是虚假的安全感。现 `git rm --cached config.yaml` 取消跟踪(磁盘文件保留、运行不受影响),`.gitignore` 的忽略规则从此真正生效。当前提交历史中的 `config.yaml` 的 `api_key` 为空,无既有密钥泄露。
- 新增跟踪的模板 `config.example.yaml`(含字段结构、默认值与"勿填真实密钥"注释):`cp config.example.yaml config.yaml` 即可起步;首次启动缺 `config.yaml` 时程序仍会自动生成默认配置。
- `AGENTS.md`:更新配置说明,指明 `config.yaml` 为 gitignored、`config.example.yaml` 为模板。

#### 验证
- `git check-ignore config.yaml` 命中(确认现被忽略)、磁盘文件仍在;`git status` 显示 `config.yaml` 为删除跟踪(D)、`config.example.yaml` 为新增。

---

### 📌 增量记录 — 2026-07-04（任务消息 i18n 收尾:start + 全部 progress + 持久化 · M65 第三步 batch5）

#### 国际化（前后端一起改，收尾）
- 迁移所有 START 初始消息(16 处 `start*Task`):新增 `startTaskMsg`/`startPausableCancelableTaskMsg` 与 `startTaskWithOptionsCore`,启动即带 message code。
- 迁移所有 PROGRESS 消息:①服务回调驱动的进度(KOReader 重建/重关联、文件身份重建、后台哈希补算)——控制器侧闭包忽略服务下发的中文 `message`、改由 `current/total` 直接发码,`internal/koreader`、`controller_maintenance` 中已无用的中文 `fmt.Sprintf` 一并清除,**无需改服务回调签名**;②`controller_scan_events.go` 的扫描进度与缩略图重建聚合器进度(含 5 分支消息、fixate、refresh、`refreshRebuildThumbTaskMessage` 改传 code+params)。
- **持久化修复**:`taskParamsWithDerivedFields`/`hydrateTaskStatusDerivedFields` 把 `message_code` 与 `msgparam.*` 一并落进已持久化的 `Params`,使已完成任务从 DB 读回后仍能本地化渲染(否则 Message 对编码任务为空、code 未落库,读回只剩任务类型名)。
- 至此后端任务消息(start/progress/final)**全部**改为 message code。全库 `task.msg.*` 码 **102 个**,Go 引用与 `zh-CN`/`en-US` 三方完全一致(102 = 102 = 102,零缺失、零冗余)。

#### 验证
- `go vet`、`go test ./...`(13 包,新增 `TestTaskMessageCodePersistRoundTrip` 验证 code/params 经 DB 记录往返不丢)、`go test -race ./internal/api ./internal/koreader`、前端 `npm run lint` + `npm run build` 全绿。

---

### 📌 增量记录 — 2026-07-04（任务消息 i18n 基础设施 + 扫描/清理批次 · M65 第三步 batch 1）

#### 国际化（前后端一起改）
- 引入任务消息码机制,让后端不再散落面向用户的中文字面量。`TaskStatus` 新增 `message_code` + `message_params`:迁移后的任务只发稳定 i18n 键 + 占位参数,由前端按当前语言渲染;未迁移的调用点仍用 `message`,前端 `message_code` 优先、`message` 兜底,支持增量迁移。
- 后端:`applyTaskMessage` 保证 `Message` 与 `MessageCode` 互斥(后设者胜);新增 code 版方法 `finishTaskMsg`/`failTaskMsg`/`failTaskErrMsg`/`completeTaskMsg`/`updateTaskMsg`/`updateTaskDetailsMsg`(原方法委托到共享 core,签名不变、增量安全)。
- 首批迁移 `controller_library.go` 的扫描/清理任务(scan_library / scan_series / cleanup_library)9 处 final+progress 消息。
- 前端:`getTaskMessage(task, t)` helper,接入侧边任务气泡(`SidebarTaskBubble`)与任务中心(`TaskCenter`)三处渲染点;`TaskStatus`/`TaskBubbleEntry`/进度覆盖事件类型补 `message_code`/`message_params`;`zh-CN`/`en-US` 各加 9 条 `task.msg.*` 文案。

#### 验证
- 后端 `go vet` + `go test ./internal/api`(新增 `TestTaskMessageCodeEmission`:code 版设 code/params 清 Message、legacy 版设 Message 清 code 的互斥);前端 `npm run lint` + `npm run build` 通过。
- 说明:任务消息共编目 115 处(final 63 / progress 30 / start 22),本批完成 controller_library.go;其余 6 文件(maintenance/recommendations/scan_events/external/koreader/scrape)分后续批次迁移,基础设施与前端渲染已一次到位。

---

### 📌 增量记录 — 2026-07-04（刮削响应结构化 outcome 码 · M65 第二步 · 前后端 lockstep）

#### 重构/健壮性（前后端一起改）
- 消除前端解析后端中文 message 决定提示级别的脆弱耦合。此前 `useSeriesScrape.ts` 靠 `res.data.message.includes('完全一致') || includes('已为您忽略')` 判断刮削 toast 是 success 还是 error——后端一改文案或英文用户就会误判。
- 后端 `scrapeSeriesMetadata`(`/scrape`)与 `applyScrapedMetadata`(`/scrape-apply`)的 7 处响应新增稳定结果码 `outcome`:`queued`(已入审核队列)/`no_changes`(数据完全一致)/`duplicate_ignored`(队列已有相同记录)/`not_found`(未匹配)。原 `message` 仍返回作为老客户端/未映射时兜底。
- 前端改为按 `outcome` 决定 toast 级别并渲染本地化文案(复用已有 `series.toast.metadataReviewQueued/noMetadataReviewChanges/scrapeDuplicate/metadataNotFound` 键),不再解析中文;顺带让这些提示对英文用户也本地化(此前直接显示后端中文)。

#### 验证
- 后端 `go vet`、`go test ./internal/api`(新增 `TestApplyScrapedMetadataOutcomeCodes`:首次入队 outcome=queued、重复提交 outcome=duplicate_ignored,经真实 handler 断言);前端 `npm run lint` + `npm run build` 通过。
- 注:后端面向用户的中文字面量整体 i18n（约 208 处）与 OPDS 按 Accept-Language 选文案表，属 M65 更大剩余工作，本次仅落地刮削响应的结构化码这一具体脆弱耦合修复。

---

### 📌 增量记录 — 2026-07-04（任务重试注册化 + 错误语义/locale 修复 · M17 核心）

#### 重构/可维护性
- 任务重试分发改为注册式:新增 `Controller.taskRelaunchers`(`map[taskType]taskRelauncher`,`buildTaskRelaunchers` 在 `NewController` 中构建),取代 `retryTask` 里的中央 `switch`。`isRetryableTaskType` 改由注册表派生(`_, ok := c.taskRelaunchers[type]`),消除此前与 switch 各自维护、易失步的**两份硬编码任务类型清单**——现只有注册表一处事实来源。

#### 修复(用户可见)
- **重试错误语义**:`retryTask` 现区分 `errTaskAlreadyRunning`(→409)与其它内部错误(→500),未注册类型 →400。此前所有重试失败(含"缺少 scope id"、`GetLibrary` 失败等内部错误)一律误报 409。配套把 maintenance/koreader/scrape 中 9 处 `fmt.Errorf("task already running")` 统一为哨兵 `errTaskAlreadyRunning`(其 `.Error()` 仍为 "task already running",相关 HTTP handler 的 `strings.Contains(..., "already running")` 检查不受影响,不回归)。
- **AI 分组重试语言**:`launchAIGroupingTask` 现把 `locale` 持久化进任务参数;重试时按"持久化 locale → 本次重试请求语言(`requestContextWithLocale(r)` 注入 ctx)→ `zh-CN`"顺序恢复,修复此前无条件硬编码 `zh-CN` 导致非中文用户重试 AI 分组回落中文的问题。

#### 验证
- `go vet`、`go test ./...`(13 包)、`go test -race ./internal/api` 全绿。新增 `TestRetryTaskErrorSemantics`(404/409/**500 内部错误**)、`TestIsRetryableTaskTypeDerivedFromRegistry`;`newTestController` 补 `taskRelaunchers` 初始化。3 路对抗性验证(注册表完整性 / 错误语义+向后兼容 / locale+初始化并发)零 findings。
- 注:任务状态机的整体跨包外迁(独立 `internal/task` 包)为更大重构,审计已注明用户可见缺陷无需依赖它,故本次未做,仅落地注册化与语义修复。

---

### 📌 增量记录 — 2026-07-04（任务进度异步落盘 · M42）

#### 性能/并发
- 任务进度持久化移出 `taskMutex` 临界区。此前每次进度更新(扫描期约 4 次/秒·每个 reporter,多库并行叠加)都在锁内同步 `UpsertTask` 写 SQLite,与扫描批量事务(含 FTS、每系列统计重算、checkpoint)争锁时可阻塞任务 API 与系列详情页最长 `busy_timeout`(15s)。现锁内只更新内存 + 记入 `taskPersistPending`(按 key 合并快照),由唯一的落盘 goroutine `startTaskPersister`(500ms 节流)在锁外批量写。
- 单一写入方 + 按 key 合并,进度写与终态写不会乱序覆盖。终态(完成/失败/取消)经 `persistTaskStatusFinal` 额外唤醒 goroutine 立即刷,缩短落库延迟;`lifecycleDone`(优雅关闭)前再刷一次,保证终态落库。
- 配套修正:①`listTaskStatuses` 对同时存在于内存与 DB 的任务改用**内存版本**(进度更新),避免 API 返回被滞后的 DB 进度覆盖;②`clearTasks` 删任务时同步清 `taskPersistPending`,防止异步落盘把刚删的任务 UpsertTask 复活。

#### 验证
- `go vet`、`go test ./...`、`go test -race ./internal/api` 全绿。新增 `TestTaskProgressAsyncPersistMemoryWins`(更新后立即经 listTaskStatuses 读到内存最新进度、flush 后 DB 亦有);既有跨实例持久化/清理测试改为显式 `flushTaskPersist()` 以适配异步语义。

---

### 📌 增量记录 — 2026-07-04（页图转码 single-flight 去重 · L82 之三，L82 收尾）

#### 性能
- `internal/api/image_controller.go`:页图服务的转码段用 `singleflight.Group`(新增 `Controller.pageTranscodeGroup`)按 `cacheKey` 合并并发。冷缓存时多客户端/预取请求同一页(同参数)只由一个 leader 取存储令牌、读归档、解码+编码、写缓存,其余等待者复用同一结果,避免重复 CPU 转码与重复归档读取。
- 关键处理:①闭包入口二次检查内存缓存(可能刚被填好);②用 `context.WithoutCancel(ctx)` 与发起请求的客户端取消解耦——single-flight 下某个客户端断开不应让所有等待者的转码失败,同时保留 ctx 上的诊断/存储 value;③HTTP 错误编码进结果结构体、闭包恒返回 nil error;④仅处理成功才写缓存,失败回退原始字节不缓存(避免缓存污染),与既有语义一致。

至此 L82 三部分全部完成:同格式透传短路、软件转码并发上限、页图转码 single-flight。

#### 验证
- `go vet`、`go test ./internal/api`(含新增 `TestServePageImageSingleFlightConcurrentSamePage`:40 并发同页请求全部 200、返回字节完全一致、不死锁),全量 `go test -race ./internal/api` 通过(无数据竞争)。

---

### 📌 增量记录 — 2026-07-04（软件转码并发上限 · L82 之二）

#### 性能/稳定性
- `internal/images/processor.go`:为纯软件转码(Go 内解码/缩放/编码)新增并发信号量 `softwareSemaphore`,上限取 `runtime.NumCPU()`,在 `InitProcessor` 中随 AI 信号量一并初始化(热更新同样重建)。此前仅 AI 超分路径(`aiSemaphore`)限流,软件路径无上限——阅读器预取多页或多用户并发时会同时软解码/编码大量图片,导致 CPU 过载抖动、整体变慢。
- 信号量仅门控软件独占的 `resize`+`encode` 段:AI 路径(`execWaifu2x`)成功时在其之前提前返回、AI 回退时已释放 `aiSemaphore` 才进入此段,故与 `aiSemaphore` 无双占;channel 快照进局部变量,acquire/release 用同一引用,避免热更新替换指针导致令牌错配。未初始化(如未调 `InitProcessor` 的测试)时 `Load()` 为 nil、跳过门控,安全。

#### 验证
- `go vet`、`go test ./internal/images ./internal/api`(含新增 `TestInitProcessorInitializesSoftwareSemaphore` 与 48 并发不死锁的 `TestProcessImageConcurrentSoftwareEncodesComplete`,`-race` 通过)。

---

### 📌 增量记录 — 2026-07-04（阅读器纯逻辑补单测）

#### 测试
- 新增 `web/src/pages/book-reader/helpers.test.ts`(13 例):覆盖 `getPagedImages`(双页配对含 LTR/RTL 顺序、单页模式、末页无配对)、`getScaleClasses`(各缩放模式 × 单/双页的 class 组合)、`getFilterStyle`(各插值滤镜→`imageRendering`)。这些是阅读器主流程的纯逻辑,此前无回归保护。前端测试总数 11→24。

#### 验证
- `npm run test`(3 文件 24 例)、`npm run lint`(0 problems)通过。

---

### 📌 增量记录 — 2026-07-04（LibraryCard React.memo 加固 · M49 收尾）

#### 性能
- `LibraryCard` 用 `React.memo` 包裹:库页在扫描/刷新期重渲染时,props 未变的卡片(最多 ~100 张)直接跳过重算。
- `library/index.tsx`:把原先内联传给 `LibraryGrid`→`LibraryCard` 的 3 个刮削菜单回调(`onOpenScrapeMenu`/`onCloseScrapeMenu`/`onChooseScrapeProvider`)改为 `useCallback` 稳定化——先解构出稳定的 `setScrapeMenuOpenId`(useState setter)/`startScrape`(useCallback)作为局部依赖,既满足 `exhaustive-deps` 又不因依赖每渲染新建的 `scraping` 对象而失去 memoization。至此 `LibraryCard` 的全部 props(series/布尔项/外部状态/回调)均为稳定引用或原始值,`React.memo` 真正生效。与前一条 Outlet context 稳定化共同消除扫描期库页的重渲染开销。

#### 验证
- `npm run build`、`npm run lint`(0 problems)、`npm run test` 通过。

---

### 📌 增量记录 — 2026-07-04（Outlet context 稳定化，消除扫描期全树重渲染根因）

#### 性能
- `web/src/components/Layout.tsx`:用 `useMemo` 稳定 `<Outlet context={{ refreshTrigger, libraries }} />` 的 context 对象。此前每次 Layout 渲染都新建该对象,而扫描期间 SSE 任务进度状态会让 Layout 以约 4 次/秒高频重渲染 → 所有 `useOutletContext()` 消费方(库页等,含大量卡片)因每次拿到新引用被迫重渲染。现仅当 `refreshTrigger`/`libraries` 真正变化时才换引用,切断了这条级联重渲染的根因。
- (后续可选加固:给 `LibraryCard` 加 `React.memo` 并 memoize 库页传入的少量内联回调,进一步防御 index 因真实原因重渲染时的卡片重算。)

#### 验证
- `npm run build`、`npm run lint`(0 problems)、`npm run test` 通过。

---

### 📌 增量记录 — 2026-07-04（axios 收口与 lint 约束 · M46 收尾）

#### 重构
- `client.ts` 再导出 `isAxiosError`/`isCancel`;把 7 个消费文件里的 `axios.isAxiosError`/`axios.isCancel` 改为从 client 导入。至此**仅 `client.ts` 与 `apiAuth.ts`(基础设施)直接 import axios**。
- `eslint.config.js` 新增 `@typescript-eslint/no-restricted-imports` 禁止直接 import axios 的**值**(强制走 apiClient),但放行 `type` 导入(如 `AxiosResponse`,无运行时);`client.ts`/`apiAuth.ts` 经 override 豁免。防止后续代码绕过统一实例的鉴权/locale 拦截器。

#### 验证
- `npm run lint`(0 problems;实测:值导入被拦、type 导入放行)、`npm run build`、`npm run test` 通过。

---

### 📌 增量记录 — 2026-07-04（统一 axios 客户端迁移 · M46 完整版）

#### 重构
- `web/src/api/client.ts` 导出统一实例 `apiClient = axios.create()`(不设 baseURL,沿用现有 `/api/...` 绝对路径,行为等价),并在其上挂载与全局 `installApiAuth` 一致的 `X-API-Token` 请求拦截器(独立实例不继承全局拦截器)。
- 把散落在 32 个文件的 **130 处裸 `axios.get/post/put/delete/patch` 调用**全部迁移到 `apiClient`。仅保留 `axios` 导入于确需静态方法(`isAxiosError`/`isCancel`/`create`/`interceptors`)的少数文件。
- **回归修复**:`LocaleProvider` 原在**全局 `axios.defaults`** 上设 `X-App-Locale`/`Accept-Language` 头;迁移后请求走 `apiClient`(不继承 `axios.defaults`)会导致这些 locale 头不再发送,破坏后端 locale 行为(LLM 提示词、OPDS i18n 等)。已改为设在 `apiClient.defaults` 上。

#### 验证
- `npm run build`(tsc 严格 + vite)、`npm run lint`(0 problems)、`npm run test`(11 例)通过;`vite preview` + 浏览器实测:应用完整渲染、apiClient 正常发起请求(无后端时返回预期新手引导页)、无导入/白屏错误。

---

### 📌 增量记录 — 2026-07-04（封面缓存键去抖动）

#### 性能
- `web/src/pages/library/LibraryCard.tsx`:移除封面 URL 上的 `?v=updated_at` cache-buster。封面 `cover_path` 是内容寻址(基于 bookHash),封面内容变化时路径本身即变化,天然是稳定缓存键;而 `updated_at` 会随任意元数据变更/扫描改变,导致未变封面在整库范围被反复失效、重新下载。现直接用 `/api/thumbnails/${cover_path}`。

#### 验证
- `npm run build`、`npm run lint`(0 problems)、`npm run test` 通过。

---

### 📌 增量记录 — 2026-07-04（OPDS feed 后端 i18n · M65 第一步）

#### 修复
- OPDS feed 的用户可见文案改为按 `Accept-Language`(或 `X-App-Locale`)选择中/英。OPDS 是给电子阅读器的 XML,前端无法翻译,故必须后端本地化(审计 M65 明确的两步方案之一)。此前 `opds_controller.go` 内 22 处标题/描述硬编码中文,英文用户看到的目录/系列/搜索/合集/阅读清单/继续阅读等全部为中文。
- 新增 `opdsMessages` 中/英文案表 + `opdsText(locale, key)`(未知 locale/key 回退 zh-CN),覆盖 12 条简单文案;3 条带占位的格式串(`搜索：%s`、`%d 个系列`、进度 `%s · 第 %d / %d 页`)因 `go vet` 非常量格式检查改由 `opdsSearchTitle`/`opdsSeriesCountText`/`opdsContinueProgress` 用常量格式串按 locale 生成。8 个 OPDS handler 均本地 `requestLocale(r)` 取语言,无需改签名。默认(无 header)仍回退中文,既有行为不变。
- 说明:任务/刮削响应等可被前端翻译的文案改为消息码由前端渲染是 M65 的第二步,属另一独立切片。

#### 验证
- `go vet`、`go test ./internal/api`(含新增 `TestOPDSRootFeedLocalization`:默认中文、`Accept-Language: en-US` 输出英文)通过。

---

### 📌 增量记录 — 2026-07-04（PauseGate 并发原语补测试 · L107）

#### 测试
- 新增 `internal/taskcontrol/pause_gate_test.go`(7 个用例,`-race` 通过):此前扫描/重建等长任务依赖的暂停/恢复/等待唤醒原语无任何测试。覆盖:未暂停立即返回、暂停时阻塞并在 Resume 后释放、暂停时响应 context 取消、并发多等待者全部释放、重复 Pause/Resume 与无 Pause 的 Resume 幂等(不 double-close)、nil 接收者安全、`WithPauseGate`/`FromContext` 往返与包级 `Wait` 从 ctx 取 gate。

#### 验证
- `go test -race ./internal/taskcontrol` 全绿。

---

### 📌 增量记录 — 2026-07-04（刮削任务去重 · M43）

#### 重构
- `internal/api/scrape_controller.go`:全库刮削(`launchBatchScrapeAllSeriesTask`)与单库刮削(`launchLibraryScrapeTask`)此前各有一份约 150 行、近乎逐行复制的 `runBackground` 闭包(且日志已发生漂移:单库版缺少 batch 版的 per-series 失败/未找到/入队日志)。现抽取共享执行体 `runScrapeTask`,两入口只负责收集 entries、命名任务与传入取消/完成/日志文案差异;并统一到带完整日志的版本,修复漂移。
- 9 键进度指标 map 此前在每个函数里各重复构造 4 次,改为 `scrapeMetrics` struct + `toMap()`,消除 8 份 map 字面量;本地 `seriesEntry` 类型提升为包级 `scrapeSeriesEntry`。文件 840→721 行。

#### 验证
- `go vet`(含非常量格式检查,完成文案用常量格式串)、`go test ./...` 全绿。

---

### 📌 增量记录 — 2026-07-04（统一 API 错误提取 · M46 第一步）

#### 重构
- 新增 `web/src/api/client.ts`,导出单一 `getApiErrorMessage`,取代此前在 9 个页面/hook 中逐字复制的同名实现(行为完全一致但难维护、易漂移):`AIGroupingReviews`、`MetadataReviews`、`useLibraryCardActions`、以及 series-detail 下 6 个 hook 全部改为从此处导入。
- 该文件预留为后续统一 axios 实例与响应拦截器的落点(M46 完整版:`axios.create({ baseURL: '/api' })` + 迁移 130 处裸 axios 调用 + ESLint no-restricted-imports,作为后续增量)。

#### 验证
- `npm run build`、`npm run test`(11 例)、`npm run lint`(0 problems)通过;9 文件仍各自使用 axios 发请求,无未用导入。

---

### 📌 增量记录 — 2026-07-04（清理 t()||兜底死代码 + t 支持 defaultValue · L96）

#### 修复
- 消除 `t('key') || '硬编码兜底'` 死代码模式(约 29 处、10 文件)。`t()` 缺 key 时返回 key 本身(真值),故 `|| 兜底` 永不触发——已核对涉及的全部 23 个静态 key 在 zh-CN 与 en-US 目录中均存在,确认为死代码后直接删除兜底。部分兜底文案已与目录漂移(如 '整理维护' vs 实际词条),正是从未执行的实证。
- `LocaleProvider` 的 `t` 新增可选第三参 `defaultValue`:缺 key 时优先返回它、再退回 key 本身,避免把原始 key 暴露给用户。用于动态 key 场景(如 `t(\`series.relations.type.${x}\`, undefined, x)`),使这些原本"死兜底"变为真正生效的回退。同步更新 `I18nContextValue` 与 `i18n/task.ts` 的 `Translator` 类型。

#### 备注(超出本项范围,留作后续)
- `Dashboard.tsx` 续作副标题处 `t('series.franchise.description')` 遮蔽了原意为 `From <源系列>` 的兜底(该 key 恒存在),当前按既有行为保留显示词条值;若要显示"来自 X"需新增带参词条,属产品文案决策,不在死代码清理范围内。

#### 验证
- `npm run build`、`npm run test`(11 例)、`npm run lint`(0 problems)通过。

---

### 📌 增量记录 — 2026-07-04（前端引入 Vitest 单元测试 · M16）

#### 新增
- 前端从零引入 Vitest 单元测试框架(此前 `web/` 无任何测试)。为保持最小依赖与稳定性,首批只装 `vitest`、用 node 测试环境覆盖纯逻辑函数,组件级测试(jsdom/testing-library)留待后续按需引入。
- 配置:新增 `web/vitest.config.ts`(node 环境,`src/**/*.{test,spec}.{ts,tsx}`);`package.json` 加 `test`/`test:watch` 脚本;`tsconfig.app.json` 排除测试文件,使生产构建与测试解耦。
- 首批测试(11 例):`i18n/status.ts` 的 `normalizeSeriesStatus`(英文别名/大小写/中文别名/未知兜底)、`series-detail/hooks/useSeriesContinue.ts` 的 `buildContinueCta`/`isFullyRead`(续读 CTA 解析、跨书页码归零、标题回退、全读判定)。
- CI:`.github/workflows/ci.yml` 新增 `npm run test` 阻塞步骤。

#### 验证
- `npm run test`(2 文件 11 例全过)、`npm run build`、`npm run lint`(0 problems)通过。

---

### 📌 增量记录 — 2026-07-04（KOReader 进度 last-write-wins · M37）

#### 修复
- KOReader 进度同步从「百分比只进不退」改为 kosync 的 last-write-wins。此前 `SaveProgress` 会直接拒绝百分比低于已存值的推送(service.go),用户无法回退/重读;kosync 载荷不含客户端时间戳,故以服务端接收时间为准、每次新推送无条件覆盖(含回退)。
- 被旧规则会拒绝的「倒退推送」现照常应用,但额外记一条 `progress_regressed` 诊断事件(带原/新百分比与设备),便于区分有意回退与异常倒退。
- 新增管理端点 `DELETE /api/system/koreader/progress/{progressId}`(`resetKOReaderProgress` + 手写查询 `DeleteKOReaderProgress`),供管理员重置单条进度记录(记 `progress_reset` 事件)。
- 修正误报:`progress_regressed` 是 LWW 的预期行为而非失败,已将其从「账号最近错误」与「最近同步失败横幅」两处错误显示中排除(设备冲突诊断列表仍保留,作为跨设备进度倒退信号)。`system` 方向的 reset/account_created 事件本就被既有 `direction != 'system'` 过滤排除。

#### 验证
- `go vet`、`go test ./...`(含新增 `TestSaveProgressLastWriteWinsAllowsRollback`:低百分比推送胜出 + 生成 1 条 `progress_regressed` 事件)全绿。

---

### 📌 增量记录 — 2026-07-04（KOReader 设备自助注册 · M36）

#### 修复
- KOReader kosync `/users/create` 不再是死开关。此前无条件 403,`config.koreader.allow_registration` 与其 UI 开关完全无效。现按 kosync 协议实现设备自助注册:`Enabled=false→503`;`AllowRegistration=false→403`;非法体→400;新用户→`201 {"username"}`;用户名已存在→`402 {"code":402,"message":"Username is already registered."}`。
- 存储/认证协调(免 schema 迁移):kosync 注册时客户端只发送 `md5(userKey)`(与后续 `x-auth-key` 同值),服务端拿不到原始密钥,故直接把该 md5 存为 `sync_key`;`Authenticate` 增加一条等值比对分支(`NormalizeSyncKey(sync_key)==x-auth-key`),与既有"管理端原始密钥 + `HashKey` 比对"路径并存且互不影响(原始密钥永不等于自身 md5)。移除随之失效的 `ErrRegistrationClosed`。

#### 验证
- `go vet`、`go test ./internal/koreader ./internal/api`(含新增 `TestKOReaderSelfRegistrationCreatesAuthenticatableAccount`:注册→用同一 md5 认证通过→重复注册 402→关闭后 403)通过。

---

### 📌 增量记录 — 2026-07-04（OPDS 整卷下载 · M38）

#### 新增
- 新增 `GET /api/books/{bookId}/file`（`serveBookFile`）:按归档扩展名以正确 MIME（cbz/zip→`application/vnd.comicbook+zip`、cbr/rar→`application/vnd.comicbook-rar`、cb7/7z→`application/x-cb7`、pdf→`application/pdf`）下发整卷原始归档,带 RFC 5987 附件文件名,经 `http.ServeContent` 支持 Range 断点续传。路由挂在 `/api/books` 组下,与既有页图链接同享 `requireAuth` 鉴权。
- `opdsBookAcquisitionLinks` 现在把整卷下载链接置于首位作为主获取项,保留首页 JPEG 作封面/预览补充与 PSE 流。此前非 PSE 的桌面/传统 OPDS 客户端只能拿到第一页 JPEG,无法整卷下载。

#### 验证
- `go vet`、`go test ./internal/api`(含新增 `TestServeBookFileWholeArchiveDownload` 与更新的 OPDS feed 断言)通过。

---

### 📌 增量记录 — 2026-07-04（vite 分包 · 首屏瘦身 · M50）

#### 性能
- `web/vite.config.ts`：把仅被特定懒加载路由使用的重型库从首屏 `vendor` 拆成独立 chunk，随对应路由按需加载：
  - `@xyflow/react` → `reactflow`（仅系列关系图谱页，122KB / gzip 40KB + 16KB CSS）
  - `@yui540/comimi*` → `comimi`（仅阅读器 Comimi 主题，137KB / gzip 32KB）
  - `react-virtuoso` → `virtuoso`（仅阅读器 Webtoon 虚拟滚动，55KB / gzip 19KB）
- 首屏 `vendor` 从 **491KB（gzip 154KB）降至 177KB（gzip 63KB）**，首次加载少下载约 91KB（gzip）。三库改为访问对应页面时才拉取。
- 三库均调用 `forwardRef/createContext`，被隔离到各自 chunk 并单向依赖 `react-core`（不放进 react-core），避免历史上的白屏问题。已用 `vite preview` + 浏览器实测首页、图谱页、阅读器路由：均正常渲染、控制台无 `forwardRef`/undefined 错误、无循环依赖告警。

#### 验证
- `npm run build`（无循环依赖告警）、`npm run lint`（0 problems）通过；浏览器运行时验证通过。

---

### 📌 增量记录 — 2026-07-04（前端 lint 清零 + 纳入 CI）

#### 修复
- `web/src/pages/franchise-graph/index.tsx`：消除 6 处 `@typescript-eslint/no-explicit-any` 错误。用 React Flow 的真实类型（`InternalNode`、`ReactFlowState`、`Node`、`React.MouseEvent`）替换 `any`，并为图谱节点 data 定义 `FranchiseNodeData` 类型；在无类型的 React Flow store 边界处收窄一次 `as`。顺带移除失效的 v11 遗留 `positionAbsolute` 兜底分支。
- `web/src/pages/library/hooks/useLibraryFilters.ts`：移除返回 `useMemo` 的多余依赖 `libId`/`settingsReadyLibId`（其效果已被派生的 `currentSettingsReady` 覆盖），消除 `react-hooks/exhaustive-deps` 告警。
- `.github/workflows/ci.yml`：前端 lint 现已清零（0 错误 0 告警），将 `npm run lint` 纳入 CI 阻塞步骤，防止 lint 债回潮。

#### 验证
- `npm run build`、`npm run lint`（0 problems）通过。

---

### 📌 增量记录 — 2026-07-04（移除只写死代码遥测 · M52）

#### 重构
- 删除 `web/src/utils/frontendPerformance.ts`（253 行）及其全部调用点（`main.tsx`、`useLibrarySeries.ts`）。该模块在**每个 API 请求**上装了全局 axios 拦截器、patch `history.pushState/replaceState`、把首屏与列表渲染指标写入 `localStorage` 并派发自定义事件——但全代码库**无任何消费方**（无监听、无读取、无调用 `getFrontendPerformanceSnapshot`）。属纯只写死代码，白白增加每次请求开销与包体。
- 连带移除因此变为冗余的 `serializedFilters`（原仅用于该遥测的埋点，刷新实由个别筛选值驱动）：跨 `useLibraryFilters.ts` / `useLibrarySeries.ts` / `library/index.tsx` 三处清理，行为不变。

#### 验证
- `npm run build`（tsc + vite）通过；`npm run lint` 回到既有基线，无本次引入的新告警。

---

### 📌 增量记录 — 2026-07-04（controller.go 上帝文件拆分 · 阶段一）

#### 重构（H7，行为保持）
- 开始拆分 5489 行的 `internal/api/controller.go` 上帝文件。按领域把内聚代码块移入同包新文件（纯文件重组，无逻辑变更，编译器与全量测试验证）：
  - `controller_search.go`：全库/系列/图书 FTS 搜索、结果合并、评分归一化、封面回填。
  - `controller_scan_events.go`：扫描器批次/指标/进度回调处理与缩略图重建聚合进度。
  - `controller_tasks.go`：任务引擎——状态模型、进度/指标聚合、持久化、生命周期（启动/更新/暂停/恢复/取消/完成）与任务列表接口。
- 阶段二继续拆出：
  - `controller_series.go`：系列分页搜索、系列信息与上下文、标签与作者查询/搜索接口。
  - `controller_system_config.go`：系统配置读写（含敏感字段脱敏）、能力查询、LLM 连通性测试、目录浏览。
- 阶段三继续拆出：
  - `controller_library.go`：资料库增删改查、校验、扫描/系列扫描/清理任务触发接口。
- 阶段四继续拆出：
  - `controller_maintenance.go`：全库扫描、索引/缩略图重建与清理、文件指纹重建、低优先级全量哈希回填等运维任务编排与接口。
  - `controller_progress.go`：上一本/下一本导航、单本与批量进度更新、KOReader 风格批量同步、阅读书签增删查。
- `controller.go` 从 5489 行降至 1684 行（约 −69%）。
- 阶段五继续拆出：
  - `controller_recommendations.go`：首页推荐计算/缓存、AI 分组任务编排、系列首字母重建接口。
  - `controller_stats.go`：仪表盘结构/易变统计缓存（失效/预热/分层加载）与看板、活跃热力图、最近阅读只读接口。
- `controller.go` 最终从 5489 行降至 1186 行（约 −78%）。已拆出 10 个领域文件（search/scan_events/tasks/series/system_config/library/maintenance/progress/recommendations/stats），`controller.go` 现仅保留 Controller 结构与构造、生命周期、鉴权中间件、SSE broker、SetupRoutes、SSE/分页等少量共享 handler。

#### 验证
- `go vet ./...`、`go test ./...`（全绿）；`goimports` 自动整理各文件 import。

---

### 📌 增量记录 — 2026-07-04（CI 加固 + 文档修正）

#### 修复
- `.github/workflows/ci.yml`：CI 新增 `go vet ./...`（M54）；ubuntu 上新增 sqlc 生成产物 drift 校验（`sqlc generate` 后 `git diff --exit-code -- internal/database`，M55），防止 SQL 源与已提交生成代码失同步；新增阻塞式竞态检测 `go test -race ./...`。
- `internal/scanner/scanner_test.go`：修复竞态检测暴露的测试插桩数据竞争——测试注入的 `s.openArchive` 闭包从 cover-worker goroutine 非同步自增 `openCount` 计数器、与测试体读竞争（生产扫描代码本身无此竞态）。计数器改用 `atomic.Int64`。
- 文档修正（M14/L111）：`AGENTS.md` 移除已删除的 `search/` 包引用、把硬编码 mac 路径改为 `$(pwd)` 相对路径；`README.md` 与代码注释中的 `bleve` 全文索引更正为 SQLite FTS5（trigram）。

#### 验证
- `go vet`、`go test ./...`、`go test -race ./...`（全部通过、无数据竞争）；`sqlc generate` 无 drift。

---

### 📌 增量记录 — 2026-07-04（文件监听处理删除/重命名）

#### 修复
- `internal/scanner/watcher.go`：文件监听器现在处理 Remove/Rename 事件。此前只响应 Create/Write，删除/重命名既不清理 `watched` 集合（Linux 下内核 watch 已回收但 map key 永久残留=无界泄漏；重建同名目录因残留 key 跳过重挂=永久失监），也不清除库中的幽灵记录（删除的文件/系列在库视图与搜索中残留，因热重载只走增量 ScanLibrary 不删缺失）。现新增 `handleRemoval`：按前缀清理 `watched` 并移除对应 fsnotify watch（修复泄漏与重建失监），并为所属库排期一次去抖后的 `CleanupLibrary`（自带根目录探测与占比熔断，存储离线不误删）清除幽灵记录。Rename 天然产出 Remove(旧)+Create(新)，二者互补。

#### 验证
- `go vet`、`go test ./internal/scanner`、`go build ./...` 通过。

---

### 📌 增量记录 — 2026-07-04（同格式图片透传短路）

#### 修复
- `internal/images/processor.go`：`ProcessImage` 在无缩放/滤镜/质量/裁切需求、且目标格式与源格式一致时直接透传原始字节（L82 的一部分）。此前只有 `format` 完全为空才短路，前端传 `format=webp` 而源本就是 webp 时仍会白白解码 + 重编码一次（且可能损质）。页图转码的 single-flight 去重与软件编码并发上限作为后续项。

#### 验证
- `go build ./...`、`go test ./internal/images ./internal/api` 通过。

---

### 📌 增量记录 — 2026-07-04（fast_scan 保留页数/封面）

#### 修复
- `internal/scanner/scanner.go`（配合 `sql/query.sql` 的 `ListBooksByLibrary` 增列）：fast_scan 档位不再清零已入库书籍的 `page_count`、置空 `cover_path`。此前 fast 档位不开归档，upsert 会把变动书籍的页数写 0、封面写 NULL 且不再重建（封面被永久抹掉直到跑一次 metadata_scan）。现增量扫描把旧快照的 `page_count`/`cover_path` 传入 worker，fast 档位下缺失时保留旧值。

#### 验证
- `sqlc generate`（PowerShell）exit 0；`go vet`、`go test ./...` 全绿。

---

### 📌 增量记录 — 2026-07-04（磁盘页缓存容量上限）

#### 修复
- `internal/config/config.go` / `internal/api/image_controller.go` / `controller.go`：磁盘页缓存新增容量上限与自动淘汰。新增配置项 `cache.page_disk_cache_max_bytes`（默认 2 GiB，0 归一化为默认，负数表示不限），并新增后台清道夫 goroutine（`startPageCacheJanitor`，每 5 分钟 + 启动兜底），超限时按最旧优先（mtime FIFO）淘汰到 90% 低水位。此前磁盘页缓存只增不减、只能手动清空，开启后会随阅读量无上限膨胀。

#### 验证
- `go vet`、`go test ./internal/config ./internal/api`、`go build ./...` 通过。

---

### 📌 增量记录 — 2026-07-04（卷话号解析抗噪）

#### 修复
- `internal/booksort/booksort.go`：卷话号提取的兜底逻辑加抗噪。此前无中文/西文前缀时直接抓文件名里第一个数字，`[2020]` 年份标签、`(C99)` 会场号、组名里的数字会被误当卷号导致排序错乱。现优先匹配带前缀的卷话号（`第X话/卷`、`Vol/Chapter/Ch/Ep/#`），兜底时先剔除括号段再跳过四位年份 token，取第一个真正的卷号；纯年份文件名仍可排序。既有中文卷话解析（第一话/第二十话/壹佰贰拾叁話等）不回归。

#### 验证
- `go test ./internal/booksort`（含新增 `TestExtractSortNumberIgnoresYearAndBracketNoise` 与既有中文卷话用例）通过。

---

### 📌 增量记录 — 2026-07-04（归档/图片/抓取安全批次）

#### 修复
- `internal/parser/zip.go` / `rar.go`：归档单页读取新增解压字节硬上限（256 MiB，`readEntryLimited`）。此前按归档头声明的解压大小预分配缓冲 + 无上限 `io.Copy`，恶意声明的超大项会在拷贝前就 OOM、高压缩比的解压炸弹会撑爆内存；现声明超限直接拒绝、实际拷贝用 `io.LimitReader` 夹紧。与图片解码像素上限（`maxDecodePixels`）在字节层互补。
- `internal/api/image_controller.go`：AVIF 变体缓存命中时 Content-Type 不再退化为 `application/octet-stream`。标准库 `http.DetectContentType` 无 AVIF 签名，改为磁盘命中按扩展名精确复原 MIME、内存命中用 AVIF 感知的 `detectImageContentType`，与首次响应一致。
- `internal/metadata/bangumi.go`：Bangumi 抓取新增有限次指数退避重试（仅 429/5xx，尊重 `Retry-After`，退避可被 context 取消打断）。此前遇限流即整段系列逐个记为失败、无自愈。

#### 验证
- `go build ./...`、`go vet`、`go test ./internal/parser ./internal/api ./internal/metadata`（含新增 `TestReadEntryLimited`）通过。

---

### 📌 增量记录 — 2026-07-04（AI 推荐并发去重）

#### 修复
- `internal/api/controller.go`：`getRecommendations` 引入 single-flight（`golang.org/x/sync/singleflight`，`go mod tidy` 提为直接依赖）。此前冷缓存/刷新时并发请求会各自同步触发一次 LLM 推理（成本×N、易触发上游限流）；现同一 locale 的并发请求合并为一次推理，其余请求搭车复用结果。抽出 `computeRecommendations`，并用 `context.WithoutCancel` 解绑 leader 的请求取消，避免其客户端断开波及所有搭车者。

#### 验证
- `go build ./...`、`go test ./internal/api`（含 `TestGetRecommendationsReturnsCachedEntries`）通过；`go.sum` 无改动。

---

### 📌 增量记录 — 2026-07-04（元数据审阅 apply 事务原子化）

#### 修复
- `internal/api/scrape_controller.go` / `metadata_review_controller.go`：元数据审阅 apply 的「写入元数据」与「标记 review 已应用」并入同一事务。此前二者是两段独立自动提交，元数据写成功但状态更新失败时 review 会停留 pending、可被重复 apply。现新增 `applyMetadataToSeriesWithHook`，把 `UpdateMetadataReviewStatus` 作为提交前钩子在同事务内执行；单条与批量 apply 均移除原先事务外的独立状态更新（失败则整体回滚、状态保持 pending，重试安全）。

#### 验证
- `go vet`、`go test ./internal/api` 通过。

---

### 📌 增量记录 — 2026-07-04（franchise 合集重建修复批次）

#### 修复
- `internal/api/franchise_service.go`：`RebuildFranchiseCollections` 重写。删旧合集 + 逐分量建新合集整体包进 `ExecTx`，先删后建原子化（此前非事务、中途失败或并发交错会留下“已删光/半重建”的不一致状态，且吞掉 create/add 错误）；用一次 `GetSeriesNamesByIDs` 批量取代表系列名，消除逐分量 `GetSeries` 的 N+1。
- `internal/api/franchise_service.go` / `controller.go` / `collection_controller.go`：系列关联增删改触发的 franchise 重建改为合并式调度 `scheduleFranchiseRebuild`——已有重建在跑时只置 pending，把一串关联编辑合并成至多再跑一轮，并经 `runBackground` 登记到 `backgroundWG`（此前是脱离生命周期、用 `context.Background()` 的 fire-and-forget goroutine，批量编辑时会瞬间并发多个全图重建争抢 SQLite 写锁）。

#### 验证
- `go vet`、`go test ./internal/api` 通过。

---

### 📌 增量记录 — 2026-07-04（批量标记已读事务收敛批次）

#### 修复
- `internal/api/controller.go`：整系列/批量标记已读（`bulkUpdateSeriesProgress`、`bulkUpdateBookProgress`）改为按系列分组、每系列一个事务写入并只刷新一次 `series_stats`。此前每本书经 `SqlStore.UpdateBookProgress` 包装器隐式触发一次全系列统计重算 + 逐条自动提交，含 N 本书的系列约 3N 次提交、N 次 O(全书) 聚合，整体 O(N²)；现收敛为「一个事务 + 一次刷新」（用事务绑定的原始 `q.UpdateBookProgress` 绕开逐书刷新）。语义变化：整系列写入现为原子（任一本失败则整系列回滚、不计入 updated）。

#### 验证
- `go vet`、`go test ./internal/api`（含既有 `TestBulkUpdateSeriesProgressMarksAllBooksReadAndUnread` 等进度用例）通过。

---

### 📌 增量记录 — 2026-07-04（后端读路径性能批次）

#### 修复
- `internal/api/metadata_review_controller.go`：元数据审阅收件箱改为一次性批量取字段（新增 `ListMetadataReviewFieldsByReviews`），消除此前每条 review 单独查字段的 N+1（查询数由 2+N 降到 3）。
- `internal/api/image_controller.go`：封面服务改用只取 `cover_path` 一列的窄查询（新增 `GetBookCoverPath`），不再每次封面请求（含 304 命中）都 `SELECT *` 整行 books、Scan 20+ 无用列；ETag/304 语义不变。
- `internal/api/request_metrics.go`：请求诊断缓冲由“切片 + 满时 copy 左移”改为真正的环形缓冲（写指针取模），`record` 从 O(n) 锁内搬移降为 O(1)；`snapshot` 仍保持 oldest-first 顺序。

#### 验证
- `sqlc generate`（PowerShell）exit 0；`go vet`、`go test ./internal/api ./internal/database`（含新增环形缓冲回归测试）通过。

---

### 📌 增量记录 — 2026-07-04（图片/存储资源上限批次）

#### 修复
- `internal/api/image_controller.go`：内存图片缓存新增单张大小上限（4 MiB）。此前 LRU 只按条数（256）限制，AI 放大（waifu2x/realcugan）后的整页可达数 MB，缓存可膨胀到 GB 级；超过上限的图不再进内存缓存，按需重算。
- `internal/parser/pool.go`：归档句柄池按文件 mtime/size 校验失效。此前仅按路径缓存、无失效机制，文件更新后最长 10 分钟仍会读到旧内容；现取用时若签名变化即关闭陈旧句柄并重建。

#### 验证
- `go build ./...`、`go test ./internal/parser ./internal/api` 通过。

---

### 📌 增量记录 — 2026-07-04（sqlc 管线修复 + 协议分页批次）

#### 修复：sqlc 生成管线
- 修复根因：`866c0f9` 给 `sql/query.sql` 与 `internal/database/schema.sql` 添加的**中文注释头**会让 sqlc 的 SQLite 解析器发生字节偏移错位（查询名被逐条累积截断、生成失败）。将这两个文件的注释头改为等义英文（SQL 语句一字未改），恢复 `sqlc generate` 正常工作。
- 新增 `.gitattributes`：强制 `*.sql` 使用 LF 换行，避免 `core.autocrlf=true` 的 Windows 检出把 SQL 转成 CRLF 再触发 sqlc 解析问题。
- `AGENTS.md`：记录“sqlc 必须在 PowerShell/cmd 下运行”（Git Bash 经 scoop shim 会返回假的退出码 1）。
- `sqlc.yaml`：关闭 `emit_prepared_queries`。该选项生成了近千个从未被调用的预编译语句字段与 `Prepare` 机制（项目统一用 `New(db)`），移除后 `internal/database/db.go` 精简约 1600 行死代码。

#### 修复：协议分页
- `internal/api/opds_controller.go`：OPDS 资源库系列 feed 改为数据库层分页（新增 `CountSeriesByLibrary` + `ListOPDSLibrarySeriesPaged`，走 series_stats 取封面），不再每次翻页都全量加载整库系列再内存切片。
- `internal/api/mihon_controller.go`：Mihon 系列搜索叠加 `libraryId` 过滤时不再走跨库 FTS 快路径（该路径按库做内存过滤会丢结果、把 total 算成当前页条数、分页失效），改为回落到原生支持库内关键字分页的 `SearchSeriesPaged`。

#### 验证
- `sqlc generate`（PowerShell）退出码 0、无致命错误；`go vet`、`go test ./internal/api ./internal/database`、`go build ./...` 通过。

---

### 📌 增量记录 — 2026-07-04（前端修复批次）

#### 修复
- `web/src/pages/Dashboard.tsx`：仪表盘首屏并发请求为每个接口各自兜底，避免 dashboard 统计接口单点失败 reject 整个 `Promise.all`、导致 libraries 不被设置而误显示“没有资源库”的新手引导页。
- `web/package.json`：移除零引用的僵尸依赖 `dagre`、`@types/dagre`、`react-select`（系列关系图谱早已从 Dagre 布局改为力导布局）；同步修正 `vite.config.ts` 中仍引用 react-select 的误导性注释。

#### 验证
- `cd web && npm run build` 通过。

---

### 📌 增量记录 — 2026-07-04（扫描与元数据修复批次）

#### 修复
- `internal/scanner/scanner.go`：扫描批量写库事务失败时，此前静默丢弃整批（最多 100 本）且任务仍报成功。现将丢弃数计入 `failed_archives` 指标，使其在扫描完成日志与诊断中可见。
- `internal/external/manager.go`：外部资料库扫描（`ScanSession`）的目录遍历与匹配循环新增 context 取消检查，取消任务时能及时中止外部盘扫描，而非无视取消跑到底。
- 删除 `internal/metadata/comicinfo.go`：其中的 `ExtractAndApply` 为从未被调用的空实现存根，`ComicInfo` 结构与 `parser` 包重复且无任何引用，属死代码。

#### 验证
- `go vet`、`go test ./internal/scanner ./internal/external ./internal/metadata` 通过。

---

### 📌 增量记录 — 2026-07-04（图片处理安全批次）

#### 修复
- `internal/images/processor.go`：大图内存保护此前只记录告警不生效。现按预检尺寸设硬上限（约 1 亿像素）拦截解码炸弹（小体积压缩文件声明极大画布、完全解码即耗尽内存），直接返回错误而非 OOM；用 int64 计算面积避免超大尺寸相乘溢出。
- `internal/api/image_controller.go`：图片处理失败回退到原始数据时，不再把该回退结果写入“已处理”缓存键。此前会将未处理的原图当作处理产物持久缓存，导致临时错误恢复后用户仍永远拿到未处理的图。

#### 验证
- `go build ./...`、`go test ./internal/images ./internal/api` 通过。

---

### 📌 增量记录 — 2026-07-04（运维与生命周期修复批次）

#### 修复
- `cmd/server/main.go`：新增优雅停机。用 `http.Server` 替代裸 `ListenAndServe`（并设置 `ReadHeaderTimeout`/`IdleTimeout`），捕获 SIGINT/SIGTERM 后先排空在途请求再调用 `apiController.Close()` 收尾后台任务（此前 `Close` 机制存在却从未接线，信号直接杀进程）。
- `cmd/server/main.go`：`/api/health` 由静态字符串改为探测数据库连接，DB 不可达返回 503，供反向代理/编排器判断实例健康（新增 `Store.PingContext`）。
- `internal/config/config.go` / `internal/api/controller.go`：配置保存改为原子写（临时文件 + rename），避免写入过程中崩溃留下半截 `config.yaml` 导致下次启动解析失败。
- `cmd/server/main.go`：配置热重载监听改为监听所在目录并按文件名过滤，修复 Linux 上原子替换/编辑器保存后 inode 级 watch 永久失效的问题。
- `internal/api/log_controller.go`：日志查询接口的解析正则提升为包级预编译（不再每行重复编译），并对 `limit` 施加上限（2000）防止超大取值按需预分配打爆内存。
- `internal/logger/logger.go` / `internal/api/log_controller.go`：日志查看接口改用 logger 实际写入的文件路径（`logger.LogFilePath()`），消除写入目录与查看路径依据不同来源推导而分叉的问题。

#### 验证
- `go vet`、`go test ./internal/api ./internal/database ./internal/config ./internal/logger ./cmd/server` 通过。

---

### 📌 增量记录 — 2026-07-04（协议层修复批次）

#### 修复
- `internal/api/opds_controller.go`：OPDS 系列书籍 feed 改用 `booksort.CompareBooks` 规范化排序，与全站阅读顺序口径一致，避免 sort_number 缺失时“第 10 话排到第 2 话之前”。
- `internal/api/opds_controller.go`：OPDS 缩略图链接的 MIME 由硬编码 `image/jpeg` 改为按封面文件扩展名推导（缩略图默认 webp，可配置 avif/jpg），与实际返回字节一致；页面图（`/api/pages`）链接保持不变。
- `internal/database/koreader_queries.go`：`koreader_sync_events` 事件写入新增保留上限（最近 10000 条），防止推/拉/认证失败事件（未认证请求也会触发）无限增长撑大数据库。

#### 验证
- `go build ./...`、`go test ./internal/api ./internal/database ./internal/koreader`（OPDS/KOReader/迁移用例）通过。

---

### 📌 增量记录 — 2026-07-04（数据库层修复批次）

#### 修复
- `internal/database/store.go`：升级迁移时一次性回填 `tags.series_count`（此前该回填函数从未被调用，触发器只维护增量，老库的标签 facet 计数/排序会一直不准）。schema 版本号提升到 2，确保已升级到 v1 的库也会补算。
- `internal/database/store.go` / `schema.sql`：为“最近阅读系列”查询新增部分索引 `idx_books_library_last_read ON books(library_id, last_read_at) WHERE last_read_at IS NOT NULL`，避免首页最近阅读随库规模全表扫描。
- `internal/database/store.go`：全局搜索、图书搜索、协议系列搜索三处 FTS 查询失败时不再静默吞掉错误、静默降级为 `instr` 全表扫描，现记录 `slog.Warn` 使故障可观测。

#### 验证
- `go test ./internal/database/`（含新增标签计数回填测试 `TestMigrateBackfillsTagSeriesCount`）通过。

---

### 📌 增量记录 — 2026-07-04（P1 性能与列表修复）

#### 后端：迁移与查询性能
- `internal/database/store.go`：引入 `PRAGMA user_version` 版本化迁移。此前每次启动都无条件全量重建 FTS 索引并回填 `series_stats`（成本随库规模线性膨胀）；现改为仅在库版本低于 `currentSchemaVersion` 时执行一次，之后每次启动跳过，运行期由触发器与 `RefreshSeriesStats` 增量维护。`migrateFTSTables` 改返回 `rebuilt` 标志，DROP 重建空表时仍强制回填一次（兼容旧版 FTS 结构升级路径）。
- `internal/database/smart_collection.go`：智能书架/合集视图查询改用预计算的 `series_stats` 缓存 + `series` 冗余统计列，取代此前对整个 `books` 表做的三重全表聚合（封面 ROW_NUMBER、已读页 SUM、完成度聚合）。查询不再触及 books 表，`WHERE s.library_id = ?` 能真正把范围收敛到本库；进度百分比口径统一为 `read_pages / total_pages`，与全站 series 列表一致。

#### 前端：资料库无限滚动
- `web/src/pages/library/hooks/useLibrarySeries.ts` / `web/src/pages/library/index.tsx`：无限滚动模式修复。此前翻页时整页替换列表，导致列表永远只显示一页、哨兵反复触发翻页；现改为按 id 合并（更新已存在项 + 追加新增项），滚动加载正确累积。分页模式保持整页替换语义；筛选/排序/分页模式切换回到第 1 页，自然清空累积列表。

#### 验证
- `go vet ./...` 通过；`go test ./...` 全部通过（新增迁移版本门控测试 `TestMigrateSetsSchemaVersionAndSkipsRebackfill`）；`cd web && npm run build` 通过；改动的前端文件 `eslint` 无告警。

---

### 📌 增量记录 — 2026-07-03（P0 安全与稳定性修复）

#### 安全：管理 API 加固
- `internal/api/controller.go` / `internal/config/config.go`：`GET/POST /api/system/config` 回显时对 LLM `api_key` 与新增的 `server.auth.token` 脱敏（占位符 `__mm_secret_unchanged__`），保存或测试时若字段仍为占位符则用当前值回填，既不泄露明文也不会误清空密钥。
- `internal/config/config.go` / `internal/api/controller.go` / `cmd/server/main.go`：新增**可选管理 API 令牌鉴权** `server.auth`（默认关闭，行为完全向后兼容）。启用后管理端点要求携带匹配令牌（`X-API-Token` 头 / `Authorization: Bearer` / `?token=` 查询参数），阅读协议 OPDS/Mihon/KOReader 不受影响、保持各自鉴权模型。
- `internal/api/controller.go`：`POST /api/system/test-llm` 增加 SSRF 出站目标 scheme 校验，仅允许 `http/https`，拒绝 `file://`、`gopher://` 等；默认仍支持本机 Ollama。
- `cmd/server/main.go`：CORS 检测到通配 Origin 时强制关闭 `AllowCredentials`（规避“通配 + 凭据”危险组合），并在启动时对无鉴权裸奔 / 鉴权配置不完整发出告警。
- `internal/images/processor.go`：自定义放大引擎路径加固，仅接受绝对路径且为存在的常规文件，降低“改配置即执行任意本地文件”的滥用面。

#### 前端：可选鉴权支撑
- `web/src/utils/apiAuth.ts`（新增）/ `web/src/main.tsx` / `web/src/components/Layout.tsx`：设置 `localStorage.mm_token` 后，所有 axios 管理请求自动附带 `X-API-Token`、SSE 连接附带 `token` 查询参数；未设置令牌时为无操作，默认行为不变。

#### 修复：数据与稳定性
- `internal/database/store.go`：修正 FTS 表迁移顺序——`migrateFTSTables` 提前到建表/建触发器之前，避免从旧版 FTS 结构升级的老库首次启动因 `no such table` 崩溃；新增迁移回归测试覆盖该升级路径。
- `internal/scanner/scanner.go`：`CleanupLibrary` 入口先探测资料库根目录，存储离线/盘符漂移时直接中止；待删系列占比超过 50% 触发熔断；权限、超时等不确定错误跳过而非删除，防止整库系列连同阅读进度被级联误删。
- `internal/images/processor.go`：全局 AI 并发信号量改用 `atomic.Pointer` 持有，acquire/release 快照同一 channel 引用，消除配置热更新重建信号量时的数据竞争与 goroutine 永久挂起。

#### 验证
- `go vet ./...` 通过；`go test ./...` 全部通过（新增 6 个针对脱敏、鉴权、SSRF、迁移的测试）；`cd web && npm run build` 通过。

---

### 📌 增量记录 — 2026-06-09（系列关系图谱展示优化）

#### 前端：Obsidian 风格松散图谱
- `web/src/pages/franchise-graph/index.tsx`：系列/资源库关系图谱从 Dagre 层级树布局改为确定性力导布局，节点间距更松散，整体更接近图谱浏览而非流程图。
- 图谱连线改为弱化的直线关系边，移除强箭头和流动动画；关系较多时自动隐藏边标签，降低大图浏览时的视觉噪声。
- 关系边改为按圆形封面节点中心计算连线，并新增更小的连线中段方向箭头，按 `source_series_id -> target_series_id` 指示前传、续作、外传等关系方向。
- `web/src/pages/franchise-graph/CustomNode.tsx`：节点由大封面卡片调整为圆形封面节点 + 轻量标签，当前系列使用青色高亮，节点大小随连接度轻微变化。
- 图谱背景、点阵、节点底色、控件与小地图改为读取现有主题 CSS 变量，浅色/深色主题切换时不再固定为深色背景。

#### 验证
- `cd web; npm run build` 通过；`git diff --check` 通过。PowerShell/fnm 权限提示仍为本机环境噪音，不影响构建结果。

---

### 📌 增量记录 — 2026-06-09（静态资源 ETag 缓存优化）

#### 后端：静态资源条件缓存
- `internal/api/controller.go`：`/api/thumbnails/*` 缩略图静态下发路径新增弱 ETag，基于缩略图相对路径、文件修改时间和文件大小生成；客户端携带匹配的 `If-None-Match` 时直接返回 `304 Not Modified`，避免来回切换页面时重复传输缩略图内容。
- `cmd/server/main.go`：前端嵌入静态资源统一通过 `writeStaticContent` 输出，为 `index.html`、`assets/*`、manifest、图标、截图等构建产物生成基于路径与内容的弱 ETag；SPA fallback 到 `index.html` 时也复用同一套条件请求逻辑。
- 已核对 `/api/pages/*` 页面图片与 `/api/covers/*` 封面已有 ETag；ComicInfo XML/ZIP 属于请求时动态生成下载内容，未按静态资源添加 ETag。

#### 验证
- `go test ./cmd/server ./internal/api` 通过；PowerShell/fnm 与 Go telemetry 权限提示仍为本机环境噪音，不影响测试结果。

---

### 📌 增量记录 — 2026-06-07（待办审核批量标记 + 资料库搜索持久化修复）

#### 前端：待办审核交互优化
- `web/src/pages/MetadataReviews.tsx` 与 `web/src/pages/AIGroupingReviews.tsx`：审核条目支持先标记“同意 / 拒绝”，再通过底部浮动栏统一应用已标记内容；点击标记后自动激活下一条未处理项，连续处理到已加载列表末尾时会主动补充加载下一批。
- 待处理列表的无限加载触发从页面滚动改为左侧待处理列表自身滚动，避免页面滚到底部误触发补充加载。
- AI 分组审核沿用既有单条 apply / reject API 做统一提交，元数据审核复用既有 bulk apply / reject API 分批提交。

#### 前端：资料库搜索条件持久化
- `web/src/pages/library/hooks/useLibraryFilters.ts` / `useLibrarySeries.ts` / `library/index.tsx`：搜索框关键字纳入库级筛选状态、URL query 与本地持久化；进入资料库时等待当前 `libId` 的持久化条件加载完成后再请求系列列表，避免先发空搜索条件请求、再立即发带关键字请求。
- 系列列表请求增加最新请求保护，降低搜索防抖、筛选切换和分页切换交错时旧响应覆盖新结果的风险。

#### 验证
- `cd web && npm run build` 通过；Vite 仍提示 vendor chunk 超过 500 kB，属既有构建体积警告。

---

### 📌 增量记录 — 2026-05-29（数据层 sqlc 全面迁移 + SSE Broker 加固 + Windows 构建加固）

#### 后端：raw-SQL → sqlc 全面迁移
- **Store / Health / 外部库 / 集合 / 智能筛选** 路径上的原生 `db.QueryContext` / `db.ExecContext` 全部下沉到 sqlc 生成的 `*database.Queries` 预编译入口：
  - `internal/database/store.go`：`UpsertTask` 改写为 `Queries.UpsertTaskRecord` + `UpsertTaskRecordParams`（`sql.NullInt64` / `sql.NullTime` 显式装箱）；`GetReadingListItemProgress` 改用 `GetReadingListItemProgressByList`；`refreshSeriesStatsForBook` / `DeleteBook` / `DeleteBookByPath` 通过 `GetSeriesIDByBookID` / `GetSeriesIDByBookPath` 查 series id；`GetDashboardStats` 改写为 `GetDashboardCoreStats` + `ListLibrarySizes`，对 `TotalPages` 做 `int64 / float64` 类型分支处理。
  - `internal/database/health.go`：`healthIssueDefinitions` 由 SQL 字符串瘦身为 `{Type, Severity}` 元组；`countHealthIssue` / `listHealthIssues` 在内部按 `issueType` 分派到 `CountHealthEmptyPages` / `CountHealthMissingCover` / `CountHealthMissingMetadata` / `CountHealthDuplicateFileHash` / `CountHealthMissingQuickHash` / `CountHealthDuplicateQuickHash` / `CountHealthUnmatchedKOReader` 等 sqlc 方法；新增 `interfaceToInt64` / `interfaceToString` / `nullToInt64Ptr` / `makeHealthIssue` 辅助；`attachLastTaskKeys` 调用 `GetLastTaskKeyForScope` + `GetLastTaskKeyForScopeParams`。
  - `internal/database/external_queries.go`：`ListExternalLibraryBooksByLibrary` 改用 `Queries.ListExternalLibraryBooks` + 行映射。
  - `internal/database/smart_collection.go`（**新增**，+199 行）：智能合集动态成员查询从 `collection_view_controller` 下沉到 store 层，对外暴露 `SearchSmartCollectionSeries(ctx, filter, limit, offset) ([]SearchSeriesPagedRow, int, error)`。
  - `internal/api/collection_controller.go`：`listCollections` 改用 `ListCollectionsWithSeriesCount`；`createCollection` / `deleteCollection` / `updateCollection` 改用 `CreateSimpleCollection` / `DeleteCollection` / `UpdateCollectionDetails` + 对应 Params。
  - `internal/api/collection_view_controller.go`：`loadCollectionViews` 删除约 70 行 collections + smart_filters 的内联 UNION ALL，改用 `ListCollectionViews`；`loadStaticCollectionSeries` 改用 `GetStaticCollectionView`。
  - `internal/api/smart_filter_controller.go`：`listSmartFilters` / `upsertSmartFilter` / `updateSmartFilter` 改用 `ListSmartFiltersByLibrary` / `UpsertSmartFilter` 与 `database.UpsertSmartFilterParams`，含完整字段（LibraryID、Name、Active*、Min/Max Rating/Progress、AddedWithinDays、Sort 与 PageSize），通过 `nullStringFromPointer` / `nullFloatFromPointer` / `nullIntFromPointer` 装箱。
  - `internal/scanner/scanner.go::runCoverJob`：去掉 `*database.SqlStore` 类型断言，改用 `store.SetBookCoverIfMissing(ctx, SetBookCoverIfMissingParams{CoverPath, ID})`。
  - `internal/koreader/service.go::applyBookProgress`：`store.LogReadingActivity` 改为 params struct 写法。
  - `internal/api/controller.go`：`getActivityHeatmap` / `getRecentReadAll` / `getRecommendations` 全部走 store 方法（前者拿 `NullFloat64` 后映射 `[]ActivityDay`，后者直接返回 sqlc Row 切片）；`recoverInterruptedTasks` 改用 `MarkInterruptedTasksParams{Message, Error}`；`clearAllCoverPaths` 拆为 `ClearAllBookCoverPaths` + `ClearAllSeriesStatsCoverPaths`；`DeleteReadingBookmark` 调用点改为 `(affected int64, err)` 返回，`affected == 0` 回 404。
- **sqlc 重新生成**：`internal/database/querier.go` +54 行、`internal/database/query.sql.go` +2063 行、`sql/query.sql` +594 行；`internal/database/db.go` +556 行（`Prepare` 注册了 `clearAllBookCoverPaths` / `clearAllSeriesStatsCoverPaths` / `countHealth*` / `getDashboardCoreStats` / `getLastTaskKeyForScope` / `getReadingListItemProgressByList` / `getSeriesIDByBookID` / `getSeriesIDByBookPath` 等大量新预编译语句）。

#### Store 接口契约调整
- **移除**：`GetActivityHeatmap`（重命名后回到 sqlc 生成版本）、旧签名 `LogReadingActivity(bookID, page)`、`ListReadingBookmarks` / `UpsertReadingBookmark` / `DeleteReadingBookmark`（旧签名）、`GetRecentReadAll`、`GetRecommendations`、`MarkInterruptedTasks`（旧签名）。所有移除项现统一通过 `*Queries` 暴露。
- **签名变更**：`LogReadingActivity` 改为 `LogReadingActivity(ctx, LogReadingActivityParams{BookID, PagesRead})`；`DeleteReadingBookmark` 改为 `(affected int64, err error)`，便于上层根据是否真正删除回 404；`MarkInterruptedTasks` 接受 `MarkInterruptedTasksParams{Message, Error}` 以便统一中断原因。
- **新增**：`SearchSmartCollectionSeries`（智能合集动态成员）。
- **测试同步**：`internal/api/controller_test.go` 中假 store `countingStore.LogReadingActivity` 与 `TestGetActivityHeatmapReturnsReadingData` 调用点一并切到新签名。

#### SSE Broker 加固
- `internal/api/controller.go::startBroker`：客户端 channel 缓冲从 16 提升到 64；写入时改为非阻塞 `select` + `default`，触发 backpressure 时主动断开慢客户端并落 `"SSE client backpressure, dropping client connection"` 日志。
- `PublishEvent`：同样改为非阻塞 `select default`，事件队列拥塞时丢弃并 warn，不再卡住生产线程。
- `sseHandler`：连接建立时下发 `retry: 5000\n\n` 给浏览器一致的重连节奏；新增 25s 心跳 ticker 输出 `: ping\n\n` 防止反代/浏览器空闲断流；`request.Context().Done()` 与 channel 关闭都会干净退出。
- 新增 `eventPrefix(event string) string` 辅助，避免日志被超长事件 payload 撑爆。

#### Windows 跨机构建加固（`build.ps1`）
- 显式锁定 `$env:GOOS = "windows"` / `$env:GOARCH = "amd64"`，避免被会话级环境变量污染产出非 Windows 二进制（这正是上一台机器跑出来"拒绝访问"的可疑根因）。
- `try { … } finally { … }` 中保存并还原 `$prevGOOS` / `$prevGOARCH`，不留副作用到调用方 shell。
- 增加 `-trimpath`；`go build` 后显式校验 `$LASTEXITCODE` 非 0 则 `throw`，防止"看起来成功但产物为空"。
- 注释明确：项目通过 `chai2010/webp` 依赖 CGO，因此沿用环境默认的 `CGO_ENABLED`（Windows 本机构建一般为 1），不强制关闭。

#### 前端连带改动
- `web/src/pages/BackgroundTasks.tsx`：取消自建 `new EventSource('/api/events')`，改为监听 Layout 中已挂载的全局 `manga-manager:task-progress` 自定义事件（同源浏览器 SSE 并发上限只剩个位数，原本任务中心 / 阅读器 / Layout 三处各开一条会快速触顶），同时把 `fetchTasks` 的"ALL 时并发拉 running/paused/cancelling/全量"四请求合并为单次请求由后端统一返回。
- `web/src/pages/library/hooks/useLibraryFilters.ts`：原本切库即 `GET /api/libraries/{id}/settings/` + 防抖 `PUT` 回写，改为纯 `localStorage` 持久化（key = `library:${libId}:settings`），避免每次切库阻塞 UI 等待 settings 接口；服务端同名接口仍可独立保留供后续多端同步。
- `web/src/pages/library/hooks/useSmartFilters.ts`：增加 `lib_smart_filters_cache_${libId}` 本地缓存 + `loadedLibIdRef`，挂载时先从缓存即时填充；新增 `ensureLoaded()`，仅在用户真正展开"智能筛选视图"面板时才发 `GET /api/libraries/{id}/smart-filters/` 并把结果回写缓存；保存 / 删除走乐观更新 + 回滚 + 同步缓存。
- `web/src/pages/library/LibrarySavedViews.tsx` & `library/index.tsx`：`LibrarySavedViews` 新增 `onExpand` 回调并在首次展开时调用，由 `index.tsx` 接到 `smartFilters.ensureLoaded`，实现"展开才加载"。
- `web/src/pages/book-reader/ReaderProgressTray.tsx` & `BookReader.tsx`：底部进度托盘新增 `readDirection` prop；当阅读方向为 `rtl` 时，左右两侧的"上一本 / 下一本"按钮整组镜像（左侧渲染 next、右侧渲染 prev，并连同 SkipBack/SkipForward 图标与 tooltip 文案一起翻转），与日漫从右往左的翻页直觉一致。
- `web/src/pages/Dashboard.tsx`：`RecentReadItem.cover_path` 由 `{String, Valid}` 调整为裸 `string`，跟随后端 `GetRecentReadAllRow` 的新映射；`coverUrl` 渲染条件同步简化。

#### 验证 & 收尾
- 改动跨 26 个文件、约 +4210 / -1402 行；本批 staged 后会进入单次提交。
- 数据层 sqlc 迁移后，旧的 `*database.SqlStore` 类型断言在调用方已无残留，全部走 `Store` 接口；`internal/database/db.go` 上游加载完整 prepared statement set，单测与现有 `controller_test.go` 已同步。
- 暂未运行 `go build`；`build.ps1` 加固后会自然在下次构建时验证 GOOS/GOARCH 与产物。

---

### 📌 增量记录 — 2026-05-29（阅读器阶段 3 重构 · 沉浸式 + 续读上下文）

#### 阅读器（阶段 3 重构）
- **沉浸式 Shell**：新增 `useReaderImmersive.ts` + `ReaderImmersiveShell.tsx`，默认 5s 自动隐藏顶部条 / 进度条，鼠标移动 / 键盘 / 触摸 / 滚轮触发的「唤醒」只在已可见时延长停留，不在隐藏态被动唤起；`forcedVisible` 模式在 `showSettings || showHelp` 期间锁定显示；顶 / 底各保留 `h-10 / h-12` 触发条作为「找不到设置」时的兜底入口。
- **中央点击切换**：`PagedReader.tsx` 通过 `pointerdown / pointerup` 检测短距离 + 短时长的 tap，按视口左 30 / 中 40 / 右 30 三段语义触发 `onPrev / onCenterTap / onNext`，hover 模式下另保留两侧大箭头作为桌面端引导；`WebtoonReader.tsx` 同款 tap 检测 + button / a / input 元素自动豁免，避免影响虚拟滚动。
- **上一本 / 下一本 / 卷内章节**：新增 `useReaderSiblings.ts`，`/api/book-prev/{id}` + `/api/book-next/{id}` 拉取兄弟书；同时按 `seriesIdRef` 复用 `/api/series/{id}/context` 的 books 列表派生 `allInVolume`。`ReaderProgressTray.tsx` 在底部进度条左右两侧增加 SkipBack / SkipForward 胶囊按钮（无可用兄弟时降级为禁用占位）；`ReaderTopBar.tsx` 右侧新增 `ListOrdered` 卷内章节 popover，列出当前卷所有书籍并高亮当前位置。
- **末页"下一本"具名**：WebtoonReader 末页按钮文案改为 `reader.nextBookNamed`（`▶ 继续阅读：{name}` / `▶ Continue: {name}`），自动从 `useReaderSiblings.next.title` 取名；无下一本时回落到原 `reader.nextBook`。
- **进度同步状态指示**：新增 `useReaderProgressIndicator.ts`，状态机 `'idle' | 'syncing' | 'synced' | 'offline-queued'`，将原 `useReaderProgressPipeline` 内的 `updateProgress` 反向注入，集中处理在线 / 离线分支与 1.5s synced 闪显。`ReaderTopBar` 在标题前显示直径 8px 状态点（gray / amber pulse / emerald / rose），hover tooltip 文案对应 4 条 `reader.progress.*` 翻译。
- **设置抽屉模式拆分**：`ReaderSettingsDrawer.tsx` 顶部新增"全局阅读偏好 / 本书状态"模式切换；切换后自动校正 tab（`reading` / `image` 归全局，`cache` / `bookmarks` 归本书），用户偏好持久化到 `localStorage('manga-reader:settings-mode')`，默认 `global`。
- **离线进度 bulk 同步**：`offlineReader.ts::syncQueuedOfflineProgress` 改为 `POST /api/books/bulk-progress/sync`，请求体 `{ items: [{book_id, page, updated_at}] }`，识别 `updated | skipped_stale | skipped_unchanged` 视为成功；HTTP 失败 / 网络异常时退回逐条 `POST /api/books/{id}/progress` 兜底，避免离线积压在恢复在线时打雷一样砸出峰值写入。
- **顶部条卷子标题**：`ReaderTopBar` 在标题下补一行 `bookVolume` 子标题（`text-[11px] text-gray-300/80`），切书时阅读器顶端能直接看到所在卷，免去回系列页确认。

#### i18n
- **新增 zh-CN / en-US 同步 key**：
  - `reader.progress.{idle | syncing | synced | offlineQueued}` —— 状态点 tooltip
  - `reader.siblings.{prev | next | volumeChapters | unavailable}` —— 上一本 / 下一本 / 卷内章节按钮 + 占位
  - `reader.center.toggleUI` / `reader.immersive.{show | hide}` —— 沉浸式辅助文案
  - `reader.settingsMode.{global | book | toggle}` —— 设置抽屉新模式开关
  - `reader.nextBookNamed` —— `▶ 继续阅读：{{name}}` / `▶ Continue: {{name}}`

#### 验证 & 收尾
- `npx tsc -b` 通过；`npx eslint src/pages/book-reader src/pages/BookReader.tsx` 0 错误 0 警告（同批新增的 6 处 set-state-in-effect / exhaustive-deps 误报已用 directive 注释豁免，含 `useReaderImmersive` 强制可见、`useReaderProgressIndicator` 状态重置、`useReaderSiblings` 切书清空、`ReaderSettingsDrawer` 模式校正、`useReaderBookmarks` / `useReaderBookData` / `useReaderOffline` 切书重置）。
- 仓库剩余 1 错（`Ops.tsx:27`）+ 1 警告（`Layout.tsx:431`）均与本批次无关，单独跟进。
- `docs/library-series-reader-todo.md` 阶段 3 八个子节点（3.1 - 3.8）全部标记 `[x]`，仅剩 3.8 浏览器手动回归；进度看板写入「阶段 3 阅读器 = ✅ 代码项已完成」。

---

### 📌 增量记录 — 2026-05-29（系列详情阶段 2 重构 + Hero 现代化）

#### 系列详情（阶段 2 重构）
- **`SeriesDetail.tsx` 整体拆分**：原 982 行单文件页面拆为 `web/src/pages/series-detail/` 下的 `index.tsx`（339 行）+ 7 个展示组件（`SeriesHeroBar` / `SeriesQuickActions` / `SeriesVolumeAccordion` / `SeriesBookGrid` / `SeriesBookCard` / `SeriesSelectionBar` / `SeriesSidePanel`）+ 12 个 hook（`useSeriesContext` / `useSeriesSelection` / `useSeriesContinue` / `useSeriesScrape` / `useSeriesEdit` / `useSeriesActions` / `useSeriesMetadataReview` / `useSeriesRelations` / `useSeriesProgress` / `useSeriesFailedTasks` / `useSeriesVolumes` / `useSeriesOpenVolumes`），删除老的 `SeriesHeader.tsx` / `SeriesContentSection.tsx` / 原 `SeriesDetail.tsx`。
- **数据请求合并**：`/tags` `/authors` `/links` `/relations` `/metadata-review` `/failed-tasks` 6 个并行请求合并为单次 `GET /api/series/{id}/context`，由 `useSeriesContext` 统一持有 + `reload()` 重置；mutation hook 通过 setters 维持本地最新态。
- **续读 CTA**：消费 `context.continue` 字段，`buildContinueCta` 输出 `{ bookId, page, totalPages, volumeLabel, bookLabel }`；hero 区域渲染三种状态文案（`series.continue.start` / `resume` / `reread`），CTA 直接 `<Link to="/reader/{bookId}">` 跳到阅读器。
- **卷折叠列表**：`SeriesVolumeAccordion` 替换原"点击卷 → 路由切换 → 子视图"模式，折叠状态写入 `localStorage('series_open_volumes_${id}')`；`useSeriesOpenVolumes` 兼容 `?volume=...` 旧链接，进入时自动展开匹配项，用户主动操作后清除 query。
- **侧抽屉下沉管理面板**：`SeriesSidePanel` 用 `createPortal` 投到 body，含三个 tab（关系 / 元数据审核 / 失败任务），主区域只在右上角显示 `SeriesSidePanelBadge`（`series.sidePanel.badge.metadata` + `badge.failed`），仅在 `pendingMetadata > 0 || failedCount > 0` 时出现。
- **多选底部栏**：`SeriesSelectionBar` 复用 `components/ui/SelectionBar.tsx` 共享基础组件，与资源库风格对齐（标已读 / 标未读 / 全选 / 反选）。
- **书卡 hover 操作**：`SeriesBookCard` 把原顶部按钮收入"⋯"菜单（导出 ComicInfo / 复制识别码），保留快速标记已读按钮和 `Y/Z` 进度徽章。
- **路由迁移**：`App.tsx` 的 `SeriesDetail` 懒加载路径由 `./pages/SeriesDetail` 改为 `./pages/series-detail`，旧 `?volume=` 链接由 hook 自动兼容。

#### 系列详情 Hero 现代化
- **整体视觉重设计**：`SeriesHeroBar` 从原"封面 + 标题 + chip 列表"扁平结构升级为：封面区（双层效果，前景圆角投影 + ring，背后 `from-komgaPrimary/40 to-komgaSecondary/40` 柔光晕，桌面端额外加封面背景模糊"halo"）→ 信息区（状态脉冲点 → "By 作者A · 作者B" 行 → `bg-clip-text` 渐变大字号标题 `text-3xl→6xl font-black` → 简介 → CTA + 进度环 → 统计卡片组 → 标签 → 外链）。
- **信息平铺，移除"更多信息" popover**：评分 / 语言 / 出版社 / 外链全部直接平铺渲染；`useState` / `useRef` / `useEffect` / `infoOpen` 相关代码连同 popover 一并删除。
- **状态彩色脉冲点**：`statusDotColor` 把 `normalizeSeriesStatus()` 输出映射到 emerald(ongoing) / sky(completed) / amber(hiatus) / rose(cancelled) / gray(unknown)，使用 `animate-ping` 在外层做扩散动画 + `shadow-[0_0_10px]` 发光。
- **主 CTA 渐变胶囊按钮**：`from-komgaPrimary to-komgaPrimaryHover` 渐变背景 + 内嵌圆形播放图标 + 尾部箭头，hover 上浮 `-translate-y-0.5` + `shadow-2xl` 加深 + 高光扫过动画（`-translate-x-full → translate-x-full`）。
- **进度环卡片**：新增 `ProgressRing` SVG 组件（`r=16`，`strokeDasharray` 动画，`rgb(var(--color-komga-primary))` 主色，已读完切翠绿 `#34d399`），与 CTA 按钮并列。读完时 label 切换为 `series.stats.completed`。
- **统计卡片组**：新增 `StatCard` 组件 + `StatTone` 类型，6 种语义化色调（amber / indigo / violet / cyan / emerald / rose），统一 `bg-{tone}-400/10` + `text-{tone}-300` + `border-{tone}-400/30` + `backdrop-blur-sm`。评分作为第一张卡片（amber 色 + ★图标 + `tabular-nums` 数字 + 小写灰底"评分"标签），其后接册数 / 卷数 / 单行本 / 语言 / 出版社。
- **标签可展开**：`tagsExpanded` state 控制；默认展示前 10 个，超出时尾部加 `▼ 展开 +N 个标签` 胶囊按钮，点击展开全部并切换为 `▲ 收起标签`；每个 tag truncate 宽度从 `10rem` 提到 `14rem` 并附 `title` 让长标签可 hover 看全。
- **外链行**：作为独立行平铺，不再嵌在 popover；图标加 `shrink-0` 保证不被压扁。
- **作者行内化**：原"作者 chip"改为 hero 顶部 "By 林惠美 · 山田悟" 语义文本（最多 3 位 + `+N`），点击跳到资源库 `?author=...`。
- **响应式优化**：移动端封面 `w-32` 居中、`sm:w-44 lg:w-52`；标题 / chips / CTA 在 mobile 居中（`text-center`），`sm:` 起左对齐；CTA 在窄屏 `w-full justify-center`，桌面端回归内联宽度；外层 padding 从 `p-6 lg:p-10` 调成 `p-4 sm:p-6 lg:p-10`，移动端不再浪费 24px 边距。

#### i18n
- **新增 zh-CN / en-US 同步 key**：
  - `series.failedTasks.retry`、`series.selection.selectVolume` / `unselectVolume`
  - `series.book.moreActions` / `copyPath` / `pathCopied` / `copyPathFailed`
  - `series.continue.reread` / `resume` / `start`
  - `series.sidePanel.title` / `tabs.relations|metadata|failed` / `metadataEmpty` / `failedEmpty` / `badge.metadata` / `badge.failed`
  - `series.header.enterSelection` / `exitSelection` / `infoMore` / `byAuthors`
  - `series.stats.books` / `volumes` / `standalones` / `pages` / `progress` / `completed` / `rating`
  - `series.tags.showAll` / `collapse`
- **`series.header.seriesSummary`** 升级为 3 占位符版本（`{{count}} · {{volumes}} 卷 · {{standalone}} 单行本`）。

#### 待办清单同步
- `docs/library-series-reader-todo.md` 阶段 2 全部 9 个子节点 (2.1 - 2.9) 标记为 `[x]`，仅剩 2.9 浏览器手动回归；进度看板更新 "阶段 2 系列详情 = ✅ 代码项已完成"；阶段 4 同步把 `web/src/pages/SeriesDetail.tsx` 删除项标 `[x]`。

---

### 📌 增量记录 — 2026-05-29（资源库重构上线 + 续读位置后端契约）

#### 资源库（阶段 1 重构）
- **新增 `/library/:libId` 路由与目录骨架**：原 `Home.tsx`（~1800 行）拆分为 `web/src/pages/library/` 下的入口 + 10 个展示组件 + 10 个 hook，旧路由经 `<Navigate replace />` 自动迁移；删除整个 `web/src/pages/home/` 目录与 `Home.tsx`。
- **数据钩子拆分**：`useLibraryFilters`（filter 状态 ↔ URL query 双向同步 + serializedFilters）、`useLibrarySeries`（封装 `/api/series/search` cursor + offset，带 `recordSeriesListRenderMetric` 埋点）、`useLibrarySelection`（多选 + 批量已读 / 收藏 / 加合集 / 重扫）、`useLibraryKeyboard`、`useExternalLibrary`、`useSeriesScraping`、`useSmartFilters`。
- **页面级派生 hook**：本轮再抽出 `useLibraryFilterOptions`（标签/作者懒加载 + 搜索）、`useLibraryCardActions`（卡片点击 / 收藏 / 重扫 + `getApiErrorMessage` 内化）、`useLibraryTransfer`（转移摘要计算 + 模态状态机），主入口稳定在 413 行。
- **Modal 抽离**：`TransferConfirmModal.tsx`（独立 props + 内部 `submitting` 守卫）、`LibraryScrapeModal.tsx`（包装 `SeriesSearchModal` 的 props mapping），不再内联在 `index.tsx`。
- **顶部 / 筛选 / 网格 / 多选 / 分页**：`LibraryHeader`（标题 + 视图 + 进入选择 + 外部库）、`LibraryFilterBar`（已应用 chip + "+ 添加筛选"）、`LibraryGrid`（IntersectionObserver 触发 `loadMore`）、`LibrarySelectionBar`（基于新增的共享 `components/ui/SelectionBar.tsx`，整合标已读 / 标未读 / 加合集 / 重扫 / 取消，以及全选 / 反选）、`LibraryPagination`（保留显式分页器，`localStorage('lib_pagination_mode_${libId}')` 切换无限滚动 vs 分页）。
- **外部库抽屉化**：`ExternalLibraryDrawer.tsx` 复用 `DirectoryPicker` + 状态 + 转移确认；外部状态、SSE 联动、`fetchExternalSession` / `startExternalLibraryScan` / `transferToExternalLibrary` 全部封装到 `useLibraryExternal.ts`，抽屉关闭 / 重开保留任务订阅；抽屉改为 `createPortal` + 主题变量驱动蒙层、面板换 `bg-komgaSurface`，修复浅色主题下整页糊白。
- **资源库快捷键**：`useLibraryKeyboard` 启用 `/`（聚焦搜索）、`g`（回顶部） / `Shift+G`（到底部）、`e`（切换选择模式）、`Esc`（退出选择）；`ShortcutsPanel` 新增 `shortcuts.group.library` 分组与对应 zh-CN / en-US 文案。
- **卡片续读角标**：`LibraryCard.tsx` 在封面右下方显示 `continue · {page}/{total}` chip（无 totalPages 时退化为 `Continue · p.{page}`），仅当 `last_read_at.Valid && last_read_page > 0 && !fullyRead` 时出现；新增 `home.card.lastReadAtPage` / `home.card.lastReadAtPageOfTotal` i18n 文案。**底部 "{P}" 总页数应用户要求保留。**
- **智能筛选视图 404 修复**：前端 `DELETE` 路径从 `/api/libraries/${libId}/smart-filters/${id}/` 改为 `/api/smart-filters/${id}` 对齐后端注册。

#### 续读位置 / 阅读进度（阶段 0 后端契约完结）
- **`SearchSeriesPagedRow` 增加续读字段**：`last_read_at` / `last_read_book_id` / `last_read_page`，由 `buildSeriesSearchQuery` 通过 `LEFT JOIN (SELECT series_id, MAX(last_read_at) ... FROM books)` 聚合得到；资源库 `/api/series/search` 现在直接返回该信息，避免前端按系列再请求一次。
- **新增 `GET /api/series/{id}/continue` 端点**：返回 `next_unread_book_id` / `last_read_book_id` / `last_read_page` / `last_read_at` / `total_books` / `read_books` / `total_pages` / `read_pages`，规则：第一本未完成的书作为 `next_unread`、`last_read_at` 最大的书作为"上次读到这里"，全部读完时 `next_unread` 为 0。
- **`SeriesContextResponse` 内联续读摘要**：`/api/series/{id}/context` 同时返回新加的 `continue` 字段，详情页 hero 一次请求即可拼出 CTA 文案，无需二次访问。
- **新增 `POST /api/books/bulk-progress/sync`**：批量同步阅读进度（含 `last_read_page` / `last_read_at`），用于 KOReader / 客户端回写场景。
- **新增 `GET /api/book-prev/{bookId}`**：阅读器 / 详情页跳"上一本"补全，与既有 `book-next` 对称。
- **`reading_list_controller.go`**：把 `if err == sql.ErrNoRows` 改成 `errors.Is(err, sql.ErrNoRows)`，避免被 `fmt.Errorf("...: %w", err)` 包装时漏判。

#### i18n / 杂项
- **`TranslationParams` 类型放宽**：增加 `unknown` 兜底，方便把后端 `sql.NullX` 字段直接塞进占位符。
- **`.gitignore`**：新增 `config.yaml` 忽略，避免本地 server 配置泄漏入库。

---

### 📌 增量记录 — 2026-05-28（前端导航重构 + 任务进度修复）

#### 侧栏导航重构
- **侧栏分组改为 4 组**：原本的"阅读空间 / 维护工具 / 系统数据"等分组重写为"我的书架（Shelf）/ 我的视图（Views）/ 整理与审核（Curate）/ 系统（System）"，新增 `SidebarGroup` 与 `SidebarLink` 复用组件，折叠/展开状态由各组独立持有。
- **资源库快速过滤**：侧栏资源库列表新增搜索框，支持按名称模糊过滤；资源库分组展开状态会写入 `localStorage` 持久化。
- **新增侧栏任务气泡**：抽出 `SidebarTaskBubble` 组件，订阅 `task_progress` SSE，在侧栏底部展示进行中、刚完成或失败的任务卡片，可单条关闭或一键清理已完成项。
- **新增键盘快捷键面板**：抽出 `ShortcutsPanel`，按 `?` 打开/关闭，按 `g+r/o/c/...` 跳转板块；面板和 i18n 文案覆盖全局、审核中心、整理工作台、任务等四组快捷键。

#### 任务与日志合并
- **任务中心与系统日志合并到 `/ops`**：新增 `Ops.tsx` 容器页，把 `BackgroundTasks` 与 `Logs` 合并为同一页签内的两个 tab，URL 通过 `?tab=tasks|logs&task_key=...` 控制；旧路由 `/organize/tasks` 与 `/logs` 自动 `Navigate` 到 `/ops` 对应 tab。
- **日志按 task_key 过滤**：`GET /api/system/logs` 新增 `task_key` 参数（在 raw 行中匹配 `task_key=...` 子串），任务详情面板可一键跳到该任务相关日志。
- **健康问题携带 last_task_key**：`HealthIssue` 增加 `last_task_key` 字段，"整理与审核"入口可由问题直接跳到对应任务的日志。

#### 概览（Dashboard）改造
- **概览首屏置顶继续阅读 Hero**：把"继续阅读"从下半区提至首屏，覆盖最近 20 本带封面与进度条；空态时回退到推荐区。
- **待审核横幅**：当 `/api/reviews/inbox/summary` 返回 total > 0 时，概览顶部展示挂起待审核条目数与"前往待办审核"入口。

#### 审核中心
- **新增统一审核入口接口**：`GET /api/reviews/inbox/summary` 返回元数据审核、AI 分组审核、KOReader 未匹配进度三类待办计数总和，前端侧栏徽标与概览横幅复用同一接口。
- **元数据 / AI 审核详情面板**：`MetadataReviews` 与 `AIGroupingReviews` 引入 `activeReviewId` + 右侧详情面板模式，列表多选与详情查看分离；toast 行为统一收敛到 `useToast` 全局通道，移除组件内部 toast 状态。
- **元数据审核字段化**：拆出 `metadataAuthorEntryString / metadataJoinAuthors / metadataJoinProposedAuthors / metadataParseAuthors` 等帮助函数，作者审核改为按 `name (role)` 规范化对比与展示。
- **批量刮削回写作者**：`applyMetadataToSeries` 在未锁定 `authors` 字段时按 `name|role` 去重 upsert 作者并 link 到系列，记录字段变更到审核日志。
- **Bangumi provider 抽取作者**：新增 `extractAuthorsFromInfobox`，从 infobox 中按 `作者 / 原作 / 漫画 / 作画` 等中文 key 推断 `SeriesAuthor`（角色映射到 `Writer / Penciller` 等标准 role）。

#### 整理工作台
- **健康问题按严重度折叠**：`Organize.tsx` 按 `error / warn / info` 分组并折叠，error/warn 默认展开、info 默认收起，状态独立持有。
- **聚焦回页面自动刷新**：监听 `visibilitychange` 与 `focus` 事件，切回页签后自动重新拉取健康报告。
- **修复元数据缺失误报**：原本只要"缺 tag 或 缺 author"就算缺失，改为同时缺 tag 和缺 author 才计入；展示语改为"missing tags and authors"。

#### 合集与阅读清单
- **合集类型 Tab**：`Collections` 顶部新增 `全部 / 手动 / 智能` 切换 tab，按 `kind` 过滤左侧列表，附计数徽标。
- **阅读清单进度聚合**：`/api/reading-lists/{id}/items` 响应增加 `read_books / completed_books / total_books`，前端把当前清单的累计进度展示在头部。
- **新增 `GetReadingListItemProgress` store 方法**：聚合系列已读/已完成/总卷数，给 reading list controller 复用。

#### 离线书架
- **离线健康条**：抽出 `OfflineHealthBar`，把在线状态、Service Worker 支持情况、配额占比、排队任务、缓存数量统一展示在书架顶部。
- **缓存列表改为卡片网格**：从原本的纵向 divider 列表改为 lg:grid-cols-2 的卡片排版，更适合大量条目浏览。

#### 任务中心（重建缩略图任务进度修复）
- **修复重建缩略图进度过早跑满**：`rebuild_thumbnails` 任务的进度展开为归档处理与封面生成两阶段（`current = processed + skipped + generated_covers`，`total = discovered + queued_covers`），归档全部入队时进度只走到约 50%，cover queue 异步生成阶段继续推进到 100%，避免任务一开始进度条就到顶但封面信息仍在滚动的视觉矛盾。
- **修复归档进度临时高于发现数时被钳成 100%**：聚合器在 `processed > discovered` 抖动窗口内不再回退到占位 `total=1`，而是让 `total` 跟住 `current`，进度条始终落在合理区间。
- **库切换边界不再覆盖归档进度**：`runGlobalScan` 的库切换回调不再用"库索引/库数"覆盖任务的 `current/total`，库进度只用于消息文案，进度条统一由聚合器基于归档与封面 metrics 驱动。
- **等待封面队列阶段保留真实进度**：`WaitForCoverQueue` 期间不再写死 `1/1`，而是用聚合器累计的 metrics 驱动进度条，配合阶段消息切换。
- **任务启动初始 total 改为 0**：`launchRebuildThumbnailsTask` 启动时不再设占位 `total=1`，前端在 metrics 抵达前显示不确定进度动画，避免 0/1 与后续大数字之间的视觉跳动。

#### 缩略图重建
- **修复重建缩略图未真正全量重建**：删盘后新增 `clearAllCoverPaths`，将 `books.cover_path` 置 NULL、`series_stats.cover_path` 置空串。原本 scanner 仅在 `cover_path` 为空时入队 cover job、cover worker 也仅在 `cover_path` 为空时回写，导致已存在 cover_path 的归档既不重新入队也不会被回填，重建后磁盘缩略图实际未恢复。
- **cover job 携带 library 上下文**：`coverJob` 增加 `libraryID` 与 `progress reporter`，cover worker 完成时按归档发布 `queueing_covers` 阶段进度事件。

### 📌 增量记录 — 2026-05-27（后台任务状态与进度可观测）

#### 任务中心
- **任务状态契约扩展**：后台任务新增阶段、当前对象、百分比、速率、ETA、暂停状态、结构化指标、标签和有效执行限制字段，并通过现有任务参数持久化兼容历史记录。
- **新增任务级暂停/继续**：扫描和批量刮削任务支持 `/api/system/tasks/{taskKey}/pause` 与 `/resume`，取消会优先唤醒暂停点并终止任务上下文。
- **扫描进度结构化上报**：扫描器按阶段上报发现文件、比较变更、读取元数据、哈希、写库和封面排队等进度，并在任务详情中展示有效扫描并发、存储策略与归档打开并发。
- **扫描指标补齐**：扫描任务新增封面入队、封面生成和失败归档指标；发现阶段使用不确定进度，发现完成后再切换为归档总数。
- **刮削进度结构化上报**：全库和单库批量刮削展示 provider、当前系列、成功、失败、未找到、入审阅队列、请求数和限速等待耗时，并支持暂停后停止发起新的 provider 请求。
- **维护页任务中心组件化**：维护页任务中心拆分为 Summary、List、Card、Progress、Limit、Metrics、Detail 和 Action 组件，实时接收 SSE `task_progress`，轮询兜底，并提供暂停、继续、取消等操作入口。
- **任务运行时统一管理**：后台任务运行时统一收敛为 `TaskRuntime`，集中保存 context、cancel 和 pause gate，减少暂停、继续、取消路径的分散状态。
- **更多后台任务接入统一控制**：缩略图重建、文件身份重建、KOReader 哈希重建、KOReader 进度重关联、外部库扫描/传输和 AI 分组均接入阶段、指标与暂停/继续检查点。
- **服务重启任务状态更明确**：服务启动时遗留的 running/paused/cancelling 任务会标记为 `interrupted`，保留错误说明和重试能力，避免任务中心出现僵尸运行态。

### 📌 增量记录 — 2026-05-27（任务中心与审核工作流收口）

#### 后台任务中心
- **任务中心迁移到系统数据工坊**：后台任务中心从维护工具和系统日志中移除，统一收敛到系统数据工坊的二级菜单，避免同一任务数据在多个页面展示不一致。
- **恢复任务扩展信息展示**：统一任务中心重新展示执行限制、扫描/IO 指标、标签、参数快照、错误详情、开始和结束时间等扩展信息，兼顾活跃任务监控与历史任务追踪。
- **系统日志回归日志职责**：系统日志页不再混合展示后台任务中心，只保留日志与性能诊断内容，降低维护入口的概念重叠。

#### 元数据审核
- **系列页元数据审核队列默认折叠**：漫画系列详情中的元数据审核队列默认收起，只展示待处理数量和入口，减少系列首屏干扰。
- **审核队列入库去重**：新的元数据审核建议入库前会与同系列现有待审核内容做字段级签名对比，完全相同的待审核项直接复用，避免批量刮削重复堆积。
- **审核中心计数实时刷新**：在审核中心应用或拒绝元数据审核、AI 分组审核后，顶部角标会重新拉取计数，避免操作完成后仍显示旧数量。
- **补齐维护与任务中心 i18n**：补充批量元数据刮削确认文案、清理阶段、任务耗时指标和任务扩展标签等缺失翻译，避免界面显示原始 key。

### 📌 增量记录 — 2026-05-22（外接机械盘低冲击 IO）

#### 存储 IO 治理
- **新增存储介质策略配置**：`library.storage_profile` 支持 `auto`、`ssd`、`hdd_external`、`network`、`custom`，并支持按路径覆盖 `library.storage_policies`。
- **外接机械盘默认低冲击**：`hdd_external` 自动将扫描归档打开、封面生成和 hash 并发收敛为 1，并打开阅读时暂停后台 IO、重任务空闲执行、同盘页面缓存禁用等策略位。
- **扫描与封面接入同盘 token 限流**：扫描打开归档、封面生成和 hash 计算会按 `volume_key` 申请重 IO token，同一外接 HDD 不再被多个后台重 IO 任务并发打满。
- **阅读路径接入统一 IO 调度**：页图请求打开归档前会申请 reader token；等待中的 reader 会阻止新的同盘后台重 IO 获取 token，阅读结束后后台任务再恢复。
- **修复阅读抢占外接盘后台任务**：当强制扫描或封面重建已占用同盘低冲击 token 时，页图 reader 可临时抢占可暂停后台 lease，避免打开漫画被后台任务长时间阻塞；阅读后的后台恢复保护窗口延长到 30 秒。
- **页面磁盘缓存避开慢盘同盘写入**：当缓存目录和漫画库处于同一磁盘，且策略开启 `disable_same_disk_page_cache` 时，处理后阅读页不会写入服务端磁盘缓存。
- **设置页增加存储 IO 策略入口**：库与扫描设置页可切换外接机械盘低冲击模式，并调整归档打开、封面生成和 hash 并发。
- **维护页增加存储 IO 诊断**：展示缓存目录所在磁盘、各资源库 `volume_key`、存储策略、重 IO 并发，以及同盘页面缓存是否已被保护。
- **支持全局暂停后台存储 IO**：维护页可暂停或恢复扫描、封面和 hash 等后台任务获取新的重 IO token，阅读请求仍可继续进入 reader 路径。
- **仅空闲运行策略落到调度层**：`idle_only_heavy_tasks` 会阻止后台重 IO 在同盘已有活跃 token 时启动，并在诊断页展示后台等待数与暂停原因。
- **任务中心补齐 IO 摘要**：扫描任务写入打开归档数、hash 文件数、IO 等待耗时和存储介质信息，文件身份重建与 KOReader full hash 后台补算也接入同盘 token 与 hash 指标聚合。
- **路径级存储策略 UI**：库与扫描设置页支持直接为单个资源库路径配置 `storage_policies`，可覆盖存储介质、扫描/归档/封面/hash 并发以及阅读让路、仅空闲执行、同盘页面缓存保护。
- **缩略图缓存写入纳入低冲击调度**：批量封面写入缓存目录会进入 `cache_write` token，缓存目录与漫画盘同盘时复用同盘低冲击策略，并记录 `thumbnail_write_ms`。
- **任务 IO 指标补充暂停耗时**：调度器区分普通 token 等待和因阅读优先、手动暂停或磁盘忙导致的 `paused_ms`，扫描和文件身份任务会把该指标写入任务中心。
- **高风险维护操作增加确认**：全量缩略图重建和文件身份重建触发前会提示可能长时间占用外接盘，确认后按低冲击策略排队执行。
- **缩略图重建任务可取消**：重建缩略图缓存不再清空后立即标记完成，而是作为可取消任务执行全库强制扫描并等待封面队列收尾，任务中心能看到真实完成/取消状态。
- **存储 IO 诊断补充速率指标**：维护页展示最近扫描和封面重建的归档打开速率，以及最近缩略图写入耗时，便于判断当前磁盘压力来源。
- **新增外接盘 IO 基准采集工具**：`cmd/storageiobench` 可对资源库目录执行 walk/stat、抽样顺序读取、并发读取对比和缓存小文件写入测试，并输出 Markdown 基线报告。
- **基准工具补充阅读延迟探针**：`cmd/storageiobench` 会在后台读取压力下对比无调度和 reader-priority 低冲击模式的阅读探针 P50/P95/Max 延迟。
- **基准报告增加策略建议**：`cmd/storageiobench` 支持 `label/profile/notes`，并在报告中输出并发、缓存位置和阅读延迟改善建议，方便做 SSD 与外接 HDD 对比。
- **新增外接盘 IO 基线采集脚本**：`scripts/storage-io-baseline.ps1` 封装外接 HDD 与 SSD 对照采集流程，默认把报告归档到 `docs/performance-baselines/`。
- **补充封面重建基线入口**：`cmd/storageiobench` 增加 `cover-rebuild-sim`，可按样本归档执行少量读取与模拟缩略图写入，用于估算全量封面重建对外接盘的压力。

### 📌 增量记录 — 2026-05-21（阅读页磁盘缓存开关）

#### 阅读缓存治理
- **新增阅读页磁盘缓存开关**：`cache.page_disk_cache_enabled` 可控制处理后页面是否写入服务端磁盘缓存，默认关闭；关闭后仍保留内存预加载、浏览器缓存与离线阅读缓存。
- **补充开关测试覆盖**：验证关闭时不会读写处理后页面磁盘缓存，开启时仍可复用历史磁盘缓存。

### 📌 增量记录 — 2026-05-21（阅读路径瘦身）

#### 阅读性能
- **默认阅读原图透传**：前端不再把 `bilinear`、`nearest`、`average` 这类浏览器端渲染滤镜传给后端，后端也会把这些滤镜归一为空处理，避免默认阅读触发无意义的图片解码与重编码。
- **新增归档页清单缓存**：后端按书籍 ID、路径、修改时间和大小缓存归档页清单，连续翻页不再重复枚举压缩包目录，源文件变化后自动失效。
- **新增轻量书籍源缓存**：页图请求缓存 `book_id -> path/mtime/size/page_count`，同一本书连续翻页不再每页都查询完整书籍记录；扫描、重扫完成后会清理阅读路径缓存。
- **新增阅读进度写入节流**：同页短时间重复上报会直接跳过，`reading_activity` 仅在阅读页码前进时写入，降低 Webtoon 滚动过程中的重复写压力。
- **补充阅读路径回归测试与基准**：覆盖浏览器端滤镜透传不处理、页清单缓存命中与源文件变更后失效、连续页图请求只查询一次书籍源、进度重复写节流，并新增 50 页连续原图读取 benchmark。

### 📌 增量记录 — 2026-05-21（Webtoon 渲染窗口化）

#### 阅读性能
- **Webtoon 模式改为虚拟列表渲染**：使用 `react-virtuoso` 只挂载当前视口附近页面，长漫画不再一次性把整本书图片全部放入 DOM。
- **保留阅读定位与跳页能力**：上次阅读页、底部进度条跳页和下一本入口改为通过虚拟列表索引定位，不再依赖全量 DOM 查询。
- **简化滚动进度监听**：移除 Webtoon 模式下对 `.ReaderScrollContainer img` 的全量 `IntersectionObserver` 监听，改由虚拟列表可见范围回写当前页，并复用统一进度节流管线。

### 📌 增量记录 — 2026-05-21（首页资源库查询重构）

#### 首页性能
- **新增 `series_stats` 读模型**：缓存封面、阅读页数、已读/完成书籍数、最近阅读书籍、标签名和作者名，迁移时会自动回填旧数据。
- **首页查询改走读模型**：`SearchSeriesPaged` 默认只查询 `series + series_stats`，不再为默认列表执行书籍窗口函数、阅读进度聚合以及无条件标签/作者多表 join。
- **筛选 join 按需化**：只有标签或作者筛选时才进入 `series_tags/tags`、`series_authors/authors` 关联表；无筛选 total 直接走 `COUNT(*)`。
- **补齐维护与验收**：扫描、书籍创建、阅读进度、标签/作者变更和元数据更新会刷新 `series_stats`；新增读模型测试、迁移测试、10k 系列 benchmark 和 queryplan 检查项。

### 📌 增量记录 — 2026-05-21（系列详情首屏合并）

#### 系列详情性能
- **强化系列详情上下文接口**：`/api/series/{seriesId}/context` 现在一次返回系列、书籍、标签、作者、外链、卷摘要、相关系列、元数据审核和失败任务摘要。
- **首屏请求收敛**：`SeriesDetail` 首屏不再额外请求关系、元数据审核和系列失败任务，编辑弹窗的全量标签/作者仍保持打开时懒加载。
- **大系列渲染边界收口**：系列顶层继续只渲染卷卡片和独立书籍，折叠卷不渲染卷内所有书籍操作按钮；批量标记和元数据保存后统一刷新 context。
- **补充上下文回归测试**：覆盖 context 聚合卷摘要、关系、待审核元数据和失败任务，防止系列详情首屏请求重新发散。

### 📌 增量记录 — 2026-05-21（扫描管线轻量化）

#### 扫描性能
- **新增扫描等级配置**：`scanner.scan_profile` 支持 `fast_scan`、`metadata_scan`、`identity_scan`、`repair_scan`，设置页可直接切换；默认保持 `metadata_scan`。
- **未变化文件快速跳过增强**：增量扫描现在同时比较路径、修改时间和文件大小，未变化文件不会进入归档打开路径；仅大小变化也会触发重扫。
- **KOReader hash 按需化**：默认元数据扫描不再同步计算 full hash；KOReader 启用且使用 binary hash 匹配时改由可取消的低优先级后台任务补算，identity/repair 扫描仍保留深度身份重建能力。
- **支持极速发现模式**：`fast_scan` 只做文件发现与 DB 占位更新，不打开归档、不生成封面、不解析 ComicInfo、不计算文件 hash，适合大库日常巡检。
- **封面生成队列化**：扫描 worker 不再同步读取第一页并转码缩略图，缺封面书籍会在入库后进入后台队列；封面完成后写回 `cover_path`、刷新 `series_stats` 并通知前端刷新。
- **任务中心支持取消扫描**：资源库扫描和系列扫描会注册可取消上下文，任务中心可发起取消；扫描收尾后状态落库为 `cancelled` 并通过 SSE 刷新。
- **补充扫描回归测试**：覆盖未变化归档不打开、大小变化触发重扫、fast scan 不打开归档，以及 KOReader binary hash 开关行为。

### 📌 增量记录 — 2026-05-22（Dashboard 与筛选懒加载）

#### 首页性能
- **Dashboard 首屏请求瘦身**：首屏只加载核心统计、资源库、最近阅读和活跃热力图；任务摘要、KOReader 概览、性能诊断延后到 idle 或进入视口后加载。
- **AI 推荐进入视口再请求**：Dashboard 推荐区不再随首屏自动触发 LLM 推荐，用户滚动到推荐区或手动刷新时再请求。
- **资源库筛选项按需加载**：资源库页不再默认拉取全量标签和作者，只有展开筛选面板或已有标签/作者筛选需要回显时才加载。
- **标签/作者筛选改为远程搜索**：新增 `/api/tags/search` 与 `/api/authors/search`，筛选面板默认只取热门前 30 个，输入关键字后 debounce 搜索，避免大库筛选一次性拉全量元数据。
- **Dashboard 核心统计增加 TTL 缓存**：`/api/stats/dashboard` 30 秒内重复请求直接返回内存快照，阅读进度、批量已读/未读、资源库变更和扫描事件会失效缓存，扫描完成后后台预热。

### 📌 增量记录 — 2026-05-22（外部协议按需启用）

#### 默认核心路径收敛
- **新增外部协议开关**：`protocols.opds.enabled` 与 `protocols.mihon.enabled` 默认关闭，外部客户端能力需要在设置页连接中心显式启用。
- **协议路由运行时拒绝**：OPDS/Mihon 关闭时对应入口返回 404，避免默认本地 Web 模式暴露外部目录协议。
- **连接中心按开关展示端点**：关闭的 OPDS/Mihon/KOReader 不再显示可复制端点和二维码，连接中心顶部提供 OPDS/Mihon 快速开关。
- **健康报告跳过关闭的 KOReader 诊断**：KOReader 关闭时 `/api/health/report` 不再计算 `unmatched_koreader`，降低维护页非核心查询负担。
- **协议查询独立优化**：OPDS 搜索和简单 Mihon 关键字搜索优先走 Bleve 搜索索引，最近添加 feed 改用 `series_stats` 封面读模型，继续阅读列表改用 `series_stats.last_read_*`，避免协议请求重复窗口函数和逐行封面子查询。

### 📌 增量记录 — 2026-05-22（性能观测与基准体系）

#### 性能可观测
- **页图请求归因补齐**：请求诊断新增 `archive_open`、`manifest_cache_hit`、`raw_passthrough`、`processed`，可区分归档打开、页清单缓存命中、默认原图透传和服务端处理路径。
- **Dashboard 性能摘要增强**：系统性能接口聚合页图归档打开次数、manifest 命中数、原图透传数和服务端处理页数，Dashboard 性能面板同步展示关键比例与计数。
- **扫描完成日志新增计数**：资源库/系列扫描完成日志输出发现、跳过、处理、打开归档和 hash 文件计数，便于验证增量扫描是否真正不打开归档、不计算 hash。
- **扩展查询计划检查**：`cmd/queryplan` 覆盖首页默认与 name/created/favorite 排序、系列详情 books、recent read 和 dashboard stats 查询形状。
- **首页深页分页支持 cursor**：`/api/series/search` 在保留 page number 兼容的同时返回 `next_cursor` / `has_more`，资源库连续翻页在 name、updated、created、favorite 排序下改用 keyset/cursor，直接跳页仍走原分页路径。
- **补强大库查询计划基线**：样本库生成后自动回填派生读模型，最近阅读固定走 `series_stats.last_read_at` 索引，Dashboard 库大小统计走 `books(library_id, size)` 聚合索引；新增 1 万和 10 万系列本地基线记录。
- **Webtoon 前端观测与释放**：Webtoon 虚拟列表按当前渲染窗口释放远离视口的 object URL，并通过浏览器事件输出当前 DOM 图片数量，便于验证长漫画窗口化效果。
- **新增前端首屏与列表渲染观测**：浏览器端记录每次路由首屏采样窗口内的 API 请求数，并记录资源库列表从请求到渲染完成的耗时；Dashboard 性能面板同步展示最近一次观测值。
- **新增性能基线模板**：`docs/performance-baseline-template.md` 统一记录 benchmark、queryplan、请求指标、扫描指标和前端观测数据。

### 📌 增量记录 — 2026-05-21（阶段 39 系统性能摘要）

#### 性能可观测
- **新增系统性能摘要接口**：`/api/system/performance` 基于最近请求诊断样本聚合总请求、错误、慢请求、平均耗时、P95、最大耗时、协议分布和热点路由。
- **Dashboard 增加性能面板**：仪表板展示慢请求率、错误率、热点路由、最近慢请求和错误请求，便于直接定位阅读、OPDS、Mihon 或 KOReader 的性能异常。
- **补充性能摘要测试**：覆盖请求聚合、协议分类、慢请求/错误提取和热点路由排序。
- **新增阶段交付文档**：`docs/phase-39-system-performance-summary.md` 记录阶段 39 的交付范围、边界与验证命令。

### 📌 增量记录 — 2026-05-21（阶段 40 页图性能归因）

#### 性能可观测
- **补齐页图请求归因字段**：请求诊断与结构化日志增加 `cache_hit`、`cache_source`、`book_id`、`page_number` 和 `transform`，满足北极星性能计划中 API 基础耗时字段要求。
- **增强系统性能摘要**：`/api/system/performance` 增加总缓存命中、页图请求数、页图缓存命中数和页图处理画像聚合。
- **Dashboard 展示页图缓存命中率**：性能面板新增页图缓存命中率与处理画像列表，可区分原图、格式转换、缩放、滤镜、自动裁边和 AI 参数请求。
- **补充请求归因测试**：覆盖请求上下文标注、日志字段输出、性能摘要缓存命中聚合和处理画像排序。

### 📌 增量记录 — 2026-05-08（北极星性能与维护改造）

#### 阅读路径性能
- **新增处理后页面磁盘缓存**：服务端缩放、裁切和格式转换后的页面会落到磁盘缓存，重启后仍可复用，降低弱网和重复阅读的转码成本。
- **新增页面图片 ETag**：页面图片响应支持 `ETag` / `If-None-Match`，重复请求可直接返回 304，减少不必要的图片传输。
- **支持归档池热调整**：归档池初始化逻辑支持按配置 resize，调整 `archive_pool_size` 后不再只能依赖首次启动值。
- **新增结构化请求耗时日志**：API、OPDS 和 KOReader 请求会记录状态码、耗时、响应字节数和路由信息，错误与慢请求自动提升日志等级，静态资源成功请求默认降噪。

#### 任务与维护
- **新增任务状态持久化**：后台任务会写入数据库，服务重启后历史仍可查询，运行中任务会标记为中断并保留重试入口。
- **增强任务清理筛选**：任务清理支持按状态、范围、类型和目标对象过滤，避免清理无关任务历史。
- **增强任务中心筛选与清理**：日志页任务中心支持按任务类型、范围和目标 ID 筛选，并可按当前非运行筛选结果清理任务记录；服务重启中断任务会独立统计和提示。
- **新增阅读缓存维护入口**：设置页维护工具可查看处理后页面缓存的文件数、占用空间和目录，并一键清理磁盘与内存缓存。

#### 阶段 1 收口
- **新增性能基准**：补充归档页面读取、图片处理和大库分页查询 benchmark，便于后续发布前检查性能回归。
- **新增阶段交付文档**：`docs/phase-1-performance-reliability.md` 记录阶段 1 已交付范围、基准运行命令、参考样例和后续阶段入口。
- **阅读器初步模块化**：将阅读页图 URL、object URL 缓存、预加载请求去重逻辑抽取到 `usePageImageCache`，降低后续拆分阅读器的风险。

### 📌 增量记录 — 2026-05-08（阶段 2 整理闭环 MVP）

#### 馆藏健康诊断
- **新增健康报告 API**：`/api/health/report` 可按资源库、问题类型和数量限制返回馆藏健康摘要与问题列表。
- **覆盖首批整理问题类型**：支持识别空页书籍、缺失封面、缺失元数据、重复文件哈希和 KOReader 未匹配记录。
- **补充健康报告测试**：数据库聚合和 API 参数校验均增加测试，降低后续扩展整理规则时的回归风险。

#### 整理工作台
- **新增整理工作台页面**：侧边栏增加“整理工作台”入口，可集中查看健康摘要、筛选资源库与问题类型，并搜索系列、书籍、路径或详情。
- **支持问题定位跳转**：问题条目可直接进入系列、阅读器、KOReader 设置或日志页，缩短从发现异常到定位对象的路径。
- **支持安全维护动作**：缺元数据可提交刮削任务，空页和缺封面可提交系列重扫任务，KOReader 未匹配可提交进度重关联任务；当前阶段不自动移动或删除文件。
- **新增阶段交付文档**：`docs/phase-2-organize-loop.md` 记录整理闭环的交付边界、健康规则、验证命令和下一阶段入口。

### 📌 增量记录 — 2026-05-08（阶段 3 智能筛选视图数据库化）

#### 智能筛选视图
- **新增智能筛选视图数据表**：常用筛选、排序和分页配置按资源库保存到 SQLite，不再只依赖当前浏览器的 localStorage。
- **新增智能筛选 API**：支持列出、保存和删除资源库智能筛选视图；同名视图会更新旧规则，参数会校验排序字段、方向和分页大小。
- **首页接入后端视图**：资源库首页加载后端保存的筛选视图，保存和删除动作支持失败回滚。
- **支持旧本地视图导入**：首次进入资源库时会把旧版 localStorage 视图导入数据库，升级后可跨浏览器和设备复用。
- **新增阶段交付文档**：`docs/phase-3-smart-filters.md` 记录阶段 3 的交付范围、边界、验证命令和下一阶段入口。

### 📌 增量记录 — 2026-05-09（阶段 4 多端连接中心 MVP）

#### 客户端连接中心
- **新增客户端连接信息 API**：`/api/system/client-connections` 返回当前访问基址、OPDS、OpenSearch、Mihon 和 KOReader Sync 入口，以及 KOReader 账号与匹配模式状态。
- **增强反向代理地址识别**：连接信息会优先使用 `X-Forwarded-Proto`、`X-Forwarded-Host` 和 `X-Forwarded-Port` 推导外部 URL，减少反代部署下手动拼接地址的成本。
- **新增设置页连接中心**：设置页增加“连接中心”分区，可查看并复制 OPDS、Mihon 和 KOReader 端点，概览页也增加对应入口。
- **补充连接中心测试**：覆盖连接信息 API、外部基址推导和 KOReader 账号统计。
- **新增阶段交付文档**：`docs/phase-4-client-connections.md` 记录阶段 4 的交付范围、边界、验证命令和下一阶段入口。

### 📌 增量记录 — 2026-05-09（阶段 5 文件身份与去重预演）

#### 文件身份底座
- **新增 quick_hash 文件身份字段**：扫描时会为书籍补写快速内容指纹，数据库与健康报告可直接识别缺失和重复的 quick hash。
- **新增文件身份重建任务**：设置页维护工具和整理工作台都可一键启动文件身份索引重建，后台会逐批补齐旧书籍的 quick hash。
- **新增重复预览项**：健康报告新增缺 quick hash 与重复 quick hash 两类问题，便于在迁移、重命名或清理前先做安全审查。

#### 整理工作台与维护入口
- **整理工作台新增文件身份面板**：展示 quick hash 覆盖率、重复 quick hash 组和重复文件哈希，并提供重建入口。
- **维护工具新增文件身份按钮**：设置页维护工具与现有索引、缩略图和批量刮削任务并列，方便集中执行后台修复。

#### 阶段交付文档
- **新增阶段交付文档**：`docs/phase-5-file-identity.md` 记录阶段 5 的交付范围、入口、验证命令和下一阶段入口。

### 📌 增量记录 — 2026-05-09（阶段 6 元数据审核与来源追踪）

#### 元数据审核管线
- **新增元数据审核队列**：Bangumi/LLM 刮削结果不再静默覆盖系列字段，而是写入 `metadata_reviews` 和字段级差异，等待人工应用或拒绝。
- **新增字段来源追踪**：已应用字段写入 `series_metadata_provenance`，记录 source、source_url、confidence 和关联 review，旧有系列元数据会回填为 manual 来源。
- **Provider 输出标准化**：元数据提供方统一携带 Provider、SourceURL 和 Confidence；LLM 提示词要求返回置信度，Bangumi 使用条目完整度和排名给出启发式置信度。
- **新增审核 API**：系列详情可读取待审核项与来源记录，并支持应用/拒绝审核项。

#### 系列详情交互
- **新增元数据审核面板**：系列详情页展示待审核字段、锁定状态、当前值、建议值、来源链接和置信度。
- **调整刮削应用流程**：搜索弹窗中的候选条目改为“加入审核队列”，首页与详情页的刮削成功提示同步为待审核状态。
- **来源可视化增强**：候选搜索结果和预览区展示 Provider、SourceURL 与 Confidence，便于用户判断可信度。

#### 阶段交付文档
- **新增阶段交付文档**：`docs/phase-6-metadata-review.md` 记录阶段 6 的交付范围、验证命令和下一阶段入口。

### 📌 增量记录 — 2026-05-09（阶段 7 全局元数据审核收件箱）

#### 审核收件箱
- **新增全局元数据审核 API**：`/api/metadata/reviews` 支持按资源库、来源和关键词查看所有待审核项，并返回系列、资源库、封面、字段数量和锁定字段数量。
- **新增批量审核操作**：支持批量应用和批量拒绝待审核项；批量应用提供 `fill_empty` 安全模式，只填充当前为空的字段。
- **保留锁定字段保护**：无论单项还是批量应用，锁定字段仍不会被覆盖，也不会写入错误来源记录。

#### 前端工作流
- **新增“元数据审核”页面**：侧边栏增加入口，可集中筛选、查看字段差异、选择本页、批量应用或拒绝审核项。
- **默认安全批量策略**：页面默认使用“只填空字段”模式，降低批量处理污染手工元数据的风险。
- **补充审核可视化信息**：列表展示来源、置信度、字段数、锁定字段、来源链接和当前/建议值对比。

#### 阶段交付文档
- **新增阶段交付文档**：`docs/phase-7-metadata-review-inbox.md` 记录阶段 7 的交付范围、验证命令和后续入口。

### 📌 增量记录 — 2026-05-09（阶段 8 AI 分组审核队列）

#### AI 分组审核
- **新增 AI 分组审核表**：AI 分组结果写入 `ai_grouping_reviews` 与候选合集明细，不再直接创建真实合集。
- **新增审核 API**：`/api/ai-grouping/reviews` 支持按资源库和状态分页查看审核单，并支持应用或拒绝 pending 审核。
- **显式应用才写合集**：应用审核时才创建 `collections` 并关联系列；拒绝审核不会产生合集。
- **加固候选清洗**：自动丢弃空名称、非候选系列、重复系列和空候选合集，避免 LLM 幻觉污染馆藏结构。

#### 前端工作流
- **新增“AI 分组审核”页面**：侧边栏增加入口，可查看审核单、候选合集、候选系列和状态，并执行应用或拒绝。
- **调整任务提示文案**：资料库 AI 分组入口明确生成的是审核计划，应用前不会修改合集。

#### 阶段交付文档
- **新增阶段交付文档**：`docs/phase-8-ai-grouping-review.md` 记录阶段 8 的交付范围、关键行为、验证命令和后续入口。

### 📌 增量记录 — 2026-05-09（阶段 9 AI 分组精修与合集来源）

#### 审核精修
- **新增候选合集编辑 API**：AI 分组审核中的单个候选合集支持修改名称、描述和包含系列。
- **新增候选合集单项处理**：支持只应用或拒绝某一个候选合集，不必整单一次性处理。
- **自动收口审核单状态**：候选合集全部处理后，系统会根据是否创建过真实合集自动标记 applied 或 rejected。
- **加固候选编辑边界**：编辑候选合集时只能使用同一审核单内出现过的系列 ID，避免跨审核污染。

#### 合集来源
- **新增合集来源字段**：`collections` 记录 `source_type` 和 `source_review_id`，手工合集默认为 manual，AI 分组应用生成的合集标记为 ai_grouping。
- **合集页展示来源标签**：合集管理页可区分手工创建和 AI 分组生成的合集。

#### 阶段交付文档
- **新增阶段交付文档**：`docs/phase-9-ai-grouping-curation.md` 记录阶段 9 的交付范围、关键行为、验证命令和后续入口。

### 📌 增量记录 — 2026-05-09（阶段 10 规则型合集与统一合集视图）

#### 规则型合集
- **新增统一合集视图 API**：`/api/collection-views` 同时返回手工合集、AI 分组生成合集和智能筛选规则合集。
- **新增规则合集成员 API**：`/api/collection-views/smart/{filterId}/series` 复用保存的智能筛选规则动态返回匹配系列。
- **保持动态语义**：规则型合集不写入 `collection_series`，成员由标签、作者、状态、首字母和排序规则实时计算。

#### 合集页整合
- **合集管理页接入统一视图**：左侧列表可同时浏览静态合集、AI 合集和智能合集。
- **规则合集只读展示**：选中智能合集时展示动态成员，并隐藏编辑、删除和移除成员等静态合集操作。
- **新增来源标签**：合集来源标签扩展为 manual、ai_grouping、smart_filter。

#### 阶段交付文档
- **新增阶段交付文档**：`docs/phase-10-smart-collections.md` 记录阶段 10 的交付范围、关键行为、验证命令和后续入口。

### 📌 增量记录 — 2026-05-09（阶段 11 规则合集编辑与快照固化）

#### 规则合集维护
- **新增规则合集编辑 API**：`PUT /api/smart-filters/{filterId}` 支持按 ID 更新保存的智能筛选规则。
- **合集页支持编辑智能合集**：可直接修改名称、标签、作者、状态、首字母、排序和分页规则。
- **合集页支持删除智能合集**：删除保存的规则，不影响漫画文件和已固化快照。

#### 快照固化
- **新增智能合集快照 API**：`POST /api/collection-views/smart/{filterId}/snapshot` 将当前动态成员复制成普通合集。
- **新增快照来源标记**：快照合集写入 `source_type = smart_snapshot`，便于和手工合集、AI 合集、动态规则合集区分。
- **限制超大误操作**：快照默认最多复制 1000 个系列，并在响应中返回是否截断。

#### 阶段交付文档
- **新增阶段交付文档**：`docs/phase-11-smart-collection-curation.md` 记录阶段 11 的交付范围、关键行为、验证命令和后续入口。

### 📌 增量记录 — 2026-05-09（阶段 12 OPDS/Mihon 合集网关）

#### 外部阅读入口
- **OPDS 根目录新增合集入口**：`/opds/v1.2/` 现在可直接进入统一合集导航。
- **新增 OPDS 合集 Feed**：`/opds/v1.2/collections` 暴露手工合集、AI 分组合集、智能快照和动态规则合集。
- **新增 OPDS 合集成员 Feed**：静态合集通过 `/opds/v1.2/collections/{collectionId}` 浏览，动态规则合集通过 `/opds/v1.2/smart-collections/{filterId}` 实时浏览。
- **新增 Mihon 合集 API**：`/api/mihon/v1/collections` 返回统一合集视图及来源类型。
- **新增 Mihon 合集成员 API**：静态合集与动态规则合集均支持分页返回系列列表，响应复用现有 Mihon 系列结构。

#### 统一数据来源
- **复用统一合集视图模型**：Web、OPDS 和 Mihon 共用同一套合集读取逻辑，避免不同客户端看到的合集范围不一致。
- **保持动态/静态语义**：智能快照按固定成员暴露，动态规则合集每次按 `smart_filters` 实时计算。
- **补充协议测试**：新增 OPDS 与 Mihon 测试覆盖合集列表、静态合集成员和动态规则合集成员。

#### 阶段交付文档
- **新增阶段交付文档**：`docs/phase-12-client-collection-gateway.md` 记录阶段 12 的交付范围、关键行为、验证命令和后续入口。

### 📌 增量记录 — 2026-05-09（阶段 13 连接中心合集入口补强）

#### 连接中心
- **连接端点新增分类字段**：`/api/system/client-connections` 的端点项增加 `category`，区分目录入口、合集入口和同步入口。
- **新增 OPDS 合集连接项**：连接中心可直接复制 `/opds/v1.2/collections`。
- **新增 Mihon 合集连接项**：连接中心可直接复制 `/api/mihon/v1/collections`。
- **设置页按用途分组展示端点**：目录入口、合集入口和同步入口分区展示，并标出每组端点数量。

#### 回归保障
- **补充连接中心测试**：覆盖新增合集端点 URL、分类字段和 KOReader 同步端点分类。
- **新增阶段交付文档**：`docs/phase-13-connection-center-collections.md` 记录阶段 13 的交付范围、关键行为、验证命令和后续入口。

### 📌 增量记录 — 2026-05-09（阶段 14 规则合集高级条件）

#### 智能合集规则
- **新增高级规则字段**：智能合集支持阅读状态、评分区间、阅读进度区间和最近新增时间窗口。
- **增强规则校验**：后端校验阅读状态枚举、评分范围、进度范围、最近新增天数和区间顺序。
- **统一动态成员查询**：智能合集成员改为专用查询，Web、快照、OPDS 和 Mihon 动态合集共用同一套高级规则结果。
- **统一命中数量计算**：`/api/collection-views` 中智能合集的 `series_count` 会同步应用高级规则。

#### 合集页维护
- **智能合集编辑弹窗新增高级条件**：可直接维护未读/阅读中/已完成、评分、进度和最近新增窗口。
- **保留旧筛选兼容**：首页保存的旧版智能筛选仍可继续使用，高级规则主要在合集页维护。

#### 回归保障
- **补充高级规则测试**：覆盖字段保存、校验和动态成员过滤。
- **新增阶段交付文档**：`docs/phase-14-smart-collection-rules.md` 记录阶段 14 的交付范围、关键行为、验证命令和后续入口。

### 📌 增量记录 — 2026-05-09（阶段 15 智能快照预览与安全确认）

#### 快照预览
- **新增智能合集快照预览 API**：`GET /api/collection-views/smart/{filterId}/snapshot-preview` 返回命中总数、预览样本、实际快照上限、预计创建数量和是否截断。
- **新增同名合集提醒**：预览接口会按普通合集名称检测冲突，便于创建前识别重复命名；同名不阻塞创建，保持整理流程灵活。
- **统一快照上限策略**：预览与创建共同使用 1000 个系列的快照上限，避免前端确认内容和实际落库结果不一致。

#### 合集页交互
- **快照弹窗新增预览面板**：创建前展示当前规则命中数量、将复制数量、上限和首批命中系列封面。
- **快照风险显式提示**：当结果会被截断或名称已存在时，弹窗直接展示风险提示；无命中结果时禁用创建按钮。
- **保留动态语义**：预览只读取当前智能合集规则，不写入静态合集；点击创建后才固化为 `smart_snapshot` 合集。

#### 回归保障
- **补充快照预览测试**：覆盖命中数量、预览数量、截断标记、同名冲突和参数校验。
- **新增阶段交付文档**：`docs/phase-15-smart-snapshot-preview.md` 记录阶段 15 的交付范围、接口契约、验证命令和后续入口。

### 📌 增量记录 — 2026-05-09（阶段 16 连接中心扫码与诊断矩阵）

#### 客户端接入诊断
- **连接端点新增诊断字段**：`/api/system/client-connections` 的端点项增加 `client_type`、`health`、`auth_note` 和 `diagnostics`，前端可直接区分就绪、需账号和关闭状态。
- **KOReader 接入状态更明确**：KOReader 端点会根据服务开关与已启用账号数返回 `disabled`、`needs_account` 或 `ready`，避免用户拿到地址后才在设备侧排错。
- **设置页新增端点健康概览**：连接中心展示可用端点数、需处理端点数和检测到的外部基址，并支持单独复制基址。

#### 扫码配置
- **端点卡片新增本地 QR 码**：OPDS、Mihon、合集和 KOReader 连接卡片都可直接扫码，手机或阅读器无需手动输入长 URL。
- **保留复制与打开兜底**：每个端点仍保留复制地址与新窗口打开动作，方便桌面客户端配置和浏览器调试。
- **端点卡片展示认证提示与诊断建议**：连接页直接提示是否需要 KOReader 账号、当前匹配模式以及反向代理排查方向。

#### 文档
- **新增兼容矩阵文档**：`docs/client-compatibility-matrix.md` 记录 OPDS、Mihon 和 KOReader 的端点映射、客户端能力与诊断步骤。
- **新增阶段交付文档**：`docs/phase-16-connection-diagnostics.md` 记录阶段 16 的交付范围、边界、验证命令和后续入口。

### 📌 增量记录 — 2026-05-13（阶段 17 OPDS/Mihon Feed 扩展）

#### 外部客户端书架
- **新增 OPDS 最近添加 Feed**：`/opds/v1.2/recent` 按系列入库时间倒序返回最近添加系列，支持 `page`、`limit` 和 `libraryId`。
- **新增 OPDS 阅读清单 Feed**：`/opds/v1.2/reading-lists` 返回阅读清单导航，`/opds/v1.2/reading-lists/{listId}` 分页返回清单成员系列。
- **OPDS 根目录补强**：根目录新增“最近添加”和“阅读清单”入口，外部客户端可直接发现更细书架。
- **新增 Mihon 最近添加与继续阅读接口**：`/api/mihon/v1/recently-added` 返回最近添加系列分页，`/api/mihon/v1/continue` 返回最近阅读书籍列表。
- **新增 Mihon 阅读清单接口**：`/api/mihon/v1/reading-lists` 和 `/api/mihon/v1/reading-lists/{listId}/series` 暴露有序阅读清单。
- **连接中心新增协议入口**：OPDS/Mihon 最近添加、继续阅读和阅读清单端点进入连接中心，可复制和扫码。
- **补充协议回归测试**：新增 OPDS 与 Mihon 测试覆盖最近添加、继续阅读、阅读清单列表和清单成员分页。
- **新增阶段交付文档**：`docs/phase-17-opds-mihon-feed-expansion.md` 记录阶段 17 的交付范围、关键行为、验证命令和后续入口。

### 📌 增量记录 — 2026-05-13（阶段 18 连接中心真实请求诊断）

#### 客户端请求可观测
- **新增请求诊断环形缓冲**：请求指标中间件会保留最近 300 条 API/协议请求的时间、路径、路由、状态码、响应字节数、耗时和远端地址。
- **连接端点新增真实请求统计**：`/api/system/client-connections` 的每个端点新增 `requests` 字段，包含总请求数、成功数、警告数、错误数、慢请求数和最后命中信息。
- **连接中心展示最近请求面板**：每个 OPDS/Mihon/KOReader 端点卡片展示请求总数、错误数、慢请求数、最后命中路径和最近 5 条匹配请求。
- **支持 KOReader 自定义路径诊断**：端点请求归类按当前 `koreader.base_path` 匹配，不再只依赖默认 `/koreader` 路径。
- **补充请求诊断回归测试**：覆盖 API 请求记录、静态资源跳过、自定义 KOReader 路径记录和连接中心端点聚合。
- **新增阶段交付文档**：`docs/phase-18-connection-request-diagnostics.md` 记录阶段 18 的交付范围、关键行为、验证命令和后续入口。

### 📌 增量记录 — 2026-05-13（阶段 19 KOReader 设备与冲突诊断）

#### KOReader 设备可观测
- **新增设备诊断 API**：`/api/system/koreader/devices` 返回 KOReader 设备摘要、设备健康列表和最近冲突队列。
- **按设备聚合同步健康**：按账号、设备名和设备 ID 统计总记录、已匹配、未匹配、最后同步时间、最新文档、最近错误和匹配方式分布。
- **新增冲突队列**：聚合未匹配进度与非成功同步事件，并返回归一化匹配键、状态、消息、严重级别和处理建议。
- **设置页新增设备诊断视图**：KOReader 设置页展示设备健康指标、每台设备的匹配分布和最近冲突，缩短跨设备同步排查路径。
- **加固 SQLite 时间兼容**：设备聚合查询兼容 `MAX(updated_at)` 返回字符串的情况，避免诊断接口在旧驱动行为下 500。
- **补充设备诊断回归测试**：覆盖设备聚合、未匹配记录、认证错误冲突和建议返回。
- **新增阶段交付文档**：`docs/phase-19-koreader-device-diagnostics.md` 记录阶段 19 的交付范围、关键行为、验证命令和后续入口。

### 📌 增量记录 — 2026-05-13（阶段 20 OPDS Page Streaming 兼容补强）

#### OPDS 流式阅读
- **新增 OPDS-PSE stream 链接**：OPDS 书籍条目新增 `http://vaemendis.net/opds-pse/stream` 链接，支持客户端按页请求漫画图片。
- **兼容 OPDS-PSE 1.0 与 1.1 属性**：stream link 同时输出 `count`、`pse:count`，继续阅读条目输出 `pse:lastRead`。
- **新增 OPDS 专用页流路由**：`/opds/v1.2/books/{bookId}/pages/{pageNumber}` 使用 OPDS-PSE 0 基页码，并复用现有缓存、图片处理和 ETag。
- **保留传统 acquisition 链接**：不支持 Page Streaming 的 OPDS 客户端仍可使用旧首图 acquisition 链接。
- **补充连接中心诊断说明**：OPDS 根入口提示书籍条目已包含 Page Streaming 能力。
- **补充协议回归测试**：覆盖 stream link 属性、继续阅读 `lastRead` 和 OPDS 页流路由。
- **新增阶段交付文档**：`docs/phase-20-opds-page-streaming.md` 记录阶段 20 的交付范围、关键行为、验证命令和后续入口。

### 📌 增量记录 — 2026-05-13（阶段 21 PWA 离线阅读缓存 MVP）

#### 离线阅读闭环
- **增强 Service Worker 阅读缓存策略**：阅读页和书籍信息支持网络优先、缓存兜底，非阅读 API 和 SSE 仍不进入离线缓存。
- **新增书籍级离线缓存工具**：阅读器可缓存当前书籍的书籍信息、阅读器路由和当前图像配置下的全部页图。
- **阅读器新增离线阅读面板**：设置面板展示在线/离线状态、缓存页数、缓存时间、图像配置，并支持缓存当前书籍和移除缓存。
- **新增离线进度排队**：断网或请求无响应时，阅读进度写入本地队列，浏览器恢复联网后自动同步到后端。
- **保留当前边界**：本阶段不做全局离线书架管理，也不排队书签等写操作。
- **新增阶段交付文档**：`docs/phase-21-pwa-offline-reading.md` 记录阶段 21 的交付范围、关键行为、验证命令和后续入口。

### 📌 增量记录 — 2026-05-13（阶段 22 离线书架管理与缓存容量统计）

#### PWA 离线书架
- **新增离线书架页面**：主路由 `/offline` 集中展示当前浏览器已缓存的离线书籍，并在侧边栏增加入口。
- **新增离线缓存统计**：页面展示离线书籍数、已缓存页数、离线缓存体积和浏览器存储占用比例。
- **支持离线缓存管理**：可从离线书架打开对应阅读器、逐本移除缓存或清空全部离线缓存。
- **增强离线缓存工具**：新增离线书籍列表、Cache Storage 体积估算、浏览器配额读取和全量清理能力。
- **预缓存离线书架入口**：Service Worker 预缓存 `/offline`，让已访问过的离线管理入口更稳定可达。
- **新增阶段交付文档**：`docs/phase-22-offline-shelf-management.md` 记录阶段 22 的交付范围、关键行为、验证命令和后续入口。

### 📌 增量记录 — 2026-05-13（阶段 23 离线同步队列可视化与手动恢复）

#### PWA 离线写入恢复
- **新增离线同步队列面板**：`/offline` 展示断网期间保存在当前浏览器的待同步阅读进度。
- **支持手动恢复操作**：离线书架可立即同步队列、丢弃单条待同步记录或清空全部待同步进度。
- **增强同步容错**：离线进度同步改为逐条尝试，失败项会保留在队列中，并返回成功、失败和剩余数量。
- **增强队列可读性**：阅读器排队进度时写入当前书名，离线书架能展示书名、页码和记录时间。
- **新增阶段交付文档**：`docs/phase-23-offline-sync-queue.md` 记录阶段 23 的交付范围、关键行为、验证命令和当前边界。

### 📌 增量记录 — 2026-05-13（阶段 24 阅读器侧栏面板模块化一期）

#### 阅读器可维护性
- **拆出离线阅读面板**：`OfflineReadingPanel` 承接阅读器设置侧栏中的离线状态、缓存进度、缓存本书和移除缓存 UI。
- **拆出书签面板**：`BookmarkPanel` 承接书签备注、保存/更新、跳页和删除 UI。
- **共享书签类型**：`ReadingBookmark` 上移到 `book-reader/types.ts`，避免面板组件重复声明接口。
- **保留无数据库边界**：本阶段不做页面列表持久化，不修改 SQL/schema/sqlc，只推进前端模块化。
- **新增阶段交付文档**：`docs/phase-24-reader-panel-modularization.md` 记录阶段 24 的交付范围、验证命令和后续拆分边界。

### 📌 增量记录 — 2026-05-13（阶段 25 阅读器导航与进度托盘模块化）

#### 阅读器导航辅助拆分
- **拆出快捷键帮助面板**：`ReaderHelpPanel` 承接快捷键、移动端说明和排障提示 UI。
- **拆出底部进度托盘**：`ReaderProgressTray` 承接页码显示、hover 页码预览、拖动条状态和提交跳页动作。
- **复用现有跳页行为**：进度托盘继续调用 `jumpToPage`，保持分页模式与 webtoon 模式的跳转语义一致。
- **保留无数据库边界**：本阶段不做页面列表持久化，不修改 SQL/schema/sqlc，只推进阅读器前端模块化。
- **新增阶段交付文档**：`docs/phase-25-reader-navigation-modularization.md` 记录阶段 25 的交付范围、验证命令和后续拆分边界。

### 📌 增量记录 — 2026-05-13（阶段 26 阅读器顶部工具栏模块化）

#### 阅读器顶部操作拆分
- **拆出顶部工具栏组件**：`ReaderTopBar` 承接返回、居中书名、书签快捷按钮、帮助按钮和设置按钮。
- **收敛主阅读器 JSX**：`BookReader.tsx` 顶部悬浮区只保留组件挂载和状态回调，继续降低后续阅读器迭代成本。
- **补齐设置按钮文案**：新增 `reader.settings` 中英文文案，避免设置按钮 tooltip 直接显示缺失 key。
- **保留无数据库边界**：本阶段不做页面列表持久化，不修改 SQL/schema/sqlc，只推进阅读器前端模块化。
- **新增阶段交付文档**：`docs/phase-26-reader-topbar-modularization.md` 记录阶段 26 的交付范围、验证命令和后续拆分边界。

### 📌 增量记录 — 2026-05-13（阶段 27 阅读器设置抽屉模块化）

#### 阅读器设置区拆分
- **拆出设置抽屉组件**：`ReaderSettingsDrawer` 承接布局、图像、网络传输、超分、预加载、离线阅读、书签和护眼模式 UI。
- **复用既有面板组件**：设置抽屉内部继续组合 `OfflineReadingPanel` 和 `BookmarkPanel`，保持离线与书签行为不变。
- **主阅读器继续收敛**：`BookReader.tsx` 设置区域变为单个组件挂载，继续降低后续拆分 `PagedReader` / `WebtoonReader` 的风险。
- **保留无数据库边界**：本阶段不做页面列表持久化，不修改 SQL/schema/sqlc，只推进阅读器前端模块化。
- **新增阶段交付文档**：`docs/phase-27-reader-settings-drawer-modularization.md` 记录阶段 27 的交付范围、验证命令和后续拆分边界。

### 📌 增量记录 — 2026-05-13（阶段 28 阅读器主体渲染区模块化）

#### 阅读器渲染区拆分
- **拆出瀑布流阅读组件**：`WebtoonReader` 承接图片列表、滤镜、缩放样式和下一本入口。
- **拆出分页阅读组件**：`PagedReader` 承接左右点击区、拖拽容器、单页/双页渲染、双页重叠修正和 loading 占位。
- **主阅读器继续收敛**：`BookReader.tsx` 阅读主体区变为 `WebtoonReader` / `PagedReader` 二选一挂载，继续降低主文件复杂度。
- **保留无数据库边界**：本阶段不做页面列表持久化，不修改 SQL/schema/sqlc，只推进阅读器前端模块化。
- **新增阶段交付文档**：`docs/phase-28-reader-surface-modularization.md` 记录阶段 28 的交付范围、验证命令和后续拆分边界。

### 📌 增量记录 — 2026-05-13（阶段 29 阅读器状态视图模块化）

#### 阅读器状态视图拆分
- **拆出护眼遮罩**：`ReaderEyeProtectionOverlay` 承接护眼模式覆盖层。
- **拆出加载态**：`ReaderLoadingState` 承接阅读器加载 spinner。
- **拆出错误态**：`ReaderErrorState` 承接加载失败、重试和返回系列操作。
- **主阅读器继续收敛**：`BookReader.tsx` 主体区域只保留状态分支、阅读模式组件挂载和进度托盘。
- **保留无数据库边界**：本阶段不做页面列表持久化，不修改 SQL/schema/sqlc，只推进阅读器前端模块化。
- **新增阶段交付文档**：`docs/phase-29-reader-state-views-modularization.md` 记录阶段 29 的交付范围、验证命令和后续拆分边界。

### 📌 增量记录 — 2026-05-13（阶段 30 阅读器书签状态 Hook 化）

#### 阅读器书签数据拆分
- **新增书签 Hook**：`useReaderBookmarks` 承接书签加载、保存、删除、当前页书签识别和备注同步。
- **保留切书防护**：书签请求完成后继续校验当前书籍 ID，避免旧请求覆盖新书籍状态。
- **主阅读器继续收敛**：`BookReader.tsx` 不再直接维护书签数组、备注和保存中状态，只消费 hook 暴露的状态与动作。
- **保留无数据库边界**：本阶段不做页面列表持久化，不修改 SQL/schema/sqlc，只推进阅读器前端状态模块化。
- **新增阶段交付文档**：`docs/phase-30-reader-bookmark-hook.md` 记录阶段 30 的交付范围、验证命令和后续拆分边界。

### 📌 增量记录 — 2026-05-13（阶段 31 阅读器离线状态 Hook 化）

#### 阅读器离线数据拆分
- **新增离线阅读 Hook**：`useReaderOffline` 承接离线能力检测、当前书籍缓存状态、缓存进度、删除状态、错误信息和联网状态。
- **收敛同步队列处理**：浏览器恢复联网时的离线进度同步、当前书籍队列页码刷新和手动排队入口统一由 hook 维护。
- **主阅读器继续收敛**：`BookReader.tsx` 不再直接导入 `offlineReader` 工具，只保留阅读进度上报失败时的排队调用。
- **保留无数据库边界**：本阶段不做页面列表持久化，不修改 SQL/schema/sqlc，只推进阅读器前端状态模块化。
- **新增阶段交付文档**：`docs/phase-31-reader-offline-hook.md` 记录阶段 31 的交付范围、验证命令和后续拆分边界。

### 📌 增量记录 — 2026-05-13（阶段 32 阅读器书籍数据 Hook 化）

#### 阅读器书籍数据拆分
- **新增书籍数据 Hook**：`useReaderBookData` 承接页列表加载、书籍信息加载、下一本查询、切书重置和阅读进度恢复。
- **保留归属保护**：`pagesBookIdRef` 继续保护阅读进度上报，避免旧页列表提交到新书籍。
- **主阅读器继续收敛**：`BookReader.tsx` 不再直接维护页列表、加载错误、书名卷号、下一本 ID 和书籍加载 effect。
- **复用下一本预热能力**：下一本预热仍通过 hook 暴露的 `fetchPagesForBook` / `fetchBookInfoForBook` 复用已有缓存。
- **保留无数据库边界**：本阶段不做页面列表持久化，不修改 SQL/schema/sqlc，只推进阅读器前端状态模块化。
- **新增阶段交付文档**：`docs/phase-32-reader-book-data-hook.md` 记录阶段 32 的交付范围、验证命令和后续拆分边界。

### 📌 增量记录 — 2026-05-13（阶段 33 阅读器进度与预热流水线 Hook 化）

#### 阅读器运行时流水线拆分
- **新增进度流水线 Hook**：`useReaderProgressPipeline` 承接阅读进度上报、离线队列兜底、webtoon 视口追踪和 paged 翻页延迟上报。
- **收拢页面预热逻辑**：当前页、双页副页、当前书后续页和下一本开头页预热统一进入 hook，并继续复用图片缓存去重。
- **主阅读器继续收敛**：`BookReader.tsx` 不再直接导入 `axios`，也不再维护 `IntersectionObserver` 和三组预热 effect。
- **保留无数据库边界**：本阶段不做页面列表持久化，不修改 SQL/schema/sqlc，只推进阅读器前端运行时逻辑模块化。
- **新增阶段交付文档**：`docs/phase-33-reader-progress-pipeline-hook.md` 记录阶段 33 的交付范围、验证命令和后续拆分边界。

### 📌 增量记录 — 2026-05-14（阶段 34 阅读器交互 Hook 收口）

#### 阅读器交互逻辑拆分
- **新增翻页导航 Hook**：`useReaderPageNavigation` 承接跳页、上一页、下一页、首页、末页和末页跳下一本。
- **新增键盘快捷键 Hook**：`useReaderKeyboardShortcuts` 承接 paged 模式方向键、PageUp/PageDown、空格、Home/End，以及全局帮助和书签快捷键。
- **新增分页拖拽 Hook**：`useReaderPointerDrag` 承接分页模式下的拖拽状态、起点记录和滚动位移计算。
- **主阅读器继续收敛**：`BookReader.tsx` 不再直接维护拖拽坐标、滚动起点、键盘监听 effect 和翻页函数。
- **保留无数据库边界**：本阶段不做页面列表持久化，不修改 SQL/schema/sqlc，只推进阅读器前端交互逻辑模块化。
- **新增阶段交付文档**：`docs/phase-34-reader-interaction-hooks.md` 记录阶段 34 的交付范围、验证命令和后续拆分边界。

### 📌 增量记录 — 2026-05-14（阶段 35 阅读器设置抽屉 Tabs 化）

#### 阅读器设置体验收口
- **设置抽屉 Tabs 化**：`ReaderSettingsDrawer` 按阅读、图像、缓存和书签四个场景分组，减少长列表滚动查找。
- **保持功能不回退**：阅读模式、单双页、缩放、滤镜、远程传输、超分、离线缓存、书签和护眼模式均保留原有状态与行为。
- **补齐中英文文案**：新增 `reader.settingsTab.*` 文案，避免 tabs 依赖硬编码文本。
- **保留无数据库边界**：本阶段不做页面列表持久化，不修改 SQL/schema/sqlc，只推进阅读器前端交互体验收口。
- **新增阶段交付文档**：`docs/phase-35-reader-settings-tabs.md` 记录阶段 35 的交付范围、验证命令和边界。

### 📌 增量记录 — 2026-05-14（阶段 36 性能样本库生成器）

#### 性能基线工具
- **新增样本数据库命令**：`cmd/sampledata` 可生成独立 SQLite 样本库，用于首页、搜索、Dashboard 和健康报告性能压测。
- **支持参数化规模**：可配置资源库数、每库系列数、每系列书籍数、页数范围和是否填充阅读进度。
- **稳定可复现**：固定随机种子并在生成后执行 `ANALYZE`，方便不同版本间对比查询性能。
- **保留无数据库边界**：本阶段不做页面列表持久化，不修改 SQL/schema/sqlc，只新增开发/测试工具。
- **新增阶段交付文档**：`docs/phase-36-sample-data-generator.md` 记录阶段 36 的交付范围、命令示例和验证方式。

### 📌 增量记录 — 2026-05-14（阶段 37 大库查询索引与计划验证）

#### 性能迁移与查询计划
- **修复旧库迁移顺序**：数据库迁移改为先建表、补历史字段、再建索引，避免旧库缺少 `quick_hash` 等列时启动报 `no such column`。
- **补齐大库筛选索引**：资源库系列列表按名称、首字母、状态、更新时间、创建时间、评分、书籍数、卷数、页数和收藏排序都有明确索引覆盖。
- **增强书籍侧索引**：系列内书籍排序、阅读进度聚合和封面候选选择新增索引入口，降低大库首页和详情页的扫描成本。
- **新增查询计划工具**：`cmd/queryplan` 可输出代表性 `EXPLAIN QUERY PLAN`，并支持 `-strict` 校验预期索引命中。
- **新增阶段交付文档**：`docs/phase-37-large-library-query-indexes.md` 记录阶段 37 的交付范围、验证命令和边界。

### 📌 增量记录 — 2026-05-14（阶段 38 公开访问与反向代理硬化）

#### 部署安全基线
- **新增监听地址配置**：`server.host` 可切换 `0.0.0.0` 局域网访问或 `127.0.0.1` 本机反代模式。
- **新增 CORS 来源配置**：`server.allowed_origins` 支持按部署域名收紧跨域来源，默认保持原有通配兼容。
- **补齐安全响应头**：所有响应默认带 `nosniff`、`DENY frame`、`no-referrer` 和基础 `Permissions-Policy`。
- **设置页可编辑**：库与扫描页的基础服务区域新增监听地址和 CORS 来源输入，并接入配置校验。
- **新增阶段交付文档**：`docs/phase-38-public-exposure-hardening.md` 记录阶段 38 的配置方式、反代建议和边界。

### 📌 增量记录 — 2026-05-14（资源库批量阅读状态）

#### 批量操作增强
- **新增系列级阅读状态接口**：`/api/series/bulk-progress` 支持按系列 ID 批量把系列下全部书籍标记为已读或未读。
- **资源库多选栏新增操作**：批量选择系列后可直接执行“标记已读”和“标记未读”，完成后刷新当前列表进度。
- **复用现有阅读语义**：标记已读会写入每本书最大页码并记录阅读活动；标记未读会清空阅读页码和最后阅读时间。

### 📌 增量记录 — 2026-04-28（资源库中文首字母筛选）

#### 阅读器跨书缓存预热
- **新增下一本滑动窗口预热**：当前卷剩余待读页数不足 `preloadCount` 时，只按缺口数量加载下一本前几页，自动跳转下一本后可直接复用客户端缓存。
- **保持跨卷预加载窗口连续**：例如当前卷 98 页、预加载 5 页时，第 93 页仍只补当前卷第 98 页，第 94 页开始补下一本第 1 页。
- **加固阅读图片缓存边界**：页图 object URL 改为按书籍分桶管理，切书、切换图片处理参数和组件卸载时会释放旧缓存，避免错图和内存滞留。
- **增强弱网阅读连续性**：当前页、双页副页、当前书预加载和下一本预加载复用同一套请求去重逻辑，降低 VPN 访问时的重复传输。

#### AI 智能分组修复
- **修复 Chat Completions 模式下分组结果为空**：AI 分组提示词要求返回 `collections`，旧版 OpenAI 兼容提供方却只读取 `groups`，会导致任务显示成功但不生成合集。
- **增强分组响应兼容性**：分组解析同时兼容 `collections` 与旧字段 `groups`，并支持清理 Markdown JSON 包裹。
- **加固合集写入链路**：AI 分组创建合集和添加系列改为 sqlc 查询并放入事务，避免部分写入和手写 SQL 分散。

#### 资源库字母筛选增强
- **支持中文系列首字母匹配**：资源库按字母筛选会将中文展示名转换为拼音首字母，例如“进击的巨人”可归入 `J`。
- **忽略名称前缀符号**：筛选首字母计算会跳过书名号、括号、破折号、空格和数字等前缀，取第一位中文或英文字符。
- **旧数据自动回填**：数据库迁移会为已有系列补齐筛选首字母字段，并在扫描、手动编辑和元数据应用时持续维护。

### 📌 增量记录 — 2026-04-27（资深漫画用户体验第一批改造）

#### 阅读器书签与弱网传输优化
- **新增页级书签与备注**：阅读器可为当前页保存书签和备注，书签按书籍持久化，并支持跳转与删除。
- **新增弱网图片传输配置**：阅读器设置中可选择原图、WebP 省流或 JPEG 兼容输出，并调节质量，适合 VPN/家庭网络远程访问。
- **修复图片转码短路条件**：服务端图片处理在仅指定输出格式或质量时也会执行转码，并纳入页图缓存键，避免重复高成本转码。

#### OPDS 目录可用性增强
- **新增 OPDS 继续阅读入口**：`/opds/v1.2/continue` 返回最近阅读卷册，便于外部阅读客户端快速续读。
- **新增 OPDS 分页链接**：资源库系列列表和系列卷册列表支持 `page` / `limit` 参数，并返回 `next` / `previous` 链接，改善大书库客户端加载体验。

### 📌 增量记录 — 2026-04-27（系列关系导航改造）

#### 系列详情关系管理
- **新增相关系列面板**：系列详情页可查看和维护前传、续作、外传、番外、改编、重制版、同宇宙等关系。
- **支持关系快速跳转**：已关联系列以标签形式展示，可直接跳转到目标系列，适合维护复杂阅读顺序。
- **新增关系候选搜索**：添加关系时可在当前资源库内搜索候选系列，并自动排除当前系列和已有关系。

#### 后端关系数据加固
- **禁止自关联**：系列不能再关联到自身。
- **防止反向重复关联**：已存在 A -> B 或 B -> A 时，重复创建会返回已有关系，不再写入重复数据。
- **校验目标系列存在性**：创建关系前会确认目标系列存在，避免孤儿关系记录。

### 📌 增量记录 — 2026-04-27（资源库智能筛选视图）

#### 资源库常用视图保存
- **新增 Smart Filters**：资源库页可将当前标签、作者、状态、首字母、排序方式和每页数量保存为命名视图。
- **支持一键应用与删除**：常用视图以快捷按钮展示，适合快速切换“未读长篇”“收藏高分”“指定作者”等书架视角。
- **本地按资源库保存**：筛选视图保存在当前浏览器的 localStorage 中，不改变数据库结构，也不影响其他设备。

### 📌 增量记录 — 2026-04-27（OPDS 搜索兼容性增强）

#### 外部阅读客户端搜索
- **新增 OpenSearch 描述端点**：`/opds/v1.2/opensearch.xml` 返回 OPDS 客户端可识别的搜索模板。
- **新增 OPDS 系列搜索 Feed**：`/opds/v1.2/search?q=...` 可按系列名称或标题搜索，并返回系列详情入口、封面缩略图、简介和分页链接。
- **根目录暴露搜索能力**：OPDS 根 Feed 增加 `rel=search` 链接，支持兼容客户端自动发现搜索端点。

### 📌 增量记录 — 2026-04-27（有序阅读清单）

#### 跨系列阅读顺序管理
- **新增阅读清单数据模型与 API**：支持创建、编辑、删除阅读清单，并将系列加入有序队列。
- **支持清单内重排与移除**：清单条目可上移、下移和删除，适合维护前传、外传、番外和同宇宙阅读顺序。
- **新增阅读清单页面**：侧边栏增加“阅读清单”入口，可搜索系列加入清单，并从条目直接进入下一本可读卷册。

### 📌 增量记录 — 2026-04-27（ComicInfo 元数据导出）

#### 标准漫画元数据互通
- **新增卷册 ComicInfo.xml 导出端点**：`/api/books/{bookId}/comicinfo.xml` 可按当前数据库元数据生成标准 XML，便于与其他漫画管理器或阅读器互通。
- **导出内容覆盖系列与卷册字段**：导出会合并卷册标题、序号、卷号、页数，以及系列标题、简介、出版社、评分、语言、标签和作者角色。
- **新增系列级批量导出**：`/api/series/{seriesId}/comicinfo.zip` 会为当前系列所有卷册生成独立 `ComicInfo.xml`，并自动处理重复文件名。
- **系列详情新增快捷导出入口**：卷册卡片悬停时可直接下载对应 `ComicInfo.xml`，方便对单本资源做元数据备份或迁移。
- **系列详情新增批量导出入口**：系列头部可直接下载整套 `ComicInfo.zip`，适合批量迁移或离线归档。

### 📌 增量记录 — 2026-04-27（阅读器快捷键增强）

#### 高强度阅读操作优化
- **扩展翻页快捷键**：翻页模式支持空格、PageDown、PageUp、Home、End，适配键盘和遥控器式操作。
- **新增书签快捷键**：阅读器内按 `B` 可直接收藏或更新当前页书签。
- **完善快捷键帮助**：阅读帮助面板补充新增快捷键说明，并避免在输入框中触发全局快捷键。

### 📌 增量记录 — 2026-04-27（自用 Mihon 扩展支持）

#### Mihon / Tachiyomi 私有客户端接入
- **新增 Mihon 专用 API**：`/api/mihon/v1` 提供资源库、系列分页搜索、系列详情、卷册列表、页面列表和阅读进度写入端点。
- **稳定移动端响应结构**：Mihon API 输出扁平 JSON，不暴露前端使用的 `sql.Null*` 结构，降低扩展端适配成本。
- **复用服务端图片管线**：页面图片端点继续支持 `format` / `q` 参数，扩展可默认请求 WebP 压缩图以降低 VPN 传输量。
- **新增独立扩展仓库**：`/Users/nicoer/dev/manga_manager_mihon_extension` 基于 Keiyoushi 扩展骨架，新增 `Manga Manager` 自用 source。

### 📌 增量记录 — 2026-04-27（系统性缺陷修复与验证收敛）

#### 后端测试兼容性修复
- **修复库创建测试参数过期**：后端测试中的 `CreateLibraryParams` 改为使用当前 `scan_mode` 与 `koreader_sync_enabled` 字段，避免 KOReader 和 API 测试因旧 `AutoScan` 字段无法编译。
- **补齐测试库默认扫描配置**：测试数据继续显式设置扫描间隔与默认扫描格式，保持与当前 schema 行为一致。

#### 前端稳定性与 lint 修复
- **修复全局搜索竞态**：全局搜索 hook 在查询变化时会取消过期请求，空查询不再被旧请求结果回写污染。
- **修复阅读器缓存依赖**：阅读器图片 URL 生成补上 `autoCrop` 依赖，避免自动裁切开关变化后复用旧缓存；键盘翻页逻辑改为稳定 callback，避免闭包使用旧翻页状态。
- **收敛前端类型与错误处理**：首页、仪表板、系列详情、设置页等页面移除残留 `any`，错误提示统一通过 `axios.isAxiosError` 提取后端返回信息。
- **恢复前端 lint 基线**：Context/Provider 文件按项目导出模式调整 Fast Refresh lint 边界，`npm run lint` 现在无错误无警告。

### 📌 增量记录 — 2026-04-24（国际化、传输优化与首页请求修正）

#### 前端国际化完善 `[P2: 文案与视觉统一]`
- **接入统一语言运行时**：`LocaleProvider` 改为前端本地持久化语言设置，启动时按当前 locale 预加载词条，避免界面语言依赖后端配置。
- **词典拆分为按语言懒加载**：原单文件 `messages.ts` 拆为 `web/src/i18n/locales/zh-CN.ts` 与 `en-US.ts`，只在需要时下载对应语言包，减少首屏常驻代码。
- **主要页面与设置项完成中英文切换**：首页、仪表板、系列详情、设置页、阅读器和若干弹窗组件统一接入翻译 key，状态值与任务文案不再依赖硬编码中文展示。

#### 前端打包与首屏体积优化 `[P2: 首屏加载与缓存效率]`
- **页面级路由懒加载**：`App.tsx` 将首页、阅读器、日志页、系列详情页、设置页及其子页改为 `React.lazy + Suspense`，避免整站代码一次性进入入口包。
- **Vite 分包策略优化**：`vite.config.ts` 新增 `manualChunks`，将 framework、icons、http、date 和 UI vendor 依赖拆分为独立 chunk，提升缓存命中率并消除大 chunk 警告。
- **全局弹窗按需加载**：`Layout` 中的全局搜索弹窗和资源库表单改为延迟加载，进一步压缩首页入口体积。

#### 服务端响应压缩与静态缓存 `[P1: 远程访问体验优化]`
- **启用 HTTP 文本压缩**：后端对 `html/css/js/json/svg/plain/xml` 响应启用 gzip 压缩，降低 VPN 和弱网环境下的文本传输成本。
- **静态资源缓存头细化**：对 `dist/assets/*` 指纹文件返回 `Cache-Control: public, max-age=31536000, immutable`，而 `index.html` 等入口资源保持 `no-cache`，避免旧入口被强缓存。
- **补充服务端回归测试**：新增 `cmd/server/main_test.go` 验证静态资源缓存策略和 Content-Type 头部行为。

#### 首页资源库重复请求修复 `[P1: 首页列表稳定性]`
- **修复资源库页重复拉取问题**：`Home.tsx` 中 SSE 静默刷新 effect 不再把 `settingsReady` 作为触发依赖，避免在恢复本地排序配置后与正常列表请求同时触发两次相同的 `series/search`。
- **保留守卫但收敛触发时机**：`settingsReady` 仅作为内部条件使用，SSE 刷新只在 `refreshTrigger` 真正变化时触发。
- **新增并发去重兜底**：相同查询参数的系列搜索请求在进行中时会复用同一 Promise，避免开发态重复触发或竞态下的重复网络请求。

### 📌 增量记录 — 2026-04-17（前端主题化）

#### 主题系统与本地持久化 `[P2: 文案与视觉统一 / UI 个性化]`
- **新增前端主题系统**：引入 `ThemeProvider` 与主题注册表，主题切换在前端本地生效，不依赖后端配置。
- **主题配置本地保存**：当前主题使用浏览器 `localStorage` 持久化，刷新页面后自动恢复；存储键为 `manga_manager_theme`。
- **内置 5 套主题**：
  - `Midnight`：默认深色主题，延续当前系统识别度；
  - `Paper`：浅色纸张风格；
  - `Forest`：低饱和墨绿主题；
  - `Amber`：暖色书房主题；
  - `Graphite`：中性工业风主题。
- **阅读器跟随应用主题**：`BookReader` 不引入独立主题状态，默认跟随应用当前主题；现有阅读模式、方向、缩放和图像处理偏好保持本地独立存储。

#### 前端颜色体系重构 `[P2: 文案与视觉统一]`
- **全局颜色 token 化**：`index.css` 新增主题变量层，统一管理 app 背景、surface、sidebar、主强调色、hover 色、阴影和灰阶文本色。
- **Tailwind 颜色映射切换为 CSS Variables**：`tailwind.config.js` 中的 `komga*`、`gray`、`slate`、`black`、`white` 颜色改为从主题变量读取，原有大量 Tailwind 类无需全量重写即可响应主题切换。
- **Modal 与通用按钮跟随主题**：
  - `ModalShell` 的遮罩、面板渐变与阴影改为读取主题变量；
  - `modalStyles` 中主按钮 hover 色不再写死为紫色，统一使用主题主色 hover token。

#### 设置页主题入口 `[P2: UI 个性化]`
- **Settings 新增“外观 / 主题”分组**：
  - 使用主题卡片而不是下拉框展示内置主题；
  - 每个主题展示名称、风格说明和颜色预览；
  - 明确提示“主题仅保存在当前浏览器”。
- **当前主题即时切换**：用户在设置页点击主题卡后立即生效，无需额外保存配置。

#### 关键界面接入主题 `[P2: 视觉统一]`
- 以下关键界面已接入主题色：
  - `Settings`
  - `Collections`
  - `BookReader`
  - `ModalShell`
  - `ErrorBoundary`
  - `DirectoryPicker`
- 若干主交互按钮的 hover 行为从固定 `purple-*` 调整为 `komgaPrimaryHover`，避免切换主题后仍残留默认紫色交互反馈。

### 📌 增量记录 — 2026-04-17（前端导航与阅读体验收敛）

#### 导航与空间利用 `[P2: 桌面端效率提升]`
- **桌面端左侧导航支持折叠**：`Layout` 新增桌面端折叠态，侧栏可在完整列表与图标窄栏之间切换。
- **折叠状态本地持久化**：侧栏折叠状态保存到浏览器本地，刷新后自动恢复。
- **折叠态保留主导航与资源库入口**：在窄栏模式下继续显示主导航图标和资源库入口，不会完全丢失切换能力。

#### 阅读器移动端交互 `[P2: 阅读体验进一步打磨]`
- **移动端阅读页隐藏翻页箭头**：`BookReader` 在移动端不再显示左右箭头提示。
- **移动端阅读页隐藏左右半透明遮罩**：翻页热区仍保留，但移动端不再显示悬浮遮罩层，减少对内容的干扰。
- **桌面端保留显式翻页提示**：桌面端继续保留左右翻页提示，仅在悬停时显示。

#### 页面与面板交互收敛 `[P1: 页面密度与操作层级优化]`
- **外部资源库面板支持折叠/展开**：资源库页的外部资源库区域新增折叠交互，默认降低页面首屏占用。
- **系列详情卷册操作图标优化**：卷册卡片的完成/未完成切换图标由编辑铅笔调整为更明确的完成态图标，降低语义歧义。
- **KOReader 设置页样式统一**：服务配置、立即操作按钮、错误提示和 Sync Key 展示统一到当前主题色体系，减少独立的蓝色强调样式。

#### 前端主题兼容修正 `[P2: 文案与视觉统一]`
- **主题变量透明度写法修正**：`ModalShell` 和全局背景中的 `rgba(var(...))` 写法调整为 `rgb(var(...) / alpha)`，避免部分浏览器下 CSS 变量透明度解析不稳定。

### ⚡ 重大变更 (Breaking Changes)

#### LLM 配置结构重构 `[P0: 修正 OpenAI/兼容 LLM 配置模型]`
- **`config.yaml` 中 `llm.endpoint` 字段已废弃**，拆分为 `base_url` + `request_path` 两个独立字段。
  - `base_url`：仅包含协议和主机部分（如 `https://api.openai.com`）。
  - `request_path`：仅包含 API 路径（如 `/v1/responses`）。
  - 旧的 `endpoint` 字段仍被保留用于向后兼容，启动时会自动拆分并迁移。
- 新增 `llm.api_mode` 字段，用于显式声明 OpenAI 协议模式：
  - `"responses"` — 使用 OpenAI Responses API（默认）。
  - `"chat_completions"` — 使用传统 Chat Completions API。
- 原 `provider: "openai-legacy"` 在规范化阶段自动映射为 `provider: "openai"` + `api_mode: "chat_completions"`。

#### 默认扫描格式变更 `[P0: 统一文件格式能力声明]`
- 默认扫描格式从 `zip,cbz,rar,cbr,pdf` 调整为 `zip,cbz,rar,cbr`，移除了 PDF。
- 数据库 `schema.sql` 中的 `scan_formats` 列默认值同步更新。

---

### 🚀 新功能 (Features)

#### 后端 — 配置管理 `[P0: 配置校验 + LLM 配置模型]`

- **配置自动规范化**：新增 `NormalizeConfig()` 和 `normalizeLLMConfig()` 函数，在配置加载及默认配置创建后统一补全缺失字段并执行迁移逻辑，包括：
  - 自动设置 `server.port`、`database.path`、`cache.dir` 等系统级默认值。
  - `endpoint` → `base_url` + `request_path` 自动拆分（`splitEndpoint`）。
  - 根据 `request_path` 自动推断 `api_mode`（`inferAPIModeFromRequestPath`）。
  - `BuildLLMEndpoint()` 函数：从 `base_url` + `request_path` 拼装最终请求地址。
- **配置校验（Validation）**：新增 `ValidateConfig()` 函数，返回 `ValidationResult` 结构体（含 `valid` 布尔值及 `issues` 列表），前端可据此高亮问题字段并阻止保存。
- **系统能力查询 API**：新增 `GET /api/system/capabilities` 端点，返回：
  - `supported_scan_formats` — 支持的扫描格式列表。
  - `default_scan_formats` — 默认扫描格式 CSV。
  - `default_scan_interval` — 默认扫描间隔。
  - `supported_llm_providers` — 支持的 LLM 提供商列表。
  - `supported_llm_api_modes` — 支持的 API 模式列表。
- **配置响应增强**：`GET /api/system/config` 返回结构由单纯配置对象升级为包含 `config`、`validation`、`capabilities` 三个字段的 `SystemConfigResponse` 信封。

#### 后端 — 任务管理增强 `[P0: 增强后台任务的用户反馈闭环]`

- **任务模型扩展**：`TaskStatus` 结构体新增字段：
  - `scope` / `scope_id` — 任务作用域及关联实体 ID（library / series / system），由 `inferTaskScope()` 自动推断。
  - `error` — 失败任务的详细错误信息。
  - `can_cancel` — 标记任务是否可取消（预留）。
  - `retryable` — 标记任务是否允许重试。
  - `params` — 任务启动参数快照（如 `provider`），用于重试时复现。
  - `started_at`、`finished_at` — 任务开始与结束时间。
- **任务列表过滤**：`GET /api/system/tasks` 支持 `status`、`scope`、`type`、`q`（关键词搜索）、`limit` 查询参数。
- **任务重试 API**：新增 `POST /api/system/tasks/{taskKey}/retry` 端点，支持重新启动已完成 / 失败的任务（`scan_library`、`scan_series`、`cleanup_library`、`rebuild_index`、`rebuild_thumbnails`、`scrape`、`ai_grouping`）。
- **刮削任务重构**：`batchScrapeAllSeries` 和 `scrapeLibrary` 拆分出可复用的 `launchBatchScrapeAllSeriesTask()` / `launchLibraryScrapeTask()` 内部方法，同时记录 `params` 以便 `retryScrapeTask()` 使用。
- **失败任务错误跟踪**：新增 `failTaskWithError()` 方法，失败任务会同时保留 `message` 与 `error` 两个独立描述。

#### 后端 — 日志系统增强 `[P1: 改善日志与任务中心]`

- **日志响应结构变更**：`GET /api/system/logs` 返回结构从纯日志数组改为 `{ items: [...], summary: { total, by_level } }` 格式。
- **日志关键词搜索**：支持 `q` 查询参数，在 `raw` 和 `msg` 字段中进行不区分大小写的全文搜索。
- **日志计数统计**：返回的 `summary` 包含筛选后的总条目数及按级别（ERROR / WARN / INFO）的分布。

#### 后端 — 扫描器改进 `[P0: 统一文件格式能力声明]`

- **统一格式判断**：`scanner.go` 中的硬编码扩展名判断替换为 `config.IsSupportedArchiveExtension()` 调用，与全局配置保持一致。
- **文件监听器格式同步**：`watcher.go` 中的 `formats` 切片不再硬编码，而是从 `config.SupportedScanFormats` 动态生成。

#### 后端 — 元数据提供者 `[P0: 修正 OpenAI/兼容 LLM 配置模型]`

- **`NewAIProvider` 签名变更**：从 `(provider, endpoint, model, apiKey, timeout)` 改为 `(provider, apiMode, baseURL, requestPath, model, apiKey, timeout)`，内部通过 `config.BuildLLMEndpoint()` 组装最终端点。
- 自动识别 `openai-legacy` 提供者并降级为 `openai` + `chat_completions` 模式。

#### 前端 — 仪表盘 (Dashboard) `[P0: 降低首次使用门槛]` `[P2: 仪表板更产品化]`

- **首次使用引导页**：当用户没有任何资源库时，展示 Onboarding 卡片引导用户：
  1. 添加资源库。
  2. 检查系统设定。
  3. 开始首次扫描。
- **失败任务通知区**：仪表盘顶部展示最近最多 3 个失败的后台任务，包含错误信息。
- **库列表与任务数据预加载**：页面加载时并行拉取 `/api/libraries` 和 `/api/system/tasks` 数据。

#### 前端 — 设置页 (Settings) 重构 `[P0: 配置校验]` `[P1: 重构设置页信息架构]`

- **全面重写**：Settings 页面逻辑和 UI 大幅重构。
- **配置信封解析**：从新的 `SystemConfigResponse` 结构中提取 `config`、`validation`、`capabilities`。
- **实时校验反馈**：保存时若后端返回 422，直接解析 `validation` 并在对应字段下方渲染错误提示。
- **LLM 配置 UI 改进**：
  - Provider 选择改为从后端 `capabilities.supported_llm_providers` 动态渲染。
  - 新增 API Mode 选择器（`responses` / `chat_completions`）。
  - `base_url` 和 `request_path` 分离输入。
- **样式统一**：提取 `sectionClassName` 和 `inputClassName` 常量，组件风格统一。

#### 前端 — 日志页 (Logs) 重构 `[P1: 改善日志与任务中心]`

- **双面板布局**：日志页不再仅展示日志条目，新增**后台任务面板**，左右分栏展示：
  - 左侧：系统日志（支持级别过滤、关键词搜索）。
  - 右侧：后台任务列表（支持状态、作用域过滤及关键词搜索）。
- **任务重试按钮**：对可重试的失败任务，页面上提供一键重试按钮。
- **日志复制功能**：每条日志旁新增复制按钮，可将原始日志文本复制到剪贴板。
- **日志级别统计栏**：页面顶部展示 ERROR / WARN / INFO 各级别的条目计数。
- **UI 全面中文化**：所有文案从英文替换为中文。

#### 前端 — 系列搜索弹窗 (SeriesSearchModal) 增强 `[P1: 完善元数据工作流]`

- **双栏预览布局**：从单栏列表改为左右分栏——左侧为搜索结果列表，右侧为选中条目与当前系列的**逐字段对比**。
- **元数据差异对比**：预览面板按字段（标题、简介、作者、出版商等）展示「当前值 → 即将更新值」，并标注变更计数。
- **选中状态高亮**：选中条目显示 `已选中` 徽章和边框高亮。
- **弹窗标题改进**：从"选择最佳匹配条目"改为"预览并选择元数据来源"，增加來源提示文案。

#### 前端 — 侧边栏 / Layout `[P0: 统一格式能力声明]` `[P1: 优化库管理与目录选择交互]`

- **最近路径记忆**：新增资源库时，自动记录最近使用的路径到 `localStorage`，`DirectoryPicker` 可利用这些历史路径提供快速选择。
- **扫描格式动态获取**：从 `GET /api/system/capabilities` 获取默认扫描格式，替代前端硬编码。
- **任务进度条改进**：进度条根据 `status` 字段（`completed` / `failed`）判断是否自动消失，不再仅依赖 `current >= total`。
- **错误信息提取改进**：新增 `extractErrorMessage()` 工具函数，可从 Axios 错误响应中提取校验 issue 或 error 消息。
- **全局事件绑定**：Dashboard 的 Onboarding 卡片可通过 `manga-manager:open-add-library` 自定义事件触发侧边栏添加资源库弹窗。

---

### 🐛 修复 (Bug Fixes)

- 修复任务完成后 `error` 字段未被清空的问题（仅在 `status != "failed"` 时重置）。
- `scrape_controller.go` 响应体不再返回 `total` 字段（该信息已由 task SSE 推送），避免前端混淆。

---

### 🧹 重构 (Refactoring)

- `scrapeLibrary` / `batchScrapeAllSeries` HTTP handler 与后台任务启动逻辑解耦，分为 handler（处理 HTTP 请求/响应）和 `launch*Task()`（可复用的启动方法）。
- `completeTask` 和 `failTask` 路径合并为统一的状态更新逻辑。
- `metadata.NewAIProvider` 工厂方法参数从拼装完成的 endpoint 改为结构化的 `baseURL` + `requestPath`，内部统一调用 `config.BuildLLMEndpoint()`。

---

### 🧪 测试 (Tests)

- 新增 `collection_controller_test.go`：覆盖收藏集 API 的边界场景。
- 新增 `opds_controller_test.go`：覆盖 OPDS 协议端点的边界场景。
- 扩展 `controller_test.go`：覆盖任务管理、API 统计等新端点。
- 扩展 `scrape_controller_test.go`：覆盖刮削控制器的边界场景。
- 扩展 `log_controller_test.go`：覆盖新的日志响应结构和搜索功能。

---

### 📦 其他

- `.gitignore` 更新。
- `config.yaml` 示例文件更新以反映新的 LLM 配置字段。
- `web/src/components/layout/constants.ts` 更新默认扫描格式常量。
- `web/src/pages/series-detail/types.ts` 新增 `VolumeCount` 字段。
- `web/src/pages/SeriesDetail.tsx` 新增 `VolumeCount` 相关展示逻辑。

### 📌 增量记录 — 2026-04-13

#### 任务中心与失败恢复 `[P1: 改善日志与任务中心]`
- **任务筛选 API 完整化**：`GET /api/system/tasks` 新增 `status`、`scope`、`type`、`q`、`limit` 过滤参数，前端可以按状态、作用域和关键字做组合筛选。
- **任务重试能力落地**：新增 `POST /api/system/tasks/{taskKey}/retry`，已支持以下任务类型的重试：
  - `scan_library`
  - `scan_series`
  - `cleanup_library`
  - `rebuild_index`
  - `rebuild_thumbnails`
  - `scrape`
  - `ai_grouping`
- **任务内部模型增强**：`TaskStatus` 新增 `retryable` 和 `params` 字段。
  - `retryable` 用于前端判断是否显示重试按钮。
  - `params` 用于保存任务启动时的关键参数快照（例如刮削任务的 provider），以便重试时复现原始行为。
- **后台任务启动逻辑抽取**：扫描、清理、重建索引、重建缩略图、AI 分组、批量刮削等任务拆出可复用的 `launch*Task()` 内部方法，使 HTTP handler 和任务重试共用同一套执行路径。
- **日志页升级为任务中心**：
  - 任务面板增加状态筛选、作用域筛选和关键字搜索。
  - 对可重试且当前非运行中的任务，提供一键重试按钮。
  - 任务中心不再只展示“最近 8 条”，而是支持按条件取回更完整的任务集合。

#### 阅读器恢复体验 `[P2: 阅读器体验进一步打磨]`
- **阅读器加载失败回退**：`BookReader` 新增 `loadError` 状态。
  - 当页面列表为空，或 `/api/pages/:bookId`、`/api/book-info/:bookId` 请求失败时，阅读器会显示明确的错误卡片，而不是停留在黑屏或加载态。
  - 错误卡片提供“重试加载”和“返回系列”两个直接动作。
- **阅读帮助入口**：阅读器顶部新增帮助按钮。
  - 增加键盘快捷帮助和移动端提示。
  - 支持通过 `H` / `?` 快速切换帮助面板。
- **返回逻辑收口**：提取 `handleBackToSeries()`，统一处理从阅读器回到系列页 / 卷视图的导航逻辑，避免不同按钮各自维护回跳规则。

#### 系列页锁定字段可见性 `[P1: 完善元数据工作流]`
- `SeriesHeader` 现在会在系列页头部直接展示**已锁定字段数量与字段列表**。
- 用户在执行刮削前，不需要先打开编辑弹窗，也能知道哪些字段不会被自动覆盖。
- 这项改动与前一轮的“元数据预览对比”配合后，系列页的信息结构更完整：
  - 先在页头确认哪些字段被锁定；
  - 再在搜索弹窗中比较当前值与候选值；
  - 最后决定是否应用。

#### Changelog 维护 `[工程记录]`
- 补充记录本次及前两次改造的详细内容，覆盖：
  - 配置模型重构与能力声明统一
  - 设置页、日志页、任务中心、仪表盘与首次引导
  - 元数据预览流程与最近使用目录
  - 任务重试、阅读器错误恢复与锁定字段提示

### 📌 增量记录 — 2026-04-13（第二次续改）

#### 任务中心与页面联动 `[P1: 改善日志与任务中心]`
- **任务列表支持 `scope_id` 精确过滤**：`GET /api/system/tasks` 新增 `scope_id` 查询参数，前端可以直接按具体资源库或系列拉取关联任务，不再依赖模糊关键词搜索。
- **日志页任务跳转**：任务中心中的每条任务都新增“打开关联页面”动作：
  - `scope=series` 时跳转到对应系列详情页；
  - `scope=library` 时跳转到对应资源库页；
  - `scope=system` 时回到系统设定页。
- **系列页失败任务面板**：`SeriesDetail` 现在会主动拉取与当前系列关联的失败任务，并在页面下方展示。
  - 展示失败消息、错误详情、更新时间；
  - 对可重试任务提供原地重试按钮。

#### 阅读器恢复体验补全 `[P2: 阅读器体验进一步打磨]`
- **阅读器帮助面板**：`BookReader` 顶栏新增帮助按钮，支持通过 `H` / `?` 快速打开。
- **阅读器空页面防御**：当归档可打开但没有任何可读页面时，阅读器会明确提示“当前书籍没有可读取的页面”，不再停留在空白阅读区。
- **阅读器回退动作统一**：返回系列的导航逻辑抽成统一函数，错误态、顶部返回按钮和其他回退入口共用同一套规则。

#### 系列页恢复提示增强 `[P1: 完善元数据工作流]`
- 系列页除了显示锁定字段之外，还会直接暴露关联失败任务，形成“看到问题 -> 就地重试”的闭环。

### 📌 增量记录 — 2026-04-13（KOReader Sync 服务端）

#### KOReader 阅读进度同步 `[P0/P1: 服务端同步能力]`
- **新增 KOReader 服务端接口**：
  - `POST /koreader/users/create`
  - `GET /koreader/users/auth`
  - `PUT /koreader/syncs/progress`
  - `GET /koreader/syncs/progress/{document}`
  - `GET /koreader/healthcheck`
  - `GET /koreader/healthstatus`
  - `GET /koreader/robots.txt`
- **单用户同步模型落地**：当前版本以单用户服务为目标，不引入完整账号系统；同步认证使用 KOReader 头部 `x-auth-user` / `x-auth-key`。
- **同步记录持久化**：新增 `koreader_settings`、`koreader_progress`、`koreader_sync_events` 三张表，分别保存同步账号、文档进度和同步审计事件。
- **书籍身份指纹**：`books` 表新增：
  - `file_hash`
  - `path_fingerprint`
  - `path_fingerprint_no_ext`
  用于当前 KOReader 匹配方案中的二进制哈希和路径索引。
- **当前匹配方案说明**：KOReader 现仅保留两种正式匹配模式：
  - `binary_hash`
  - `file_path`
  其中 `file_path` 模式只比较“文件名 + 向上最多两层路径”，并支持可选的“忽略扩展名”规则。
- **进度投影回本地阅读状态**：当 KOReader 文档成功命中本地书籍时，会把 `percentage` 按 `page_count` 投影回 `books.last_read_page/last_read_at`，并写入 `reading_activity`。
- **防止进度回退**：服务端采用“更远进度优先”，较小的 `percentage` 不会覆盖已有较大进度。

#### 扫描器与维护任务 `[P1: 资源识别与恢复能力]`
- **扫描时自动生成指纹**：扫描器在入库/更新书籍后会同步写入 KOReader 文档匹配指纹，不再依赖纯人工维护。
- **新增系统维护任务**：
  - `POST /api/system/koreader/rebuild-hashes`：按当前匹配模式重建 KOReader 索引
  - `POST /api/system/koreader/reconcile`：重关联未匹配的 KOReader 同步记录
- **任务中心集成**：新增任务类型
  - `rebuild_book_hashes`
  - `reconcile_koreader_progress`
  并支持任务列表展示、错误提示与重试。

#### 设置页与管理入口 `[P1: 配置与可运维性]`
- 设置页新增 **KOReader Sync** 分组，支持：
  - 启用/关闭同步服务
  - 配置同步路径

### 📌 增量记录 — 2026-04-15（KOReader 多账号与服务端生成密钥）

#### 多账号模型 `[P1: KOReader 服务端能力增强]`
- **KOReader 账号从单账号升级为多账号**：新增 `koreader_accounts` 表，账号字段包含：
  - `username`
  - `sync_key`
  - `enabled`
  - `created_at`
  - `updated_at`
- **旧单账号自动迁移**：数据库迁移时会尝试把历史 `koreader_settings` 中的单账号数据迁入 `koreader_accounts`，避免已有接入用户被直接清空。
- **按账号鉴权**：KOReader 协议层不再读取单一账号配置，而是按 `x-auth-user` 查找对应账号，再校验其 `x-auth-key`。

#### 服务端生成 Sync Key `[P1: 账号管理与接入流程]`
- **Sync Key 改为由服务端生成**：新增随机 16 字节十六进制生成逻辑，每次创建账号或轮换密钥时由服务端生成新的 32 位小写十六进制 Sync Key。
- **账号管理接口落地**：
  - `GET /api/system/koreader/accounts`
  - `POST /api/system/koreader/accounts`
  - `POST /api/system/koreader/accounts/{id}/rotate-key`
  - `POST /api/system/koreader/accounts/{id}/toggle`
  - `DELETE /api/system/koreader/accounts/{id}`
- **设备自助注册不再作为主路径**：`POST /koreader/users/create` 现在返回“请从管理后台创建账号”的兼容拒绝响应，以避免与“服务端生成 Sync Key”模型冲突。

#### 系统状态与进度投影 `[P1: 多账号运行态可见性]`
- `GET /api/system/koreader` 现在返回服务级统计，而不是单账号字段，新增：
  - `account_count`
  - `enabled_account_count`
- KOReader 进度仍按账号分别保存在 `koreader_progress` 中，但对本地 `books.last_read_page/last_read_at` 的投影仍然保持**全局合并**，继续采用“更远进度优先”规则。
- 删除 KOReader 账号时，会同步清理该账号关联的 `koreader_progress` 与 `koreader_sync_events`，避免遗留脏数据。

#### 设置页重构 `[P1: 管理界面]`
- KOReader 设置页从“单账号表单”改为“服务配置 + 账号列表”：
  - 服务配置仍负责开关、路径和匹配模式
  - 账号列表负责创建、查看 Sync Key、复制、轮换、启停和删除
- 状态卡从展示单一账号改为展示：
  - 已启用账号数 / 总账号数
  - 最近同步时间
  - 最近错误
- 设备接入文案同步更新为：
  - 自定义同步服务地址从服务配置读取
  - 用户名和 Sync Key 从后台账号列表复制

#### 测试与验证 `[工程质量]`
- 新增 / 更新测试覆盖：
  - 创建 KOReader 账号并生成 Sync Key
  - 轮换 Sync Key
  - 启停账号
  - 多账号下按用户名鉴权
  - 现有 binary_hash / file_path 进度同步回归
- 验证通过：
  - `GOCACHE=/Users/nicoer/dev/manga_manager/.gocache GOTMPDIR=/Users/nicoer/dev/manga_manager/.tmp go test ./...`
  - `npm run build` in `web/`

### 📌 增量记录 — 2026-04-15（KOReader 最近错误修复）

#### 系统事件与错误提示分离 `[P1: 可观测性修正]`
- **修复“轮换 Sync Key 后显示最近错误”的问题**：`latest_error` 的查询逻辑此前把所有 `status != ok` 的事件都当成错误，包括：
  - `account_rotated`
  - `account_enabled`
  - `account_disabled`
- 现在 `latest_error` 只会读取**非系统事件**中的失败记录，系统管理操作不再污染账号卡片或服务状态中的“最近错误”提示。
- 新增回归测试，覆盖“轮换/启停账号后 `latest_error` 仍为空”的场景。

### 📌 增量记录 — 2026-04-15（KOReader 原始密钥与登录排障）

#### 原始 Sync Key 语义修正 `[P0: 鉴权兼容性修复]`
- **修复“新生成账号仍然 Unauthorized”的问题**：多账号版本初版误把服务端保存的 `sync_key` 直接与请求头 `x-auth-key` 比较。
- 现已改为符合 KOReader 客户端行为：
  - 后台展示和复制的是**原始 Sync Key**
  - KOReader 设备使用该原始值配置账号
  - 客户端请求时会发送 `MD5(原始 Sync Key)` 到 `x-auth-key`
  - 服务端鉴权时按 `MD5(保存的原始 Sync Key)` 与请求头比较
- 设置页文案同步修正，明确说明：
  - 设备侧直接填写原始 Sync Key
  - 不需要手动对密钥做 MD5

#### 登录排障日志增强 `[P1: 可观测性与故障定位]`
- 为 KOReader 协议入口新增结构化日志，覆盖：
  - `/koreader/users/auth`
  - `/koreader/syncs/progress`
  - `/koreader/syncs/progress/{document}`
- 新增日志字段：
  - `username`
  - `client_ip`
  - `user_agent`
  - `accept`
  - `document`
  - `device`
  - `device_id`
  - `client_key_prefix`（仅前缀，不记录完整密钥）
- 服务层鉴权分支现在会明确记录失败原因：
  - 缺失凭据
  - 账号不存在
  - 账号停用
  - 账号未保存原始密钥
  - 客户端 header 与预期 MD5 不匹配
  - 鉴权成功
- 失败事件仍会写入 `koreader_sync_events`，同时补充一条结构化 `slog.Warn`，便于直接从服务日志定位问题。

#### 测试与验证 `[工程质量]`
- 新增 / 更新测试覆盖：
  - 新生成账号 + 客户端发送 MD5 后的 `x-auth-key` 可以成功登录
  - 服务层按“原始 Sync Key -> MD5 请求头”语义鉴权
- 验证通过：
  - `GOCACHE=/Users/nicoer/dev/manga_manager/.gocache GOTMPDIR=/Users/nicoer/dev/manga_manager/.tmp go test ./internal/api ./internal/koreader ./internal/database`
  - `npm run build` in `web/`

### 📌 增量记录 — 2026-04-15（KOReader 协议兼容修复）

#### KOReader 鉴权与协议兼容 `[P0: 修正 KOReader 自定义同步协议实现]`
- **修复设备登录 `Unauthorized`**：服务端不再把 `x-auth-key` 当作明文密码做二次哈希，改为按 KOReader 客户端实际发送的 **32 位 MD5 Sync Key** 直接存储和校验。
- **切断旧错误密钥语义**：旧实现里保存的非 KOReader 兼容 key 不再被视为有效配置；管理员需要重新填写正确的 Sync Key。
- **KOReader 专用响应对齐**：
  - `GET /koreader/healthcheck` / `GET /koreader/healthstatus` 现在返回 `{"state":"OK"}`
  - `GET /koreader/users/auth` 成功时返回包含 `state` / `authorized` 的兼容响应
  - `PUT /koreader/syncs/progress` 与 `GET /koreader/syncs/progress/{document}` 的成功响应统一带 `state: OK`
- **支持 KOReader vendor JSON**：当客户端发送 `Accept: application/vnd.koreader.v1+json` 时，接口会返回对应 `Content-Type`，否则仍返回普通 JSON。

#### KOReader 设置与状态展示 `[P1: 配置可见性与排障能力]`
- **设置字段语义切换**：系统管理接口与设置页从旧的 `password` 语义切换为 `sync_key`，明确表示这里填写的是 **KOReader Sync Key (MD5)**，不是原始密码。
- **新增 Sync Key 格式校验**：保存 KOReader 配置时，`sync_key` 必须是 32 位小写十六进制字符串，否则返回字段级校验错误 `koreader.sync_key`。
- **状态接口增强**：`GET /api/system/koreader` 新增：
  - `has_valid_sync_key`
  - `latest_error`
  用于区分“已设置但格式无效”和“真正可用”的同步配置。
- **设置页状态卡改进**：
  - 显示 `Sync Key 已配置 / 格式无效 / 未设置`
  - 显示最近一次 KOReader 协议错误
  - 文案明确说明设备端应填写的是 32 位 MD5 Sync Key

#### 日志与事件记录 `[P1: 排障闭环]`
- KOReader 认证失败会写入 `koreader_sync_events`，状态区分为：
  - `auth_failed_invalid_key`
  - `auth_failed_forbidden`
- `GET /api/system/koreader` 会读取最近一次非成功事件，作为设置页排障提示来源。

#### 测试补充 `[工程质量]`
- 新增 / 更新 KOReader 相关测试，覆盖：
  - 合法 `sync_key` 登录成功
  - 非法 `sync_key` 配置保存被拒绝
  - KOReader vendor JSON `Accept` 头返回正确的 `Content-Type`
  - 旧格式无效存储 key 被识别为不可用
- 验证通过：
  - `GOCACHE=/Users/nicoer/dev/manga_manager/.gocache GOTMPDIR=/Users/nicoer/dev/manga_manager/.tmp go test ./...`
  - `npm run build` in `web/`
  - 配置用户名与同步密钥
  - 控制是否允许首次注册
  - 查看当前模式下的索引进度、已匹配/未匹配同步记录数

### 📌 增量记录 — 2026-04-15（KOReader 索引重建与模式感知文案）

#### KOReader 索引重建修正 `[P1: 资源识别与恢复能力]`
- **索引重建不再截断在 10000 条**：`RebuildBookIdentities` 和 `ReconcileProgress` 从一次性 `LIMIT 10000` 改为分页批处理。
  - `limit` 现在表示批次大小，当前任务入口默认按 `500` 条一批循环执行。
  - 重建流程会持续迭代直到所有缺失索引的书籍都处理完成。
  - 未匹配进度重关联同样改为全量分页扫描，不再只处理前一段记录。
- **数据库层新增分页/计数接口**：
  - `CountBooksMissingIdentity`
  - `CountUnmatchedKOReaderProgress`
  - `ListBooksMissingIdentityBatch`
  - `ListUnmatchedKOReaderProgressBatch`
  用于支持全量迭代和更准确的任务总量显示。
- **索引更新改为按字段选择性写入**：`UpdateBookIdentity` 现在只更新本次模式需要写入的字段，不会把未参与本轮重建的字段清空。

#### KOReader 匹配模式驱动索引构建 `[P1: 匹配模型一致性]`
- **二进制哈希模式与路径模式的索引构建正式分离**：
  - `binary_hash` 模式下只计算并写入 `file_hash`
  - `file_path` 模式下只计算并写入 `path_fingerprint` / `path_fingerprint_no_ext`
- **路径模式不再额外计算二进制哈希**，避免在仅使用路径匹配时仍进行不必要的大文件读取和哈希开销。
- **索引覆盖统计改为模式感知**：
  - `GET /api/system/koreader` 中的 `stats.hashed_books` 现在表示“当前匹配模式下已完成索引的书籍数”
  - 不再要求 `file_hash`、`path_fingerprint`、`path_fingerprint_no_ext` 三类字段同时存在才算完成

#### KOReader 任务文案与前端状态同步 `[P1: 配置与可运维性]`
- **后端任务消息改为模式感知**：
  - `rebuild_book_hashes` 在二进制模式下显示“二进制哈希索引”
  - `rebuild_book_hashes` 在路径模式下显示“路径索引”或“路径索引（忽略扩展名）”
  - `refresh_koreader_matching` 中的阶段消息与完成消息同步使用当前模式名称
- **任务参数快照补全**：`rebuild_book_hashes` 和 `reconcile_koreader_progress` 现在都会记录：
  - `match_mode`
  - `path_ignore_extension`
  便于日志页、仪表板和重试逻辑基于实际模式展示准确说明。
- **前端任务中心/仪表板/设置页文案更新**：
  - 日志页 `Logs` 中的 KOReader 任务标题和操作建议会根据当前模式动态显示“二进制哈希索引”或“路径索引”
  - 仪表板中的运行中/失败任务卡片同步显示模式感知的 KOReader 任务名称
  - 设置页中的“匹配索引进度”“重建匹配索引”按钮与变更提示，统一改为显示当前实际索引类型

#### 测试补充 `[Tests]`
- 新增 `internal/koreader/service_test.go`，覆盖：
  - 小批次参数下索引重建仍会分页处理完整批量数据
  - `file_path` 模式下只构建路径索引，不写入 `file_hash`
  - 一键触发“重建书籍指纹”和“重关联未匹配记录”
- `config.yaml` 示例文件新增 `koreader` 配置段：
  - `enabled`
  - `base_path`
  - `allow_registration`

#### 测试 `[回归保障]`
- 为 KOReader 设置保存、鉴权、进度写入与进度拉取补充控制器测试。
- 已验证：
  - `GOCACHE=/Users/nicoer/dev/manga_manager/.gocache GOTMPDIR=/Users/nicoer/dev/manga_manager/.tmp go test ./internal/api ./internal/config ./internal/scanner ./internal/database`
  - `npm run build`（`web/`）

### 📌 增量记录 — 2026-04-13（资源库级 KOReader 开关）

#### 资源库配置 `[P1: 库管理交互 + 同步范围控制]`
- **新增资源库级 KOReader 开关**：`libraries` 表新增 `koreader_sync_enabled` 字段，默认开启，保持现有“全库可同步”的行为不变。
- **创建/编辑资源库弹窗支持配置**：添加资源库和编辑资源库时，可以直接决定该资源库是否参与 KOReader 阅读进度同步。
- **旧请求兼容**：若旧客户端或脚本调用创建/更新资源库接口时未传 `koreader_sync_enabled`，后端会按“开启”处理，避免无意中把已有同步范围关掉。
- **同步匹配范围收敛**：KOReader 文档匹配现在只会命中 `koreader_sync_enabled = true` 的资源库；被关闭的资源库不会再接收新的 KOReader 进度投影。
- **侧边栏可见反馈**：资源库列表直接显示 “KOReader Sync 开启/关闭”，无需打开编辑弹窗才能确认状态。
- **库页状态提示**：资源库主页顶部新增当前库的 KOReader 状态提示条，会明确说明该库是否参与同步，以及系统级 KOReader 服务是否已启用。
- **直接编辑当前资源库**：库页提示条提供“编辑当前资源库”按钮，通过全局事件直接唤起对应资源库的编辑弹窗。
- **仪表板全局概览**：仪表板新增 KOReader Sync 覆盖范围卡片，展示：
  - 启用同步的资源库数量
  - 关闭同步的资源库数量
  - 已匹配同步记录数
  - 待重关联记录数

### 📌 增量记录 — 2026-04-13（KOReader 匹配模式重构）

#### 匹配方案重构 `[P0: KOReader 匹配规则收敛]`
- **直接切换到新方案，不做旧逻辑兼容**：KOReader 匹配现在只保留两种明确模式：
  - `binary_hash`
  - `file_path`
- **移除旧的隐式多路匹配**：不再采用“哈希优先，路径/文件名兜底”的混合逻辑，也不再使用文件名作为正式匹配输入。
- **路径模式规则固定**：`file_path` 模式只比较“文件名 + 向上最多两层路径”，更高层目录全部忽略。
- **路径模式新增扩展名开关**：可配置是否忽略扩展名；若不忽略，则按保留扩展名的路径片段精确匹配。
- **新增配置字段**：
  - `koreader.match_mode`
  - `koreader.path_ignore_extension`
- **新增路径索引字段**：
  - `books.path_fingerprint_no_ext`
- **保留但停用旧字段语义**：旧的 `filename_fingerprint` 不再参与 KOReader 新匹配方案。

#### 扫描器与重建任务 `[P1: 匹配索引维护]`
- 扫描器现在会为每本书同时生成：
  - `file_hash`
  - `path_fingerprint`
  - `path_fingerprint_no_ext`
- 原“重建书籍指纹”任务语义调整为 **重建 KOReader 匹配索引**。
- 重关联任务严格按当前 `match_mode` 执行，不再跨模式尝试。

#### 设置页与状态展示 `[P1: 可配置与可解释性]`
- 设置页 KOReader 区块新增匹配模式选择器和“忽略扩展名”开关。
- 设置页、仪表板、资源库页都会明确展示当前使用的是：
  - 二进制哈希匹配
  - 或文件路径匹配（并说明只比较向上两层路径）
- **未匹配记录排障视图**：设置页新增最近未匹配 KOReader 记录列表，展示原始文档标识、当前归一化匹配键、设备信息、时间与建议动作。
- **匹配规则变更维护入口**：当保存了新的匹配模式或扩展名规则后，设置页会提示执行维护操作；新增一键“应用匹配规则变更”，会顺序触发：
  - 重建 KOReader 匹配索引
  - 重关联未匹配记录

#### 系统接口 `[排障与维护]`
- 新增 `GET /api/system/koreader/unmatched`
- 新增 `POST /api/system/koreader/apply-matching`
- 任务中心新增任务类型 `refresh_koreader_matching`

#### 测试 `[回归保障]`
- 新增并通过以下场景测试：
  - 二进制哈希模式进度同步
  - 文件路径模式精确匹配
  - 文件路径模式忽略扩展名匹配

### 📌 增量记录 — 2026-04-13（第三次续改）

#### 任务上下文增强 `[P1: 改善日志与任务中心]`
- **任务列表新增 `scope_id` 精确过滤**：后端任务接口支持按具体系列或资源库 ID 拉取任务，前端不再需要使用文本模糊匹配。
- **任务对象新增 `scope_name` 展示字段**：常见任务在创建时会记录目标名称，例如资源库名、系列名或“系统”“全库”等上下文名，前端可以直接显示更可读的任务对象。
- **仪表板失败任务卡片可跳转**：Dashboard 中的失败任务不再只是只读文本，点击后可直接进入关联系列、资源库或日志页。
- **日志页任务卡片增加类型标签与动作建议**：任务中心会显示任务类型中文标签、目标名称，以及基于任务类型的建议动作提示。
- **系列页失败任务卡片增加上下文名**：系列页中的失败任务面板会显示任务类型和目标名称，减少排错时的判断成本。

### 📌 增量记录 — 2026-04-13（第四次续改）

#### 任务看板可读性增强 `[P1: 改善日志与任务中心]`
- **日志页新增任务统计卡片**：在原有日志统计之外，增加运行中任务、失败任务、已完成任务三类任务统计，减少用户在长列表里手动判断状态分布的成本。
- **日志页任务按时间分组**：任务中心现在会按“今天 / 昨天 / 更早”分组展示任务，并同时显示相对时间和绝对时间。
- **日志页任务卡片信息量提升**：
  - 展示任务类型中文标签和目标上下文；
  - 展示基于任务类型的建议动作；
  - 保留原有重试和打开关联页面能力。

#### 仪表板恢复入口增强 `[P2: 仪表板更产品化]`
- **仪表板新增运行中任务面板**：除失败任务外，也会展示当前正在执行的后台任务，方便用户在首页快速了解系统活动。
- **仪表板失败任务区增加统一入口**：失败任务区新增“打开任务中心”按钮，便于用户从首页快速进入更完整的恢复界面。

### 📌 增量记录 — 2026-04-13（第五次续改）

#### 任务生命周期收口 `[P1: 改善日志与任务中心]`
- **任务清理 API**：新增 `DELETE /api/system/tasks`，支持通过 `status` 和 `scope` 过滤条件批量清理任务记录。
- **任务保留上限**：任务内存池加入基础保留策略，最多保留最近 `200` 条任务，避免任务中心无限膨胀。
- **日志页任务清理入口**：任务中心新增“清理已完成任务”和“清理失败任务”两个快捷动作，操作后会立即刷新列表。

### 📌 增量记录 — 2026-04-13（第六次续改）

#### 任务详情下钻 `[P1: 改善日志与任务中心]`
- **日志页任务卡片支持展开详情**：任务中心中的任务卡片可展开查看更多上下文，而不是只显示一行摘要。
- **新增任务详情内容**：
  - 开始时间与结束时间
  - 参数快照（如 `force`、`provider` 等任务启动参数）
  - 错误详情原文
  - 更明确的进度展示（当前值 / 总数 / 百分比）
- 这项改动不引入新接口，只消费现有任务对象中的 `params`、`started_at`、`finished_at` 和 `error` 字段。

### 📌 增量记录 — 2026-04-16（外部资源库任务链路修复）

#### 外部资源库管理与同步能力落地 `[P1: 外部资源管理]`
- **新增类似 Calibre 设备管理的外部资源库工作流**：本地资源库现在可以把一个外部目录作为“外部资源库”来管理，用于识别哪些资源已经存在，以及把缺失资源批量同步过去。
- **支持指定外部资源库位置并即时扫描**：
  - 在资源库页选择一个外部目录后，系统会创建一个外部资源会话；
  - 后端会递归扫描该目录中的归档资源，并生成当前会话的外部资源状态快照。
- **支持与当前主资源库做内容对比**：
  - 对比逻辑基于主资源库中的相对路径与文件名；
  - 会把扫描结果映射回当前资源库中的系列与书籍，计算“已命中 / 总数”。
- **资源库页直接展示外部存在性信息**：
  - 当前页增加外部存在性摘要；
  - 每个系列卡片可直接看到该系列在外部资源库中的存在情况、命中数量和进度条；
  - 用户无需进入系列详情页即可判断哪些资源已同步、哪些仍缺失。
- **支持批量传输到外部资源库**：
  - 资源库页可多选系列；
  - 提交后仅同步外部资源库中缺失的书籍，不覆盖已存在文件；
  - 传输目标目录保持主资源库中的原始相对路径结构。
- **任务系统接入外部扫描与传输**：
  - 新增外部资源库扫描任务
  - 新增外部资源库传输任务
  - 可在底部全局任务浮层和任务中心查看进度与结果

#### 外部资源库扫描/传输稳定性修复 `[P1: 外部资源管理]`
- **故障根因确认**：外部资源库扫描和传输任务在批量处理时，会对 `c.updateTask()` 进行高频调用；每次调用都会向 SSE Broker 写入一条 `task_progress` 消息。
- **SSE 消息洪水问题**：
  - 服务端 `c.messages` 通道容量有限；
  - 外部扫描/传输在处理大量文件且速度很快时，会在短时间内塞入大量中间态消息；
  - 当客户端消费速度跟不上时，后续关键消息可能无法及时送达，包括：
    - 最终 `completed` 任务状态
    - 任务结束后的 `refresh` 页面刷新通知
- **表面症状统一解释**：此前反复出现的两个问题，其实来自同一底层并发通信故障：
  - 扫描进度条停留在中间状态，不会收口；
  - 传输完成后，本地资源库页中的外部存在性标记不刷新。

#### 修复策略 `[后端通信收敛]`
- **扫描任务增加节流**：在外部资源扫描的进度回调中加入时间节流控制，不再为每一个文件都推送一次 SSE 进度；中间态更新收敛为固定时间窗内最多一次。
- **传输任务增加节流**：外部资源传输循环中的 `updateTask()` 调用同样改为节流发送，避免“开始复制”和“复制完成”两类中间态把消息通道挤满。
- **优先保证尾部关键消息送达**：在中间态消息被显著削减后，最终的：
  - `c.finishTask(...)`
  - `c.PublishEvent("refresh")`
  能稳定进入消息通道并送达前端。

#### 任务链路重构 `[前后端状态统一]`
- **外部扫描/传输接口显式返回 `task_key`**：
  - `POST /api/libraries/{libraryId}/external-libraries/session` 现在返回 `{ session, task_key }`
  - `POST /api/libraries/{libraryId}/external-libraries/session/{sessionId}/transfer` 现在返回 `task_key`
- **前端改为按 `task_key` 跟踪任务**：资源库页不再通过任务类型、作用域或 `session_id` 猜测“哪一个任务属于当前页面”，而是显式记录：
  - `externalScanTaskKey`
  - `externalTransferTaskKey`
- **扫描进度条补收口机制**：若扫描会话已经从 `scanning` 进入 `ready/failed`，但底部全局任务浮层还未收到最终完成态，前端会按同一个 `task_key` 主动补一条最终状态，用于收口进度条并自动隐藏。
- **全局任务浮层增强**：`Layout` 中的全局 `taskProgress` 结构新增 `key` / `params` 字段，并支持通过内部覆盖事件按 `task_key` 精确修正当前展示中的任务状态。

#### 动态接口缓存修复 `[状态刷新可靠性]`
- **外部资源会话状态接口禁缓存**：
  - `GET /api/libraries/{libraryId}/external-libraries/session/{sessionId}`
  - `GET /api/libraries/{libraryId}/external-libraries/session/{sessionId}/series`
  均显式返回 `Cache-Control: no-store` 与 `Pragma: no-cache`。
- **前端请求增加时间戳参数**：资源库页在拉取外部会话状态和系列存在性状态时追加 `_ts` 参数，避免浏览器或中间层复用旧响应。

#### 资源库页 UI 与交互完善 `[外部资源可视化]`
- **当前页外部存在性摘要**：资源库页新增“当前页外部存在情况”摘要卡，按当前分页结果展示：
  - 已完整存在
  - 部分存在
  - 尚未同步
- **外部资源扫描新增扩展名匹配开关**：
  - 在资源库页“同步资源库”面板中，可为当前外部扫描会话选择“匹配时是否忽略外部资源库文件扩展名”；
  - 默认仍保持严格匹配，即相对路径与扩展名都必须一致；
  - 勾选后，外部资源库中的 `.zip/.cbz` 等同路径同文件名资源会被视为同一资源，不再重复传输。
- **系列卡片新增独立外部状态区**：
  - 直接显示状态标签（已完整存在 / 部分存在 / 尚未同步）
  - 显示 `命中数 / 总数`
  - 显示命中比例进度条
- **状态展示移出封面叠层**：外部状态不再覆盖评分、操作按钮和封面底部信息，而是放到卡片正文中的独立区块，减少视觉冲突。
- **资源库跨页多选**：
  - 资源库页的批量选择状态在翻页时不再清空，支持跨页累计选择系列；
  - 仅在切换资源库、切换筛选条件、切换排序方式或手动退出批量模式时清空选择；
  - 顶部工具栏新增“全选本页 / 取消本页”；
  - 底部批量操作栏会同时显示总选择数与当前页命中数，便于跨页整理后统一执行传输、收藏或加入合集。

#### 任务完成后的页面刷新 `[存在性数据闭环]`
- 资源库页在收到外部扫描/传输任务的完成态后，会显式重拉：
  - 当前外部会话状态
  - 当前页系列的外部存在性状态
- 同时，收到全局 `refresh` 时，如果当前页面存在外部资源会话，也会顺带重取外部存在性数据，确保“系列列表刷新”和“外部命中状态刷新”保持一致。

#### 用户可见结果 `[UI 恢复闭环]`
- 扫描外部资源库时，底部全局进度条不再长时间卡在中途。
- 传输到外部资源库完成后，资源库页中的“外部存在性”状态可随 `refresh` 正常重取并更新。
- 同时降低了高频任务更新对前端 React 渲染造成的额外压力，使外部资源库页面在大批量文件操作时更平稳。

#### 验证说明 `[回归保障]`
- 本次修复聚焦于 `internal/api/external_controller.go` 中的外部扫描与传输任务推进逻辑。
- `internal/api/controller_test.go` 已补充对外部资源扫描/传输响应体中 `task_key` 的校验。
- `internal/api/controller_test.go` 也已补充“忽略扩展名”匹配开关的回归测试，覆盖：
  - 默认要求扩展名一致
  - 勾选后按无扩展名路径匹配并跳过重复传输
- 修复后的核心目标不是增加更多前端补丁，而是从后端消息流层面保证：
  - 进度中间态适度发送；
  - 任务完成态和 `refresh` 刷新信号稳定送达。

### 📌 增量记录 — 2026-04-17（弹出框视觉系统统一）

#### 弹出框视觉骨架统一 `[P1: 用户交互与视觉一致性]`
- **新增统一弹出框基座**：前端新增通用 `ModalShell`，统一管理遮罩、圆角、边框、阴影、头部标题区、正文滚动区、底部操作区和进入动画。
- **新增共享样式令牌**：补充一组弹窗内通用按钮、输入框、选择器、分区块和标签样式，避免不同弹窗继续各自维护一套深色样式。
- **交互行为同步收敛**：
  - 打开弹窗时锁定页面滚动；
  - 支持 `Esc` 关闭；
  - 遮罩点击关闭与关闭按钮风格保持一致；
  - 大型弹窗统一为“面板内滚动”，避免整页与弹窗双滚动混杂。

#### 主要业务弹窗统一换壳 `[P1: 用户交互与视觉一致性]`
- **已统一到同一套弹窗视觉语言的模块**：
  - 添加到合集
  - 添加/编辑资源库
  - 全局搜索
  - 系列元数据编辑
  - 元数据来源预览与应用
  - 合集页“新建合集”
- **视觉效果调整方向**：
  - 靠拢当前首页、仪表板和设置页的深色卡片风格；
  - 强化头部层级、正文留白和页脚操作区；
  - 取消此前各弹窗在边框、圆角、按钮样式和阴影上的不一致表现。

#### 原生浏览器弹窗清理 `[P1: 用户交互与视觉一致性]`
- **新增统一确认弹窗组件**：新增 `ConfirmDialog`，用于替换系统中残留的原生 `confirm()`。
- **系统内原生确认框已统一替换**：
  - 删除合集
  - 强制全量扫描资源库
  - 批量刮削缺失元数据
  - 清理失效资源
  - AI 智能分组
  - 删除资源库
  - 传输到外部资源库确认
- **原生提示框已统一改为页面内反馈**：
  - 原有 `alert()` 成功/失败提示改为项目现有 toast 风格；
  - `Layout` 也新增统一 toast，用于承接资源库管理相关的成功/失败反馈。

#### 用户可见结果 `[界面一致性]`
- 主要弹出框现在与站内页面卡片风格一致，不再出现“某些是自定义面板、某些还是浏览器原生框”的割裂感。
- 资源库操作和外部资源库传输确认不再跳出浏览器默认对话框，整体视觉和交互体验更连续。
- 后续新增确认类操作可直接复用统一确认弹窗，避免再次引入原生浏览器弹窗。

### 📌 增量记录 — 2026-04-18（设置页结构重构）

#### 设置页信息架构重构 `[P1: 重构设置页信息架构]`
- **设置页由单页堆叠改为多子页面结构**：`/settings` 现在作为设置壳层路由，内部拆分为：
  - `概览`
  - `外观`
  - `库与扫描`
  - `图片与缓存`
  - `AI / 元数据`
  - `KOReader`
  - `维护工具`
- **新增左侧设置导航**：桌面端改为左侧设置导航，移动端提供顶部分类切换，不再把所有配置和操作挤在同一页里。
- **概览页新增状态总览**：设置首页改为摘要面板，展示配置健康状态、已绑定目录数、KOReader 状态和主题摘要，并提供进入对应设置页的入口。

#### 设置数据流重构 `[P1: 设置页可维护性]`
- **新增共享设置状态容器**：将原 `Settings.tsx` 中的大量请求、保存、校验、KOReader 管理逻辑抽离到统一的 `SettingsContext`，由各子页面按需读取。
- **子页面独立保存模型落地**：
  - `外观` 继续本地即时生效；
  - `库与扫描`、`图片与缓存`、`AI / 元数据` 分页独立保存；
  - `KOReader` 保持服务配置单独保存，账号与维护动作继续即时执行；
  - `维护工具` 页面仅保留即时后台任务入口，不再混入持久配置。
- **当前已绑定目录归位**：原来放在设置页底部的“当前已绑定目录”已并入 `库与扫描` 页面，更符合信息归类。

#### 未保存修改拦截 `[P1: 设置页交互改进]`
- **设置页内部切换拦截**：当前设置分组有未保存修改时，切换到其他设置分类会弹统一确认框。
- **离开设置页拦截**：从设置页跳转到仪表板、资源库、日志等其他路由时，同样会拦截未保存修改并提示确认。
- **浏览器刷新/关闭保护**：保留 `beforeunload` 提示，避免刷新页面时直接丢失当前分组的编辑内容。

#### 浏览器路由兼容修复 `[Bug Fix]`
- **修复设置页进入即崩溃的问题**：此前尝试直接使用 `useBlocker` 做未保存拦截，但当前项目仍运行在 `BrowserRouter` 下，导致打开设置页时报错：
  - `useBlocker must be used within a data router`
- **改为兼容 `BrowserRouter` 的导航拦截方案**：设置页现在通过底层导航上下文实现自定义阻塞，不再要求整个项目迁移到 data router。

#### 请求循环修复 `[Bug Fix]`
- **修复设置页持续刷新和反复请求后端的问题**：
  - 根因是 `SettingsContext` 中 `fetchConfig` / `fetchKOReader` 的 `useCallback` 依赖链包含会在请求完成后再次变化的状态；
  - 这些 callback 变化又触发初始化 `useEffect` 重新执行，形成请求循环。
- **修复方式**：
  - 将初始化请求函数改为稳定引用；
  - 使用 `ref` 保存最新 `config` 与 `koreaderStatus`；
  - 避免在初始化加载链路中因为状态变化反复重建 callback。
- **用户可见结果**：
  - 进入设置页后不再持续刷新；
  - 不再反复请求 `/api/system/config`、`/api/system/koreader` 等接口。

#### 验证说明 `[回归保障]`
- 设置页重构后已通过前端构建验证：`npm run build`
- 当前重构重点放在：
  - 结构拆分
  - 保存边界收口
  - 未保存修改保护
  - 请求稳定性修复
- 尚未在本轮中继续做更细的按页懒加载请求优化；当前目标是先保证设置页稳定、清晰且不再失控刷新。

### 📌 增量记录 — 2026-04-18（GitHub Actions Node 24 兼容）

#### CI / Release 工作流兼容升级 `[P1: 构建与发布链路]`
- **处理 GitHub Actions Node 20 弃用警告**：CI 和 Release 工作流已切换到 Node 24 兼容模式，避免后续 GitHub runner 默认运行时升级后产生不确定行为。
- **顶层环境变量已启用**：
  - `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24=true`
- **Actions 版本同步升级**：
  - `actions/checkout@v5`
  - `actions/setup-go@v6`
  - `actions/setup-node@v5`
  - `actions/upload-artifact@v5`
  - `actions/download-artifact@v6`

#### 用户可见结果 `[发布链路稳定性]`
- 触发 GitHub `CI` 和 `Release` 工作流时，不再继续沿用即将弃用的 Node 20 JavaScript action 运行时。
- 后续 GitHub 从 Node 20 默认切换到 Node 24 时，当前仓库的构建和发布链路具备更好的前向兼容性。

### 📌 增量记录 — 2026-04-18（阅读器页图缓存修复）

#### 翻页预加载去重 `[Bug Fix]`
- **修复翻页时重复预加载后续页的问题**：此前阅读器每次翻页都会重新按“当前页后的预加载窗口”创建 `Image()` 请求，导致窗口重叠部分被反复请求。
- **新增按最终图片 URL 去重的预加载缓存**：
  - 同一张页图在同一组图像处理参数下只会预加载一次；
  - 只有在切换书籍或切换图像处理参数时，预加载去重缓存才会重置。

#### 当前页图像缓存 `[Bug Fix]`
- **预加载改为真正的前端页图缓存**：阅读器现在通过 `fetch -> blob -> object URL` 预取页图，并把结果缓存到前端内存中。
- **翻页时优先复用已缓存页图**：翻到下一页时，如果该页已在预加载阶段取回，就直接使用已缓存的 object URL，而不是重新请求 `/api/pages/...`。

#### 图像处理切换双请求修复 `[Bug Fix]`
- **修复切换图像处理方式时当前页连续触发两次相同请求的问题**：
  - 之前一条链路来自前端 `fetch` 预热；
  - 另一条链路来自 `<img>` 在缓存未就绪时直接回退到网络 URL。
- **翻页模式渲染策略已收紧**：
  - 当前页和双页跨页只使用前端缓存过的 object URL；
  - 在缓存尚未完成时，显示轻量加载占位；
  - 不再对翻页模式下的当前页使用网络 URL fallback，因此不会再出现同页同时 `fetch + img` 再请求一次。

#### 用户可见结果 `[阅读体验稳定性]`
- 缓存页数设为 5 时，从第 1 页翻到第 2 页，不会再重复请求之前已经预加载过的 `3-6` 页。
- 切换图像处理方式后，当前页不会再立刻对同一资源发起两次并行请求。
- 翻页模式下，下一页在预加载已命中的情况下会直接使用阅读器内存缓存，提高翻页稳定性。

### 📌 增量记录 — 2026-04-20（Windows 测试兼容修复）

#### Go 测试链路的 Windows 阻塞点修复 `[P1: 跨平台测试]`
- **修复 Windows 上 `go test ./...` 先卡死在图像处理依赖编译阶段的问题**：此前 `internal/images` 直接依赖 `github.com/chai2010/webp`，在 Windows 测试链路下会先因为 `webp` 编译失败而无法进入业务测试。
- **引入按平台分离的 WebP 编码实现**：
  - 非 Windows 平台继续使用现有 `chai2010/webp`；
  - Windows 平台改为兼容降级策略，避免测试和编译链路被 `webp` 依赖阻断。

#### 缩略图输出路径兼容收口 `[Bug Fix]`
- **缩略图文件扩展名不再只依赖请求的目标格式推断**：扫描器现在会根据 `ProcessImage` 的实际返回 `content-type` 决定封面缓存文件后缀。
- **用户可见结果**：
  - 当平台兼容分支改变实际输出格式时，缩略图路径和文件后缀仍然保持一致；
  - 避免出现“内容实际已降级为 PNG，但文件名仍写成 `.webp`”这类错位。

#### 测试中的 Unix 路径假设修复 `[Bug Fix]`
- **修复控制器测试中写死 Unix 路径的问题**：
  - 不再使用 `"/definitely/missing"`、`"/definitely/missing/cache"` 之类路径；
  - 改为基于 `t.TempDir()` 生成跨平台缺失路径。
- **修复路径参数直接拼接到 URL 的问题**：
  - `browse` 相关测试现在会对文件系统路径执行 URL 编码；
  - 避免 Windows 临时目录中的盘符和反斜杠破坏 query string 解析。

#### CI 增加 Windows 覆盖 `[P1: 持续集成]`
- **CI 工作流已改为多平台 matrix**：
  - `ubuntu-latest`
  - `windows-latest`
- **结果**：
  - 之前只在 Linux 上可见的问题，现在会在 CI 中直接暴露；
  - Windows 不再是“理论支持但无人验证”的状态。

#### 验证说明 `[回归保障]`
- 已通过本地验证：
  - `GOCACHE=/Users/nicoer/dev/manga_manager/.gocache GOTMPDIR=/Users/nicoer/dev/manga_manager/.tmp go test ./internal/api ./internal/scanner ./internal/images`
  - `npm run build`
- 额外检查：
  - `GOOS=windows GOARCH=amd64 go test ./...`
  - 当前已不再报此前的 `chai2010/webp` 编译错误；
  - 在 macOS 本地继续失败为 `exec format error`，这是因为交叉编出的 Windows 测试二进制无法在当前宿主机直接执行，属于预期现象。

### 📌 增量记录 — 2026-04-20（资源库册数量排序修复）

#### 资源库分页排序修复 `[Bug Fix]`
- **修复资源库界面按“册数量”排序时报错的问题**：此前 `SearchSeriesPaged()` 的动态排序分支使用了 `actual_book_count` 作为 `ORDER BY` 字段，但 SQL 查询中并没有这个列或别名，导致 SQLite 报错：
  - `SQL logic error: no such column: actual_book_count`
- **修复方式**：
  - 排序改为直接使用真实列 `s.book_count`；
  - 查询结果中的 `ActualBookCount` 现在显式对齐到 `BookCount`，避免前端读取零值。

#### 验证说明 `[回归保障]`
- 已通过本地验证：
  - `GOCACHE=/Users/nicoer/dev/manga_manager/.gocache GOTMPDIR=/Users/nicoer/dev/manga_manager/.tmp go test ./internal/api ./internal/database`
  - `npm run build`

### 📌 增量记录 — 2026-04-21（系列详情页打开目录）

#### 系列目录快速打开 `[体验增强]`
- **系列详情页新增“打开目录”按钮**：在系列总览头部操作区增加目录打开入口，位置与编辑、添加到合集、重新扫描等操作保持一致。
- **通过系统默认文件管理器打开当前系列目录**：
  - macOS 使用 `open`
  - Windows 使用 `explorer.exe`
  - Linux 使用 `xdg-open`
- **交互约束**：
  - 仅在系列总览显示，不在卷视图显示；
  - 点击后提供成功/失败 toast；
  - 按钮带 loading 态，避免重复点击。

#### 后端接口与测试 `[实现细节]`
- **新增系列级专用接口**：`POST /api/series/{seriesId}/open-dir`
- **接口仅允许打开当前系列自身目录**，不暴露任意路径打开能力。
- **新增控制器测试覆盖**：
  - 正常打开目录
  - 系列不存在
  - 打开文件管理器失败

## [Unreleased] — 2026-04-10

> 本次集中改动对应 [面向公开发布的系统性改进路线图] 中的 **P0 发布阻塞** 全部五项、**P1 产品完善**（设置页信息架构、日志与任务中心、库管理交互、元数据工作流）以及 **P2 体验增强**（仪表板产品化、文案与视觉统一）。下方各节标题中的 `[Pn]` 标记对应路线图条目。
