# 02: 提案应用接入共同规则

**What to build:** 管理员从现有单条、批量或系列提案入口应用提案时，由系列元数据 module 完成已裁决字段的写入，提案 module 保留裁决与生命周期。用户得到的应用、部分应用、跳过和冲突结果不变，内容与提案状态仍按原有事务关系共同生效。

**Blocked by:** 01 — 人工编辑接入系列元数据 module。

**Status:** ready-for-agent

**规格覆盖：** US 38–50、54、57–64、66–67。

- [x] 现有提案应用流程实际调用来源明确的元数据入口，输入为提案已经选定的字段、既有系列快照和来源信息；不要求调用方组合写入策略标志。
- [x] 入队、签名去重、待裁决判定、fill_empty 筛选、部分应用、拒绝和关单仍由提案 module 管理，不迁入共同写入 implementation。
- [x] 扩展已有事务能力契约，生产查询对象在编译期满足；提案事务契约组合所需能力，不用运行时类型强转，不把全局 Store 暴露给新 module。
- [x] 元数据写入不另开事务；内容、来源沿革、已应用字段删除或关单 CAS，以及原有系列统计刷新保持既有事务关系。
- [x] 标量保留现有非空覆盖与状态归一规则；语言和字段锁保留原值，不因共用字段能力扩大提案支持范围。
- [x] 只有参与本次写入的标签和作者整体替换；未参与字段保持不变；明确空集合与没有参与写入不是同一含义。
- [x] 保留提案流程的字段锁过滤和写入处现有保护，以及锁定跳过、无变化、部分应用等原有结果分类。
- [x] 保留 fill_empty 当前值读取、提案加载和写入快照的原有时机；不重新读取最新行来修正既有并发行为。
- [x] 保留作者角色、集合判重、来源值格式与排序、空集合沿革等现有差异，不复用人工规则改变提案规范化结果。
- [x] source、URL、confidence、提案关联及字段值保持原有沿革；来源默认置信度只有一份现有判据，不复制默认表或增加新的来源判断。
- [x] Bangumi 链接保留既有匹配、缺失判定和追加规则，不将其转成人工链接整体替换，也不为其他来源新增链接行为。
- [x] 通过元数据 interface 和真实临时 SQLite 验证标量、集合替换、锁、链接及沿革；保留提案高层测试验证裁决、CAS 抢先、回滚和结果映射。
- [x] 延续 TestApplyRemovesTagsAndAuthorsMissingFromProposal、TestApplyKeepsLockedCollectionsIntact 及既有部分应用、来源和并发测试的保护，不把真实差异改写成统一行为。
- [x] 已有提案查询归属守卫继续有效；入站 adapter 不绕过提案 module 直接处理提案表，共同元数据 module 也不取得提案关单能力。
- [x] 删除被替代的提案字段写入实现及重复规则测试，保留仍有裁决或事务验证价值的测试；不保留第二套可独立演进的 writer。
- [x] 不新增统一缓存失效、用户聚合刷新、来源记录或公开契约变化；既有刷新与错误传播保持。
- [ ] 相关测试及仓库后端门禁通过；交付记录说明实际验证结果。发现提案部分应用并发等疑点时单独记录，不在本票修复。

本票不依赖批量编辑或扫描接入。扩展共同契约时保持人工流程兼容，不以同一文件可能发生编辑冲突增加业务阻塞关系。


## Implementation evidence

- `seriesmeta.ApplyProposal` owns the actual adjudicated-field writer, source formatting, collection replacement, secondary lock checks, Bangumi link append and existing transactional stats refresh. `ProposalQueries` excludes proposal lifecycle capabilities and is composed into `proposal.Queries`; `database.Queries` satisfies it at compile time.
- `proposal.Service.Apply` retains field selection, fill_empty reads, the existing pre-transaction snapshot, partial field deletion, terminal CAS and outcome mapping. Its effective confidence uses the single existing `defaultConfidence` function; seriesmeta does not define another fallback table.
- Removed the original proposal writer. Writer compatibility tests now exercise the public seriesmeta interface in real SQLite transactions; the proposal high-level collection replacement, locked/partial application, provenance, CAS-preemption and rollback tests remain.
- TDD tracer: `TestProposalDistinguishesAbsentAndEmptyCollections` first failed to compile against missing `ApplyProposal` / `ProposalInput`, then passed through the new source interface. Module tests also cover scalar preservation/status normalization, language/locks, initials, deduplication/author roles, source formatting/review linkage and Bangumi-only links.
- Passed: `go test ./internal/seriesmeta ./internal/proposal -count=1`; `go test ./internal/api -run 'Test.*(MetadataReview|ScrapedMetadata|ApplyReports|ApplyFillEmpty|SingleApply|BulkApply|APIDoesNotQueryProposalTables)' -count=1`; `gofmt -l ./cmd ./internal` (empty); `git diff --check`; focused `golangci-lint run --allow-parallel-runners ./internal/seriesmeta/... ./internal/proposal/...` (`0 issues.`).
- Full repository backend test/lint gates remain for final integration validation, so the final gate checkbox and ticket closure remain pending. No SQL/schema or public HTTP contract changes; no unrelated concurrency behavior changed.
