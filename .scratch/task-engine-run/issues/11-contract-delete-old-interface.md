# 11 — 收缩：删除老接口

**What to build:** expand–contract 的收缩步。所有启动点都已迁移、所有测试脚手架都已改走辅助方法之后，把老接口整体删除。

这一步**必须与前面的迁移落在同一个 PR 内**。本仓库现有接口里那条「i18n 码版 / 字面量版」的分裂轴，就是一次停在半路的迁移冻结进签名的产物——生产走一支、测试走另一支，两套词汇永久共存。用同样的方式收尾只会再造一条相同的疤。

删除后，启动相关接口收到一位数。票 03 在证伪进度句柄的所有权模型时补了 `TaskProgress.Report(TaskFrame)` 与 `AddMetrics`，票 05 又补了 `MergeParams`（理由各见那两张票的结论），因此终值不是原先估的 3 个：启动入口 1 个，进度句柄 8 个方法加一个 `TaskFrame`。收敛的实质仍在——只剩一条启动通路、一种消息词汇、没有哨兵值。

**Blocked by:** 04、05、06、07、08、09、10（03 由这些票传递依赖）

**Status:** ready-for-agent

- [x] 字面量文案版的启动、进度、失败、完成方法整支删除
- [x] 旧的启动方法（含各种可取消/可暂停组合的变体）与其七参数核心方法删除，其职责由任务声明承担
- [x] 九参数的详细进度方法及其 `-1` 哨兵删除

  上面三条到本票开工时只剩 ComicInfo 回写一个调用方：i18n 码版的那六个（启动、两个进度方法及其
  核心、完成、失败）在票 09 转换掉最后一个调用点时就已删除——`unused` 是 CI 门禁，留不到本票。
  本票要删的是字面量文案版那一支，以及它们共用的核心方法。
- [x] 建上下文、写元数据两个方法降为引擎内部，不再对包内其余代码可见（写并发上限那个已在票 07 删除：本票之前它就没了调用点，而 `unused` 是 CI 门禁）
- [x] 控制器上不再有「起一个任务型后台 goroutine」的方法（通用后台运行能力保留，作为注入依赖）
- [x] `TestTaskLaunchersUsePanicGuardedBackground` 一并删除：它以源码扫描守卫「占了任务键就必须走带兜底的入口」，本票让该约束由「只有一条通路」结构性成立，守卫随之失去对象
- [x] 全仓库不再有任何代码路径可以绕过启动入口创建一个任务
- [x] 无任何遗留的兼容垫片或过渡别名
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [x] `cd web && npm run build` 通过（本次不改前端，用于确认线上契约未变）
- [x] `CHANGELOG.md` 记录本批次的架构改动，以及各票留下的用户可见改动：
  - 票 01 后续修掉的那条缺陷（缩略图清理、文件身份重建、低优先级哈希回填三个任务此前 panic 后会永远停在运行中，且其任务键恒定返回 409 直到重启进程）
  - 票 03：缩略图重建在「清空封面索引」这一步被取消时，此前显示为失败「清空封面索引失败」，现在显示为已取消
  - 票 09：外部库传输期间的进度推送变密（手写的 500ms 节流删除，只剩引擎的 200ms 水位）。
    **不要写成「不再滞留」**：水位不比对当前条目，一本大书的开工帧若落进窗口内仍会被吞，
    气泡在那几分钟里停在上一本书上。这是量的改善不是质的，理由见票 09 的偏离 1
  - 票 09：外部库传输的逐条目文案不再在「正在传输 X」与「已传输 k / N」之间逐本来回切换，
    统一为前者；后者移到收尾那一帧（计数本就由进度条承担）
  - 票 09：外部库扫描的逐条目进度文案改为按语种渲染（此前对所有用户固定为中文）
  - 票 10：ComicInfo 回写的起始、逐条目与**完成**文案改为按语种渲染（此前对所有用户固定为中文，
    且完成那句由后端拼好），逐条目进度期间显示的不再一直是启动文案
  - 票 10：ComicInfo 回写被取消时显示本任务专属的已取消文案，此前显示的是通用的「正在取消任务...」
    ——一条**终态**在说自己正在取消
  - 票 10：ComicInfo 回写在任务中心的**任务类型**标签与**阶段**名不再显示成裸文案键
    （`task.type.write_comicinfo` 与 `settings.maintenance.taskPhase.writing` 此前两个语种都缺）
  - 票 05：资料库扫描与系列扫描进入**终态**后，迟到的扫描器报文不再改写它的进度与参数（此前任务面板上一条已完成的扫描仍会继续变动）
  - 票 06：索引重建的失败与取消文案、缩略图清理的逐条目进度文案改为按语种渲染（此前分别对所有用户固定为英文与中文），且索引重建的两步失败文案分开
  - 票 06：索引重建在关服时落**已取消**而不是失败
  - 票 06：缩略图清理收尾那一瞬不再单独显示一句「清理完成，共删除 N 个」，直接落到**完成**文案（该帧此前也随即被覆盖）

