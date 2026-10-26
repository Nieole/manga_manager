# 四方向联合验收

验收日期：2026-10-26。需求来源为 [spec.md](spec.md)，交付过程见 [implementation.md](implementation.md)。本记录按 112 条故事分别核对当前规则 owner、源码入口、测试断言和真实执行结果；工单 `done` 不是运行证据。

代码基础为集成 `cb85378`（含 24 的独立身份 fixture 修正），加本票 `14483e1` 阅读器日界修复及无人使用的生产排序桥移除。联合代码在 `codex/architecture-25` 验证，随后由集成分支进行独立双线审查。本记录不声称该后续审查或远端 CI 已完成。

## 当前职责与依赖

| 归属 | 生产源码入口 | 规则和边界 |
| --- | --- | --- |
| 阅读状态 | `internal/reading/position.go`、`mark.go`、`activity.go`、`time.go`、`bookmarks.go`、`queries.go`、`catalogue.go` | `Service.RecordPosition/SyncPositions/MarkBooks/MarkSeries/RecordTime`、`Bookmarks`、`Queries` 隐藏主体、时间、拒旧、清除、批次、活动及读态解释；输入不持配方。 |
| 阅读落盘 | `internal/database/reading_position.go` 及数据库迁移 | 原子持久化位置、清除依据、事实/操作身份、系列聚合；服务规则决定是否应用，存储不产生另一套来源规则。 |
| 藏书编排 | `internal/organize/collections.go`、`smart_collections.go`、`relations.go`、`franchise.go`、`ai_grouping.go`、`reading_lists.go` | 人工、动态、快照、推导及有序成员分别有明确用例；共享名称约束，AI 建合集与裁决同事务；清单读态复用 reading。 |
| 应用协作 | `internal/application/runs.go`，`scanning/`、`relocating/`、`maintenance/`、`diskjobs/`、`intelligence/` | 共用领域准入与上下文；拥有扫描/清理、封面、迁移、维护和后台发起的完整跨功能交接；原领域保留内部规则。 |
| 应用进程 | `internal/application/runtime/runtime.go`、`registry.go`、`lifecycle.go` | 装配同一 Engine、Manager 和全部重启能力；恢复先于开放；停止排空并等待资源使用者退出。 |
| 传输适配 | `internal/api/controller_progress.go`、`reading_mark_controller.go`、合集/协议 handler、`runtime_application.go`；`internal/koreader/service.go` | HTTP 解析、授权、身份和响应，SSE 编解码/权限、缓存 leaf effects 留在 API。KOReader 认证/匹配/协议记录独立，书位置调用 reading；生产适配不持重复规则。 |
| 前端会话 | `web/src/session/ApplicationSessionProvider.tsx`、`useEventStream.ts`、`web/src/utils/runPush.ts`，`web/src/App.tsx` | 路由范围声明单连接；解码、重连、投递链与刷新信号集中；角色边界释放敏感投影。 |
| 前端功能与页面 | `web/src/features/task-observation/`、`external-library/`，`web/src/pages/book-reader/` 和各页面 | 功能各自持有查询、实况/历史/气泡/传输投影；局部筛选、选区、弹窗和草稿留在页面；下载、离线 owner 与设备偏好生命周期分开。 |

人工核对并扫描生产导入/调用：reading、organize 不导入 API 或 application；application 不导入 API；task 不引入 taskstore/SQL。功能依赖采用各自的 Store/ProjectionStore/BookmarkStore/CollectionStore 等窄能力。reading.Service 的写用例共享一个相关能力集合，runtime 装配根保留 database.Store，未宣称所有存储接口都已经逐方法最小化。原始调用审计保存为 `ownership-audit.log`。

迁移范围的在线/离线、标记、书签、时长、继续阅读、合集与应用运行配方只有上述 owner。`sortBooksForReading` 已无生产调用，仅一份测试 fixture 借用，已删除并改为直接使用 `reading.SortBooks`。24 保留的旧 Run/start、可替换装配只在测试支持文件中，仍服务故障/协议/权限回归，保留这些 fixture 不产生生产权威副本。实际 `EventSource` 创建只在 session，消费者不解释 `run_snapshot:` / `run_live:`。

## 联合行为与本票纠正

- 阅读链联合复用真实 SQLite 用例与 HTTP/协议矩阵：Web、离线批量/回退、KOReader 新推送/旧记录重放、按书/系列标记；覆盖用户/匿名隔离、原时间拒旧、较新回退、同页新阅读、重试去重、跨重启清除后拒旧及新阅读重建。未知页数的完成态独立于真实页数，旧活动不猜测重算。
- 故障链复用批内回滚、后批失败保留前批/缓存失效、位置成功而统计告警、全局/用户统计独立尝试，以及设备协议记录和位置投影失败的不同结果。活动页数仍是每日每书最大绝对页码，不是精确翻页增量。
- 藏书链复用人工只读边界、智能条件/计数/分页、一次快照人工所有权、作品群自然身份/成员差集/时间稳定、AI 消失成员/CAS/事务回滚、OPDS/Mihon 身份与有序清单主体进度回填回归。
- 会话链运行真实路由与消费者回归：普通刷新、管理员实况、后端敏感帧过滤、重连/投递缺口补拉、后端汇总、切换身份/旧回调丢弃、草稿/选区保持；阅读器与独立图谱沿用原连接范围。AuthProvider 的换人和显式登出回归增加 `manga_reader_theme=comimi` 的值保持断言，现有真实 BookReader 回归验证偏好消费，不改变设备偏好归属。
- 生命周期链运行 `application/runtime` 的生产等价真实 SQLite 图：首启重启能力、恢复失败不开放、原 force 参数恢复历史、Manager 发布驱动同队列、延迟交接任务取消并等待、配置/SSE 先停和请求排空、任务退出后释放归档池；扫描/改名后清理、排队/合并封面认领、迁移补偿及保护恢复由各完整协作 seam 联合支持。没有为了逐条故事而造一个初始化整个 HTTP 的规则测试入口。
- 本票先将 indicator 时钟固定到北京时间日末最后一毫秒，在线、离线、网络失败回退三项均红：第二条报告实际变为次日。移除 `Math.max(Date.now(), previous+1)`，改用真实发生时间后转绿；稍后同页测试实际推进时钟。相同真实毫秒仍按后端显式历史等时拒旧规则处理，不制造时间、不移除 updated_at、不新增事件溯源。`indicator-red.log` 保留 3 个失败，`frontend-focused-green.log` 为随后 40 项通过。
- 24 的 `TestRunReleasesRuntimeOnEveryExitPath` 原精确失败是 fixture 未登记身份，不能归为系统策略；后续修复合入后，本轮 Release 和 Claims 分别用完整锚定名称独跑且均退出 0。全量通过与独立选择通过分别保留。

