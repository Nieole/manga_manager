# 41: 目录组合查询与评价用例

**What to build:** 系列上下文、搜索、推荐、看板和用户评价经完整查询或命令能力处理，HTTP 不拼数据与缓存配方。

**Blocked by:** 02: 系列列表的主体筛选与分页；29: 元数据与 AI 消费能力。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 10、18、21、22、29、40、174、181、182、183。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 消费主体明确的基础查询与供应商能力，按场景组织批量结果，用户评价保持独立写入意图。
- [x] 主干/附属错误、统计与推荐缓存有明确语义；评价的主体、值域和既有写入约束归完整用例。
- [x] 实际 HTTP/页面消费完整查询，保持评分/归一、筛选、用户投影和原公开结果。
- [x] SQLite、可控依赖和消费者回归验证批量、缓存隔离、失败与真空。

## Comments


### Implementation verification (2026-10-29)

- Complete query/command ownership is split by actual scene: `catalogsearch`, `seriescontext`, `recommendation`, `dashboard` and `seriesreview`. HTTP parses/authorizes and maps transport DTOs. Search owns FTS combination and score normalization and returns typed hits; series context composes narrow catalogue/reading, relation, proposal and task ports, including volume and summary projections, without importing API DTOs.
- Reading subjects are explicit. Context reuses `reading.Queries`; proposal fields and task identities retain their existing batch paths. Recommendations retain candidate sampling and map-based enrichment, use only the Recommender capability, and own user+locale cache partitions, singleflight, generation guards and invalidation. Dashboard owns structural/volatile caches and reader projection without mutating shared cached facts. Series reviews own rating bounds, text normalization, clear/delete and subject isolation as a separate write intent.
- Main or ancillary failures are no longer returned as successful empty data or another subject's statistics. Context ancillary failure carries a typed part/cause and the HTTP query reports failure; this ticket does not add the later stale-state UI. Original success DTOs, scores, ordering, recommendation nil versus empty-array results, anonymous compatibility and cache lifetimes remain intact.
- Genuine red evidence: `.tmp/ticket41-search-red.log` executed the prior series-lookup error as HTTP 200 with an empty result. Search/failure regressions and direct complete-owner regressions use real temporary SQLite. Owner coverage verifies review isolation/failed-write preservation/clear, recommendation user+locale partitioning and purge, and typed ancillary failure (`ticket41-owner-final.log`, actual 4.009s).
- Earlier full API suite executed 126.304s (`ticket41-full-go.log`); relevant API race executed 85.423s (`ticket41-race.log`). After merging integration eae7394, full API actually passed 103.522s (`ticket41-merged-full-go.log`); final owner/context/volume/failure race passed 16.036s (`ticket41-final-owner-race.log`), and final typed-search projection consumers passed 6.021s (`ticket41-final-search-projection.log`). All new owner boundary tests reside in the API fixture package and invoke their public capabilities against real resources; no test execution is implied by the new packages' no-test-files entries.
- Existing series-context and search page consumers passed 6 tests (`ticket41-page.log`). Merged frontend full suite passed 101 files / 942 tests, lint and production build (`ticket41-merged-ui.log`, `ticket41-merged-ui-lint.log`, `ticket41-merged-ui-build.log`). No frontend production source or generated contract targets changed in this ticket; SQL was unchanged.
- Final Go lint reported `0 issues.` (`ticket41-handoff-lint.log`); gofmt, whole-baseline whitespace checks and tracked conflict-marker scans were clean. The integration merge retained both changelog entries and both concurrent controller-test imports. Ticket closure remains pending merger verification.

### Integration verification (2026-10-29)

- Merged through 7da6d95b09a32be1ebf9a6abaf2e0ab5e9f01e67 without conflicts. Main-tree complete-owner SQLite and HTTP search/context/recommendation/dashboard/review consumers actually passed 4.754s. The five owner packages compile and have no separate package tests; their complete-use-case seams are exercised by these real SQLite API fixtures, not inferred from compilation.
- Main-tree race launches were blocked twice by Application Control and are not counted as passes (.tmp/merge-wave15-queries.log and merge-wave15-queries-retry.log); ordinary execution passed in merge-wave15-queries-ordinary.log. Implementer post-merge race 16.036s remains the recorded actual race execution. Existing series-context page consumers passed all 3 tests (merge-wave15-web.log).
- Go 1.26.10 lint reported 0 issues, gofmt emitted no files, and whole-baseline whitespace/conflict-marker scans passed. No frontend production, SQL or generated target changed.
