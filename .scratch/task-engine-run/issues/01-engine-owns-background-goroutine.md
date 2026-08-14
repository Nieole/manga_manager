# 01 — 引擎自己申领后台 goroutine

**What to build:** 任务引擎不再依赖 HTTP 控制器替它开后台 goroutine。引擎新增第四个装配期注入依赖——一个「开一个受停机管辖的 goroutine」的能力——并把「任务体 panic 则把该任务置为失败态」这条兜底一并收进引擎内部。

对外行为完全不变：16 个启动点一行不改，用户看到的任务行为、停机行为、失败提示都与今天一致。

这一步单独成票，是因为它让「在测试里注入一个**同步执行**的后台运行能力」成为可能——后续所有契约用例都依赖它才能确定性地断言**终态**，而不是等待真实 goroutine。

**Blocked by:** 无 — 可立即开始

**Status:** ready-for-agent

- [x] 引擎构造点接受第四个注入依赖；全仓库两个构造点（生产装配处、引擎白盒用例）均已更新
- [x] 「任务体 panic → 该任务置为失败态」的逻辑位于引擎内部，控制器不再反向调用引擎的失败方法
- [x] 控制器仍持有并提供那个后台运行能力（停机登记、关闭后拒绝新任务的语义不变）
- [x] 新增用例：注入同步执行版本，验证任务体 panic 后任务进入失败态（此前零覆盖）
- [x] 16 个启动点的源码未改动
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿

## Comments

**实现记录**

改动落在四个文件：

- `internal/api/task_engine.go`：`taskEngine` 新增 `runBackground func(func())` 字段，`newTaskEngine` 收第四个参数；新增 `runTaskGoroutine(key, fn)`，panic 兜底（记日志 + `failTaskWithError`）搬到这里。
- `internal/api/controller.go`：生产装配处传入 `c.runBackground`；`Controller.runBackgroundTask` 退化为一行转发，不再反向调用 `c.taskEngine.failTaskWithError`。`runBackground` 本身一字未改，停机登记与「关闭后拒绝新任务」语义原样保留。
- `internal/api/task_panic_test.go`（新增）：四条用例。
- `internal/api/task_publish_throttle_test.go`：白盒构造点补第四个参数。

新增用例：

1. `TestTaskBodyPanicMarksTaskFailed` — 票要求的那条覆盖；断言投递出去的载荷（与 SSE 订阅者所见一致）进入 failed、带上 panic 值、落 FinishedAt、控制能力清零。
2. `TestTaskPanicReleasesKeyAndRuntime` — panic 后运行时句柄不泄漏，且同一任务键可再次启动（即「恒定 409」这条缺陷的用户可见面）。
3. `TestTaskBodyRunsThroughInjectedBackgroundCapability` — 守卫「引擎不自己开 goroutine」：注入一个只登记、不执行的后台能力，断言任务体没跑。若引擎绕开注入的能力直接 `go fn()`，这条会红。这正是仓库里已发生过一次的缺陷形状。
4. `TestTaskGoroutineOnlyGuardsPanics` — 划出 `runTaskGoroutine` 的职责边界：正常返回的任务它一个字段都不该动。票 02 的启动入口在任务体外侧再包一层来决定终态，与这条不冲突。

**评审修正**

`/code-review` 两轴各自独立指到同一处：`runTaskGoroutine` 里那条 `runBackground == nil` 守卫。它是不可达分支（三个构造点无一传 nil），且 `return` 掉之后任务恰好停在 running——复刻的正是它上方注释声称要消灭的缺陷。已删除。同轮还改了：引擎侧方法名 `runTaskBackground` → `runTaskGoroutine`（与 `Controller.runBackgroundTask` 同词异序、难以区分）；测试注释里「闸门」误指任务键门禁（该词按 CONTEXT.md 专属**暂停闸门**）；节流用例不再跨文件消费 panic 用例里的 helper。

**留给后续票的一处观察**

`launchCleanupThumbnailsTask`、`launchRebuildFileIdentitiesTask` 与低优先级哈希回填三处启动点直接用 `c.runBackground` 而非 `runBackgroundTask`，因此至今没有 panic 兜底。这是既有缺口，不在本票范围（本票要求启动点一行不改）；它们迁到启动入口后（票 06）缺口自动消失。

**验证**

- `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿。
- `go test -race ./internal/api/` 全绿（本票动的是 goroutine 归属，额外跑一遍）。
- `go vet ./internal/api/`、`gofmt -l` 干净。
- `git status` 确认生产侧只动了 `controller.go` 与 `task_engine.go` 两个文件，16 个启动点未被触及。
