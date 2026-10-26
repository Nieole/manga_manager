# 10: 智能书架与快照落地

**What to build:** 条件、计数、跨页成员和快照创建完整接入，快照保持人工所有权和名称约束。

**Blocked by:** 09: 人工合集的完整用例

**Status:** done

**验收基准：** [父规格](../spec.md)。本票遵守其中的跨功能约束和范围，不将历史实现偏差重新定义成业务规则。

**规格覆盖：** US 54、US 55、US 56、US 57、US 58、US 65、US 70。

- [x] 智能书架条件保存、成员查询、计数和快照落地接入藏书编排真实用例，页面与协议继续走其完整行为。
- [x] 查询与计数使用一致过滤含义，保留按用户计算读态的条件及跨页成员，不把动态推导结果永久固化。
- [x] 快照使用明确时点的结果创建人工所有合集，保留限制、截断提示、名称冲突和已有安全预览语义。
- [x] 快照内容与创建结果在必要事务内一致，之后智能条件变化或新书加入不回头覆盖固化合集。
- [x] 快照落地后可以人工维护，调用 09 的共同名称与所有权规则，不另造第二种创建约束。
- [x] 真实 SQLite 与功能测试覆盖高级条件、计数列表一致、多页、主体差异、快照、冲突及失败回滚；保留页面/协议回归。
- [x] 删除被替代的过滤与快照规则副本，保持查询集合式能力，完成相关后端和前端门禁。


## 实施与验证

`internal/organize.SmartCollections` 持有保存条件的归一化与验证、保存/更新/删除、动态成员与计数、预览和快照完整用例。页面及 OPDS/Mihon 接入同一成员查询；HTTP 只做输入、身份与响应转换。条件保存与查询构造、来源/名称约束不再在 API 内另有规则副本。

快照在同一个 SQLite 写事务内读取保存条件、计算成员及总数、检查共同合集名称、写入合集与成员。查询仍使用原集合式 SQL；Search/Count 能力放到 `database.Queries` 以供现有事务复用。快照明确保留 `smart_snapshot` 身份，落地后的人工增删改走 09 用例，智能条件后续变化不重写快照。

真实 SQLite 用例验证：高级标签/作者/状态/首字母/评分/进度/日期窗口条件、用户读态隔离、计数与多页一致、条件覆盖保存、校验和重命名冲突、快照截断与预览、快照主体差异、人工所有权、成员写入失败整批回滚。协议新增 OPDS 120 条跨页与 Mihon 100 条限制回归，防止输入各自分页语义在迁移后丢失。

已通过（Go 1.26.10，共享 GOCACHE，本树独立 lint cache）：

- `go test ./internal/organize ./internal/database`。
- 完整 `internal/api` 测试在合入 04 后通过；智能书架、快照、页面聚合与 OPDS/Mihon focused 回归再次通过。
- `golangci-lint run`：`0 issues.`；`gofmt -l ./cmd ./internal` 无输出；`git diff --check`。
- `go run ./cmd/tsgen` 无契约漂移。本票无 SQL 源和前端实现变更，无需 sqlc 再生成和额外前端变更门禁。

完整 `go test ./...` 尚未一次全绿：`internal/application/scanning` 与 `internal/diskwork` 测试可执行文件被 Windows Application Control 拦截；另一次 `internal/scanner` 的临时目录清理竞态（directory is not empty）单独重跑 `TestScanAccountsArchiveFailuresAndKeepsScanning` 已通过。本票不改动这些包，也未绕过系统策略。日志：`C:/Users/saber/.codex/tmp/architecture-deepening/10-go-test-resumed.log`；待集成工作区完成全仓补验后将最后验收项勾选并标记 done。

## 时钟修复后的集成补验

在含 11、20 及 04 时钟修复的集成提交 9b55d36 上运行完整 go test ./...，退出码 0；全量 lint 为 0 issues，gofmt 输出为空。reading、scanner 及 API 全部通过，相关回归缺口已补齐。本票适用验收项满足并关闭；漏洞扫描仍是 25 联合验收的独立待执行门禁。日志位于外部 post-clock-go-test.log。
