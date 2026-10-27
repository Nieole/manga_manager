# 08: 人工元数据保存与编辑草稿

**What to build:** 人工系列编辑从表单到保存结果成为完整用例，业务输入不再使用数据库可空字段形状。

**Blocked by:** None (can start immediately)。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 33、34、38、39、40、41、173、178。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 表单草稿表达未提供、清空和具体值，读取 DTO 与写入意图分开。
- [x] 完整保存 owner 组合事务、seriesmeta 来源规则、统计与必要刷新，HTTP 不拼配方。
- [x] 保持锁定、版本/身份及来源语义，窄事务适配不形成循环依赖。
- [x] 真实 SQLite 与编辑页面验证成功、清空、失败回滚和已提交后续告警。

## Comments

### 实施证据

- 完整保存入口：`application/seriesediting.Service.Save`，`Store.Edit` 仅向用例暴露人工元数据事务能力；SQLite 适配在同包内对接现有事务，不使 database 反向依赖应用层。HTTP 只转换输入与映射结果。
- 来源规则继续复用 `seriesmeta.SaveManual`；版本校验、当前系列读取、来源写入、统计刷新和响应读取处于同一事务，失败不返回已提交视图。标量替换、集合缺席保留/显式空集合清空、锁与身份保持。
- `features/series-metadata` 拥有详情读取 DTO、普通字段编辑草稿和保存动作；页面与编辑弹窗实际消费。保存成功后关闭编辑器，后续刷新失败单独提示已保存，并保留上下文错误与跨页面刷新影响；后台刷新不替换脏草稿版本。
- TDD：先添加完整保存 SQLite 用例，确认缺失生产入口时编译失败，再实现并通过；SQLite 统计故障触发器验证整批回滚。既有 HTTP 冲突、链接回滚和来源差异回归继续通过。
- 本轮通过（Go 1.26.10）：`go test ./internal/application/seriesediting ./internal/seriesmeta ./internal/api -run 'TestManual|TestUpdateSeriesInfo|TestSeriesMetadata' -count=1`；人工编辑与来源规则完整包测试也通过。Windows Application Control 曾阻拦一次新测试二进制，重跑成功；阻拦记录不计为通过。
- 本轮通过：`gofmt -l ./cmd ./internal` 无输出；独立 lint 缓存下 `golangci-lint run` 返回 `0 issues.`；前端 `npm run build`、`npm run lint` 和 `npm run test -- --maxWorkers=2`（94 文件、916 测试）；最终编辑/上下文/会话消费者回归 18 项通过。
- 本票已完成集成验证；整个规格和后续查询/操作影响分离票仍按各自验收推进。

### 集成验收

- 已合入 `codex/architecture-refinement`：01 为 `f06f77b`，10 为 `afffb1e`，08 为 `53e11c8`；合并无冲突。
- 01/10 合并后的 reading、proposal、api 完整包测试全部通过（API 117.205s）；08 合并后 seriesediting、seriesmeta 与人工保存 HTTP 定向回归全部通过。
- 三票合并后 `gofmt` 无输出，独立缓存的 `golangci-lint run --allow-parallel-runners` 返回 `0 issues.`；阅读器/审核消费者 13 项、编辑/上下文/会话消费者 18 项通过。原始集成输出在主工作树 `.tmp/merge-first-*.log`。
