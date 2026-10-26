# 01: 单书读者视图与导航

**What to build:** 新增明确主体的书事实/阅读视图，并让单书详情、上下本与阅读器实际消费新入口，保留公开响应。

**Blocked by:** None (can start immediately)。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 15、16、17、18、19、20、23、24、25、26、27、28、29、30。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 藏书事实与读者状态可区分，非法主体不降为匿名，用户与匿名兼容路径隔离。
- [x] 详情、上下本与阅读器使用完整查询入口，公开字段兼容，未知完成及有效回退保持。
- [x] 新增形式旁保留仍有消费者的旧形式；新形式至少有一条生产链使用，后续迁移不破坏编译。
- [x] 真实 SQLite、HTTP 和阅读器消费者验证，不以新增无人调用类型作为完成。

## Comments

### 实现与验证

- `reading.Subject` 显式区分站点用户与匿名兼容主体，零值及非正用户标识被拒绝。`BookFacts` 与 `ReaderState` 组成绑定主体的 `BookView`；`BookView`、`SeriesBooks`、`AdjacentView` 提供完整查询。
- 单书详情、系列书列表、上下本 HTTP 消费新查询；`readerBookResponse` 保留公开 JSON 与统一路径脱敏。尚未迁移的协议和编排消费者继续使用原查询，收缩由 07 承接。
- 真实 SQLite 验证用户隔离、匿名兼容、排序导航、未知页数完成和有效回退；HTTP 验证非法主体、完整字段兼容及既有多主体/协议矩阵。阅读器消费者验证回退位置、无页数哨兵的完成态与下一本目标。
- TDD：先执行新模型与导航查询测试，确认缺少入口导致失败，再实现至通过；HTTP 非法主体回归先确认原入口返回 200/404，再迁移至明确 400。执行记录位于本工作树 `.tmp/ticket01-*.log`。
- Go 1.26.10：`go test ./internal/reading ./internal/api` 通过；`gofmt -l ./cmd ./internal` 无输出；独立缓存执行 `golangci-lint run --allow-parallel-runners` 为 `0 issues.`。
- 前端：`npm run test` 全量 94 文件、915 测试通过；`npm run lint` 与 `npm run build` 通过。额外阅读器定向回归 31 项通过。
- 本票验收已满足并通过集成验证。

### 集成验收

- 已合入 `codex/architecture-refinement`：01 为 `f06f77b`，10 为 `afffb1e`，08 为 `53e11c8`；合并无冲突。
- 01/10 合并后的 reading、proposal、api 完整包测试全部通过（API 117.205s）；08 合并后 seriesediting、seriesmeta 与人工保存 HTTP 定向回归全部通过。
- 三票合并后 `gofmt` 无输出，独立缓存的 `golangci-lint run --allow-parallel-runners` 返回 `0 issues.`；阅读器/审核消费者 13 项、编辑/上下文/会话消费者 18 项通过。原始集成输出在主工作树 `.tmp/merge-first-*.log`。
