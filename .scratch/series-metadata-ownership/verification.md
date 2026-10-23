# 系列元数据归属：实现验收记录

本记录覆盖 `spec.md` 的全部 68 条故事。行为测试使用正式来源入口和真实临时 SQLite；结构与范围要求通过接口、调用点及差异审查验证。测试覆盖不等于真实部署验证，也不等于全项目架构重构完成。

## 实际归属与退出结果

| 来源 | 规则入口 | 调用流程保留的职责 |
| --- | --- | --- |
| 人工替换 | `seriesmeta.SaveManual`、`LoadVersion`、`Version` | `Controller.updateSeriesInfo` 的 HTTP 解析、事务、统计与冲突响应；`getSeriesContext` 下发版本 |
| 批量增量 | `seriesmeta.EditSelection` | `Controller.bulkEditSeries` 的整批事务、提交后尽力刷新及缓存失效 |
| 扫描初值、更新与追加 | `PrepareScanInitial`、`PrepareScanUpdate`、`ParseScanLocks`、`AppendScanAssociations` | `Scanner.ingestResults` 的身份、路径、书批次、快照缓存、失败容忍；扫描生命周期、节流刷新与封面运行 |
| 已裁决提案写入 | `seriesmeta.ApplyProposal` | `proposal.Service` 的入队去重、字段裁决、fill_empty、来源默认置信度、部分应用及终态 CAS |

四来源的字段空值、集合、锁及沿革规则集中在 `internal/seriesmeta`。调用方通过来源明确的参数表达意图；扫描把只含元数据的 `ScanMetadata` 合入既有身份 upsert。没有公开变更集、通用策略标志、续跑协议或新的读取步骤。人工与提案快照读取时机、扫描预载与同批缓存时机保持原流程。

事务交接采用 `ManualQueries`、`SelectionQueries`、`ScanAssociationQueries`、`ProposalQueries` 等结构型窄能力，由事务绑定的 `*database.Queries` 满足；`proposal.Queries` 编译期组合 `seriesmeta.ProposalQueries`。业务 module 不接收全局 Store、Controller、配置或原始 SQL 执行器，不自行开启/提交事务。数据库与 metadata 无反向业务依赖。

已退出人工 adapter 内的字段替换和指纹副本、`proposal/writer.go`、scanner 的标量投影和锁解析副本、数据库 Store 上的 `BulkEditSeries` 高层业务入口及 `BulkSeriesEdit`。数据库保留 `SetSeriesStatusBatch`、`SetSeriesPublisherBatch`、`LinkSeriesTagBatch`、`UnlinkSeriesTagByNameBatch` 的集合 SQL 和 `sqlInBatches`。全局 `RenameTag`、`MergeTags`、`DeleteTag`、扫描身份 upsert 与合法底层查询仍有独立归属，不宣称相关表独占写入。

失去独立价值的 writer、旧批量字段测试迁入公共 interface seam；HTTP 并发/原子性、提案裁决/关单、扫描批次/取消/统计保护保留。没有增加通用架构检查平台，既有提案查询归属守卫继续由完整测试运行验证。验收按规则 locality、调用方职责与兼容事实判断，不按行数变化承诺收益。

## 跨来源与范围外验收

`api.TestSeriesMetadataSourcesComposeWithoutChangingIdentity` 对同一系列依次调用人工 HTTP 保存、批量 HTTP 编辑、真实 CBZ 的 `ScanLibrary`、真实 `Service.Queue`/`Apply`。每阶段断言标量、标题首字母、作者和标签最终事实，区分替换、增量、追加、替换；每次内容变化都使 HTTP 编辑版本失效，旧版本保存返回 409。人工/批量/扫描不新增沿革，提案沿革保留 source、URL、置信度和 review ID。最后验证系列/书/资料库 ID、路径、人工封面、旧及用户阅读进度、关系、自定义字段、FTS、标签计数和系列标签缓存。

`api.TestMetadataFingerprintIgnoresCollectionStorageOrder` 经真实人工保存重建标签、作者和链接并调换锁顺序，验证内容指纹保持不变。`api.TestGlobalTagMaintenanceInvalidatesMetadataFingerprint` 经既有全局 HTTP handler 顺序执行改名、合并、删除，每一步均改变相关系列指纹，最终集合为空。

