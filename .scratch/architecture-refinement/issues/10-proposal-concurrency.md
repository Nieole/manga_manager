# 10: 提案部分应用与终态竞争

**What to build:** 部分和完整应用都遵守事务内待裁决守卫，抢先拒绝后的写入返回冲突。

**Blocked by:** None (can start immediately)。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 37、38、45、46、47、48、49、50、51、52、53、54、55。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 事务内守卫并重载字段、系列与锁定，再选择本次可写内容。
- [x] 冲突或故障时元数据、字段及来源沿革同回滚；拒绝不撤销此前成功字段。
- [x] 两个部分应用不能复用已移除字段，部分后剩余待裁决、全部后终态保持。
- [x] 真实 SQLite 屏障构造并发，HTTP/审核页面区分冲突、无变化与故障。

## Comments


### 实施与验证

- `proposal.Service.Apply` 在写事务内重读提案待裁决状态、剩余字段、系列当前值及锁定，再计算本次写入；沿用 `seriesmeta.ApplyProposal` 的来源规则。事务外仅保留快速拒绝。
- 已执行 red → green：`TestPartialApplyConflictsWhenRejectCommitsBeforeItsTransaction` 在修复前返回 `partial`（应为 `conflict`）；修复后通过。真实 SQLite 屏障还验证交错部分应用不复用已消解字段、拒绝保留此前成功字段；存储触发器故障验证字段消解、元数据和来源沿革同回滚。
- `metadata_review_concurrency_test.go` 验证事务窗口中的拒绝、手工填值与存储故障分别进入 conflict、skipped、failed；`metadataReviewConcurrency.test.tsx` 验证实际审核页面显示三个独立结果并重读收件箱。
- Go 1.26.10：`go test ./internal/proposal -count=1`、相关 HTTP 回归、`go test -race ./internal/proposal -run 'TestPartialApplyConflicts|TestConcurrentPartial|TestApplySelects|TestPartialApplyFailure' -count=1` 均通过；`gofmt -l ./cmd ./internal` 输出为空，`golangci-lint run` 为 `0 issues.`。
- 前端 `npm run test -- --maxWorkers=2`：95 文件、914 测试通过；`npm run lint` 与 `npm run build` 通过。
- `go test ./...` 的 api、proposal、scanner 等实际通过；七个包被本机 Application Control 随机阻止启动，单独重跑后 application、koreader、metadata、seriesmeta、storageio 通过，booksort、database 仍被系统阻止。未将拦截计为通过；最终全量门禁由集成验收重跑。
- 未修改 SQL 或生成传输契约。状态暂留 ready-for-agent，待集成合并验证后关闭。
