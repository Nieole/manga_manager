# 性能改造待办清单

更新日期：2026-05-22

本清单与 `docs/performance-north-star-plan.md` 的 8 个阶段一一对应。

状态说明：

- `[x]` 已完成并验证。
- `[~]` 部分完成，仍有剩余拆项。
- `[ ]` 未完成。
- `[F]` 当前约束下冻结或暂缓。

## 阶段 1：阅读路径瘦身

- [x] 默认阅读原图透传。
  - [x] 前端不再把 `bilinear`、`average`、`nearest` 传给后端。
  - [x] 后端把浏览器端滤镜视为无服务端处理。
  - [x] 默认阅读不进入无意义图片解码/编码路径。
  - [x] 单测覆盖 `bilinear` 透传。
- [x] 书籍页清单缓存。
  - [x] 新增内存型 manifest cache。
  - [x] key 包含 `book_id + path + file_modified_at + size`。
  - [x] value 为 `[]PageMetadata`。
  - [x] `getBookArchivePage` 复用 manifest。
  - [x] `/api/pages/{bookId}` 页列表路径复用同一套 manifest 读取逻辑。
  - [x] 文件 mtime/size 变化后 cache 自动失效。
  - [x] 单测覆盖 manifest cache 命中和失效。
  - [x] 扫描、重扫任务完成时显式清理相关 manifest cache。
- [x] 减少每页 DB 查询。
  - [x] 设计 `BookPageSource` 轻量缓存。
  - [x] 缓存 `book_id -> path/mtime/size/page_count`。
  - [F] 不采用“把 book 元信息附加到 manifest cache value”的备选方案，当前选择独立 `BookPageSource` 缓存。
  - [x] 为文件变化、扫描、重扫补失效机制。
  - [x] 增加单测覆盖缓存命中。
- [x] 阅读进度写入节流。
  - [x] 前端只在页码变化稳定后写入。
  - [x] Webtoon 模式避免滚动时高频 POST。
  - [x] 后端跳过页码未前进的 `reading_activity` 重复写。
  - [x] 后端跳过极短间隔重复写。
  - [x] 增加阅读进度写入节流测试。
- [x] 阶段 1 benchmark。
  - [x] `BenchmarkGetPagesByBook_WithManifestCache`。
  - [x] 同一本书连续读 50 页，验证默认原图连续翻页热路径。

## 阶段 2：Webtoon 渲染窗口化

- [x] Webtoon 虚拟列表。
  - [x] 确认 `react-virtuoso` 当前依赖可用。
  - [x] 将 Webtoon 渲染迁移为虚拟列表。
  - [x] 只渲染当前视口附近页面。
  - [x] 保留阅读进度定位。
  - [x] 保留滚动到上次页。
  - [x] 保留书签跳转。
- [~] 图片懒加载与近屏预热。
  - [x] 当前页预热。
  - [x] 下一页预热。
  - [x] 后续 N 页预热。
  - [x] 视口外释放 object URL。
  - [x] 保留当前书少量缓存。
  - [x] 保留下一卷少量缓存。
  - [x] 离线阅读缓存逻辑不被破坏。
- [x] `IntersectionObserver` 简化。
  - [x] 移除 Webtoon 全量 `querySelectorAll('.ReaderScrollContainer img')` 监听。
  - [x] 虚拟项内部上报当前页。
  - [x] 补构建验证。
- [~] 阶段 2 验收。
  - [x] Webtoon DOM 图片节点数量由虚拟窗口控制。
  - [ ] 移动端滚动无明显掉帧。

## 阶段 3：首页资源库查询重构

- [x] 新增 `series_stats` 或等价冗余结构。
  - [x] 确定 schema。
  - [x] 写迁移。
  - [x] 生成 sqlc。
  - [x] 字段：`series_id`。
  - [x] 字段：`cover_path`。
  - [x] 字段：`cover_book_id`。
  - [x] 字段：`read_pages`。
  - [x] 字段：`read_book_count`。
  - [x] 字段：`completed_book_count`。
  - [x] 字段：`last_read_at`。
  - [x] 字段：`last_read_book_id`。
  - [x] 字段：`tag_names_cache`。
  - [x] 字段：`author_names_cache`。
- [x] 维护 `series_stats`。
  - [x] 扫描时维护。
  - [x] 阅读进度更新时维护。
  - [x] 元数据更新时维护。
  - [x] 封面更新时维护。
  - [x] 标签/作者变化时维护。
- [x] 改写 `SearchSeriesPaged`。
  - [x] 默认列表只查 `series + series_stats`。
  - [x] 移除默认路径中的窗口函数。
  - [x] 移除默认路径中的 `SUM(last_read_page)`。
  - [x] 移除默认路径中的重复 tag/author join。
  - [x] tag/author 筛选时才 join 关联表。