## 全部用户故事追踪

以下每条列出实现入口、可执行证据或审查证据。测试名使用所属包前缀；重复的测试名表示同一业务场景承担多个验收点，而不是新增私有 helper 测试面。完整后端门禁与最终分支复验状态见末节。

| US | 证据与边界 |
| --- | --- |
| 1 | `seriesmeta` 四来源入口是字段规则审查位置；归属表和调用点审查。 |
| 2 | `api.TestSeriesMetadataSourcesComposeWithoutChangingIdentity` 贯穿四条真实来源。 |
| 3 | `ManualInput`、`SelectionInput`、`ScanMetadata`、`ProposalInput` 表达来源意图；接口审查没有万能策略组合。 |
| 4 | `seriesmeta` 仅依赖所需数据库类型、parser/metadata 值及标准库；不依赖 Controller；独立包测试。 |
| 5 | `updateSeriesInfo`、`bulkEditSeries`、`ingestResults`、`Service.Apply` 调用新入口；字段判据集中，身份与编排仍由调用方持有。 |
| 6 | 旧人工替换/指纹、proposal writer、扫描字段/锁副本与 Store 批量业务入口退出；差异和符号检索审查。 |
| 7 | 本记录分别验证规则归属、编排和最终事实；不使用测试或文件数量推导完成。 |
| 8 | 全局标签、身份、书、阅读状态与其他范围外写入保持原归属；全局标签验收与范围审查。 |
| 9 | `seriesmeta.TestManualPreservesRawValuesAndAbsentCollections` 验证空标量及非正评分 NULL。 |
| 10 | 同上，nil 集合保留原有标签、作者与链接。 |
| 11 | 同上，显式空集合清空关联。 |
| 12 | 同上，人工仍能写锁定字段及锁本身；`SaveManual` 不按锁拒绝人工输入。 |
| 13 | 同上，保留原始空白值和过滤规则，没有全局归一。 |
| 14 | `api.TestUpdateSeriesInfoConcurrentEditConflict` 及跨来源、全局标签指纹验收。 |
| 15 | `SaveManual` 在调用方 `ExecTx` 内 `LoadVersion`；`api.TestManualCompetingSavesCommitExactlyOneCompleteEdit` 同一启动屏障放行两次同版本 HTTP 保存，恰一成功一冲突，最终标量/集合完整属于胜者。 |
| 16 | `api.TestUpdateSeriesInfoConcurrentEditConflict` 覆盖省略版本兼容与显式空版本拒绝。 |
| 17 | `seriesmeta.TestManualConflictAndAssociationFailurePreserveContent`；`api.TestUpdateSeriesInfoLinkRebuildRollsBack` 保留真实约束失败回滚。 |
| 18 | `api.TestManualStatsRefreshFailureRollsBackContentAndAssociations` 的真实 series_stats 触发器失败返回 500，全部内容指纹与缓存保持原样；跨来源验收验证正常刷新最终事实。 |
| 19 | `SelectionInput` 仅支持状态、出版社及标签增删；`TestSelectionPreservesRawScalarsAndRemovesAfterAdding`。 |
| 20 | 同上，nil 标量保留；`EditSelection` 不读取/整行写回系列。 |
| 21 | 同上，同名先添加后删除，最终删除生效。 |
| 22 | 同上，空白、重复和缺失 ID；`api.TestBulkMetadataTransactionAndRefreshFailure` 验证重复/缺失选区响应。 |
| 23 | `TestSelectionCrossChunkFailureRollsBackAllChanges`、`api.TestBulkMetadataTransactionAndRefreshFailure` 验证跨分块/HTTP 整批回滚。 |
| 24 | `seriesmeta.TestBulkOperationsExceedVariableLimit`；数据库事务批量原语维持集合式 SQL 和 `sqlInBatches`。未执行独立吞吐基准。 |
| 25 | `TestSelectionPreservesRawScalarsAndRemovesAfterAdding` 验证锁仍不拦批量；跨来源验收验证无新沿革。 |
| 26 | `api.TestBulkMetadataTransactionAndRefreshFailure` 的 series_stats 插入失败场景仍返回成功且内容已提交。 |
| 27 | `TestScanInitialMetadataAndExistingLocks`；`scanner.TestScanKeepsUserEditedSeriesTitleAndLocks` 和 `TestScanKeepsSeriesLocksWhenAddingVolume` 首建子例。 |
| 28 | 首建默认值由 `PrepareScanInitial` 返回；`ScanMetadata` 限定输出字段，与更新规则同包审查。 |
| 29 | `Scanner.ingestResults` 保留身份、路径、书入库；跨来源验收、`TestRenamedBookKeepsIdentityAndProgress`；既有 `api.TestOldScanSnapshotCannotRunAfterRelocation` 与 `TestRelocationProtectionSurvivesFreshStartupAndAllCleanupEntrypoints` 保留。 |
| 30 | `TestScanTitleFallbackAndLockedScalarMatrix`、`scanner.TestScanKeepsUserEditedSeriesTitleAndLocks`。 |
| 31 | `TestScanInitialMetadataAndExistingLocks`、`TestScanTitleFallbackAndLockedScalarMatrix`；跨来源验收也观察扫描空 status。 |
| 32 | `TestScanAssociationsAppendDespiteCollectionLocksAndErrors`；跨来源验收保留人工和批量标签、作者。 |
| 33 | `scanner.TestScanMetadataFailurePathsKeepTheirBatchSemantics` 区分首建失败、已有行失败和书事务回滚；module 测试验证关联失败容忍。 |
| 34 | 同上，书触发事务回滚后无空系列残留；`ingestResults` 在原批次事务中组合元数据与书。 |
| 35 | `scanner.TestScanLibraryDeferredRefreshPopulatesStats`、`TestScanReportsStaleSeriesStatsInsteadOfSilentlyDropping`；dirty-series 节流代码保持原调度。 |
| 36 | `ScanLocks` 显式交接既有缓存；`ingestResults` 保持预载/同批缓存；标题锁和多卷高层用例及代码审查。 |
| 37 | `scanner.TestScanDrainsDirtySeriesAfterCancel` 验证取消后已提交书和统计；`TestScanLibraryReturnsContextCancelled`。 |
| 38 | `Service.Queue`/`Apply` 保留生命周期；`TestQueueReusesPendingProposalWithSameSignature`、`TestQueueReportsRejectedBefore` 等原有服务测试。 |
| 39 | `seriesmeta.TestProposalPreservesSnapshotValuesAndUnsupportedFields`、`TestApplyMetadataHonorsLockedFields`；`proposal.TestApplyWritesEveryFieldAndClosesProposal` 与状态相关 API 测试。 |
| 40 | `proposal.TestApplyRemovesTagsAndAuthorsMissingFromProposal`；跨来源验收最终仅有提案集合。 |
| 41 | `seriesmeta.TestProposalDistinguishesAbsentAndEmptyCollections`；`proposal.TestApplyFillEmptyLeavesUntouchedCollectionsAlone`。 |
| 42 | `proposal.TestApplyFiltersFieldsLockedAfterQueueing`、`TestApplyReportsAllFieldsLockedWhenEverythingIsFiltered`、`seriesmeta.TestApplyMetadataHonorsLockedFields`。 |
| 43 | `proposal.TestApplyFillEmptyJudgesEmptinessBySeriesNotSnapshot`、`TestApplyFillEmptyTreatsCollectionMembersAsFilled`；读取仍在原事务外位置。 |
| 44 | `proposal.TestApplyRollsBackWhenLinkWriteFails`、`TestApplyReportsConflictWhenPreempted`、`TestApplyWritesEveryFieldAndClosesProposal`；`ExecTx` 内写入/沿革/统计与 CAS 或字段删除。 |
| 45 | `proposal.TestApplyFillEmptyKeepsUnappliedFields`、`TestApplyReportsConflictWhenPreempted`；`api.TestSingleApplyReportsPartialOutcome`、`TestBulkApplyReportsPartialBucket`。 |
| 46 | `seriesmeta.TestProposalPreservesSnapshotValuesAndUnsupportedFields` 验证 language、locks 保留。 |
| 47 | `seriesmeta.TestApplyMetadataRecordsProvenance`、`proposal.TestApplyRecordsProvenanceMatchingSeriesContent`；跨来源验收检查 source/URL/confidence/review ID。 |
| 48 | `seriesmeta.TestApplyMetadataWritesBangumiLinkOnce`、`TestApplyMetadataSkipsSourceLinkForOtherProviders`；未统一为人工链接替换。 |
| 49 | 跨来源验收在提案前查询沿革为空；人工、批量、扫描入口没有沿革写入。 |
| 50 | `TestManualPreservesRawValuesAndAbsentCollections`、`TestScanTitleFallbackAndLockedScalarMatrix`、跨来源验收的 M/P 首字母；提案锁用例。 |
| 51 | 跨来源验收 FTS/标签 count/series_stats；扫描 deferred/cancel/dirty-series 与批量刷新容错高层测试；原有 trigger 未改。 |
| 52 | `api.TestGlobalTagMaintenanceInvalidatesMetadataFingerprint` 验证改名、合并、删除与最终内容。 |
| 53 | 跨来源验收稳定 ID/path/人工封面/旧与用户阅读记录/关系/自定义字段；`scanner.TestRenamedBookKeepsIdentityAndProgress` 与封面锁高层测试。 |
| 54 | `internal/metadata` 无本期业务写入变动、无数据库依赖；提案有效置信度仍由既有 `defaultConfidence` 提供，审查确认。 |
| 55 | 数据库仅提供 SQL 与类型，不导入 `seriesmeta`；依赖审查与编译验证无环。 |
| 56 | Store 旧批量业务入口删除；`bulkEditSeries` 持有整选区事务，事务绑定 batch 原语保持集合效率；规模/分块回滚测试。 |
| 57 | `proposal.Queries` 嵌入 `seriesmeta.ProposalQueries`；生产事务 adapter 编译期满足，无运行时强转。 |
| 58 | `seriesmeta_test` 经公开来源 interface、真实事务与最终数据库事实验证；新验收经正式高层入口。 |
| 59 | 所有新增行为测试使用真实迁移临时 SQLite；trigger 故障注入验证回滚，不为测试复制存储模型。 |
| 60 | 四来源高层测试均保留；跨来源验收补全接线证据，不能由 module 测试替代。 |
| 61 | manual/bulk/scan/proposal 四组 module 测试覆盖各自 nil、empty、锁、集合与错误差异，见 US 9–49。 |
| 62 | proposal writer 测试迁入 module，旧批量规则测试退出；HTTP/裁决/扫描事务保护保留，差异审查。 |
| 63 | 人工冲突竞争、提案 CAS、批量超变量限制/跨块回滚、扫描 dirty-series/取消/收尾测试均保留。 |
| 64 | 真实 HTTP 原有冲突、响应、部分应用和 bulk 请求测试；新验收多来源 stale 409；请求响应结构与路由未扩展。 |
| 65 | schema、迁移和生成模型无格式变动；临时真实迁移测试通过。没有执行真实用户数据库升级演练。 |
| 66 | 下节单列兼容观察和疑点；不以本期结构调整授权修复旁支问题。 |
| 67 | 无前端功能、导航或流程重构；本期只集中系列元数据规则，其他业务仍归原流程。 |
| 68 | 本 effort 的 spec、01–05 工单与实现证据分开；状态必须待最终门禁与审查完成后由集成人更新。 |

