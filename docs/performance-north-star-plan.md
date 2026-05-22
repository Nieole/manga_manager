# 性能改造北极星计划书

更新日期：2026-05-21

本文档是 Manga Manager 性能改造的执行版本，按“整批改造、整批验收”的方式组织。核心原则是先优化用户每天都会感知的阅读和首页路径，再处理大库查询、扫描、外部协议和基准体系。

## 总目标

- 默认阅读路径尽量直接：能原图透传就不进入服务端图片解码/编码。
- 长漫画滚动稳定：Webtoon 模式不把整本书所有图片挂进 DOM。
- 大库首页可用：资源库列表避免全库窗口函数、聚合和无条件多表 join。
- 系列详情轻量：首屏请求数量和前端状态拼装成本下降。
- 增量扫描轻量：未变化文件不打开归档、不算全量 hash。
- 非核心能力按需加载：AI、诊断、外部协议不拖慢内置 Web 阅读核心路径。
- 每批改造有可量化验收：benchmark、查询计划、请求指标或日志能证明效果。

## 阶段 1：阅读路径瘦身

目标：让最核心的“打开漫画、翻页”路径尽量直接。

### 改造项

1. 默认阅读原图透传
   - 前端不再把 `bilinear`、`average`、`nearest` 传给后端。
   - 后端把这几类仅影响浏览器渲染的 `filter` 视为无服务端处理。
   - `ProcessImage` 只有在 `format`、`q`、`w/h`、`auto_crop`、高级滤镜、超分时才执行。
2. 书籍页清单缓存
   - 新增 `PageManifestCache`。
   - key：`book_id + path + file_modified_at + size`。
   - value：`[]PageMetadata`。
   - `getBookArchivePage` 和 `getPagesByBook` 复用同一份 manifest。
   - 扫描、重扫、文件变化时失效。
3. 减少每页 DB 查询
   - 当前每张图请求都 `GetBook`。
   - 增加轻量 `BookPageSource` 缓存，缓存 `book_id -> path/mtime/size/page_count`。
   - 或在 manifest cache value 中附带 book 元信息。
4. 阅读进度写入节流
   - 前端只在页码变化稳定后写入。
   - 后端若页码未前进或与上次写入间隔很短，可跳过 `reading_activity` 重复写。
   - Webtoon 模式避免滚动时高频 POST。

### 验收标准

- 默认阅读请求不进入图片解码/编码路径。
- 翻页日志中默认来源应为 `raw`，不再出现默认 `processed`。
- 新增单测覆盖：
  - `bilinear` 透传。
  - manifest cache 命中。
  - 文件 mtime/size 改变后 cache 失效。
- Benchmark：
  - 同一本书连续读 50 页，归档 `GetPages()` 不应被调用 50 次。

## 阶段 2：Webtoon 渲染窗口化

目标：长漫画滚动不把整本书的图片全部挂进 DOM。

### 改造项

1. Webtoon 虚拟列表
   - 使用现有 `react-virtuoso`。
   - 每次只渲染当前视口附近页面。
   - 保留阅读进度定位、滚动到上次页、书签跳转。
2. 图片懒加载与近屏预热
   - 当前页、下一页、后续 N 页预热。
   - 视口外释放 object URL。
   - 保留当前书 + 下一卷少量缓存。
3. `IntersectionObserver` 简化
   - 不再 `querySelectorAll('.ReaderScrollContainer img')` 全量监听。
   - 虚拟项内部上报当前页。

### 验收标准

- 300 页书籍 Webtoon 模式 DOM 中图片节点数量稳定在窗口范围内。
- 滚动到上次阅读页仍准确。
- 离线阅读缓存逻辑不被破坏。
- 移动端滚动无明显掉帧。

## 阶段 3：首页资源库查询重构

目标：资源库列表不再每次做全库窗口函数、聚合和多表 join。

### 改造项

