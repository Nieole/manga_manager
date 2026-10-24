# 17: 应用运行入口与历史保留清理

**What to build:** 以历史保留清理跑通应用侧运行发起、上下文及观测；原发起方式兼容接入同一个任务引擎，为后续逐批迁移提供入口。

**Blocked by:** None (can start immediately).

**Status:** done

**验收基准：** [父规格](../spec.md)。本票遵守其中的跨功能约束和范围，不将历史实现偏差重新定义成业务规则。

**规格覆盖：** US 1、US 4、US 5、US 7、US 9、US 90、US 91、US 93、US 94、US 95、US 106、US 107、US 109。

- [x] 以运行历史保留清理这条完整用例建立应用侧运行发起与装配入口，实际从定时发起到保留裁剪、运行观测和收尾验证。
- [x] 沿用同一任务领域引擎、存储、槽位和资料库访问协调；旧发起入口通过兼容方式接入，不产生第二个准入守卫或状态事实源。
- [x] 保持任务与运行身份、任务键仅供日志、运行句柄、上下文日志、暂停取消和排队合并等既有领域分工。
- [x] 重启能力与可重试、可续跑判定仍来自装配期完整注册的单一事实源；本票不为实现兼容而改成首次发起时懒登记。
- [x] 应用入口不反向依赖请求处理或 HTTP 快照类型；传输转换留给 adapter，旧调用方可逐批迁移且保持可编译。
- [x] 只迁历史清理所需完整行为和共同运行接入，给遗留功能保留明确可移除的兼容方式，不先造任意流程框架。
- [x] 测试实际历史清理发起、存活运行不被裁剪、上下文观测、重试能力与共享准入，规则测试可通过应用 seam 验证。
- [x] 保持 CI 可通过，完成相关后端与生成物检查，记录过渡职责及需由后续票移除的兼容入口。

## Comments

### 2026-10-24 实施与验证

- `internal/application.Runs` 接入装配方提供的同一 `task.Engine`，共同发起入口为 `Runs.Start`；保留裁剪、每日首次立即发起、运行指标与收尾文案由应用用例负责。
- `api.taskEngine.start` 保留传输声明转换和资料库访问解析，经 `Runs.Start` 发起；prepare 桥只为首帧缓存身份投影，不保存准入状态。运行日志装饰归应用层，暂停、取消、槽位及排队合并仍由领域引擎负责。
- 重启能力仍由 `Controller.buildTaskDispatch` 在装配阶段完整登记，尚未迁移的重启函数及 HTTP 快照转换是单向兼容桥；后续编排迁移与生命周期收口票负责移除。清理自身维持不可重试的既有能力。
- TDD：先新增应用 seam 测试，观察到应用包尚未实现的编译失败，再实现用例。真实 SQLite 用例覆盖历史裁剪、存活及排队运行保留、上下文运行标识与任务键、共享引擎准入合并、首次立即清理与停止。保留阈值规则测试迁至应用 seam，不再构造 Controller。
- 通过：`CGO_ENABLED=0 go test ./internal/application ./internal/task ./internal/taskstore`；`golangci-lint run ./internal/application/... ./internal/task/... ./internal/taskstore/...` 为 `0 issues.`；`gofmt -l ./cmd ./internal` 输出为空；`git diff --check`。
- 受阻：API 清理与装配重启能力回归、全量后端门禁和依赖 API 的生成物检查需要 CGO；系统 Application Control 阻止 `gcc.exe`。关闭 CGO 会使现有 `github.com/chai2010/webp` 编译失败。全量 lint 也因这一依赖的 C 预处理失败退出 1，不能把同时打印的 `0 issues.` 视为通过。没有绕过系统策略。
- 新增 API 装配回归 `TestApplicationCleanupKeepsAssemblyRestartCapabilities` 检查首次发起之前和之后的可重试与可续跑能力；保留已有清理 HTTP/Controller 回归。这些测试已写入，但本机尚未执行成功，因此最后两项验收仍未勾选，状态不置 done。
- 没有改动 SQL、对外响应结构或前端生成契约；`web/dist` 仅从主树复制用于现有 Go embed，不进入提交。

## 集成补验

04 基于含 19 的集成分支完成最终全量 Go 测试、全量 lint（0 issues）、格式、SQL 与 TypeScript 生成检查，全部通过；前端 893 项测试、build/lint 通过。04 合入 cb8fab1 后 reading/database 及 HTTP 节流、批量读态、未知页数定向回归再次通过。此前记录的环境阻碍与旧测试缺口已补验，全部本票验收项满足，关闭工单。
