# 阶段 8：观测与基准体系

更新日期：2026-05-22

本阶段补齐北极星性能计划的可量化验收基础，让阅读、扫描、首页查询、系列详情和 Dashboard 的性能变化都有 benchmark、queryplan 或运行时指标可追踪。

## 本批交付

- 页图请求诊断字段：
  - `archive_open`：本次页图请求是否打开归档读取图片数据；
  - `manifest_cache_hit`：归档页清单是否命中内存 manifest；
  - `raw_passthrough`：是否为默认原图透传；
  - `processed`：是否进入服务端处理画像。
- 系统性能摘要聚合：
  - `page_image_archive_opens`；
  - `page_image_manifest_hits`；
  - `page_image_raw_passthroughs`；
  - `page_image_processed`。
- Dashboard 性能面板：
  - 展示原图透传率；
  - 展示页清单命中率；
  - 展示归档打开次数；
  - 展示服务端处理页数。
- Webtoon 前端观测：
  - 虚拟列表范围变化时通过 `manga-reader:webtoon-dom-images` 浏览器事件输出当前 DOM 图片数量；
  - 该事件可用于浏览器自动化或手工记录长漫画窗口化效果。
- 首屏与列表渲染前端观测：
  - 全局 axios 拦截器记录每次路由切换后首屏采样窗口内的 `/api/*` 请求数量、失败数、慢请求数和最大耗时；
  - 资源库首页记录系列列表从请求发出、响应返回到 React 下一帧渲染完成的总耗时、请求耗时、渲染耗时和当前页项数；
  - Dashboard 性能面板展示最近一次首屏请求数、首屏采样窗口、列表渲染总耗时和列表渲染项数。
- 扫描完成日志指标：
  - `discovered_archives`；
  - `skipped_archives`；
  - `processed_archives`；
  - `opened_archives`；
  - `hashed_files`；
  - `duration_ms`。
- 查询计划检查扩展：
  - 首页默认 updated 排序；
  - 首页 name / created / favorite 排序；
  - 系列详情 books；
  - recent read；
  - dashboard stats。
- 首页深页分页：
  - `/api/series/search` 保持原有 `page` / `limit` / `total` 响应；
  - 支持 `next_cursor` / `has_more`，前端连续翻页时对 `name`、`updated`、`created`、`favorite` 排序使用 keyset/cursor；
  - 直接跳页、首页、末页和不支持 cursor 的排序仍走原有 page number / offset 路径。
- 性能基线模板：
  - 新增 `docs/performance-baseline-template.md`，统一记录 benchmark、queryplan、请求指标、扫描指标和前端观测。
- 性能基线记录：
  - 新增 `docs/performance-baselines/2026-05-22-phase-3-8-local.md`；
  - 记录 1 万系列 benchmark；
  - 记录 1 万系列和 10 万系列样本库 strict queryplan；
  - 记录仍需真实浏览器/设备采集的剩余验收项。

## 边界

- 本阶段不新增持久化请求指标表，请求诊断仍保持内存环形缓冲。
- 本阶段不改变扫描语义，只增加线程安全计数和完成日志。
- 本阶段不做移动设备自动化验收，移动端滚动、500 本系列首屏和 1 万/10 万系列样本结果仍需要后续在真实浏览器/样本库记录。

## 验收命令

```powershell
$env:GOCACHE='C:\Users\saber\dev\manga_manager\.gocache'
$env:GOTMPDIR='C:\Users\saber\dev\manga_manager\.tmp'
go test ./internal/api -run "RequestMetrics|ServePageImage|BookArchivePageManifestCache" -count=1
go test ./internal/scanner -run "ScanLibrary" -count=1
go test ./...
```

```powershell
cd web
npm run build -- --configLoader runner
```

本阶段未修改 `schema.sql` 或 `sql/query.sql`，无需重新生成 sqlc 代码。