1. 新增 `series_stats` 或冗余字段
   - 建议优先新增 `series_stats`：
     - `series_id`
     - `cover_path`
     - `cover_book_id`
     - `read_pages`
     - `read_book_count`
     - `completed_book_count`
     - `last_read_at`
     - `last_read_book_id`
     - `tag_names_cache`
     - `author_names_cache`
   - 扫描、进度更新、元数据更新、封面更新时维护。
2. 改写 `SearchSeriesPaged`
   - 移除每次列表查询中的：
     - `ROW_NUMBER() OVER(PARTITION BY series_id...)`
     - `SUM(last_read_page)`
     - 重复 tag/author join
   - 默认列表只查 `series + series_stats`。
   - tag/author 筛选时才 join 关联表。
3. total 计算策略优化
   - 无筛选场景直接用 `COUNT(*) WHERE library_id = ?` 或缓存。
   - 筛选场景再算 count。
   - 可选：前端允许 total 延迟更新。
4. 分页深页优化
   - 常用排序支持 cursor/keyset：
     - name
     - updated
     - created
     - favorite
   - 保留 page number UI，但内部可逐步切 cursor。

### 验收标准

- 1 万系列、2 万书籍样本库：
  - 默认列表 P95 < 300ms。
  - 翻页不随页码线性恶化。
- `cmd/queryplan` 增加新查询计划检查。
- `BenchmarkSearchSeriesPaged` 扩展到 1 万系列。

## 阶段 4：系列详情首屏合并

目标：打开系列页减少请求数量和前端状态拼装成本。

### 改造项

1. 统一上下文接口
   - 新增或强化：`GET /api/series/{seriesId}/context`
   - 返回：
     - series info
     - books
     - tags
     - authors
     - links
     - volumes summary
     - pending metadata review count
     - failed task summary
     - relations summary 可选
2. 前端 `SeriesDetail` 首屏只打一条主接口
   - 编辑弹窗、关系搜索、刮削搜索、全量 tags/authors 改为打开时懒加载。
3. 大系列分卷懒渲染
   - 默认只渲染当前卷。
   - 其他卷折叠时不渲染所有书籍按钮。

### 验收标准

- 打开系列详情首屏接口数量从多条下降到 1-2 条。
- 大系列 500 本书以内首屏稳定。
- 编辑、批量标记已读未读功能保持可用。

## 阶段 5：扫描管线轻量化

目标：增量扫描尽量只做文件系统比较，不打开归档、不算全量 hash。

### 改造项

1. 扫描分级
   - `fast_scan`：只发现文件、比较 path/mtime/size。
   - `metadata_scan`：打开归档、取页数、封面、ComicInfo。
   - `identity_scan`：计算 quick/full hash。
   - `repair_scan`：强制重建所有派生数据。
2. KOReader hash 按需化
   - KOReader 未启用时不计算 `FingerprintFile`。
   - `quick_hash` 可保留为轻量身份。
   - full hash 放后台任务，低优先级批处理。
3. 封面生成队列化
   - 扫描时发现缺封面只入队。
   - 后台按并发生成缩略图。
   - 首屏可先显示占位图，封面完成后 SSE 刷新。
4. 扫描任务可取消
   - 将 ctx 贯穿 worker、ingester、封面任务。
   - 任务中心提供取消能力。

### 验收标准

- 未变化文件扫描不会 `OpenArchive`。
- KOReader 关闭时不会算 full hash。
- 首次扫描可以边入库边逐步生成封面。
- 扫描期间阅读翻页延迟不明显上升。

## 阶段 6：Dashboard / 推荐 / 诊断懒加载

目标：主界面只加载核心数据，AI、诊断、运维数据不拖慢日常使用。

### 改造项

1. Dashboard summary cache
   - 新建 `dashboard_stats_cache` 或内存 TTL cache。
   - 扫描结束、进度更新后刷新。
   - 页面加载不实时全表 COUNT/SUM。
2. 推荐懒加载
   - LLM 未配置或 AI 功能关闭时不请求推荐。
   - 推荐区进入视口后再加载。
   - 失败不影响首页。
