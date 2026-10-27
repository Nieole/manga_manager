# 04: OPDS 与 Mihon 的读者视图

**What to build:** 协议中的书与系列读取迁移至明确主体的查询，设备客户端继续看到原有兼容响应。

**Blocked by:** 02: 系列列表的主体筛选与分页。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 18、19、23、28、30、133。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 协议认证得到的用户明确传入功能查询，不继承另一主体的兼容读态。
- [x] 书、系列、导航及必要分页消费已迁查询，无独立读态规则。
- [x] 协议名称、字段、寻址及账号边界保持兼容。
- [x] 既有协议回归加主体矩阵验证，旧查询迁移保持可构建。

## Comments

### 实现与验证

- OPDS 与 Mihon 书列表使用明确 `Subject` 的 `SeriesBooks`，响应适配直接消费 `BookFacts`、`ReaderState`；Mihon 退出接受数据库 Book 的读态映射入口。系列排序、OPDS 分页、书寻址与获取链接保持原协议。
- `reading.RecentBooks` 返回绑定主体的批量最近阅读视图，两个协议的继续阅读均消费该入口；匿名兼容与用户读取明确分流，非法主体在查询前被拒绝。仍被 HTTP 最近阅读消费者使用的旧入口留给 07 收缩。
- Mihon 系列分页/筛选回退消费 `CatalogueSearch`，主体进入 SQL 查询；跨库全文搜索及只含藏书事实的协议列表/详情保留现有高效事实投影，不引入逐系列或逐书装载。03 已迁移的合集、智能书架与清单协议入口保持兼容。
- HTTP 红绿测试先确认非法用户在书列表、继续阅读及 Mihon 两种搜索路径上返回 200，再迁移至明确 400。真实 SQLite 主体矩阵验证两个用户与匿名兼容的不同页码、OPDS 页边界/身份/PSE 位置/获取链接、Mihon 继续阅读目标，以及未知页数完成不输出页数哨兵。
- 阅读查询测试直接验证最近阅读视图归属、用户隔离、匿名原进度和非法主体；原 OPDS/Mihon 名称、字段、协议开关、Basic Auth、强制改密、资源作用域及已知页数/未知页数主体矩阵回归通过。
- Go 1.26.10：`go test ./internal/reading` 全量通过；`go test ./internal/reading ./internal/api -run 'TestRecentBooks|TestOPDS|TestMihon|TestProtocol|TestReadingProjection|TestNavigation' -count=1` 通过；`golangci-lint run --allow-parallel-runners` 为 `0 issues.`；`gofmt -l ./cmd ./internal` 与 `git diff --check` 无输出。
- 原始记录在本工作树 `.tmp/ticket04-*.log`。本票没有前端生产、SQL、生成响应结构或账号/匹配算法变更，因此不重跑无关前端构建与 SQL/TS 生成；未使用真实设备客户端作实机验证。
- 验收条件已满足并通过集成分支复核。
### 集成验收

- 已由合并提交 `b1b82cc` 纳入 `codex/architecture-refinement`，无冲突。
- Go 1.26.10 的最近阅读、OPDS/Mihon、协议鉴权、主体投影、导航及编排兼容回归在 reading/API 两包实际通过（11.216s、8.438s）。
- `gofmt -l ./cmd ./internal` 无输出，独立缓存 `golangci-lint run --allow-parallel-runners` 为 `0 issues.`。原始输出保留在主工作树 `.tmp/merge-wave4-04-*.log`；未运行真实设备客户端。