## Comments

**实现记录**

票面前六条到开工时已由票 10 提前满足（`unused` 是 CI 门禁，留不到本票）。真正剩下的工作串在同一条链上：

- `internal/api/task_engine.go`：新增常量 `taskPanicMessageCode`（`task.msg.control.panicked`），
  panic 兜底改调 `failTask`；删除 `failTaskWithError`——它是一行转发，而那句硬编码英文
  「Background task panicked」正是 `finalizeTaskCore` / `failTaskCore` / `applyTaskMessage`
  三个 `message` 形参今天唯一的非空实参来源。三处形参随之消失。
- `internal/api/task_model.go`：`applyTaskMessage` 收成只认 i18n 码，空码定义为**无操作**
  （「这一帧不改文案」）。`task.Message = ""` 保留——它与 `MessageCode` 必须互斥。
- `internal/api/task_run.go`：`newTaskContext` 折进 `claimTaskSlot` 的同一次持锁，成为
  `newTaskRuntimeLocked`；任务声明的三份 map 落地时克隆（票 08 留的那条）。
- `web/src/i18n/locales/`：两个语种各补一条 `task.msg.control.panicked`。

**`*Core` 后缀一并去掉**（评审指出）。`failTaskWithError` 删掉之后，`Core` 不再与任何非-Core
兄弟对照，只剩两个孤儿后缀——那正是本票第 8 条要收的过渡期命名。现为 `finalizeTask` / `failTask`。

**对票面的五处偏离**

1. **第 4 条「不再对包内其余代码可见」在 Go 里做不到字面意义**：未导出方法对整个包可见，而本票
   同时要退休源码扫描守卫（第 6 条），不能再用扫描去补。落地的是能做到的最强形式——把建句柄折进
   `claimTaskSlot` 的持锁段，于是「拿得到一份任务句柄」等价于「刚刚申领到一个任务槽位」，
   `newTaskRuntimeLocked` 只有一个调用方且调用方只有 `Run`。**这处偏离在此备案。**
   顺带关掉一个窗口：分成两次持锁时，其间任务已入表、首帧已投递、`CanCancel` 已为真而句柄尚未登记，
   用户在那一瞬按取消会吃一条 `errTaskCancelUnavailable`。