- [~] total 计算策略优化。
  - [x] 无筛选场景直接 `COUNT(*) WHERE library_id = ?` 或缓存。
  - [x] 筛选场景再算 count。
  - [ ] 评估前端 total 延迟更新。
- [x] 分页深页优化。
  - [x] name 排序 cursor/keyset。
  - [x] updated 排序 cursor/keyset。
  - [x] created 排序 cursor/keyset。
  - [x] favorite 排序 cursor/keyset。
  - [x] 保留 page number UI 兼容。
- [~] 查询计划和 benchmark。
  - [x] 已有 `cmd/queryplan` 基础工具。
  - [x] 已有大库查询索引阶段。
  - [x] 为新 `series_stats` 查询增加 queryplan 检查。
  - [x] `BenchmarkSearchSeriesPaged` 扩展到 1 万系列。
  - [x] 默认列表 P95 < 300ms 验收记录。

## 阶段 4：系列详情首屏合并

- [x] 统一上下文接口。
  - [x] 设计或强化 `GET /api/series/{seriesId}/context`。
  - [x] 返回 series info。
  - [x] 返回 books。
  - [x] 返回 tags。
  - [x] 返回 authors。
  - [x] 返回 links。
  - [x] 返回 volumes summary。
  - [x] 返回 pending metadata review 数据与数量。
  - [x] 返回 failed task summary。
  - [x] 返回 relations summary。
- [x] `SeriesDetail` 首屏接口合并。
  - [x] 首屏只请求 1 条主接口。
  - [x] 编辑弹窗打开时再加载全量 tags/authors。
  - [x] 关系搜索输入后再加载候选。
  - [x] 刮削搜索打开时再加载。
- [~] 大系列分卷懒渲染。
  - [x] 默认只渲染当前卷入口。
  - [x] 折叠卷不渲染所有书籍按钮。
  - [ ] 500 本书以内首屏稳定的浏览器实测记录。
- [x] 功能回归。
  - [x] 编辑功能走 context 刷新。
  - [x] 批量标记已读走 context 刷新。
  - [x] 批量标记未读走 context 刷新。

## 阶段 5：扫描管线轻量化

- [x] 扫描分级。
  - [x] `fast_scan`。
  - [x] `metadata_scan`。
  - [x] `identity_scan`。
  - [x] `repair_scan`。
  - [x] 配置/API 支持选择扫描等级。
  - [x] 设置页支持切换扫描等级。
- [~] 未变化文件快速跳过。
  - [x] path/mtime/size 比较。
  - [x] 未变化文件不 `OpenArchive`。
  - [x] 仅 size 变化会触发重扫。
  - [x] benchmark 覆盖 no changes 增量扫描。
- [~] KOReader hash 按需化。
  - [x] KOReader 已有启用开关。
  - [x] KOReader 未启用时不计算 `FingerprintFile`。
  - [x] `identity_scan` / `repair_scan` 计算 `quick_hash`。
  - [x] KOReader binary hash 启用时按需补算 full hash。
  - [x] full hash 放后台低优先级批处理。
- [x] 封面生成队列化。
  - [x] 扫描时缺封面只入队。
  - [x] 后台按并发生成缩略图。
  - [x] 首屏可先显示占位图。
  - [x] 封面完成后通过批量刷新事件通知前端。
- [x] 扫描任务可取消。
  - [x] ctx 贯穿 worker。
  - [x] ctx 贯穿 ingester。
  - [x] ctx 贯穿封面任务。
  - [x] 任务中心提供取消能力。
- [ ] 阶段 5 验收。
  - [x] 单测覆盖未变化文件不打开归档。
  - [ ] 扫描期间阅读翻页延迟不明显上升。
  - [x] 首次扫描可以边入库边逐步生成封面。

## 阶段 6：Dashboard / 推荐 / 诊断懒加载

- [x] Dashboard summary cache。
  - [x] 设计 `dashboard_stats_cache` 或内存 TTL cache。
  - [x] 扫描结束刷新。
  - [x] 阅读进度更新刷新。
  - [x] 页面加载不实时全表 COUNT/SUM。
- [~] 推荐懒加载。
  - [x] 推荐请求已独立加载，不阻塞 Dashboard 主体。
  - [x] Dashboard 推荐区进入视口后再加载。
  - [x] 资源库页 AI 推荐只在用户点击生成/刷新时请求。
  - [x] 推荐失败不影响首页主体。
- [~] 标签/作者按需搜索。
  - [x] 首页不再默认拉 `/api/tags/all`。
  - [x] 首页不再默认拉 `/api/authors/all`。
  - [x] 展开筛选面板或已有筛选值需要回显时再加载标签/作者。
  - [x] 筛选框输入时远程搜索。
  - [x] 热门标签/作者按使用频率返回前 N 个。
