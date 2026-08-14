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

**Status:** ready-for-agent

- [ ] 启动入口同步执行槽位闸门、异步执行任务体（返回时任务体尚未运行，HTTP 层可立即返回 202）
- [ ] 启动入口返回错误：`nil` 表示已启动，沿用既有的「同类任务已在运行」哨兵错误
- [ ] 引擎按任务体返回的错误决定分支：`nil` → **完成**；取消错误 → 已取消；其余 → 失败
- [ ] `TaskResult` 中的文案码覆盖任务声明里对应分支的默认码；零值时用默认码
- [ ] 任务体只收 `ctx` 与进度句柄两个参数；`ctx` 是纯粹的 `context.Context`，既有的扫描与**暂停闸门**调用无需改动
- [ ] 进度句柄已绑定**任务键**，任务体内部不再出现任务键字符串
- [ ] **计数推进**与**阶段**播报是两个独立方法；「不改变总数」通过调用阶段方法表达，不再需要 `-1` 哨兵
- [ ] 资料库扫描任务改用新入口；其领域副作用（缓存失效、预热、串联后续任务）仍留在任务体内
- [ ] 契约用例（注入同步后台运行能力 + 可控时钟 + 捕获式投递，无数据库/无配置/无扫描器）覆盖：三条终态分支、文案码覆盖与默认、同键已有**活动态**任务时返回哨兵错误**且任务体不被执行**、任何退出路径上**运行时句柄**均被清理
- [ ] 资料库扫描的既有控制器级用例保持通过，未新增该层用例
- [ ] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