2. **CHANGELOG 的清单逐条核实过，与票面有三处不同**，理由如下：
   - **删去**「缩略图清理收尾那一瞬不再单独显示一句」：票 06 的实现记录自己就写了「实际观察不到」
     （那一帧与完成帧之间只隔任务体的 return）。写进 CHANGELOG 等于给用户一条他从没见过的改动。
   - **改写**「索引重建的两步失败文案分开」：`main` 上 `launchRebuildIndexTask` 本来就是
     `SQLite series search index rebuild failed` 与 `SQLite book search index rebuild failed`
     两句不同的英文，本批是**保住**这个区分而不是新增。真正新增的用户可见差别是失败提示不再把
     技术错误串 `%v` 拼进展示文案，故改写成那一条。
   - **扩大**「终态后迟到的扫描器报文」：范围补上缩略图重建，并把票 03 漏报的「重建期间并发的
     系列扫描会把自己的指标并进重建任务」并进同一条——它们是同一处改动，而后者更容易被用户注意到
     （指标与进度条分母一起被抬高）。
   另补两条票面没有的：本票自己的 panic 文案国际化；票 09 的手写节流实为三处（扫描那一处票面漏了）。
   票 09 那条「不要写成不再滞留」的口径限制照办。

3. **顺带修掉两处同类的 map 别名**（评审与审计各自独立指到）。票 08 只点名了 `claimTaskSlot`，
   但 `applyTaskMessage` 的 `task.MessageParams = params` 是同一形状，探针实测确认写穿；
   `cloneTaskStatus` 则漏了 `TaskStatus` 的第五个引用字段 `EffectiveLimit`（`applyTaskLimitParam`
   会穿过这个指针写），而它的 doc 写着「逐一复制四个可变 map 字段」——一个会腐坏、且恰好把这个
   字段挡在读者视野外的说法。两处都改成无例外的口径：任务上的每个可变字段都归引擎所有。
   `EffectiveLimit` 那条今天不可达（要求活任务的参数里出现 `limit.` 前缀的键，17 个启动点都没有），
   记为隐患而非缺陷。

4. **`web/src/api/generated.ts` 重新生成，单独成一个提交**。票面第 9 条写「本次不改前端」、
   spec 的 Out of Scope 写「前端任何代码」，因此它刻意不混进收缩提交。但票 05/07/08/09/10 一路
   顺延的那处头注释漂移让 CI 的契约漂移校验恒红，而本票是这条 PR 的最后一张；差异只有一行注释，
   类型无变化。

5. **一处刻意不做**：`TestNewControllerMarksPersistedRunningTasksInterrupted` 直接 `store.UpsertTask`
   造一条 running 的落盘记录。按第 7 条的字面意思这是一次绕过启动入口的建任务，但它模拟的正是
   「**上一个进程**留下的行」——闸门是进程内的内存状态，这条行按构造就早于本进程。改走启动入口
   会让这个用例不再测它要测的东西。第 7 条针对的是生产路径，那一侧已结构性闭合：
   `Run` → `claimTaskSlot` → `admitTaskLocked` 是内存任务表唯一的建行路径，
   `newTaskRuntimeLocked` 是运行时句柄唯一的建立点，而 `UpsertTask` 在生产侧的唯一调用方是
   `flushTaskPersist`，喂给它的都是已经在内存表里的行。

**评审修正**

`/code-review` 两轴各自跑完后改了五处，均不涉及行为：`*Core` 孤儿后缀去掉（见上）；
`claimTaskSlot` 里那段三行的克隆说明并进符号 doc（doc-style 的行内预算是一两行，且它与测试头
逐字重复）；「进度帧」按 CONTEXT.md 改回**一帧**；两条测试 doc 不再复述实现侧的机制，只答守哪条
不变量（doc-style 对测试文件头的规定）；「Message 只来自落盘中断记录」这条理由三处重复，
收归 `TaskStatus.Message` 的字段 doc 独占，另两处改为引用。

**评审查出、本票范围之外、单独成提交的一条缺陷**

`failTask` 少清 `task.PauseReason`（`finalizeTask` 有）。暂停中失败的任务带着 `pause_reason`
进终态，而任务中心会把它渲染成一行「暂停原因：手动暂停」——一条自称手动暂停的失败任务。
配 `TestFailedTaskDropsPauseReason`。

评审同时建议把 `finalizeTask` 与 `failTask` 合并（两者躯体大段雷同），**未采纳**：另一处分歧
`if task.Total > 0 { task.Current = task.Total }` 是有意义的——一个跑到 7/20 就失败的任务应当
诚实显示 7/20，把它抹成 20/20 是撒谎。两者不是同一个函数。