## 兼容观察与另行核实的疑点

- 已验证并保留：扫描标签/作者追加不检查集合锁；单个关联失败继续处理其他关联。`TestScanAssociationsAppendDespiteCollectionLocksAndErrors` 钉住这些事实，不将其表述为新增修复。
- 已验证并保留：有 ComicInfo 的已有系列更新，未锁空标量会清空，扫描 status 仍为空。`TestScanTitleFallbackAndLockedScalarMatrix` 及跨来源验收观察此结果。是否应调整需要另行讨论。
- 已验证并保留：扫描首建失败跳过书、已有元数据更新失败仍保留书、书事务失败回滚系列；批量提交后统计刷新失败不撤销成功内容。对应故障测试见 US 23、26、33。
- 尚未独立复现的既有并发线索：`proposal.Service.Apply` 的部分应用分支没有终态 CAS；在事务外读取之后被并发拒绝时，可能仍写元数据并删字段。完整应用分支 CAS 的回滚有现有测试；本期未强化部分应用语义。
- 原有入口间 dashboard cache、SSE、用户聚合和统计刷新安排不同。本期审查保留调用位置，未统一刷新策略；不声称覆盖所有生产缓存组合或进行了性能基准。
- 路径迁移及持久化保护已有实际实现。本期没有修改其业务规则；既有 `TestRelocationCommitPreservesSeededMetadataAndUserAssociations`、`TestOldScanSnapshotCannotRunAfterRelocation`、`TestRelocationProtectionSurvivesFreshStartupAndAllCleanupEntrypoints` 由来源 tip 完整后端测试覆盖，最终集成复验亦已通过。ADR 0009 的“尚未实施”是该文档中的历史状态文字，不能据此判断当前功能不存在。

