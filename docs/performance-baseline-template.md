# 性能基线记录模板

更新日期：2026-05-22

本模板用于每批性能改造完成后记录 benchmark、查询计划和运行时指标，避免只凭主观体感判断优化效果。

## 批次信息

- 批次：
- 对应北极星阶段：
- 提交范围：
- 样本库规模：
- 运行环境：
- 记录时间：

## 后端 Benchmark

```powershell
$env:GOCACHE='C:\Users\saber\dev\manga_manager\.gocache'
$env:GOTMPDIR='C:\Users\saber\dev\manga_manager\.tmp'
go test ./internal/database -bench "BenchmarkSearchSeriesPaged_10k" -benchmem -run "^$"
go test ./internal/api -bench "BenchmarkServePageImage_RawConsecutivePages|BenchmarkGetPagesByBook_WithManifestCache" -benchmem -run "^$"
go test ./internal/scanner -bench "BenchmarkScanLibrary_Incremental_NoChanges" -benchmem -run "^$"
```

| Benchmark | Before | After | Delta | Notes |
| --- | --- | --- | --- | --- |
| BenchmarkSearchSeriesPaged_10k |  |  |  |  |
| BenchmarkServePageImage_RawConsecutivePages |  |  |  |  |
| BenchmarkGetPagesByBook_WithManifestCache |  |  |  |  |
| BenchmarkScanLibrary_Incremental_NoChanges |  |  |  |  |

## 查询计划

```powershell
go run ./cmd/queryplan -db data/sample.db -library 1 -series 1 -strict
```

| Query Case | Expected Index | Result | Notes |
| --- | --- | --- | --- |
| home/default-updated | idx_series_library_updated_name |  |  |
| home/name | idx_series_library_name |  |  |
| home/created | idx_series_library_created_name |  |  |
| home/favorite | idx_series_library_favorite |  |  |
| series-detail/books | idx_books_series_sort |  |  |
| recent-read/all | idx_series_stats_last_read |  |  |
| dashboard/stats-counts | idx_books_read_progress_series / idx_reading_activity_date |  |  |
| dashboard/library-sizes | idx_books_library_id |  |  |

## 请求指标

从 Dashboard 性能面板或 `/api/system/performance` 记录。

| Metric | Value | Notes |
| --- | --- | --- |
| page_image_requests |  |  |
| page_image_cache_hits |  |  |
| page_image_archive_opens |  |  |
| page_image_manifest_hits |  |  |
| page_image_raw_passthroughs |  |  |
| page_image_processed |  |  |
| average_ms |  |  |
| p95_ms |  |  |

## 扫描日志指标

扫描完成日志应包含以下字段。

| Metric | Value | Notes |
| --- | --- | --- |
| discovered_archives |  |  |
| skipped_archives |  |  |
| processed_archives |  |  |
| opened_archives |  |  |
| hashed_files |  |  |
| duration_ms |  |  |

## 前端观测

| Metric | Value | Notes |
| --- | --- | --- |
| Webtoon DOM image count |  |  |
| Dashboard first-screen request count |  |  |
| Library first-screen request count |  |  |
| Large-library render time |  |  |

## 结论

- 是否满足本批验收：
- 剩余风险：
- 下一批建议：
