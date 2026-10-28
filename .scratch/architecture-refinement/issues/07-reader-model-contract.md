# 07: 退出旧读者模型兼容面

**What to build:** 在所有阅读及协议消费者迁移后删除旧公共投影/写入旁路，保留必要持久化记录与测试支持。

**Blocked by:** 03: 藏书编排的读者查询；04: OPDS 与 Mihon 的读者视图；05: 阅读位置与标记的窄事务；06: 书签与时长的完整命令。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 13、17、31、44、202。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 广泛迁移收缩。

- [x] 确认所有生产消费者已迁移，再删除旧模型入口及无人调用桥。
- [x] 数据库持久化行仍可保留；禁止的是未绑定主体却冒充阅读视图的公共形式。
- [x] 不删除仍有明确用途的测试故障 seam 或合理兼容匿名能力。
- [x] 全体受影响查询、协议、命令和生成契约验证保持通过。

## Comments


### Implementation evidence

- Audited all production reading/protocol/organization consumers before removal. Removed public mutable `Queries.ProjectBooks`, unbound `Queries.Book/Series/Adjacent`, `SeriesView`, `InspectBook`, `SortBooks`, `Summarize` and `RecentReads`; `BookQueryStore` explicitly retains only technical catalogue/batched progress reads. Typed detail/series/navigation queries now construct `BookView` directly, without overwriting `database.Book` read fields. Private bound-view summarization is the single continuation authority; each series still uses one batched progress lookup.
- The final Dashboard recent-reading consumer now uses validated `Subject` and `RecentBooks`; HTTP red reproduced a negative subject returning anonymous completion data, then green verified rejection. Series volume summaries consume `BookView.Inspect` before response mapping. Existing JSON, anonymous compatibility, unknown-page completion, order, receipt times, SQL filters/counts/pagination and natural book comparison remain intact.
- Persisted database rows, technical SQL writes used by atomic adapters, source-specific reading commands, failure injection and historical fixtures remain. Removed the unused `LogUserReadingActivity` receipt-date bridge after all three test callers moved to the proven-time adapter. Prior tickets already removed the unused progress/time business bridges; no alternate public projection remains. The host-path fixture retains its needed technical comparison directly through `booksort`, outside the retired reading projection API.
- ADR 0010 now describes the implemented boundary. `go run ./cmd/tsgen` actually passed and generated contract diff was empty (`.tmp/ticket07-tsgen-retry.log`); the initial launch was blocked by Application Control and is not pass evidence (`.tmp/ticket07-tsgen.log`). No SQL/schema or frontend production source changed, so SQL regeneration and repeated frontend build/full suite were not needed.
- CI Go 1.26.10 red/green HTTP evidence: `.tmp/ticket07-red.log`, `.tmp/ticket07-green.log`. Reading, organize and maintenance full query/command/recovery tests passed (`.tmp/ticket07-query-consumers.log`). Final HTTP/protocol reader projection matrix and Dashboard regression passed (`.tmp/ticket07-final-consumers.log`). Frontend actual detail/series/navigation consumers: 5 files / 34 tests passed (`.tmp/ticket07-frontend-consumers.log`).
- Initial `go test ./...` ran every package. API failed because the test fixture migration had omitted natural ordering; the fixture was corrected using the existing technical comparator. Maintenance, seriesediting, bookcontent, diskwork, runhandle and runtimecfg launches were blocked by Application Control; all six actually passed when only failed packages were rerun (`.tmp/ticket07-failed-packages-retry.log`). API was blocked in that retry, then its final complete-package rerun actually passed in 100.439s (`.tmp/ticket07-api-final-retry.log`). Every Go package has actual pass evidence across the initial run and targeted reruns; no blocked item remains unexecuted. Initial full-command failures are retained in `.tmp/ticket07-go-tests.log` and are not reported as a successful full command.
- Final `golangci-lint run --allow-parallel-runners` passed with `0 issues.` (`.tmp/ticket07-lint-final.log`); final `go run ./cmd/tsgen` passed with no generated contract diff (`.tmp/ticket07-tsgen-final.log`). `gofmt -l ./cmd ./internal` and `git diff --check` are clean.
- Based on integration `096b8cc` with ancestry verified; merged ticket 17 integration and latest execution documentation `cca0f0b` before handoff. Integration verification is complete; this ticket is closed.
### 集成验收

- 已由合并提交 `8b74750` 纳入 `codex/architecture-refinement`，无冲突。
- Go 1.26.10 的 reading/maintenance 整包实际通过（13.011s/6.021s）；organize 首次程序启动遭 Application Control 拦截，未计通过，随后单独整包实际通过（6.283s）。HTTP/OPDS/Mihon、看板主体、投影、导航与读态兼容专项实际通过（17.197s）。
- `go run ./cmd/tsgen` 实际退出 0，生成契约无漂移；gofmt 无输出，独立缓存 lint 为 `0 issues.`。原始输出保留在主工作树 `.tmp/merge-wave7-07-*.log`；没有前端生产或 SQL 生成源变化，未重复无关构建/生成或全仓测试。