- [~] 诊断模式。
  - [x] 连接中心已有真实请求诊断。
  - [x] Dashboard 已有系统性能摘要。
  - [x] Dashboard 首屏不再请求任务中心。
  - [x] Dashboard 首屏不再请求 KOReader 概览。
  - [x] Dashboard 首屏不再请求性能诊断。
  - [x] 日志默认不参与主界面请求。
  - [x] 健康报告默认不参与主界面请求。
  - [x] 进入维护页后再加载。
- [ ] 阶段 6 验收。
  - [x] 首页首屏请求数量减少。
  - [x] Dashboard 首屏请求数量减少。
  - [x] 大量 tags/authors 不影响资源库页初次打开速度。
  - [x] 标签/作者筛选不会为了打开资源库页而拉取全量列表。
  - [x] Dashboard 核心统计重复打开命中缓存，进度与扫描变更后失效。

## 阶段 7：外部客户端能力按需启用

- [x] 统一协议配置。
  - [x] `protocols.opds.enabled`。
  - [x] `protocols.mihon.enabled`。
  - [x] `koreader.enabled` 已存在。
  - [x] 设置页接入 OPDS/Mihon 开关。
- [~] 路由按配置挂载或运行时拒绝。
  - [x] OPDS 关闭时不暴露入口。
  - [x] Mihon 关闭时不暴露入口。
  - [x] 关闭时连接中心不显示相关端点。
  - [x] Dashboard 不请求 KOReader 诊断。
  - [x] 健康报告不计算 KOReader 未匹配项。
- [x] 协议查询独立优化。
  - [x] OPDS 搜索使用 FTS 或 search engine。
  - [x] Mihon 搜索使用 FTS 或 search engine。
  - [x] 最近更新走 `series_stats`。
  - [x] 继续阅读走 `series_stats`。
- [x] 阶段 7 验收。
  - [x] 默认本地 Web 模式无 OPDS/Mihon/KOReader 连接中心端点与健康报告 KOReader 查询。
  - [x] 开启协议后路由兼容性测试通过。

## 阶段 8：观测与基准体系

- [~] 后端 benchmark 扩展。
  - [x] `BenchmarkSearchSeriesPaged_10k`。
  - [x] `BenchmarkServePageImage_RawConsecutivePages`。
  - [x] `BenchmarkGetPagesByBook_WithManifestCache`。
  - [x] `BenchmarkScanLibrary_Incremental_NoChanges`。
  - [x] 已有 parser/images/database 基础 benchmark。
- [~] 查询计划检查扩展。
  - [x] 已有 `cmd/queryplan`。
  - [x] 首页默认排序。
  - [x] 首页 updated/favorite/name。
  - [x] 系列详情 books。
  - [x] recent read。
  - [x] dashboard stats。
- [~] 请求指标补充。
  - [x] page image 已有 `cache_hit`。
  - [x] page image 已有 `cache_source`。
  - [x] page image 已有 `book_id`。
  - [x] page image 已有 `page_number`。
  - [x] page image 已有 `transform`。
  - [x] page image 增加 `archive_open`。
  - [x] page image 增加 `manifest_cache_hit`。
  - [x] page image 增加 `raw_passthrough`。
  - [x] page image 增加 `processed`。
  - [x] scan 增加 `opened_archives`。
  - [x] scan 增加 `hashed_files`。
  - [x] Webtoon DOM 图片数。
  - [x] 首屏请求数。
  - [x] 大库列表渲染耗时。
- [~] 性能基线产物。
  - [x] 本批性能改造已补 benchmark 与查询计划对比记录。
  - [x] 新增性能基线记录模板。
  - [x] 保存 1 万系列样本结果。
  - [x] 保存 10 万系列样本结果。

## 推荐下一批

### 批次 1：阶段 1 收口

- 减少每页 DB 查询，落地 `BookPageSource` 缓存。
- 阅读进度写入节流。
- 增加同一本书连续 50 页 benchmark。

### 批次 2：阶段 2 Webtoon 预窗口化

- 先做图片懒加载与视口外 object URL 释放。
- 再替换为 `react-virtuoso` 虚拟列表。
- 最后清理全量 `IntersectionObserver`。

### 批次 3：阶段 6 首页懒加载（已完成）

- [x] AI 未配置时不请求推荐。
- [x] 推荐区进入视口后加载。
- [x] tags/authors 改远程搜索。

### 批次 4：阶段 3 查询重构准备

- 设计 `series_stats` schema。
- 建立迁移和 sqlc 查询。
- 用样本库验证默认列表查询计划。

## 每批完成标准

后端相关批次必须通过：

```powershell
$env:GOCACHE='C:\Users\saber\dev\manga_manager\.gocache'
$env:GOTMPDIR='C:\Users\saber\dev\manga_manager\.tmp'
go test ./...
```

前端相关批次必须通过：

```powershell
cd web
npm run build -- --configLoader runner
```

修改 SQL/schema/query 时必须追加：

```powershell
sqlc generate
```