## 文档与决定状态

`AGENTS.md` 描述当前模块和前端归属；`CONTEXT.md` 的位置、最近阅读时间、阅读活动与运行词义仍正确，未加入实现细节。设计和规格保留批准需求，仅更新阶段和指向实施/验收；queue 只做索引；CHANGELOG 记录用户可见语义纠正。

ADR 核查：0001 的装配期完整重启登记、0003 的字节精确身份、0005 的单一运行准入/独立封面、0007 的 ID 寻址保持有效；0002 仍由 0005 取代。0004、0006、0009 已有对应生产实现，过期的“尚未实施”状态已更正。0008 **尚未完整实施**：已有 Args/Labels，但部分扫描环境仍入 Args，合并未按入参差异逐次记录有损事件；它属于独立 `inputs-are-not-labels` 工作，当前批次保留既有契约，不借验收扩大该迁移。

## 实际门禁与证据

本轮日志统一保存在库外 `C:/Users/saber/.codex/tmp/architecture-deepening/acceptance-25/`，工作树清理后仍可读取。G/F 代表本轮实际执行，不使用早期草图的“测试存在”或旧票 done 代替。G 对应删除排序空桥前 `d3d2831` 上完整 Go 退出 0 的执行；`story-test-evidence.json` 已在该次结果上对表列 196 个 Go 故事/测试引用逐个核对源码符号与同名 pass，并保存 30 包通过清单。前端索引逐个核对 JSON 中精确标题的 passed 状态。结构、文档与阶段故事同时保留人工判断，不声称一个自动测试证明所有权全局成立。

证据保留限制：执行者随后复用同一日志名，误将清理前完整绿 JSON 覆盖；其工具调用的进程退出 0、核对时生成的上述派生映射仍在，但**初次完整绿的原始 JSON 未保留**，不能把派生文件冒充原始日志。清理后的额外完整运行退出 1，logger/parser/organize/seriesmeta/taskstore 五包被 Application Control 拒执行，其余 25 包包括完整 API 和 reading 实际通过，无测试断言失败。已保留 `go-tests-post-cleanup-blocked.jsonl`，不对未变源码继续抽签。此次清理仅删除无生产调用的转发并令测试直接调用 reading，受影响包通过和随后全静态门禁支持该小改动；不宣称最新全量全绿。

| 检查 | 本机结果 | 原始证据 |
| --- | --- | --- |
| 清理前 `go test ./... -count=1 -json` | 退出 0，30 个包通过，实际执行未用缓存 | 工具进程结果及 `story-test-evidence.json`；原始 JSON 覆盖限制见上 |
| 清理后额外完整 Go / 受影响包 | 整体退出 1：5 包系统拒执行；API、reading 等 25 包实际 pass，无断言失败 | `go-tests-post-cleanup-blocked.jsonl` |
| 精确 Release / Claims | 分别退出 0，Release 四分支与 Claims 同步槽位行为通过 | `runtime-release-exact.jsonl`、`runtime-claims-exact.jsonl` |
| `go vet ./...` / `go build ./...` | 退出 0 | `go-vet.log`、`go-build.log`、`go-static-status.log` |
| `gofmt -l ./cmd ./internal` | 退出 0，输出为空 | `gofmt.log` |
| `golangci-lint run --allow-parallel-runners` | 退出 0，`0 issues.` | `golangci-lint.log` |
| 前端 `npm test` | 退出 0，94 文件、913 项通过 | `frontend-tests.json`、`frontend-tests.log` |
| 前端 `npm run build` / `npm run lint` | 退出 0，dist 为本树构建；node_modules 为物理目录 | `frontend-build.log`、`frontend-lint.log` |
| 设备偏好末次定向复验 | 退出 0，6 项通过 | `device-preferences-final.log` |
| PowerShell `sqlc generate` | 退出 0，生成无漂移；query.sql 非 ASCII 字节为 0 | `sqlc.log`、`generated-drift.log` |
| `go run ./cmd/tsgen` | 退出 0，generated.ts 无漂移 | `tsgen.log`、`generated-drift.log` |
| 文档检查器 / diff 格式 | 退出 0 | `doc-style.log`、`diff-check.log` |

Go 使用 `GOTOOLCHAIN=go1.26.10`，共享主树 GOCACHE，本树 GOTMPDIR；lint v2.12.2 使用本树独立缓存。历史受阻记录仍归 implementation 和各票，不改写成通过。

未执行：Ubuntu-only `govulncheck`、`go test -race`，以及远端多平台 CI；已核对 CI 的平台条件。本 Windows 门禁不替代这些结果，也不以它们阻断本机验收。未进行手工浏览器全站巡检、外部真实设备/供应商调用或发布；已运行对应页面、协议、可控外部依赖和生命周期回归。对没有原始事实时间/身份的兼容输入仍不承诺推断重试或补造历史；这属于批准范围。

## 112 条故事的当前实现与执行映射

US 编号指向 spec 的原文，避免再复制一份需求。下表测试路径是实际测试源码，owner 的生产路径见上方职责图。共同测试可支持多条故事，逐行 pass 关联不扩大其断言范围。

