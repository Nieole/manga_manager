# 01: 人工编辑接入系列元数据 module

**What to build:** 管理员通过现有人工编辑入口保存系列时，由独立的系列元数据 module 处理本期字段规则；请求、响应、内容版本保护和回滚结果保持原样。以这条完整流程建立后续来源可扩展的 interface 和事务能力交接，删除对应旧规则，不先建设没有真实调用方的通用框架。

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

**规格覆盖：** US 1–7、9–18、50、54–55、57–59、61–62、64–65、67–68。

- [x] 建立独立的系列元数据 module，采用人工来源明确的入口；metadata 继续负责外部获取与 AI，proposal 继续负责提案，module 不持有 Controller、不管理后台运行或配置。
- [x] 人工保存从现有 HTTP 入口实际接入 module，不是仅增加尚未使用的写入实现；HTTP 仍只解析、调用与翻译结果。
- [x] module 接受本流程所需的事务绑定窄查询能力，不自行开始、提交或回滚事务，不接收全局 Store，不提供任意原始 SQL 执行能力。
- [x] 只建立这条流程实际需要的事务能力；契约允许后续来源按真实需要扩展，不提前实现万能 patch、来源注册或公开变更集。
- [x] 标题、简介、出版社、状态、评分、语言、字段锁，以及标签、作者和链接的现有人工编辑语义保持；不扩大到封面、路径、身份、阅读记录、关系或自定义字段。
- [x] 集合 nil 不改、非 nil 替换、空集合清空；标量空值及非正评分保持现有 NULL 行为；保留当前空白过滤和实际落库值，不套用提案或扫描的规范化策略。
- [x] 人工仍可修改被锁字段及锁本身，不新增来源记录，标题与首字母联动保持。
- [x] 统一元数据内容指纹的既有算法与加载能力，避免调用方复制版本规则；保留指纹字段范围、集合归一、原始响应值及原有快照读取时机。
- [x] 省略版本继续兼容既有客户端，空版本仍拒绝，版本过期仍返回原有冲突分类与当前版本；版本读取比较和写入处于同一事务。
- [x] 字段、标签、作者或链接写入失败仍使人工保存整体回滚；系列统计刷新与内容写入保持原有同事务关系。
- [x] 通过 module 的正式 interface、真实临时 SQLite 与事务断言字段、关联、版本和回滚结果，不依赖私有 helper 或内部布局。
- [x] 保留人工入口的请求兼容、并发版本竞争、关联写入失败回滚和响应测试；可沿用 TestUpdateSeriesInfoConcurrentEditConflict 及系列元数据原子性测试。
- [x] 删除人工入口中被替代的逐字段规则、重复版本实现及失去价值的规则测试；保留仍验证 HTTP 与事务接线的高层测试。
- [x] 其余三条来源可暂时保留原实现，既有流程仍可验证通过；不要求提前迁移，不修改现有外部契约或数据库格式。
- [ ] 相关测试及仓库后端门禁通过；如调整生成查询，按仓库要求生成并验证产物。交付说明区分实际通过的检查与尚未执行的检查，不声称首期必须净减代码行数。

本票是首个真实来源的贯通，不是整个规格完成。保持所有既有行为；旁支疑点单独记录，不借迁移修复。

## Implementation evidence

- `internal/seriesmeta.SaveManual` owns manual scalar and association replacement with `ManualQueries`, composed with `VersionQueries`; `database.Queries` directly satisfies both. It does not own transactions, stats refresh, identities or Controller state.
- The real HTTP save calls the module inside `ExecTx`; HTTP validation, snapshot timing, conflict translation, transactional stats refresh and response remain in the adapter. GET context uses the shared `seriesmeta.Version`; transactional conflict checks use `LoadVersion`.
- Existing API field replacement and fingerprint implementation were removed. Existing HTTP conflict, race, response and atomicity tests remain because they verify integration.
- TDD tracer: new public-interface manual integration test initially failed because seriesmeta did not exist, then passed with implementation. Real SQLite tests cover raw persisted values, nil and explicit empty collections, locks, initials, conflicts, and association-trigger failures rolling back the original metadata version.
- Passed: `go test ./internal/seriesmeta ./internal/api -run 'TestManual|TestUpdateSeriesInfo|TestSeriesMetadata' -count=1`, `gofmt -l ./cmd ./internal` (empty), `git diff --check`.
- Full backend suite and final lint gate are reserved for integration validation; ticket stays ready-for-agent until all gate criteria pass.