## 检查记录与最终门禁

- 集成负责人在来源集成 tip `9bb275d` 上实际执行并报告通过：完整 `go test ./...`、`gofmt -l ./cmd ./internal` 空输出、完整 `golangci-lint run` 为 `0 issues.`、`go vet ./...`、server build；PowerShell `sqlc generate` exit 0 且数据库生成物无漂移，`go run ./cmd/tsgen` 无前端生成物漂移，`git diff --check` 干净。运行使用 Go 1.26.10。此记录明确只证明该来源 tip，不能代替本验收提交后的最终门禁。
- 本票新增高层验收的定向运行：`go test ./internal/api -run 'TestSeriesMetadataSources|TestGlobalTag|TestMetadataFingerprint' -count=1` 已通过。真实 CBZ 中只放 ComicInfo，避免为元数据验收引入无关封面后台生成；人工封面保持由最终事实验证。
- 本票两项人工事务验收：`go test ./internal/api -run 'TestManualCompeting|TestManualStats' -count=1` 已通过；这补充了原有顺序 stale-save 测试的真实竞争和统计失败覆盖。
- 本票来源聚焦运行 `go test ./internal/api ./internal/seriesmeta ./internal/proposal ./internal/scanner -run 'TestSeriesMetadataSources|TestGlobalTag|TestMetadataFingerprint|TestManual|TestSelection|TestBulk|TestEmptySelection|TestScan|TestProposal|TestApply|TestUpdateSeriesInfo' -count=1` 已通过。
- 本票聚焦 `golangci-lint run --allow-parallel-runners ./internal/api/... ./internal/seriesmeta/... ./internal/proposal/... ./internal/scanner/...` 已通过，输出 `0 issues.`；`gofmt -l ./cmd ./internal` 空输出、`git diff --check` 通过。lint 使用工作树独立缓存。
- 最终集成代码 `5ded495` 使用 Windows amd64、Go 1.26.10 复验：完整 `go test ./...` 通过（API 104.763s）；完整 `golangci-lint run --allow-parallel-runners` 输出 `0 issues.`；`gofmt -l ./cmd ./internal` 空输出；`go vet ./...`、服务端 `go build`、`scripts/check-doc-style.sh` 与 `git diff --check` 均通过。PowerShell `sqlc generate` exit 0，`go run ./cmd/tsgen` exit 0，数据库及前端生成物均无漂移。
- 双轴审查以 `3e1764b` 为固定基线，审查到 `5ded495`：Standards 0 项，Spec 0 项；没有待修复审查项。68 条故事逐项唯一覆盖，引用的具体测试符号已核对存在。01–05 与规格在证据齐备后关闭。
- 一次完整复验中，既有 `TestRelocationRepairLooseRootBooksAndVirtualSeries/true` 在测试准备的 `os.Rename` 收到 Windows `Access is denied`。未修改代码的完整复跑通过，该迁移测试独立连续 10 次通过；新增五个跨来源/指纹/人工事务验收也连续 10 次通过。没有将未知的目录占用原因归因于元数据重构，也未越界修改迁移行为。
- 本机初始 Go 1.27 与固定 lint 的导出格式不兼容，最终门禁统一使用 CI 的 Go 1.26.10。部分工作树首次执行新测试二进制曾被 Windows Application Control 拦截，未修改代码重试后通过；这些运行失败没有计作通过证据。
- 验证限制：未部署到真实用户数据库，未在 Linux/macOS 运行或执行 race 检查、独立性能基准。没有前端功能改动，未重跑前端门禁。生产运行、跨平台与性能结果不由本次本机测试推导。
