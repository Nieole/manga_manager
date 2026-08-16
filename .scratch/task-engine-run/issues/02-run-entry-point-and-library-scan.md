# 02 — 启动入口落地，资料库扫描任务先走通

**What to build:** 任务引擎对外只暴露一个启动入口。调用方提交一份任务声明和一个任务体，引擎负责其余全部——槽位闸门、可取消可暂停的上下文、后台 goroutine 生命周期、进度节流、三条**终态**分支、以及 panic 兜底。

资料库扫描任务完整走这条新路径作为第一发曳光弹：用户在界面上点「扫描」，任务启动、报**计数推进**与**阶段**、可暂停可取消、**完成**/已取消/失败三种**终态**都正确落地并推送到任务中心。

老接口原样保留，其余 15 个启动点一行不改。

下列类型形状来自本次改造前的盘问，比散文更精确地编码了已定的决定，故内联：

```go
type TaskSpec struct {
    Key, Type   string
    StartCode   string              // 起始文案的 i18n 码
    Params      map[string]string
    Total       int
    CanCancel   bool
    CanPause    bool
    Metadata    map[string]string
    ScopeName   string
    Limits      TaskLimits
    CompleteCode, CancelCode, FailCode string   // 三条终态分支的默认文案码
}

type TaskResult struct {
    Code   string              // 覆盖对应分支的默认码；零值表示用默认
    Params map[string]string
}

func (e *taskEngine) Run(spec TaskSpec,
    fn func(ctx context.Context, tp *TaskProgress) (TaskResult, error)) error

func (tp *TaskProgress) Advance(current, total int, code string, params map[string]string)
func (tp *TaskProgress) Stage(phase, code string, params map[string]string)
func (tp *TaskProgress) Metrics(m map[string]int64)
func (tp *TaskProgress) Item(name string)
```

**Blocked by:** 01

**Status:** done

- [x] 启动入口同步执行槽位闸门、异步执行任务体（返回时任务体尚未运行，HTTP 层可立即返回 202）
- [x] 启动入口返回错误：`nil` 表示已启动，沿用既有的「同类任务已在运行」哨兵错误
- [x] 引擎按任务体返回的错误决定分支：`nil` → **完成**；取消错误 → 已取消；其余 → 失败
- [x] `TaskResult` 中的文案码覆盖任务声明里对应分支的默认码；零值时用默认码
- [x] 任务体只收 `ctx` 与进度句柄两个参数；`ctx` 是纯粹的 `context.Context`，既有的扫描与**暂停闸门**调用无需改动
- [x] 进度句柄已绑定**任务键**，任务体内部不再出现任务键字符串
- [x] **计数推进**与**阶段**播报是两个独立方法；「不改变总数」通过调用阶段方法表达，不再需要 `-1` 哨兵
- [x] 资料库扫描任务改用新入口；其领域副作用（缓存失效、预热、串联后续任务）仍留在任务体内
- [x] 契约用例（注入同步后台运行能力 + 可控时钟 + 捕获式投递，无数据库/无配置/无扫描器）覆盖：三条终态分支、文案码覆盖与默认、同键已有**活动态**任务时返回哨兵错误**且任务体不被执行**、任何退出路径上**运行时句柄**均被清理
- [x] 资料库扫描的既有控制器级用例保持通过，未新增该层用例
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿

## Comments

**实现记录**

新增的 `internal/api/task_run.go` 承载全部新接口：`TaskSpec`、`TaskResult`、`TaskProgress`、
`Run`、`claimTaskSlot`、`settleTask`、`applyTaskProgress`。其余改动：

- `internal/api/task_engine.go`：三处，均为评审后落定——`newTaskContext` 归还句柄的判定
  （见下「顺带修掉的竞态」）、抽出 `admitTaskLocked`、`completeTaskCore` 改名 `finalizeTaskCore`
  （见下「评审修正」）。旧启动方法族的签名与行为一字未改。
- `internal/api/controller_library.go`：`launchLibraryScanTask` 从「五次调用 + 手写三分支闭包」
  收成「一份任务声明 + 一个任务体」，返回值由 `bool` 改为 `error`。
- `internal/api/controller_tasks.go`：`scan_library` 的**重启函数**原样透传启动入口的错误，
  不再把布尔值转换回哨兵错误。

**两处刻意的形状决定**

1. **任务声明一次性落地。** 元数据、作用域名与并发上限此前是启动之后三次独立的写入。合并进
   `claimTaskSlot` 之后，首帧就带齐全部字段——此前存在一个「任务已在列表里、却还没有作用域名」
   的窗口，任务列表接口能观察到它。附带的事实：并发上限那次写入今天本来就被自己刚写下的
   节流水位吞掉，从没有真正投递过，所以合并后投递帧数从 2 变 1，而不是从 3 变 1。
2. **`TaskSpec.Limits` 零值表示「没有上限可报」**，引擎不为它造一份全零的上限。多数维护任务
   从不调用 `setTaskEffectiveLimit`，若无条件写入，它们的任务面板会多出一组「0 并发」的假数据。

**顺带修掉的竞态**

