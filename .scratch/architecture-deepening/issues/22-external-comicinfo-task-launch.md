# 22: 外部库与 ComicInfo 磁盘用例发起

**What to build:** 扫描传输和回写的后台发起脱离请求处理，保留会话、访问覆盖、磁盘许可、取消及不可自动续跑约束。

**Blocked by:** 17: 应用运行入口与历史保留清理

**Status:** done

**验收基准：** [父规格](../spec.md)。本票遵守其中的跨功能约束和范围，不将历史实现偏差重新定义成业务规则。

**规格覆盖：** US 7、US 90、US 93、US 94、US 95、US 97、US 106。

- [x] 外部库扫描、传输和 ComicInfo 回写的完整后台发起接入应用运行入口，真实 HTTP 输入继续得到兼容会话与运行结果。
- [x] 外部库模块继续拥有传输会话与计划，回写继续使用已有归档及元数据能力；应用编排只承担完整协作，不吞并全部磁盘规则。
- [x] 保留资料库访问覆盖、外部路径与资料库关系判断、旧路径闭包约束，与迁移独占仍共享同一协调。
- [x] 保留磁盘许可、运行槽位、暂停取消、日志与进度归属，任务体不依赖请求对象或全局当前用户。
- [x] 改变磁盘内容的工作保持原有重试/续跑能力，不新增自动续跑或凭空重建不持久的会话计划。
- [x] 结束、失败、取消和传输后扫描刷新仍有唯一所有者，不能由 HTTP 在发起后自行拼接多个可能丢失的步骤。
- [x] 通过应用用例及现有外部库、归档 fixture 验证扫描传输回写、排队、取消、迁移冲突与会话失效，保留相关页面回归。
- [x] 删除被替代的启动配方，完成后端、相关前端及生成物门禁；过渡时与未迁移迁移编排共用访问事实源。


## 实施记录（2026-10-25）

- `application/diskjobs.Service` 持有完整会话创建与扫描发起、单次传输规划与发起、系列 ComicInfo 聚合与发起、运行体及发布收尾。统一经 `application.Runs` 和原 `task.Engine` 准入，任务体只捕获已接受的事实，不捕获 HTTP 请求或用户全局状态。
- HTTP 仅解析输入、调用完整用例并映射兼容响应。原后台配方已删除；旧任务观测 fixture 的转发仅留在 `_test.go`，生产不保留无人调用的启动壳。
- external 继续持有内存会话、TTL、覆盖率、计划与逐项标记，并拥有原子复制规则；复制的真实文件与 Unix FIFO 回归随规则迁至 external。seriesmeta 持有 ComicInfo 已知事实投影，parser 保留安全合并与归档原子写入。系列回写继续逐本取得原磁盘许可，不改变跳过、失败计数和不可暂停的声明。
- `application.CoverPaths` 从现有资料库事实计算保留路径覆盖，并在准入重验根目录；资料库迁移与磁盘用例共用 `application.PathsIntersect` 及同一个 Engine。外部目标覆盖其他受管库时，其独占也会阻断扫描/传输；排队运行继续占用访问，取消后释放。ComicInfo 在准入重验系列及所保留书路径，拒绝迁移后的旧闭包。失效会话在传输实际开跑时再次验证，复制前失败。
- 外部扫描通过原任务上下文在目录遍历、匹配循环响应暂停和取消；运行声明、作用域、文案、整帧进度与日志归属保持兼容。没有第二调度器、访问守卫或自动恢复计划。
- **传输方向与实际后续动作：** 当前 `external.Manager.PrepareTransfer` 的 SourcePath 是受管资料库文件、Destination 是外部目标，前端 `useExternalLibrary` 发起向外导出。原流程为 Copy → MarkTransferred → refresh，没有源资料库重扫。本票把这条实际链收进应用，未新增无依据的源库扫描；目标受管库继续由其现有监听策略处理。扫描监听入口仍为 `application/scanning`，迁移后的监听切换仍由 `application/relocating` 负责。
- **重试与续跑：** 核实 `buildTaskDispatch` 实际未登记外部扫描、传输或 ComicInfo 回写；本票不凭旧注释增加能力，也不从历史运行重建不可持久的会话/计划。保留其他已有重试项，修正注册表注释使其反映当前能力。

### TDD 与验证

- 规格已确认的完整应用 seam：真实临时 SQLite、真实源/目标文件和 CBZ。空扫描、导出复制与会话匹配计数、数据库元数据写回实际归档均先红后绿。
- 再现并修复：外部目标另一资料库正独占时仍可复制；排队后会话失效仍写文件；已暂停的空扫描直接完成；旧 ComicInfo 书路径快照仍可准入。相应用例均先记录失败再通过。
- 真实队列取消确认：等槽位的导出不会执行复制；保留路径阻止迁移独占；取消后释放访问。原 API 继续覆盖全成功、部分失败、全已存在、取消、二次排队、帧节流、身份与作用域、ComicInfo 安全合并/不支持格式、迁移及保护恢复。
- 外部/回写/迁移/独占 API 组通过（45.446s）；最终普通用户携合法 CSRF 请求外部扫描、传输和系列回写均为 403，相关 API 组再次通过（2.843s）。
- 前端相关回归：`npm run test -- src/features/external-library/useExternalLibrary.test.tsx src/pages/library/hooks/useLibraryTransfer.test.ts`，2 文件 9 用例通过；`npm run build`、`npm run lint` 通过。没有前端源码或协议结构变更。
- Go 工具链 `GOTOOLCHAIN=go1.26.10`；共享主树 `.gocache`、树外独立 `lint-22` 缓存。首次 lint 因新树缺少忽略的 web/dist 不能类型检查，随后以前端真实构建生成 dist；最终 lint 为 `0 issues.`。`gofmt -l ./cmd ./internal` 空输出、`git diff --check` 通过；`go run ./cmd/tsgen` 成功生成 24 类型且无 diff。未改 SQL，无 sqlc 生成需求。
- 已在 fa2afd7（含 04 固定时钟修复）完成 `go test ./... -json -timeout 180s` 全包通过，API 96.88s；完整原始结果在树外 `C:/Users/saber/.codex/tmp/architecture-deepening/all-22-tests.jsonl`。提交前再次整合最新集成代码并复验，结果见最终门禁记录。


### 最终门禁

- 提交前整合 `codex/architecture-deepening` 的 5cd4cec（AI 分组所有权实现），在合并后的源码上再次 `go test ./... -json -timeout 180s` 全包通过，API 92.341s；完整结果：`C:/Users/saber/.codex/tmp/architecture-deepening/all-22-final-tests.jsonl`。随后整合 2173deb，仅共享发布/实施记录变化，无源码变化。
- 最终 `golangci-lint run` 为 `0 issues.`；`gofmt -l ./cmd ./internal` 空输出、`git diff --check` 通过；`go run ./cmd/tsgen` 24 类型、生成物无漂移。此次 Go 测试未出现系统执行策略阻拦；未改任何系统策略，也未绕过被限制工具。
- 相关前端 9 用例、build、lint 均通过；未修改共享 CHANGELOG/spec/implementation，未改 SQL 或公共响应契约。所有验收项已满足，交集成审查。