3. 标签/作者按需搜索
   - 首页不再默认拉 `/api/tags/all`、`/api/authors/all`。
   - 筛选框输入时远程搜索。
   - 热门标签可单独缓存前 N 个。
4. 诊断模式
   - 日志、任务、健康报告默认不参与主界面请求。
   - 进入维护页后再加载。

### 验收标准

- 首页首屏请求数量减少。
- AI 未启用时不出现 AI 相关请求。
- 大量 tags/authors 不影响首页打开速度。

## 阶段 7：外部客户端能力按需启用

目标：OPDS/Mihon/KOReader 不影响内置 Web 阅读核心路径。

### 改造项

1. 统一协议开关

```yaml
protocols:
  opds:
    enabled: false
  mihon:
    enabled: false
koreader:
  enabled: false
```

2. 路由按配置挂载或运行时拒绝
   - 关闭时不暴露连接中心中的相关端点。
   - Dashboard 不请求 KOReader 诊断。
   - 健康报告不计算 KOReader 未匹配项。
3. 协议查询独立优化
   - OPDS/Mihon 搜索使用 FTS 或 search engine。
   - 最近更新、继续阅读走 `series_stats`。

### 验收标准

- 默认本地 Web 模式下，无 OPDS/Mihon/KOReader 额外查询。
- 开启协议后兼容性测试通过。

## 阶段 8：观测与基准体系

目标：改造不是凭感觉优化，每个阶段都有可量化结果。

### 改造项

1. 后端 benchmark 扩展
   - `BenchmarkSearchSeriesPaged_10k`
   - `BenchmarkServePageImage_Raw`
   - `BenchmarkGetPagesByBook_WithManifestCache`
   - `BenchmarkScanLibrary_Incremental_NoChanges`
2. 查询计划检查扩展
   - 首页默认排序。
   - 首页 updated/favorite/name。
   - 系列详情 books。
   - recent read。
   - dashboard stats。
3. 请求指标补充
   - page image 增加：
     - `archive_open`
     - `manifest_cache_hit`
     - `raw_passthrough`
     - `processed`
   - scan 增加：
     - `opened_archives`
     - `hashed_files`
   - Webtoon DOM 图片数。
   - 首屏请求数。
   - 大库列表渲染耗时。

### 验收标准

- 每次性能改造 PR 必须带 benchmark 或查询计划对比。
- 慢路径日志能定位到扫描、SQL、归档、图片处理中的具体一层。

## 推荐交付顺序

| 顺序 | 阶段 | 原因 |
| --- | --- | --- |
| 1 | 阶段 1 阅读路径瘦身 | 用户感知最直接，风险最低 |
| 2 | 阶段 2 Webtoon 窗口化 | 解决长漫画卡顿 |
| 3 | 阶段 3 首页查询重构 | 解决大库打开慢 |
| 4 | 阶段 4 系列详情合并 | 减少请求和前端复杂度 |
| 5 | 阶段 5 扫描轻量化 | 大库维护体验提升最大 |
| 6 | 阶段 6 懒加载非核心模块 | 降低默认负载 |
| 7 | 阶段 7 协议按需启用 | 收敛产品复杂度 |
| 8 | 阶段 8 基准体系 | 固化成果，防回退 |

## 第一批建议实际落地范围

第一批建议做这 4 个点：

1. `bilinear/nearest/average` 阅读默认透传。
2. `PageManifestCache`。
3. Webtoon 图片增加懒加载和预窗口化基础。
4. 首页 AI 推荐、tags/authors 改成懒加载。

这批改动风险可控，不需要大迁移，能快速改善阅读和首页体验。

## 标准验证命令

后端：

```powershell
$env:GOCACHE='C:\Users\saber\dev\manga_manager\.gocache'
$env:GOTMPDIR='C:\Users\saber\dev\manga_manager\.tmp'
go test ./...
```

前端：

```powershell
cd web
npm run build -- --configLoader runner
```

修改 SQL/schema/query 时追加：

```powershell
sqlc generate
```
