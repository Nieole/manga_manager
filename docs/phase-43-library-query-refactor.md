# 阶段 43：首页资源库查询重构

日期：2026-05-21

## 目标

本阶段对应性能北极星计划的“阶段 3：首页资源库查询重构”，目标是让资源库首页默认列表不再每次执行全库书籍窗口函数、阅读进度聚合和无条件标签/作者 join。

## 已完成

- 新增 `series_stats` 读模型：
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
- 数据库迁移会创建 `series_stats` 并回填既有系列。
- `sqlc generate` 已重新生成数据库绑定。
- `SearchSeriesPaged` 默认列表改为 `series + series_stats`。
- 默认路径移除：
  - 书籍封面 `ROW_NUMBER() OVER(PARTITION BY series_id...)`
  - 每次列表查询的 `SUM(last_read_page)`
  - 无条件 `series_tags/tags` 和 `series_authors/authors` join
- 标签/作者筛选改为按需 `EXISTS` 子查询。
- 无筛选 total 直接 `COUNT(*)`，筛选时才使用筛选条件计数。
- 维护链路已覆盖：
  - 书籍创建和 upsert
  - 书籍删除
  - 阅读进度更新
  - 系列统计更新
  - 标签/作者关联变更
  - 扫描批处理事务
  - 系列元数据更新事务
- `cmd/queryplan` 增加 `series-stats/library/default` 检查项。
- 新增 `BenchmarkSearchSeriesPaged_10k`。
- 深页分页已支持 cursor/keyset：
  - `name`
  - `updated`
  - `created`
  - `favorite`
- `/api/series/search` 保留 `page` / `limit` / `total` 兼容，同时返回 `next_cursor` / `has_more`。
- 资源库页保留 page number UI，连续翻页优先使用 cursor，直接跳页仍走 offset。
- 已保存本地 1 万与 10 万系列样本查询计划记录：`docs/performance-baselines/2026-05-22-phase-3-8-local.md`。

## 尚未完成

- 标签/作者筛选仍保留 page number + offset 语义。
- 浏览器端真实大库渲染体验仍以 Dashboard 前端观测继续采集。

## 验证

```powershell
sqlc generate
$env:GOCACHE='C:\Users\saber\dev\manga_manager\.gocache'
$env:GOTMPDIR='C:\Users\saber\dev\manga_manager\.tmp'
go test ./internal/database -run "Migration|SeriesStats|Search" -count=1
go test ./internal/api -run "SearchSeriesPaged|GlobalMetadata|UpdateBookProgress|BulkUpdate.*Progress" -count=1
go test ./cmd/queryplan -count=1
go test ./internal/database -run '^$' -bench 'BenchmarkSearchSeriesPaged_10k' -benchtime=10x
```

参考基准结果：

```text
BenchmarkSearchSeriesPaged_10k-32    964    2106098 ns/op    129612 B/op    2514 allocs/op
```

## 后续入口

- 阶段 4：系列详情首屏合并。
- 阶段 6：Dashboard / 推荐 / 诊断懒加载。
