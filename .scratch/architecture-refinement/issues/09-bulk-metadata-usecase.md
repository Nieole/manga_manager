# 09: 批量元数据编辑的提交结果

**What to build:** 批量编辑复用已建立的元数据用例边界，保留批量语义并返回真实提交及刷新影响。

**Blocked by:** 08: 人工元数据保存与编辑草稿。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 35、38、41、42、178。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 沿用人工编辑已稳定的应用能力与适配形状，批量仍保留独立意图。
- [x] 标量输入、标签先加后删及必要统计按原语义执行，不加入通用策略开关。
- [x] 成功、未提交和部分/后续失败结果可区分，前端不丢已发生成果。
- [ ] SQLite 和批量界面回归验证，删除被替代的 API 事务配方。

## Comments

### 实施与证据（待主树最终门禁）

- `application/seriesediting.SelectionService.Edit` 拥有批量命令、窄事务、已提交结果和提交后统计/cache 交接；HTTP 只转换输入与响应。继续复用 `seriesmeta.EditSelection`，人工全量保存与批量增量编辑保持各自意图。
- 元数据跨 SQL 批次仍是一笔事务；标量保持原值及 nil/空值差异，标签先加后删，锁与来源沿革不改。统计沿用原有提交后尽力刷新边界，逐个收集失败而不将已提交元数据回滚或误报失败。
- 响应保留原有 `updated` 请求计数，新增 `committed`、`committed_series_ids` 与 `refresh_failed_series_ids`；已提交系列按真实存在的 ID 去重。空选择和事务失败不宣告已提交工作。
- 前端 `features/series-metadata/useBulkSeriesEdit` 根据明确提交事实宣告刷新影响；批量弹窗显示真实提交数，区分未提交失败、提交后统计告警以及后续页面处理失败，并保留错误响应中已明确提交的成果。
- TDD：完整命令入口缺失时先确认编译失败；新增 HTTP 提交事实断言先确认旧响应失败，再接入 owner 通过；批量界面先确认遗漏告警，再接入功能动作通过。
- 已实际通过：早期完整 owner 包（包含跨 SQL 批次失败回滚、标量/标签语义、统计部分失败继续刷新）；`go test ./internal/seriesmeta -count=1`；最终 HTTP 真实 SQLite 测试 `TestBulkMetadataTransactionAndRefreshFailure`（502 项选择、跨批次重复 ID、缺失 ID、事务失败、提交后告警，7.668s）。最近一轮 API 元数据聚合回归通过（7.812s）。
- 已实际通过：前端完整测试 96 文件、923 测试；批量弹窗 4 项、合并后会话消费者等 8 项；`npm run build`、`npm run lint`；最终合并后 `gofmt -l ./cmd ./internal` 无输出，`golangci-lint run --allow-parallel-runners` 返回 `0 issues.`。
- 精确限制：owner 首项测试后来扩大到 502 个输入，最终 standalone owner 二进制多次在启动时被 Windows Application Control 拦截；完整 `-race` owner 二进制同样被拦。最终扩大后的 standalone 用例尚未实际运行，不将这些拦截记作通过。HTTP 生产消费者的对应跨批次重复 ID 用例已有真实通过结果。未修改操作系统安全策略。
- 本票保留 `ready-for-agent` 和最后一项未勾选，交由集成负责人/merger 在主树取得最终 owner 用例真实运行结果并完成联合门禁后关闭。