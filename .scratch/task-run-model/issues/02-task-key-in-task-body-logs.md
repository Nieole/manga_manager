# 02 — 任务体日志带上任务键

**What to build:** 任务体跑出来的每一行日志都带上**任务键**，「查看日志」按钮点开之后真的有内容。
今天它的实现是在日志文件里 grep `task_key=` 子串，而全仓只有 4 处日志带这个字段，且全部是引擎
自身的故障（落盘失败、序列化失败、panic、重试失败）——**任务体自己一行都不带**，所以那个按钮
对绝大多数任务返回空列表。

**Blocked by:** 无 —— 可立即开始

**Status:** done

## 做法

启动入口把任务键放进交给任务体的 ctx；日志侧包一层能读 ctx 的 handler，在 `Handle` 里把它取出来
附加成属性。任务体因此**不必在任何一个调用点手写这个键**——只要跑在任务 ctx 上、且那行日志走的是
带 ctx 的调用，就自动带。

查看侧的过滤口径（`task_key=<键>` 子串）不变，本票只补写入侧。

## 边界

- `scanner` 的日志在跑在任务 ctx 上时同样应当带上。**守护扫描、watcher 派生扫描与建库首扫今天
  不属于任何任务**，它们的 ctx 里没有任务键，因此这几条路径的日志仍然不带——那是对的，
  它们要到票 12 才有归属。
- 任务体走到的日志改成 `slog.XxxContext(ctx, …)`。handler 只有从这条路才拿得到 ctx：包级的
  `slog.Info/Warn/Error` 交给 `Handle` 的是 `context.Background()`，任务键怎么放进任务 ctx 都到
  不了。要避开的是在调用点手写 `"task_key", key`——那才是今天只有 4 处带的原因；换成带 ctx 的
  调用形状不等于手写这个键，而上一条边界（同一段 `scanner` 代码，跑在任务 ctx 上带、跑在守护/
  watcher/首扫上不带）也只有认 ctx 的调用做得到。
- 这里的 ctx **只作日志载体，不参与取消判断**：为此新收 ctx 形参的函数一律不看 `ctx.Err()`，
  行为零变化，各自的符号 doc 写明这一句。
- 代价记在这里：`images.ProcessImage` / `ProcessImageDetailed` 与 `metadata` 各 Provider 的
  `SearchMetadata` 是导出签名，前台阅读取页与交互式搜索跟着改了形参——那两条路径不属于任何任务，
  永远取不到任务键。
- 本票不引入运行标识，那要等新模型（票 07 会把它加进同一个 handler）。

## 验收

- [x] 任务 ctx 携带任务键，日志 handler 从 ctx 取出并附加为属性
- [x] 资料库扫描、重建缩略图、刮削三类任务体走到的日志带 `task_key`，且键不在任何一个调用点手写
  ——`internal/database`、`internal/parser` 与 `internal/koreader` 上那几处不在本票范围内，见挂账 D5
- [x] 一次失败的资料库扫描之后，按该任务键过滤日志能拿到非空结果，有一条用例守着
- [x] 无归属的扫描（守护 / watcher / 首扫）日志不带任务键，这是本票的已知边界而非缺陷
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [x] `go vet ./...`、`golangci-lint run` 无 issue、`gofmt -l` 干净、`check-doc-style.sh` 通过
