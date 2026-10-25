# 23: 刮削与 AI 分组后台发起

**What to build:** 来源、语种、作用域及重启入参由应用发起归属，保留计费相关不可自动续跑规则与既有提案分工。

**Blocked by:** 17: 应用运行入口与历史保留清理

**Status:** done

**验收基准：** [父规格](../spec.md)。本票遵守其中的跨功能约束和范围，不将历史实现偏差重新定义成业务规则。

**规格覆盖：** US 7、US 67、US 90、US 91、US 93、US 94、US 95、US 106。

- [x] 全库及单资料库刮削、AI 分组的实际后台发起与重启参数解释迁入应用编排，HTTP 不持有工作循环。
- [x] 保留来源、语种、作用域、发起方、任务变体及原始重启入参；重试不能悄悄更换来源或丢失语种。
- [x] 涉及外部来源或 LLM 计费的任务保持不可自动续跑的既有取舍，手动重试仍按现有能力执行。
- [x] metadata 继续负责外部获取与 AI，proposal 与 seriesmeta 继续持有既有规则；AI 分组的建议仍不同于元数据提案。
- [x] 本票迁移的是后台生成与发起，不依赖 AI 分组接受票 12；保持建议生成与人工裁决的职责分离。
- [x] 保留访问覆盖、运行槽位、取消暂停、进度、来源限流和上下文日志，不引入额外请求侧准入。
- [x] 通过可控外部来源和应用用例验证参数重放、禁止自动续跑、取消、生成结果与相关权限，保留页面和协议契约回归。
- [x] 删除被替代的发起与重启参数配方，完成相关后端、前端及生成检查。


## 实施记录（2026-10-25）

- `application/intelligence.Service` 接管全库/单库刮削及 AI 分组完整后台发起、原生运行声明、重启入参解释、运行进度与终态映射、语义刷新。HTTP 仅解析请求、调用完整用例并响应，不保留工作循环或重启参数配方；旧任务观测 fixture 的桥只在 `_test.go`。
- 刮削统一使用已有 `application.Runs`/`task.Engine`，系统全库覆盖 `AllLibraries`、单库及分组覆盖原资料库；保留来源键/显示标签、全库与单库 variant、作用域、触发类型、原整帧进度、取消暂停、500ms 来源限流与带 ctx 日志。删除最后失去生产引用的 `libraryScopeName` 旧 helper。
- 刮削来源构造由 `metadata.NewConfiguredProvider` 统一现有别名与运行时配置规则，HTTP 单次查询与后台用例共享同一实现；metadata 继续外部获取与 LLM 能力，proposal/seriesmeta 仍持元数据提案及接受规则。
- organize 的 `AIGrouping.Generate` 拥有未归组合集候选选择、原 50 个候选上限、LLM 建议生成协作与审阅单事务落盘；`QueueGenerated` 保留候选约束、去重、排序和原始 payload。应用观察者只将语义阶段映射到运行进度。生成不调用人工接受/拒绝，不创建正式合集，也不写元数据提案。
- `intelligence.Restarts` 是这三种重启能力的单一装配声明，组合根据此登记原引擎 dispatch，全部不可自动续跑。`Restart` 解释持久化来源/语种，手动重试保留原值；旧运行没有语种时才使用本次请求语种。刮削现在同时保存 locale，避免英文发起后以中文重试悄悄改变来源请求语种。AI 分组沿用原配置驱动的 LLM 构造，保留原 locale；未新增计费自动执行或无人确认的恢复。

### TDD 与行为证据

- 按已确认的完整应用 seam，用真实临时 SQLite 和可控外部来源先红后绿验证：单库刮削形成真实元数据提案；持久化来源键与 en-US；用 zh-CN 请求手动重试后，工厂仍收到原来源、外部源仍收到 en-US，且建出新的独立运行。
- AI 分组同样先红后绿：真实候选生成 pending 分组审阅单，重复 ID/幻觉 ID 不进入有效候选；正式合集和元数据提案仍为空；手动重试保留 en-US。
- 实际运行 ctx 的 run ID/任务键经可控来源验证，两个重试运行分别携各自 ID。全库覆盖无法越过资料库迁移独占。三种恢复发起均在调用外部来源前被拒，未建出运行。
- 真实 SQLite 排队取消验证：单库刮削与分组等同一个原运行槽位；排队期间阻断迁移独占；取消两条后不调用任何外部来源、不执行后台体，访问覆盖释放。原 API 回归继续验证来源错误/未找到、全库及单库终态、身份变体、原始来源重放、暂停期间停止新来源请求、建议接受协议与权限。
- 测试使用 fake metadata/grouping 来源及既有本地 fixture，没有真实付费调用或联网刮削。

### 门禁

- 固定自身全量证据：基于 c7331ff 与本票改动，`GOTOOLCHAIN=go1.26.10`、共享主树 `.gocache`，`go test ./... -json -timeout 180s` 全包通过，API 88.117s。原始日志 `C:/Users/saber/.codex/tmp/architecture-deepening/all-23-tests.jsonl`。
- 提交前 merge 集成 tip `021676b`（设备阅读同步），无冲突。按受影响范围复验 application/intelligence、organize、metadata、proposal、reading、koreader、database 全包通过；API 刮削/分组/提案/权限/重试/暂停恢复/中断/KOReader 组通过（9.918s）。日志为树外 `intelligence-23-merged-packages.jsonl`、`intelligence-23-merged-api.jsonl`。最终统一全量门禁归票 25，不重复无变化整轮。
- 独立树外 lint 缓存 `C:/Users/saber/.codex/tmp/architecture-deepening/lint-23`，`golangci-lint run` 最终 `0 issues.`；`gofmt -l ./cmd ./internal` 空输出，`git diff --check` 通过。`go run ./cmd/tsgen` 24 类型且无契约漂移。本票未改 SQL；集成带入的 schema/生成物保持原提交内容。
- 前端 `npm run test -- src/hooks/useScrapeProviders.test.ts src/pages/library/hooks/useSeriesScraping.test.ts src/pages/metadataReviewBulk.test.ts src/pages/aiGroupingReviewInbox.test.tsx`，4 文件 17 用例通过；`npm run build`、`npm run lint` 通过。没有前端源码变更。
- 普通用户带合法 CSRF 请求全库刮削、单库刮削及 AI 分组均为 403。未修改共享 CHANGELOG/spec/implementation，未改系统执行策略，所有验收项满足。
