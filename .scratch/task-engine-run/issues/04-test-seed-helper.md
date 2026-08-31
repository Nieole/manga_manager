# 04 — 测试脚手架辅助方法

**What to build:** 一个专门的测试辅助方法，用来「往任务表里放一条任务」，供那些真正想测剪枝、节流水位、快照竞态、任务列表与重试路由行为的用例做 arrange 用。

今天这些用例（共 76 处）用的是字面量文案版的启动方法作为最短写法——它们没有一处断言消息内容，那些文案参数是扔掉不看的。把它们迁到一个语义诚实的辅助方法上，为后续删除整支字面量文案接口扫清障碍。

关键约束：辅助方法必须**建立在真实的启动路径之上**（而不是直接写内存表），否则「同一**任务键**同时只能有一个**活动态**任务」这道闸门在测试中就没有覆盖了。

这张票不删除任何生产代码，也不改动任何启动点。

**刻意留下的死代码窗口**：本票完成后，字面量文案接口在测试侧零调用，但仍然存在——真正的删除在票 11。这是 expand–contract 的正常形态（老形式必须活到最后一个调用点迁走为止），但它意味着本票必须迁得**彻底**：只要漏下一处，票 11 就会被卡住。因此本票的完成判据是可机械验证的调用点计数，而不是「看起来改完了」。

注意生产侧的残留由**别的票**负责：外部库扫描（票 09）、缩略图清理（票 06）、ComicInfo 回写（票 10）各有一处生产调用点。票 11 的前提是这三张票加本票都已完成。

**Blocked by:** 02

**Status:** done

- [x] 辅助方法可设置测试关心的属性（任务类型、**作用域**、总数、可取消/可暂停、**终态**）
- [x] 辅助方法内部走真实启动路径；同键重复播种时闸门仍然生效，且有用例证明这一点
- [x] 76 处脚手架调用全部迁移完毕，用例的断言逻辑本身不变
- [x] 迁移后不再有测试代码调用字面量文案版的启动、进度或收尾方法——以整个仓库范围的调用点计数为准（`grep` 结果为零），不接受抽样核对
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿

## Comments

**实现记录**

新增 `internal/api/task_seed_test.go`：`taskSeed` 声明 + `seedTask` / `trySeedTask` /
`settleSeededTask` / `seededTaskContext`，以及守卫脚手架自己的四条用例。播种走的是启动入口
`Run`，因此**任务键**闸门、**运行时句柄**登记、三条终态分支的裁决都与生产同一条路径。
9 个测试文件、83 处调用（票面估作 76）迁移完毕，字面量文案方法在测试侧的 `grep` 计数为零。

**播种如何做到确定性**

`seedTask` 在调用 `Run` 期间临时把引擎注入的后台运行能力换成「只登记不执行」，据此决定任务体
何时跑：活动态播种要的是「任务行已落地、任务体不跑」，终态播种要的是「任务体同步跑完并收尾」。
这正是票 01 把「开一个受停机管辖的 goroutine」做成注入依赖买到的东西——不必 sleep 或轮询去等
一个真实 goroutine。附带收益：`newTaskContext` 由 `Run` 代劳，此前六处「播种完还要手工补一次
运行时句柄，否则 pause 一律 409」的样板随之消失。

**对票面的三处偏离**

1. **删除了四个已成孤儿的字面量文案方法**（`startTask`、`startPausableCancelableTask`、
   `updateTask`、`failTask`）。票面写的是「不删除任何生产代码，死代码窗口留到票 11」，但
   `golangci-lint` 的 `unused` 是 CI 门禁的一部分（`.golangci.yml` 自述「catch real bugs /
   **dead code**」），这四个方法在最后一处测试调用迁走后变成零调用点，不删则 CI 红。
   expand–contract 的条件「老形式活到最后一个调用点迁走为止」对它们已经满足；仍有生产调用点的
   `startCancelableTask`、`finishTask`、`updateTaskDetails`、`failTaskWithError` 原样保留，
   照票面交给 06 / 09 / 10 / 11。
2. **补上 `TaskProgress.Labels`**。票 02 的实现记录点名「进度句柄没有 labels 通道，票 03 会撞上」，
   本票先撞上了：`TestTaskStatusTracksScrapeMetricsAndLabels` 与快照竞态用例都要并发写 Labels，
   没有这条通道就只能把覆盖丢掉。票 03 届时直接用即可。
3. **两条断言换了观测字段，判定意图不变**。`TestRetryTaskRestartsRetryableTask` 原本断言
   「重试后 Message 不再是那句失败文案」——播下的任务不再有字面量文案，改为断言 `MessageCode`
   不再是播种时那个码。`TestTaskMessageCodeEmission` 的后半段原本靠 `finishTask` 造一条字面量
   文案任务来验证「Message 与 MessageCode 互斥」，改为直接调 `applyTaskMessage`——那条互斥本就是
   模型层纯函数的职责，不依赖任何引擎路径，票 11 删掉旧接口后它也不必再动。
4. **删掉一条已成假覆盖的断言**。`TestCancelTaskRequestsRunningCancellation` 里的
   `task.Message == ""`：它此前证明的是「取消写入消息码时清掉了启动时那句直接文案」，而播下的
   任务从来就没有直接文案，这条断言从此永不会红。留着比删掉更坏——它看着像覆盖，实际不是。
   它原本守的互斥现由上一条那处 `applyTaskMessage` 断言承担。

**给票 03 的提醒**：`TaskProgress.Labels` 已在本票落地（见偏离 2），03 直接用即可，不必再补。

**顺带增强的一处覆盖**

快照竞态用例的写侧此前只有一次 `updateTaskDetails`，其 `Params` 一路是 nil，读侧那段遍历
`item.Params` 形同虚设。改为按生产里 `rebuild_file_identities` 的真实形状写：进度句柄一次
（Advance / Item / Metrics / Labels）+ `mergeTaskParams` 一次，四个可变 map 这才全都被并发写到。

**验证**

- `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿。
- `go test -race -count=1 ./internal/api/` 全绿（播种要换引擎字段，额外跑一遍）。
- `go vet ./internal/api/`、`golangci-lint run ./internal/api/...`（0 issues）、`gofmt -l` 干净。
- 未改动任何**启动点**，未改动前端契约（`TaskStatus` / `TaskLimits` 未动）。