`newTaskContext` 返回的清理函数此前是无差别 `delete(e.runtimes, key)`。终态写入本身也会清掉
这一项，因此一个走 `defer` 清理的任务体（`rebuild_thumbnails`、低优先级哈希回填等今天就是这么写的）
在收尾**之后**才执行清理；此间同名任务若已重新启动并登记了自己的句柄，那次 delete 会把新任务的
ctx 与**暂停闸门**一起抹掉，该任务从此暂停不了也取消不了。改为只归还自己那份句柄。

这不是本票凭空扩张范围：本票的验收标准要求「任何退出路径上运行时句柄均被清理」，而 `Run` 用
`defer` 归还（好处是 panic 路径也覆盖到）正好落在上述形状里，不修就是把竞态写进新接口。

**用例**

契约用例集中在 `internal/api/task_run_test.go`（12 条），全部在引擎构造点这一个 seam 上，
注入同步后台能力 + 可控时钟 + 捕获式投递，无数据库/配置/扫描器。除票面要求的四类之外还钉了：
**取消中**属于活动态（此时同键不得再启动）、panic 仍落失败态、任务声明首帧完整、
零值 `Limits` 不落假数据、进度句柄在终态之后失效（扫描器回调晚一拍不得把任务拽回进行中）。

`newBackgroundTestEngine` / `runTaskBodySynchronously` / `lastPublishedTask` / `fakeClock`
从票 01 与节流用例里搬进 `internal/api/task_engine_seam_test.go`——它们是这个 seam 的共用装置，
放在某一组用例的文件里会让后来者跨文件消费（票 01 评审已就此提过一次）。

**对规格内联形状的三处偏离**（后续票请以本节为准）

1. `TaskProgress.Stage` → **`Phase`**。CONTEXT.md 把 `stage` 列为**阶段**这个概念要避开的词，
   而 spec 的 Further Notes 明写「词汇以仓库根部的领域词汇表为准」，故词汇表压过内联形状。签名不变。
2. `TaskSpec.Params` → **`StartParams`**。原名与 `Metadata` 互为陷阱：`Params` 落进
   `TaskStatus.MessageParams`，`Metadata` 才落进 `TaskStatus.Params`。接反不会有编译错误，
   后果是任务参数丢失——票 05 点名的「AI 分组重试回落到错误语言」正是这个形状。
3. `TaskResult` 保留 `Code` / `Params` 原名：它只承载终态文案，同一类型内无歧义。

**评审修正**

`/code-review` 两轴各自独立指到同一处：`scanLibrary` 把启动入口的**任何**错误都翻成 409。
已改为 `errors.Is(err, errTaskAlreadyRunning)` 分支 + 500 兜底，判定口径与 `retryTask` 一致。
另外两处：

- `completeTaskCore` → **`finalizeTaskCore`**。新代码的取消分支调它写入**已取消**终态，
  于是「用『完成』这个词写入已取消终态」这处模糊——spec 的 Further Notes 点名要消除的那一处——
  在新接口上原样重现了。改名后它诚实地表示「落成 status 指名的那个终态」。
- 抽出 `admitTaskLocked`。`claimTaskSlot` 与 `startTaskWithOptionsCore` 此前逐行同形
  （闸门、编号、入表、裁剪、落盘、投递）。这几步正是「抄对」风险最高的地方，让两个构造点共用
  同一份机制，迁移期间不会各自漂移；票 11 删掉旧启动方法族后它自然只剩一个调用方。

**评审提出、本票不做的三条**（留给后续票）

- **曳光弹没有真正驱动进度句柄。** 资料库扫描的进度由 `handleScannerProgressEvent` 从任务体
  之外写入（扫描器回调在装配期注册，按 scope+id 拼任务键），因此 `Advance` / `Phase` / `Item` /
  `Metrics` 转换后仍是生产零调用点。把它接到句柄上，需要的正是票 03 要验证的「句柄交给外部写入者」
  那套所有权模型——在这里先做一遍等于抢先替 03 做决定，而 03 存在的意义就是让这个模型先被证伪。
  **附带一条给票 03 的更正**：票 03 称缩略图重建是「唯一一个进度由任务体之外写入的」，不成立——
  资料库扫描与系列扫描也是。03 设计所有权模型时要把这三个一起考虑。
- **进度句柄没有 labels 通道。** 规格内联的四个方法漏了这一路，而生产有 3 处在用
  （缩略图重建 2 处、刮削 1 处）。票 03 会立刻撞上，届时补一个方法即可。
- **停机后 `runBackground` 静默丢弃任务体。** 此时任务行与**运行时句柄**已落地却永不归还。
  这与今天 16 个启动点的行为一致（非本次引入），且能自愈：任务快照已落盘，下次启动由
  `recoverInterruptedTasks` 转入**中断**终态——正确的终态，且可重试。要在结构上堵死它，
  得让注入的后台能力回报「有没有受理」，那会改动票 01 定下的依赖形状，不属于本票。

**验证**

- `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿。
- `go test -race -count=1 ./internal/api/` 全绿。
- `go vet ./internal/api/`、`golangci-lint run ./internal/api/...`（0 issues）、`gofmt -l` 干净。
- `go run ./cmd/tsgen` 无产物漂移（本次未动 `TaskStatus` / `TaskLimits`）。
