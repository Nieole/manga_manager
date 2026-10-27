# 13: 关系编辑与作品群持久修复

**What to build:** 关系保存后作品群可异步更新；失败和重启均可继续修复，旧结果可读且状态明确。

**Blocked by:** None (can start immediately)。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 59、60、61、62、65、66、67、68、69、70、71。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 关系变更与全局更新依据同事务提交，重建原子更新稳定成员和完成依据。
- [x] 退避重试、启动恢复和立即重试经现有应用运行时协调，失败不丢责任。
- [x] 新变更不被旧成功清掉，旧失败不覆盖新成功；升级保留原结果与身份。
- [x] 查询和页面区分陈旧、失败及首次无成功结果，SQLite 重开及并发回归通过。

## Comments


### 实施与验证

- 全局 singleton `franchise_rebuild_state` 保存期望/已应用版本、是否已有成功结果、最近尝试与持久退避；关系创建/修改/删除在原事务内调用 `MarkFranchiseRebuildPending`，重建在一致图事务中提交稳定身份、成员差集和完成版本。
- 失败记录按尝试版本与已应用版本守卫，较旧失败不能覆盖较新成功；新关系提交后的期望版本不会被已完成重建清除。迁移以已有作品群作为可读陈旧结果保留，首次核对前不宣称追平。
- 应用运行时启动修复循环，消费持久退避，停机取消并等待；原后台合并入口仍可即时尝试，失败不会丢失责任。既有管理员重建端点提供立即重试，新增只读 `/api/franchise-state`；合集页区分首次不可用、更新中、旧结果及失败，保留人工合集与智能书架语义。
- TDD 首个持久状态用例在新增能力前失败，随后 SQLite 回归验证跨资料库图、无通知仍恢复、数据库关闭重开、标记故障的关系回滚、重建故障保留旧成员、持久退避、旧失败晚到、新关系在旧重建返回前提交、升级保留原合集身份。HTTP 与实际前端状态组件验证状态与立即重试；运行时回归验证启动修复及暂时故障后自动成功。
- Go 1.26.10：`go test ./internal/database ./internal/organize ./internal/application/runtime -count=1` 通过；相关 api 工单回归通过；`go test -race ./internal/organize ./internal/application/runtime -run 'TestFranchise|TestRelationWrite|TestOldFranchise|TestNewRelation|TestRuntimeResumes' -count=1` 通过。
- `gofmt -l ./cmd ./internal` 输出为空，`golangci-lint run --allow-parallel-runners` 为 `0 issues.`。前端 `npm run test -- --maxWorkers=2`：95 文件、915 测试通过；`npm run lint` 与 `npm run build` 通过。
- `sqlc generate` 在 PowerShell 退出 0，SQL 查询源保持 ASCII；新状态未改动既有生成前端传输结构。当前证据来自本票独立工作树，合并最新集成分支后的复验另行记录。
- 本票不提前承诺 14 的级联删除/系列事实标记覆盖；状态暂留 ready-for-agent，待集成验证后关闭。
