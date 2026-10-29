# 27: 扫描摄入的完整内部用例

**What to build:** 发现、解释、身份认领和入库按规则归属组织，扫描仍向用户交付完整摄入结果。

**Blocked by:** 26: 书身份与设备匹配能力。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 36、38、123、124、126、129、134、135。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 消费中立身份能力，来源元数据仍用既有锁定与写入规则。
- [x] 事务只暴露所需摄入能力，保留批次、回滚、聚合和统计事件。
- [x] 改名重连、根可达/真实书/比例保护及封面独立交接保持。
- [x] 真实 SQLite/归档扫描用例验证结果，adapter 不公开 worker/SQL 配方。

## Comments

### Implementation and verification — 2026-10-29

- Both complete public scan entrypoints now use one internal ingestion process. `archiveDiscovery` owns format and profile-aware incremental admission, `archiveInterpreter` owns archive parsing, source placement, cover hints and neutral typed evidence, and `ingestionWriter` alone claims old identities and persists batches. Interpreted source facts contain no ownership IDs or SQL parameters; `claimedArchive` adds the writer's optional old-record claim, and only the writer maps committed ownership into persistence inputs. Callers still invoke complete library/series scans and never schedule workers or flush SQL.
- `database.ScanIngestionWrite` exposes only series/book/evidence writes and source-specific append associations inside `WithScanIngestion`. Its concrete adapter holds a private query field instead of embedding Queries or exposing DB/ExecTx. The writer receives only ingestion, series-snapshot and derived-refresh capabilities. `seriesmeta` retains the existing cached scan locks, default locked title, source-specific updates and tolerant association append semantics. Single-writer batching and the existing profile behavior remain intact.
- Transactional series and lock-cache additions are staged until commit. Renaming metrics and success logs also publish only committed rehomes. Real SQLite rollback reproduced two visible defects before the fixes: a failed first batch poisoned the next batch with nonexistent series IDs (zero books survived), and a rolled-back rename was reported as `RehomedBooks=1`. The final regressions use deferred-FK triggers to fail at commit while the transaction retains all writes; they verify the next successful batch and original identities through the public scanner/store/observer seams. CHANGELOG records these actual fixes.
- `derivedSeries` owns committed dirty membership, throttled retries and the bounded cancellation-independent final drain. `libraryCleanup` owns root reachability, surviving real book files, uncertain-error retention, ratio protection and protected deletion. Independent cover batches still receive only committed jobs and outlive the scan through the existing handoff; unique run admission and size+mtime/ambiguity/copy policies are unchanged. Watcher scheduling and stop coordination remain ticket 28.
- Red evidence: `.tmp/ticket27-red-cache-real.log` and `ticket27-red-rehome.log` contain actual scanner failures before the fixes. The initial trigger exercised rollback during a statement; final `ticket27-green-commit-trigger.log` passes with commit-boundary deferred constraints. Complete source metadata failure, title/lock, cancellation, renamed-record, cleanup and cover consumer regressions remain in the scanner suite.
- Actual passing executions: scanner full 16.114s and complete scanner race 45.243s; full API 96.980s; scanning 3.479s, maintenance 3.153s, runtime 5.562s and database 8.288s. Ordinary seriesmeta execution was blocked by Windows Application Control and is not a pass; its complete race retry actually passed 22.005s. Backend build passed. Lint reported 0 issues after correcting a file-comment attachment; gofmt output was empty. Raw logs are under `.tmp/ticket27-*.log`.
- No frontend, SQL source or generated response structures changed. The complete baseline whitespace/conflict-marker audit and final merge validation follow below. Status stays ready-for-agent until merger validation and closure.