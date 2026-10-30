# 28: 文件监听与安全清理协调

**What to build:** 监听只交付变化，摄入协调统一真实扫描完成、失败重排及清理资格。

**Blocked by:** 27: 扫描摄入的完整内部用例。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 125、126、127、128、129、135。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 消费已稳定摄入结果，合并/排队/停发不冒充扫描成功。
- [x] Rename/Create、扫描在飞、失败后重排和危险清理资格集中。
- [x] 保持唯一任务准入与独立封面，迁移保护和关闭等待不丢失。
- [x] 可控监听事件、真实记录重连和停止回归验证，移除重复协调状态。

## Comments

### Implementation handoff — 2026-10-30

- `ScanOutcome` reports complete/partial/failure/cancellation and safe cleanup eligibility. Watch ingestion coordination owns change versions, inflight work and retry; queued cleanup rechecks the original input context and generation when its body actually starts. Parsing, identity uncertainty and transaction rollback never authorize missing-record deletion.
- Directory registration uses a nonblocking coalescing worker. Stop cancels recursive registration, closes the source and joins registration and coordination. `JoinWatched` cancels only eligible watcher work and awaits actual handed-off body exit, including terminal/panic windows. Frozen queued history has no body to await; its retained closure checks input cancellation before any effect. Ordinary `AwaitWatched` and user cancellation policy are unchanged.
- Red/green evidence is retained in `.tmp/ticket28-red-*.log`: rollback after rename, stale generation, blocked registration consumer, recursive registration after Stop, body join, uncertain identity and series completion. Real Windows rename/recreate burst and Stop, frozen queued cleanup, actual SQLite identity protection and terminal-before-exit scenarios pass.
- Actual full race executions: scanner 64.136s, application 2.928s, scanning 16.777s, task 1.956s, runtime 9.293s and maintenance 8.286s (`ticket28-full-race.log`). Final shutdown regressions passed scanning 9.183s/scanner 1.315s/application 1.238s. Build and final golangci-lint (`0 issues.`), gofmt, fixed-baseline whitespace and tracked conflict-marker checks passed. No frontend production, SQL or generated-contract changes.
- Initial full API execution failed only the pre-existing broken-archive RunEvents test's assumption that incomplete ingestion succeeds (109.862s). The test still uses a broken archive and verifies failed status and its detailed file/reason events, now obtaining the snapshot by admitted RunID rather than the global legacy-key fixture. Its isolated corrected execution passed 0.183s. The initial failure is retained, not counted as pass.
- Status remains ready-for-agent until merger verification. Resources ticket 31 carries the complementary stopping notification and shutdown order; combined lifecycle verification belongs at integration.
- Corrected full API execution actually passed 131.829s (	icket28-api-full-fixed.log); this is the full-suite pass after the retained failed run.
