# 03: 批量编辑接入共同规则

**What to build:** 管理员通过现有批量编辑入口修改系列选区时，由共同 module 处理批量增量规则，调用流程拥有整组选区事务和提交后的既有刷新。迁出数据库中的高层业务编排，同时保留大选区效率、全部成功或全部回滚以及现有成功和失败结果。

**Blocked by:** 01 — 人工编辑接入系列元数据 module。

**Status:** ready-for-agent

**规格覆盖：** US 19–26、51、55–56、58–65、66–67。

- [x] 现有批量 HTTP 入口实际调用来源明确的选区增量 interface；支持范围仍仅为状态、出版社、添加标签和移除标签。
- [x] 高层批量业务编排迁到调用流程，数据库不反向依赖系列元数据 module；新 module 只通过事务绑定查询能力使用具体存储原语。
- [x] 批量元数据规则、操作顺序和输入处理集中在共同 module；数据库只保留具体批量 SQL 原语，不保留第二份业务规则。
- [x] 状态和出版社指针 nil 不改，非 nil 保留原有值语义；标签空白处理、缺失系列容忍、重复 ID 与空选区结果保持原样。
- [x] 保留先改标量、添加标签、再移除标签的现有顺序，特别验证同一标签同时添加和移除的最终结果。
- [x] 保留当前不检查字段锁、不写入来源沿革的差异，不因复用人工或提案 implementation 扩大锁或来源规则。
- [x] 一次选区只有一个事务，SQL 参数分块不形成独立提交；跨块失败仍回滚整个选区。
- [x] 保留现有集合式 SQL、参数分块及缺失 ID 处理，不改成逐系列加载再整行写回，不更新本次未涉及的字段。
- [x] 主事务提交后仍按原选区进行既有统计刷新；刷新失败保留当前告警和成功响应语义，不撤销已提交内容，不新增自动重试或统一刷新机制。
- [x] 标签关联触发器、派生统计和现有 dashboard cache 失效保持原有结果与时机；不新增用户阅读聚合重算。
- [x] 元数据 interface 测试用真实临时 SQLite 覆盖 nil/空值、标签先加后删、缺失与重复 ID、大选区、跨块回滚及未涉及字段保留。
- [x] 保留 HTTP 和调用流程测试验证整组选区原子性、响应分类及提交后刷新失败；延续 TestBulkOperationsExceedVariableLimit 的大选区保护，不用逐行循环绕过原问题。
- [x] 删除旧 BulkEditSeries 高层入口、失去职责的高层输入类型和重复规则测试；保留数据库低层批量原语及仍有独立价值的分块测试。
- [ ] 不改变数据库格式和外部请求响应。必要新增查询符合仓库生成约束，相关测试、生成产物检查和后端门禁通过。
- [x] 交付说明能指出批量编排的拥有者、实际验证的事务和规模行为，以及旧入口已退出；不声称做了未执行的性能测量。

本票不依赖提案或扫描迁移。必须保留现有刷新失败语义；发现其他行为问题另行记录，不扩大本票范围。

## Implementation evidence

- `seriesmeta.EditSelection` owns bulk scalar/tag rules through `SelectionQueries`; `Controller.bulkEditSeries` owns the entire selection transaction and the existing best-effort per-selected-ID refresh after commit. `database.Queries` provides concrete set SQL primitives using the existing parameter chunker; `Store.BulkEditSeries` and `BulkSeriesEdit` are removed.
- Real SQLite module tests cover nil and raw empty scalar values, trimmed/blank tags, add-before-remove, missing/repeated IDs, unchanged title/summary/language/locks, no provenance, empty selection, 40,000-ID operations and a trigger failure in the last SQL chunk rolling back both earlier associations and all scalars/tag creation. The former database bulk-rule tests and 40,000-ID case move to this public module seam; independent SQL chunker/progress tests remain.
- HTTP tests cover malformed/empty requests, unchanged updated counts including repeated/missing IDs, write failure rollback, successful derived-cache refresh, and refresh failure retaining committed metadata and the success response.
- Focused module/database/API tests passed. `gofmt -l ./cmd ./internal` returned no output; scoped `golangci-lint run --allow-parallel-runners ./internal/seriesmeta/... ./internal/database/... ./internal/api/...` returned `0 issues.`. One newly compiled API test execution was transiently blocked by Windows Application Control; the unchanged retry passed.
- Integration-wide backend gates and final review remain pending on the integration branch. No schema, sqlc source or external request/response contract changed; no performance measurement was made.
