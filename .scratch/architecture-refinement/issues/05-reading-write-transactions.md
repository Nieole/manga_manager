# 05: 阅读位置与标记的窄事务

**What to build:** 在线、离线及批量标记经完整用例维护位置和读态，事务只暴露所需一致性能力。

**Blocked by:** 01: 单书读者视图与导航。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 25、26、31、32、39、41、42、43、44。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 事务回调不能取得全量 sqlc 查询或任意 DB，实际适配不形成反向包循环。
- [x] 原时间、事实身份、同页新阅读、回退、清除跨重启及批内聚合全部保持。
- [x] 部分提交范围与缓存失效准确，统计告警不否定已保存位置。
- [x] 真实 SQLite、并发和 HTTP/离线矩阵验证，退出对应无生产用途的绕过入口。

## Comments

### 实现与验证

- `reading.Store` 只提供原子位置/批量标记决策、活动投影和完整时长存储动作；删除通用 `ExecTx(func(*database.Queries))` 能力。位置回调仅收到值状态并返回写入决定，不能取得全量 sqlc 或任意 DB。数据库适配不反向导入 reading，未引入通用 UoW 或新事件账本。
- `AccumulateReadingTimeForBook` 在数据库适配内保持原有“书存在 + 累计时长/日活动同事务”保证；服务保留原活跃性、上限、墙钟与日志语义。此调整仅用于收窄共享服务依赖，06 继续承接时长/书签的主体与完整接口迁移。
- 现有 `MutateReadingPosition(s)` 原子决策边界保留；在线、离线同步、批量书/系列标记及 KOReader 继续消费完整 reading 用例。原时间、事实身份、同页新阅读、回退、清除跨重启、批内聚合、部分已提交范围、缓存失效与独立统计告警规则未改变。
- 审计确认旧 `SetUserBookProgress` 与 `SqlStore.UpdateBookProgress` 业务配方无生产消费者，均已移除，后者独占的私有统计辅助同时删除。原子适配仍需使用的 sqlc 技术查询保留。必要的前迁移/未带收据持久态种子改为测试 fixture：数据库包使用 `_test.go`，其他包共享仅由测试导入的 `internal/testfixture`；不以新完整用例伪造历史活动或收据。
- TDD 先以真实 SQLite 的受限写能力端口确认旧 `reading.New` 要求 `ExecTx` 而编译失败，再收窄至通过。该用例经公开服务验证前进、回退、清除、已识别重放、缓存失效与时长。既有真实 SQLite 故障、竞争、跨重启、HTTP/离线矩阵继续验证保存/统计及部分提交边界。
- Go 1.26.10：两轮 `go test ./...` 分次覆盖所有包的实际通过；第一轮仅 proposal 被 Windows Application Control 阻止执行，intelligence 实际通过；第二轮 proposal 实际通过，仅 intelligence 被拦，其定向重试仍被拦。未把拦截记为通过，也未宣称单次全仓零退出。原始记录分别为 `.tmp/ticket05-go-tests.log`、`ticket05-go-tests-final.log`、`ticket05-intelligence-retry.log`。
- `go test -race ./internal/reading` 实际通过；独立缓存 `golangci-lint run --allow-parallel-runners` 最终 `0 issues.`；`gofmt -l ./cmd ./internal` 与 `git diff --check` 无输出。前端在线进度、离线队列、切书迟到报告及阅读时长消费者 4 文件、42 测试通过。其余原始记录在本工作树 `.tmp/ticket05-*.log`。
- 未改变前端生产源码、SQL 查询/DDL 或生成响应契约；不重复无关构建及生成。已在本分支合入最新集成 c2ca015。验收条件已满足并通过集成分支复核。
### 集成验收

- 05 由 `488e6ec`、16 由 `6b5e8f7` 合入 `codex/architecture-refinement`，两次合并均无冲突。
- Go 1.26.10：完整 `bookcontent`/`parser` 的 `-race` 测试实际通过（5.132s/5.687s）；reading/database 的原子写入、位置、时长、标记及同步回归通过；API 页图、来源缓存、封面、进度/离线与迁移兼容回归通过（48.889s），scanner 封面消费者通过（4.641s）。
- 迁移/旧阅读态 fixture 的真实消费另行执行：database、KOReader、scanner、application/relocating、application/scanning 五包的对应回归实际通过；未将初轮筛选中无匹配测试的包计作行为通过。
- 前端阅读器、进度/离线、时长、页缓存及图像选项消费者 7 文件、73 测试通过；gofmt 无输出，独立缓存 lint 为 `0 issues.`。未更改 SQL/TS 生成源，未重复无关生成或全仓门禁。原始输出保存在主工作树 `.tmp/merge-wave5-*.log`。