| US | 当前规则 owner / 实现入口 | 直接行为测试（路径及符号） | 当前执行及人工核对 |
| --- | --- | --- | --- |
| 1 | reading / organize / application / session职责图 | `TestReaderViewsIsolateSubjectAndReflectBackwardPositionAndClear` — `internal/reading/queries_test.go`<br>`TestManualCollectionsNameAndIdentity` — `internal/organize/collections_test.go`<br>`TestAssemblyRegistersRestartCapabilitiesBeforeAnyLaunch` — `internal/application/runtime/runtime_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 人工核对下方职责图及 runtime 生产图；行为测试只支持各链结果。 |
| 2 | reading.Service;organize.Collections;application各完整用例 | `TestPositionAllowsNewerBackwardReadingAndRejectsDelayedReports` — `internal/reading/position_test.go`<br>`TestManualCollectionsNameAndIdentity` — `internal/organize/collections_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 人工核对完整用例与 adapter 调用点；不以移动目录替代规则集中。 |
| 3 | RecordPosition/SyncPositions/MarkBooks/MarkSeries;Collections.AddMembers | `TestSyncPositionsKeepsOrderAndIndependentResults` — `internal/reading/sync_test.go`<br>`TestMemberBatchIsAtomicForMissingSeries` — `internal/organize/collections_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 人工核对 Position/MarkBooksInput 与各藏书用例，无来源策略开关。 |
| 4 | application.Runs与scanning/relocating/maintenance/diskjobs/intelligence | `TestApplicationRelocationPreservesIdentityAndReleasesOnlyAfterEffects` — `internal/application/relocating/relocating_test.go`<br>`TestShutdownDrainsTransportBeforeTaskCancellationAndResourceRelease` — `internal/application/runtime/runtime_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 人工核对 runtime 只装配，功能内规则仍在各 owner。 |
| 5 | reading/organize窄依赖；application不导入api | `TestReaderViewsIsolateSubjectAndReflectBackwardPositionAndClear` — `internal/reading/queries_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 导入扫描没有反向依赖，见 ownership-audit.log。 |
| 6 | reading.Store/ProjectionStore/CatalogueQueries；organize各Store | `TestPositionsAreIsolatedClampedAndSupportUnknownPageCounts` — `internal/reading/position_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 逐接口核对功能窄 Store；reading.Service 共享位置/标记/时长写面，装配根保留 database.Store。 |
| 7 | task/taskstore/seriesmeta/proposal/relocation保留分工 | `TestCalibrationReplayUsesOriginalFactAndClearSurvivesReopen` — `internal/application/maintenance/maintenance_test.go`<br>`TestApplicationRelocationPreservesIdentityAndReleasesOnlyAfterEffects` — `internal/application/relocating/relocating_test.go`<br>`TestInterruptedHistoryReplaysThroughFirstProcessRegistry` — `internal/application/runtime/runtime_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 ADR 0001/0003–0009 的有效决定保留；0002 仍被取代，0008 未完整实施另列限制。 |
| 8 | 完整功能seam与真实SQLite fixture | `TestMarkBatchFailureRollsBackProgressBasisAndAggregate` — `internal/reading/mark_test.go`<br>`TestAIGroupingCASLossRollsBackNewCollection` — `internal/organize/ai_grouping_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 按规则 owner、真实事务和公开结果验收；未设删行指标。 |
| 9 | KOReader协议记录与reading位置；时间短时尽力而为 | `TestDeviceCanonicalSaveFailureDoesNotHideCommittedBookPosition` — `internal/koreader/position_test.go`<br>`TestReadingTimeCompatibilityAndCap` — `internal/reading/time_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 10 | 离线原时/同页/批量活动/部分失败纠正 | `TestOfflineTransportsShareOccurrenceAndActivityMatrix` — `internal/api/offline_position_test.go`<br>`TestLaterMarkBatchFailureRetainsAndInvalidatesCommittedRange` — `internal/reading/mark_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 11 | spec Implementation Decisions与issues边界 | 无专门运行测试 | 按批准规格与测试 seam 完成工程选择，没有新增未决产品裁决。 |
| 12 | 迁出adapter旧位置/活动/编排副本 | `TestOnlinePositionPreservesOccurrenceAndRefreshesSamePage` — `internal/api/reading_position_test.go`<br>`TestAssemblyRegistersRestartCapabilitiesBeforeAnyLaunch` — `internal/application/runtime/runtime_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 人工核对迁移范围的旧配方删除；测试支持桥仅在 _test.go，生产薄委托保留合理用途。 |
| 13 | reading.Service.RecordPosition | `TestPositionAllowsNewerBackwardReadingAndRejectsDelayedReports` — `internal/reading/position_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 14 | koreader.Service.SaveProgress -> reading.RecordPosition | `TestMatchedDevicePushAllowsBackwardCurrentPosition` — `internal/koreader/position_test.go`<br>`TestMatchedKOReaderProtocolUpdatesPositionAndInvalidatesDashboard` — `internal/api/koreader_reading_position_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 15 | reading.RecordPosition/staleOccurrence + database.MutateReadingPosition | `TestPositionAllowsNewerBackwardReadingAndRejectsDelayedReports` — `internal/reading/position_test.go`<br>`TestConcurrentPositionsKeepNewestOccurrence` — `internal/reading/position_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 16 | api.bulkSyncBookProgress/updateBookProgress -> reading.SyncPositions/RecordPosition | `TestOfflineTransportsShareOccurrenceAndActivityMatrix` — `internal/api/offline_position_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 17 | reading.Position.OccurredAt | `TestOfflineTransportsShareOccurrenceAndActivityMatrix` — `internal/api/offline_position_test.go`<br>`TestBulkSyncProgressUTCTimestampWinsContinueReading` — `internal/api/progress_offline_timezone_test.go`<br>`Fclock`（精确标题见前端索引） | G：表列 Go 测试均有本轮同名 pass 事件。 F：对应前端精确标题本轮均 passed。 |
| 18 | reading.RecordPosition ReceivedAt/服务端clock | `TestReceivedFactsAtSameTickRetainRetryIdentity` — `internal/reading/sync_test.go`<br>`TestFreshDeviceReportsInSameReceiveTickDoNotBecomeHistoricalReplays` — `internal/koreader/position_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 19 | reading.RecordPosition同页新事实 | `TestSamePageNewFactRefreshesButIdentifiedRetryDoesNot` — `internal/reading/position_test.go`<br>`TestOnlinePositionPreservesOccurrenceAndRefreshesSamePage` — `internal/api/reading_position_test.go`<br>`Fclock`（精确标题见前端索引） | G：表列 Go 测试均有本轮同名 pass 事件。 F：对应前端精确标题本轮均 passed。 |
| 20 | database事实身份 + RecordPosition | `TestIdentifiedRetryRemainsDuplicateAfterAnotherFact` — `internal/reading/position_test.go`<br>`TestReceivedFactsAtSameTickRetainRetryIdentity` — `internal/reading/sync_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 21 | koreader.GetProgress/ReconcileProgressWithMatching；maintenance校准 | `TestMatchedPullUsesOriginalPreciseDeviceOccurrence` — `internal/koreader/position_test.go`<br>`TestCalibrationReplayUsesOriginalFactAndClearSurvivesReopen` — `internal/application/maintenance/maintenance_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 22 | reading.MarkBooks Clear持久化basis | `TestMarkUnreadRejectsOldReplayAndAllowsNewReading` — `internal/reading/mark_test.go`<br>`TestDeviceReplayAfterClearAndRestartCannotRestoreOldPosition` — `internal/koreader/position_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 23 | reading.RecordPosition在清除后接受新事实 | `TestMarkUnreadRejectsOldReplayAndAllowsNewReading` — `internal/reading/mark_test.go`<br>`TestClearEvidenceSurvivesRestartForUserAndAnonymousReader` — `internal/reading/mark_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 24 | database reading_position_basis + reading.MarkBooks | `TestClearEvidenceSurvivesRestartForUserAndAnonymousReader` — `internal/reading/mark_test.go`<br>`TestDeviceReplayAfterClearAndRestartCannotRestoreOldPosition` — `internal/koreader/position_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 25 | reading.MarkBooks Clear与LastReadAt分离 | `TestMarkSeriesIsolatedAndUnreadPreservesActivity` — `internal/reading/mark_test.go`<br>`TestMarkUnreadRejectsOldReplayAndAllowsNewReading` — `internal/reading/mark_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 26 | reading主体显式输入；reading.Bookmarks；KOReader账户主体 | `TestPositionsAreIsolatedClampedAndSupportUnknownPageCounts` — `internal/reading/position_test.go`<br>`TestBookmarksScopeEveryOperationToReaderAndBookWithoutReadingEffects` — `internal/reading/bookmarks_test.go`<br>`TestBookmarkHTTPUsesAuthenticatedReaderDespitePayloadIdentity` — `internal/api/reading_bookmarks_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 27 | reading.UserID=0兼容路径 | `TestOfflineTransportsShareOccurrenceAndActivityMatrix` — `internal/api/offline_position_test.go`<br>`TestClearEvidenceSurvivesRestartForUserAndAnonymousReader` — `internal/reading/mark_test.go`<br>`TestFreshLegacyMarksApplySequentiallyWithinOneClockTick` — `internal/reading/mark_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 28 | RecordPosition夹取；MarkBooks未知完成；Bookmarks.Save页码约束 | `TestPositionsAreIsolatedClampedAndSupportUnknownPageCounts` — `internal/reading/position_test.go`<br>`TestMarkSelectionLimitsAndUnknownPages` — `internal/reading/mark_test.go`<br>`TestBookmarksClampPagesTrimNotesAndKeepExistingIdentity` — `internal/reading/bookmarks_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 29 | reading.MarkBooks -> recordActivity | `TestBulkReadActivityUsesOriginalDayAndRetryDoesNotAddAnotherDay` — `internal/reading/activity_test.go`<br>`TestBulkUpdateBookProgressMarksReadAndUnread` — `internal/api/controller_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 30 | reading.MarkSeries -> MarkBooks | `TestMarkSeriesIsolatedAndUnreadPreservesActivity` — `internal/reading/mark_test.go`<br>`TestBulkUpdateSeriesProgressMarksAllBooksReadAndUnread` — `internal/api/controller_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 31 | MarkBooks IsRead=false不调用活动写入 | `TestMarkSeriesIsolatedAndUnreadPreservesActivity` — `internal/reading/mark_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 32 | MarkSeries/MarkBooks未读保留历史 | `TestMarkSeriesIsolatedAndUnreadPreservesActivity` — `internal/reading/mark_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 33 | MarkBooks unknown IsRead + zero-page activity | `TestUnknownReadStateHasActivityWithoutInventedPages` — `internal/reading/activity_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 34 | reading.InspectBook/Summarize；数据库显式is_read迁移；前端bookState | `TestUnknownReadStateHasActivityWithoutInventedPages` — `internal/reading/activity_test.go`<br>`TestLegacyCompletionMigrationPreservesHistoryAndDoesNotInventCurrentPages` — `internal/reading/activity_test.go`<br>`Funknown`（见文末真实it标题/路径） | G：表列 Go 测试均有本轮同名 pass 事件。 F：对应前端精确标题本轮均 passed。 已核对未知完成独立 is_read；旧历史活动保留，真实大页数不按哨兵猜改。 |
| 35 | recordActivity ActivityDayKey(LocalWallClock(at)) | `TestOfflineTransportsShareOccurrenceAndActivityMatrix` — `internal/api/offline_position_test.go`<br>`TestReadingActivityDateMatchesLastReadAt` — `internal/database/activity_timezone_test.go`<br>`Fclock`（精确标题见前端索引） | G：表列 Go 测试均有本轮同名 pass 事件。 F：对应前端精确标题本轮均 passed。 后端原时/本地日历矩阵与前端日界三出口红绿分别证明传递与时间源。 |
| 36 | MarkBooks ResolveReadingOperationTime | `TestBulkReadActivityUsesOriginalDayAndRetryDoesNotAddAnotherDay` — `internal/reading/activity_test.go`<br>`TestIdentifiedBulkRetryWithoutTimestampRetainsOriginalDay` — `internal/reading/activity_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 37 | 位置FactID/标记OperationID | `TestSamePageNewFactRefreshesButIdentifiedRetryDoesNot` — `internal/reading/position_test.go`<br>`TestPartialBulkActivityOnlyUsesCommittedBooksAndKeepsOriginalDayOnRetry` — `internal/reading/activity_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 38 | SyncPositions逐项事实；离线队列只保留可证明输入 | `TestOfflineTransportsShareOccurrenceAndActivityMatrix` — `internal/api/offline_position_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 仅处理输入已证明的事实；没有按最终位置猜补多日历史。 |
| 39 | reading.recordActivity每日每书最大绝对页码 | `TestOfflineTransportsShareOccurrenceAndActivityMatrix` — `internal/api/offline_position_test.go`<br>`TestBulkReadActivityUsesOriginalDayAndRetryDoesNotAddAnotherDay` — `internal/reading/activity_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 reading.recordActivity 注释明确每日每书最大绝对页码；不是翻页增量。 |
| 40 | database升级保留legacy活动 | `TestLegacyCompletionMigrationPreservesHistoryAndDoesNotInventCurrentPages` — `internal/reading/activity_test.go`<br>`TestMigrationRetainsLegacyIdentityAndRejectsOlderReading` — `internal/reading/position_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 41 | MarkBooks markBatchSize分批 | `TestLaterMarkBatchFailureRetainsAndInvalidatesCommittedRange` — `internal/reading/mark_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 42 | database.MutateReadingPositions原子聚合 | `TestMarkBatchFailureRollsBackProgressBasisAndAggregate` — `internal/reading/mark_test.go`<br>`TestPositionFailureRollsBackStateAndDoesNotInvalidate` — `internal/reading/position_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 43 | MarkResult.CommittedBookIDs保留前批 | `TestLaterMarkBatchFailureRetainsAndInvalidatesCommittedRange` — `internal/reading/mark_test.go`<br>`TestPartialBulkActivityOnlyUsesCommittedBooksAndKeepsOriginalDayOnRetry` — `internal/reading/activity_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 44 | MarkBooks延后失效按CommittedBookIDs | `TestLaterMarkBatchFailureRetainsAndInvalidatesCommittedRange` — `internal/reading/mark_test.go`<br>`Fpartial`（见文末真实it标题/路径） | G：表列 Go 测试均有本轮同名 pass 事件。 F：对应前端精确标题本轮均 passed。 服务层按已提交范围失效；前端部分审核消费者是另一条刷新证据，未冒充所有 HTTP 路径。 |
| 45 | reading.Result.Warnings和MarkResult.Warnings | `TestActivityFailureKeepsCommittedPositionAndAttemptsUserProjection` — `internal/reading/position_test.go`<br>`TestBulkReadActivityFailuresRemainWarningsAndProjectionsAreIndependent` — `internal/reading/activity_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 46 | reading.recordActivity slog.WarnContext；RecordTime警告 | `TestReadingTimeStorageFailureIsObservable` — `internal/api/progress_reviews_stats_test.go`<br>`TestActivityFailureKeepsCommittedPositionAndAttemptsUserProjection` — `internal/reading/position_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 结果 Warning、HTTP 可观测时长故障及 recordActivity 的 WarnContext 日志共同支持；没有新增日志镜像测试。 |
| 47 | KOReader SyncResult与设备canonical保存独立 | `TestMatchedDeviceActivityWarningIsNotAPositionFailure` — `internal/koreader/position_test.go`<br>`TestDeviceCanonicalSaveFailureDoesNotHideCommittedBookPosition` — `internal/koreader/position_test.go`<br>`TestSaveProgressSurfacesApplyFailure` — `internal/koreader/service_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 48 | reading.recordActivity独立尝试global/user | `TestBulkReadActivityFailuresRemainWarningsAndProjectionsAreIndependent` — `internal/reading/activity_test.go`<br>`TestActivityFailureKeepsCommittedPositionAndAttemptsUserProjection` — `internal/reading/position_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 49 | reading.RecordTime；useReaderReadingTime | `TestReadingTimeAccumulatesOnlyForItsReaderAndReceiptDay` — `internal/reading/time_test.go`<br>`TestReadingTimeActivityFailureRollsBackTimeButKeepsSavedPosition` — `internal/reading/time_test.go`<br>`Ftime`（见文末真实it标题/路径） | G：表列 Go 测试均有本轮同名 pass 事件。 F：对应前端精确标题本轮均 passed。 |
| 50 | reading.Bookmarks.List/Save/Delete；api只解析主体和响应 | `TestBookmarksCRUDPreservesIdentityAndOrdersPages` — `internal/reading/bookmarks_test.go`<br>`TestBookmarksScopeEveryOperationToReaderAndBookWithoutReadingEffects` — `internal/reading/bookmarks_test.go`<br>`TestBookmarksExposeStorageFailuresAndKeepSavedNotes` — `internal/reading/bookmarks_test.go`<br>`TestBookmarkHTTPUsesAuthenticatedReaderDespitePayloadIdentity` — `internal/api/reading_bookmarks_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 51 | reading.Queries/InspectBook/CatalogueQueries/RecentReads | `TestReaderViewsIsolateSubjectAndReflectBackwardPositionAndClear` — `internal/reading/queries_test.go`<br>`TestReadingProjectionHTTPAndProtocolMatrix` — `internal/api/reading_projection_test.go`<br>`TestCatalogueQueriesFilterAndPageUsingRequestedSubject` — `internal/reading/queries_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 52 | reading.RecordTime/Queries/Bookmarks各完整能力 | `TestReadingTimeCompatibilityAndCap` — `internal/reading/time_test.go`<br>`TestSeriesQueriesKeepNaturalOrderUnknownCompletionAndReadOnlyTimes` — `internal/reading/queries_test.go`<br>`TestBookmarksScopeEveryOperationToReaderAndBookWithoutReadingEffects` — `internal/reading/bookmarks_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 书签 fixture 先记录位置/时长，CRUD 后确认读态、活动、时长不变。 |
| 53 | organize.Collections Create/AddMembers/RemoveMember/Delete | `TestOnlyContinuouslyDerivedCollectionsRejectManualChanges` — `internal/organize/collections_test.go`<br>`TestMemberBatchIsAtomicForMissingSeries` — `internal/organize/collections_test.go`<br>`TestRegularUserCanReadButCannotMutateManualCollections` — `internal/api/collection_authorization_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 54 | organize.SmartCollections.Members与保存条件 | `TestSmartShelfSavedConditionsDriveMembersAndCount` — `internal/organize/smart_collections_test.go`<br>`TestSmartCollectionSeriesUsesSavedFilter` — `internal/api/collection_view_controller_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 55 | SmartCollections.Views/Members共同过滤 | `TestSmartShelfSavedConditionsDriveMembersAndCount` — `internal/organize/smart_collections_test.go`<br>`TestSmartCollectionCountMatchesListing` — `internal/api/collection_view_count_test.go`<br>`TestSmartCollectionCountIsPerUser` — `internal/api/collection_view_count_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 56 | SmartCollections.Members分页 | `TestSmartShelfAdvancedConditionsAreUserScopedAndPageable` — `internal/organize/smart_collections_test.go`<br>`TestSmartCollectionSeriesPagesBeyondConfiguredPageSize` — `internal/api/collection_view_paging_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 57 | organize.SmartCollections快照用例 | `TestSmartSnapshotIsOneTimeAndManuallyOwned` — `internal/organize/smart_collections_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 58 | 快照归人工；Collections成员写入 | `TestSmartSnapshotIsOneTimeAndManuallyOwned` — `internal/organize/smart_collections_test.go`<br>`TestOnlyContinuouslyDerivedCollectionsRejectManualChanges` — `internal/organize/collections_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 59 | organize.Relations + franchiseRebuilder | `TestRelationChangesRebuildDerivedCollections` — `internal/organize/relations_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 60 | organize.Collections派生所有权限制 | `TestOnlyContinuouslyDerivedCollectionsRejectManualChanges` — `internal/organize/collections_test.go`<br>`TestFranchiseCollectionRejectsManualEdits` — `internal/api/franchise_readonly_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 61 | organize作品群自然身份重建 | `TestFranchiseMergeSplitKeepsUnaffectedIdentitiesAndTimes` — `internal/organize/relations_test.go`<br>`TestFranchiseRebuildKeepsCollectionIdentity` — `internal/api/franchise_stability_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 62 | 作品群成员差集 | `TestFranchiseMergeSplitKeepsUnaffectedIdentitiesAndTimes` — `internal/organize/relations_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 63 | 作品群必要变化时间 | `TestFranchiseMergeSplitKeepsUnaffectedIdentitiesAndTimes` — `internal/organize/relations_test.go`<br>`TestFranchiseRebuildKeepsUnrelatedCollectionsStableWhenOneChanges` — `internal/api/franchise_stability_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 64 | organize.AIGrouping.ApplyCollection/ApplyReview | `TestAIGroupingApplyCollectionOwnsIdentityAndAdjudication` — `internal/organize/ai_grouping_test.go`<br>`TestAIGroupingLateFailureRollsBackIdentityAndDecision` — `internal/organize/ai_grouping_test.go`<br>`TestAIGroupingCASLossRollsBackNewCollection` — `internal/organize/ai_grouping_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 65 | organize.CheckCollectionName被人工/快照/AI使用 | `TestRenameUsesSQLiteNameComparison` — `internal/organize/collections_test.go`<br>`TestSmartSnapshotPreviewConflictAndFailureRollback` — `internal/organize/smart_collections_test.go`<br>`TestAIGroupingBatchNameConflictRollsBack` — `internal/organize/ai_grouping_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 66 | AIGrouping裁决过滤消失系列 | `TestAIGroupingBatchSkipsGoneSeriesAndLeavesThemForDecision` — `internal/organize/ai_grouping_test.go`<br>`TestAIGroupingAllGoneDoesNotPublishOrAdjudicate` — `internal/organize/ai_grouping_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 67 | organize.AIGrouping与proposal/seriesmeta分离 | `TestScrapeLibraryQueuesProposalWithOriginalSourceAndLocale` — `internal/application/intelligence/intelligence_test.go`<br>`TestGroupingQueuesIndependentReviewAndReplaysLocale` — `internal/application/intelligence/intelligence_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 生成、分组审核与系列元数据 proposal/seriesmeta 的模型和裁决分开。 |
| 68 | api OPDS/Mihon读取organize身份 | `TestOPDSCollectionFeeds` — `internal/api/opds_controller_test.go`<br>`TestMihonCollectionEndpoints` — `internal/api/mihon_controller_test.go`<br>`TestApplyAIGroupingReviewCollectionReportsCreatedID` — `internal/api/ai_grouping_review_apply_conflict_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 69 | organize.ReadingLists Items/Page/Reorder -> reading投影 | `TestOrderedReadingListsOwnMembershipAndAtomicReorder` — `internal/organize/reading_lists_test.go`<br>`TestReadingListProgressFollowsReaderFactsAndNeverCreatesActivity` — `internal/organize/reading_lists_test.go`<br>`TestReadingListProtocolsKeepIdentityOrderAndPagination` — `internal/api/reading_list_controller_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 70 | organize各明确用例类型 | `TestManualCollectionsNameAndIdentity` — `internal/organize/collections_test.go`<br>`TestOrderedReadingListsOwnMembershipAndAtomicReorder` — `internal/organize/reading_lists_test.go`<br>`TestAIGroupingApplyCollectionOwnsIdentityAndAdjudication` — `internal/organize/ai_grouping_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 人工核对 Collections/SmartCollections/Relations/AIGrouping/ReadingLists 各用例和窄存储接口。 |
| 71 | ApplicationSessionScope/Provider + App主导航装配 | `Froutes`（见文末真实it标题/路径）<br>`Fsession`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 72 | session onMessage(refresh)->useLibraryRefresh | `Froutes`（见文末真实it标题/路径）<br>`TestServerEventsFiltersTaskFramesByRole` — `internal/api/sse_audience_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 F：对应前端精确标题本轮均 passed。 |
| 73 | task-observation.useTaskObservation/useTaskBubbles | `Ftask`（见文末真实it标题/路径）<br>`Fbubbles`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 74 | api.serveEvents/SSE角色过滤；session非admin忽略运行帧 | `TestServerEventsFiltersTaskFramesByRole` — `internal/api/sse_audience_test.go`<br>`Fsession`（见文末真实it标题/路径） | G：表列 Go 测试均有本轮同名 pass 事件。 F：对应前端精确标题本轮均 passed。 |
| 75 | 一个SessionInstance一个useEventStream | `Fsession`（见文末真实it标题/路径）<br>`Froutes`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 76 | App主导航Scope与Layout | `Froutes`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 77 | App独立Reader/Graph不挂SessionScope | `Froutes`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 78 | session.useEventStream重连backoff | `Freconnect`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 79 | useSessionRecovery -> task/bubble/external补拉 | `Ftask`（见文末真实it标题/路径）<br>`Fbubbles`（见文末真实it标题/路径）<br>`FexternalRecovery`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 80 | session.trackRunPush(prev链接) | `Fchain`（见文末真实it标题/路径）<br>`Fsession`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 81 | useTaskObservation服务器live汇总 | `Ftask`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 82 | useTaskObservation/useTaskBubbles/useExternalLibrary独立投影 | `Ftask`（见文末真实it标题/路径）<br>`Fbubbles`（见文末真实it标题/路径）<br>`FexternalRecovery`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 83 | useLibraryInvalidation/Refresh + 各功能提交后失效 | `Fpartial`（见文末真实it标题/路径）<br>`Fcatalog`（见文末真实it标题/路径）<br>`FexternalRecovery`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 84 | 页面草稿/选区/筛选持有，session只发布水位 | `Fcatalog`（见文末真实it标题/路径）<br>`Ftask`（见文末真实it标题/路径）<br>`Fsession`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 85 | 身份/角色key重建Scope，useEventStream回调disposed保护 | `Fsession`（见文末真实it标题/路径）<br>`FtaskScope`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 86 | AuthProvider会话过期与显式logout区分；offlineOwner | `Fexpiry`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 87 | useReaderOffline当前书panel与跨书download；进度pipeline世代 | `Fdownload`（见文末真实it标题/路径）<br>`FlateReader`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 |
| 88 | useReaderPreferences设备localStorage；session不触及 | `Fexpiry`、`Fprefs`（精确标题见前端索引） | F：对应前端精确标题本轮均 passed。 真实 AuthProvider 换人/登出后设备键值保留；真实 BookReader 偏好消费回归支持重新读取。 |
| 89 | session协议解码；消费者用useSessionRunPush/Recovery/LibraryRefresh | `Ftask`（见文末真实it标题/路径）<br>`FexternalRecovery`（见文末真实it标题/路径）<br>`Fchain`（见文末真实it标题/路径） | F：对应前端精确标题本轮均 passed。 消费者仅订阅 session 语义 interface；传输事件名留在 session/useEventStream 和解码层。 |
| 90 | application.Runs.Start -> 同task.Engine；scanning统一准入 | `TestProtectedLibraryRejectsAutomaticScansAndEveryCleanup` — `internal/application/scanning/scanning_test.go`<br>`TestWatchedQueueWaitsForTheRealRunAndObservesCancellation` — `internal/application/runs_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 91 | maintenance.Restarts/intelligence.Restarts等装配期登记 | `TestRestartPreservesForegroundMatchingArguments` — `internal/application/maintenance/maintenance_test.go`<br>`TestGroupingQueuesIndependentReviewAndReplaysLocale` — `internal/application/intelligence/intelligence_test.go`<br>`TestAssemblyRegistersRestartCapabilitiesBeforeAnyLaunch` — `internal/application/runtime/runtime_test.go`<br>`TestInterruptedHistoryReplaysThroughFirstProcessRegistry` — `internal/application/runtime/runtime_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 Registry 在任何发起前建立，Retryable/Resumable/Restart 使用同一声明。 |
| 92 | relocating.Recover先于后台和HTTP开放 | `TestRuntimePolicyFailureKeepsAccessProtectedUntilRecovery` — `internal/application/relocating/relocating_test.go`<br>`TestFailedConsistencyRecoveryNeverOpensBackgroundInputs` — `internal/application/runtime/runtime_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 93 | task.Identity/task.Run，Runs.Start与Engine.Retry | `TestRetryIsANewRunAndKeepsThePreviousOne` — `internal/task/slots_test.go`<br>`TestForegroundAndBackfillRetainDistinctConcurrentIdentitiesAndReason` — `internal/application/maintenance/maintenance_test.go`<br>`TestInterruptedHistoryReplaysThroughFirstProcessRegistry` — `internal/application/runtime/runtime_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 94 | run ID控制；DecorateRunContext仅任务键日志 | `TestCoverWorkerLogsUseItsOwnRunAfterTheScanFinishes` — `internal/application/scanning/cover_test.go`<br>`TestRunClaimsSlotSynchronouslyAndDefersBody` — `internal/api/task_run_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 24 后续 fixture 仅显式登记观测身份，未恢复任务键寻址；Release/Claims 已分别精确独跑通过。 |
| 95 | task领域规则/taskstore DDL和落盘 | `TestMigrateCreatesTaskRunTablesAndDropsLegacyTasks` — `internal/database/taskstore_schema_test.go`<br>`TestCleanupPrunesHistoryAndPreservesLiveRuns` — `internal/application/runs_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 task 不持 SQL；taskstore 继续独立拥有 DDL 和落盘，runtime 只连接。 |
| 96 | relocation领域+application.relocating | `TestApplicationRelocationPreservesIdentityAndReleasesOnlyAfterEffects` — `internal/application/relocating/relocating_test.go`<br>`TestRelocationRepairEditedRootRequiresExplicitSourceAndPreservesReading` — `internal/api/relocation_repair_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 97 | application.relocating effects访问/配置/缓存/外部/watcher | `TestApplicationRelocationPreservesIdentityAndReleasesOnlyAfterEffects` — `internal/application/relocating/relocating_test.go`<br>`TestConfigurationWriteFailureCompensatesBeforeOpeningAccess` — `internal/application/relocating/relocating_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 98 | 恢复失败保留保护 | `TestRuntimePolicyFailureKeepsAccessProtectedUntilRecovery` — `internal/application/relocating/relocating_test.go`<br>`TestRestorationRequiresTheExplicitAdministratorDecision` — `internal/application/relocating/relocating_test.go`<br>`TestFailedConsistencyRecoveryNeverOpensBackgroundInputs` — `internal/application/runtime/runtime_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 99 | scanning.WatchLibrary/WatchCleanup -> Runs.AwaitWatched/AwaitOutcome | `TestWatchedQueueWaitsForTheRealRunAndObservesCancellation` — `internal/application/runs_test.go`<br>`TestWatchedFailureCannotBeReportedAsSuccess` — `internal/application/runs_test.go`<br>`TestWatchRenameCompletesBeforeCleanup` — `internal/application/scanning/scanning_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 100 | scanning.WatchLibrary重连后WatchCleanup | `TestWatchRenameCompletesBeforeCleanup` — `internal/application/scanning/scanning_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 101 | scanning.DispatchCoverBatch运行体claim | `TestQueuedAndMergedCoverBatchesAreClaimedOnlyAtExecution` — `internal/application/scanning/cover_test.go`<br>`TestFailedCoverAdmissionLeavesOtherBatchesAndDoesNotGenerateWithoutARun` — `internal/application/scanning/cover_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 102 | scanning封面运行独立身份与槽位 | `TestCoverBackfillUsesItsOwnRun` — `internal/application/scanning/cover_test.go`<br>`TestFullSlotsDoNotStartCoverGeneration` — `internal/application/scanning/cover_test.go`<br>`TestCoverPauseCancellationAndRestartBackfill` — `internal/application/scanning/cover_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 103 | DispatchCoverBatch非阻塞发起 | `TestApplicationScanCompletesWhileItsCoverRunStillWaits` — `internal/application/scanning/cover_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 104 | scanning.Service.BatchEvent(effects) | `TestBatchEventKeepsAllEffects` — `internal/application/scanning/scanning_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 105 | application/runtime.Shutdown/Close 与 host 请求排空 | `TestSSEShutdownReleasesInFlightConnections` — `internal/api/sse_broker_test.go`<br>`TestSSEMainLoopExitsOnShutdown` — `internal/api/sse_broker_test.go`<br>`TestShutdownWaitsForAlreadyAdmittedBackgroundHandoff` — `internal/application/runtime/runtime_test.go`<br>`TestShutdownDrainsTransportBeforeTaskCancellationAndResourceRelease` — `internal/application/runtime/runtime_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 生产等价图验证配置/SSE先停、请求排空、取消并等待任务及后台、最后释放归档资源。 |
| 106 | application.DecorateRunContext及各体slog.*Context | `TestCoverAdmissionWarningKeepsTheOriginatingScanContext` — `internal/application/scanning/cover_test.go`<br>`TestCoverWorkerLogsUseItsOwnRunAfterTheScanFinishes` — `internal/application/scanning/cover_test.go`<br>`TestScrapeLibraryQueuesProposalWithOriginalSourceAndLocale` — `internal/application/intelligence/intelligence_test.go`<br>`TestShutdownDrainsTransportBeforeTaskCancellationAndResourceRelease` — `internal/application/runtime/runtime_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 107 | 按票完整用例独立提交与门禁 | `TestOrderedReadingListsOwnMembershipAndAtomicReorder` — `internal/organize/reading_lists_test.go`<br>`TestIndexMaintenanceCompletesThroughApplicationAtOneSlot` — `internal/application/maintenance/maintenance_test.go`<br>`TestManagerPublicationReleasesTheSameEngineQueue` — `internal/application/runtime/runtime_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 01–24 独立提交/集成记录保留；25 当前联合执行不代替各批历史证据。 |
| 108 | reading.positionFixture；organize.newCollections；application可控依赖fixture | `TestMarkBatchFailureRollsBackProgressBasisAndAggregate` — `internal/reading/mark_test.go`<br>`TestAIGroupingCASLossRollsBackNewCollection` — `internal/organize/ai_grouping_test.go`<br>`TestIndexMaintenanceCompletesThroughApplicationAtOneSlot` — `internal/application/maintenance/maintenance_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 reading/organize/application 公共完整接口直接用真实 SQLite，功能规则无需 Controller。 |
| 109 | HTTP/KOReader/OPDS/Mihon/页面保留集成 | `TestReadingProjectionHTTPAndProtocolMatrix` — `internal/api/reading_projection_test.go`<br>`TestReadingListProtocolsKeepIdentityOrderAndPagination` — `internal/api/reading_list_controller_test.go`<br>`Froutes`（见文末真实it标题/路径） | G：表列 Go 测试均有本轮同名 pass 事件。 F：对应前端精确标题本轮均 passed。 |
| 110 | database阅读basis/facts/is_read升级；重开store | `TestMigrationRetainsLegacyIdentityAndRejectsOlderReading` — `internal/reading/position_test.go`<br>`TestLegacyCompletionMigrationPreservesHistoryAndDoesNotInventCurrentPages` — `internal/reading/activity_test.go`<br>`TestClearEvidenceSurvivesRestartForUserAndAnonymousReader` — `internal/reading/mark_test.go` | G：表列 Go 测试均有本轮同名 pass 事件。 |
| 111 | SQL/TS生成、Go1.26.10、格式lint及前端门禁 | 无专门运行测试 | 见门禁表及对应原始日志；SQL/TS 生成无漂移。 |
| 112 | spec ready-for-agent / issues实际状态 / implementation记录 | 无专门运行测试 | spec 保持需求 ready-for-agent；工单、implementation 与本验收分别表达交付和实际执行。 |

## 前端精确标题索引

所有 F 引用均来自本轮 frontend-tests.json 的 passed 断言，不据旧票状态推断。

- `Funknown`：`web/src/reading/unknownState.test.tsx`：`shows an unknown-page completion as mark-unread without inventing volume pages`。
- `Froutes`：`web/src/session/ApplicationRoutes.test.tsx`：`actual navigation routes share the session, refresh the library and release it in reader and graph`。
- `Fsession`：`web/src/session/ApplicationSession.test.tsx`：`shares one connection, decodes refresh once, and preserves consumer local input`；`replaces sensitive projections on identity/role change and rejects old connection callbacks`。
- `Fpartial`：`web/src/session/catalogConsumers.test.tsx`：`refreshes known committed review work even when a later operation fails`。
- `Fcatalog`：`web/src/session/catalogConsumers.test.tsx`：`scan refresh updates selected collection members and preserves the unsaved creation form`；`session refresh preserves the series draft and its original save version`。
- `Ftime`：`web/src/pages/book-reader/useReaderReadingTime.test.tsx`：`reports only active visible reading and resumes after reader input`；`stops counting idle time until reader activity resumes`；`flushes once through beacon on hide and page exit without resending on unmount`。
- `Ftask`：`web/src/features/task-observation/useTaskObservation.test.tsx`：`recovers expanded history and backend totals on gaps/reconnect while preserving filters`。
- `FtaskScope`：`web/src/features/task-observation/useTaskObservation.test.tsx`：`does not issue follow-up requests when an old control completes after leaving its scope`。
- `Fbubbles`：`web/src/features/task-observation/useTaskBubbles.test.ts`：`recovers a missed terminal frame for an existing bubble and still expires it`。
- `FexternalRecovery`：`web/src/features/external-library/useExternalLibrary.test.tsx`：`recovers a missed transfer terminal after reconnect without a snapshot DOM bridge`。
- `Freconnect`：`web/src/session/useEventStream.test.ts`：`reconnects with backoff after the browser gives up (readyState CLOSED)`；`does not interfere while the browser is retrying on its own`；`closes the connection and cancels pending retries on unmount`。
- `Fchain`：`web/src/utils/runPush.test.ts`：`序号跳号但链接得上：不算缺口`；`prev 对不上手里那一号就是缺口`。
- `Fexpiry`：`web/src/auth/sessionExpiryOffline.test.tsx`：`后端答「未登录」后，离线书目、缓存字节与进度队列原样还在`；`任意请求收到 401 后，同样不动本机的离线数据`；`B 登录后，A 的书目、字节与队列都不在了`。
- `Fdownload`：`web/src/pages/book-reader/useReaderOffline.test.tsx`：`上一本书还在下载时换书，新书的面板不挂它的进度与禁用态`；`切回原来那本书，仍看得见它在下载以及下到第几页`。
- `FlateReader`：`web/src/pages/book-reader/readerSwitchLateReport.test.tsx`：`queues a late failed report for its original book without polluting the new book UI`。
- `Fexpiry` 补充：`web/src/auth/sessionExpiryOffline.test.tsx`：`先把队列回传上去，再清掉书目、字节与队列`。
- `Fprefs` 补充：`web/src/pages/book-reader/readerImageOptionsSwitch.test.tsx`：`档位不进请求地址时不清缓存，当前页原地不动`；`档位进了请求地址时旧图作废，但当前页要被重新取回来`。
- `Fclock` 补充：`web/src/pages/book-reader/useReaderProgressIndicator.test.tsx`：`keeps the real reading date at the local day boundary through online`；`keeps the real reading date at the local day boundary through offline`；`keeps the real reading date at the local day boundary through network-failure`；`reports a later same-page reading as a distinct fact`。