**验证**

- `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿。
- `go test -race -count=1 ./internal/api/ ./internal/external/ ./internal/koreader/ ./internal/scanner/` 全绿。
- `go vet ./...`、`golangci-lint run ./internal/...`（0 issues）、`gofmt -l` 干净。
- `scripts/check-doc-style.sh` 通过。
- `go run ./cmd/tsgen` 无漂移。
- `cd web && npx tsc -b`、`npm run test`（31 文件 / 266 条）、`npm run build` 全过。
- 两个语种按机器比对：各 1953 个键、集合完全相同。
- **反向验证两条**：删掉 `applyTaskMessage` 里的 `task.Message = ""`，
  `TestTaskMessageCodeEmission` 变红；把 `cloneStringMap(params)` 改回裸赋值，
  `TestTaskMapsAreOwnedByTheEngine` 变红。两次均已还原。

**对抗复核（24 路）查出的三处 CHANGELOG 准确性缺陷**

这三处全在**面向用户**的 `docs/changelog/2026-08.md` 里，代码侧无问题。逐条核实后已修：

1. **写了一个从未发生过的缺陷。** 外部库传输那条原文说老路径「等于没有节流」，投递「会以数万条的
   规模冲进 SSE」「所有人的任务中心当场停止更新」。不成立：`main` 的 `launchExternalLibraryTransferTask`
   循环里拷贝前后那两次上报**共用同一个 `lastUpdate`**，老路径整条循环被限到约 2 帧/秒，洪水够不到。
   那一幕是「若把两条文案码原样搬到引擎水位上**才会**发生」的后果——它是本票合帧的**理由**，
   不是历史。已改写成条件句。这是票 09 那条口径限制的同类问题，而当时只照办了「不再滞留」那一条。

2. **「进度节流从此只有一处实现」是过度断言。** `internal/scanner/scanner.go` 的
   `scanProgressReporter.publish` 至今仍是一道 250ms 墙上时钟进度节流，且喂的就是同一条任务进度流
   （扫描器回调 → `handleScannerProgressEvent` → `TaskProgress.Report` → `publishTaskProgressLocked`），
   资料库扫描与系列扫描今天叠着 250ms + 200ms 两道。`taskProgressPublishInterval` 自己的 doc 就写着
   这件事，两处当场矛盾。已收窄到本次范围。
   **附带一条给票 09 的更正**：它的偏离 5 写「`internal/` 里不再有第二个按墙上时钟计时的进度节流」，
   同一处过度断言，正确的说法是「外部库这两条路径的任务体里不再有手写进度节流」。

3. **「任务键恒定 409」从 2 个任务被外推到 3 个。** 低优先级哈希回填的任务键
   `lowPriorityBookHashTaskKey` **没有任何 HTTP 启动点**（它由资料库扫描与系列扫描的任务体串联发起），
   而任务中心的重试按钮对活动态根本不渲染。它的用户可见面是另一种：此后每次扫描收尾的自动回填
   静默不再发起，任务中心多一条永远运行中的幽灵任务；而且 `main` 上它归还上下文那次是裸调用而非
   `defer`，panic 时句柄反而留着，用户点一次取消就永远停在**取消中**。已按三者各自的表现分开写。

同时补上 `6320072`（失败任务不再带暂停原因进终态）的 CHANGELOG 条目——AGENTS.md 要求用户可见改动
与改动落在同一批次，它单独成提交时漏了。

复核对代码侧的结论：15 个老接口符号全仓无残留、空码无操作与开工时的行为等价、
`errTaskCancelUnavailable` 那个窗口确实被关掉、被闸门挡下的启动换不掉在跑任务的句柄、
超范围的三处（map 克隆、`EffectiveLimit`、panic 文案）都是收缩逼出来或前序票顺延的，逐条备案。
