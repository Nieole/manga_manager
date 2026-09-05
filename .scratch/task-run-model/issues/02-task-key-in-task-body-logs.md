# 02 — 任务体日志带上任务键

**What to build:** 任务体跑出来的每一行日志都带上**任务键**，「查看日志」按钮点开之后真的有内容。
今天它的实现是在日志文件里 grep `task_key=` 子串，而全仓只有 4 处日志带这个字段，且全部是引擎
自身的故障（落盘失败、序列化失败、panic、重试失败）——**任务体自己一行都不带**，所以那个按钮
对绝大多数任务返回空列表。

**Blocked by:** 无 —— 可立即开始

**Status:** ready-for-agent

## 做法

启动入口把任务键放进交给任务体的 ctx；日志侧包一层能读 ctx 的 handler，在 `Handle` 里把它取出来
附加成属性。任务体因此**一处都不用改**——它们本来就在用全局 logger，只要跑在任务 ctx 上就自动带。

查看侧的过滤口径（`task_key=<键>` 子串）不变，本票只补写入侧。

## 边界

- `scanner` 的日志在跑在任务 ctx 上时同样应当带上。**守护扫描、watcher 派生扫描与建库首扫今天
  不属于任何任务**，它们的 ctx 里没有任务键，因此这几条路径的日志仍然不带——那是对的，
  它们要到票 12 才有归属。
- 不要为此改任何一处 `slog.Info/Warn/Error` 的调用形状。凡是需要在调用点手写 `"task_key", key`
  才能带上的做法都不对：那正是今天只有 4 处带的原因。
- 本票不引入运行标识，那要等新模型（票 07 会把它加进同一个 handler）。

## 验收

- [x] 任务 ctx 携带任务键，日志 handler 从 ctx 取出并附加为属性
- [ ] 资料库扫描、重建缩略图、刮削三类任务体跑出的日志均带 `task_key`，且调用点未改动
- [x] 一次失败的资料库扫描之后，按该任务键过滤日志能拿到非空结果，有一条用例守着
- [x] 无归属的扫描（守护 / watcher / 首扫）日志不带任务键，这是本票的已知边界而非缺陷
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [x] `go vet ./...`、`golangci-lint run` 无 issue、`gofmt -l` 干净、`check-doc-style.sh` 通过

未勾的那条两个半句都不成立，见挂账 D4 与 D5。「调用点未改动」做不到：Go 的 slog 包级函数交给
handler 的是 `context.Background()`，任务键要落到日志上，调用点必须改成 `slog.InfoContext(ctx, …)`
一族。「均带」也还差几行：`internal/database`（本轮归并行 agent）、`internal/parser`（要把 ctx
穿进归档接口）上的那几处仍不带。
