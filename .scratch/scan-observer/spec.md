# Spec: 扫描进度的接缝从装配期挪到每次扫描

Status: ready-for-agent

## Problem Statement

扫描器把进度与指标交给谁，是在**装配期**决定的：`SetScanProgressCallback` 与
`SetScanMetricsCallback` 各注册一个进程级回调，对此后每一次扫描生效。

于是每一份报文都必须自带身份（`Scope` + `ID`），api 层再靠这份身份反查回它所属的任务——
`scan_progress_handles.go` 整个文件（一张带锁的侧表、一个 `scanTarget` 身份类型、三个构造函数）
存在的唯一理由就是这次反查。

登记方与写入方因此各自拼一次身份：任务体拼 `libraryScanTarget(lib.ID)`，扫描器报文里拼
`Scope: "library", ID: libraryID`，api 层再拼 `scanTargetOf(report.Scope, report.ID)`。
三处只要有一处对不上，不会有任何编译错误——后果是那次扫描全程进度条一动不动。

这与刚刚落地的**进度句柄**所有权模型正好差最后一段：句柄已经把「谁有资格写某个任务的进度」
变成了结构约束（谁被交给了句柄），唯独扫描这一路仍要经一张侧表才能到达写入方。

## Solution

进度与指标的接收器改由**每次扫描调用**传入。扫描器不再持有它们的回调字段。

调用方视角的变化：

- 发起扫描时交出一个**扫描观察者**；无归属的扫描（守护全局扫描、建库首扫、watcher 派生）
  显式交出 `nil`。
- 报文不再携带身份。「是谁」由观察者的绑定回答，不由报文回答。
- 缩略图重建的跨库聚合改成每库一个观察者，各自持有自己那份记账；「创建第 i 个观察者」
  就是「开始第 i 个库」。

`SetBatchCallback` 保留在装配期：它从入库批次**和封面 worker** 两处发出，消费方是缓存失效
与 SSE 广播，与「哪一次扫描」无关。

## User Stories

1. 作为维护者，我希望发起扫描时把进度交给谁写在调用点上，这样我不必在两处各拼一次身份，
   也不会因为拼错而得到一个全程不动的进度条。
2. 作为维护者，我希望「这次扫描不属于任何任务」由一个显式的 `nil` 表达，而不是由「侧表里
   恰好查不到」表达。
3. 作为维护者，我希望缩略图重建的每库记账挂在那个库自己的观察者上，这样我不必维护三张按
   库索引的表，也不必记住它们之间的同步规则。
4. 作为维护者，我希望扫描器的接口上不再有「注册一个对所有扫描生效的回调」这种东西，
   这样「一个任务的进度被另一次扫描写入」在结构上写不出来。

## Implementation Decisions

**接缝形状。** `scanner.ScanObserver` 接口，两个方法：

```go
type ScanObserver interface {
    Progress(ScanProgressReport)
    Metrics(ScanMetricsReport)
}
```

三个生产适配器（任务进度、缩略图重建的每库聚合、`nil`）加测试探针，接缝是真的而不是假想的。
不用两个裸函数字段：那样「只接了一半」是一种写得出来的状态。

**观察者是位置参数，不是 options 字段。** 它是协作方不是旋钮，且位置参数不可省略——
写 `nil` 是一个看得见的决定，而漏填结构体字段与故意留零值长得一模一样。

```go
ScanLibrary(ctx, libraryID, rootPath, force, observer)
ScanLibraryWithOptions(ctx, libraryID, rootPath, opts, observer)
ScanSeries(ctx, seriesID, force, observer)
```

`CleanupLibrary` 不报进度，签名不动。

**报文去掉 `Scope` 与 `ID`。** 不删的话侧表随时能长回来：只要报文自带身份，就会有人再写一次
按 ID 路由。随之删除 `scan_progress_handles.go` 整个文件与 `scanTarget` / `scanTargetOf` /
`libraryScanTarget` / `seriesScanTarget`。

**缩略图重建的三张按库索引的表变成观察者上的字段。** `perLibPending` / `finalizedLibs` /
`finalizedCoverSeen` 合并进每库观察者自己的 `pending` / `finalized` / `coverSeen`——
它本来就「是」那个库，不必再拿键去查。`trackLibrary` 与 `runGlobalScan` 的
`progress func(current, total int, lib)` 一并消失，换成 `observerFor func(lib) ScanObserver`：
创建观察者这个动作本身就是库切换的边界。聚合器只留 `baseline` 与活观察者集合。

**观察者的寿命超出调用，不设撤销机制。** 见 `docs/adr/0002-scan-observer-outlives-the-call.md`。
扫描任务这一侧的迟到帧由任务引擎的终态守卫丢弃，与改造前逐条等价。

**同键跨运行的封面数串台不在本次范围。** 同一个库的第二次扫描复用同一个**任务键**，
而第一次的封面 worker 仍在上报，于是旧扫描的封面数会计进新任务。改造前后行为一致
（改造前侧表的 `lookup` 拿到的同样是新句柄），本次不引入也不修复。

## Testing Decisions

**删除三个用例**，它们守的是侧表自身的交回语义，而侧表被删除后那些缺陷在结构上写不出来
（与票 11 删除 `TestTaskLaunchersUsePanicGuardedBackground` 同一个理由）：

- `TestScanProgressHandleGoesInertAfterRelease`
- `TestScanProgressHandleReleaseKeepsTheNextScansHandle`
- `TestScanProgressHandlesSeparateScopes`

**改写四个用例**，对着 `ScanObserver` 重写，守的不变量不变：

- `TestScanProgressFlowsThroughRegisteredHandle`
- `TestScanMetricsFlowThroughRegisteredHandle`
- `TestScanWritersAreInertWithoutHandle` —— 改为「观察者为 nil 时写入方无操作」
- `TestScanFramesArePublishedWholeAndOnce`

`rebuild_thumb_progress_test.go` 的六个用例守的是聚合规则（合并、去重、节流、分母），
规则不变，只改驱动方式。

## Out of Scope

- `SetBatchCallback` 与它的三个触发点。
- 同键跨运行的封面数串台（见上）。
- 扫描器内部 250ms 的 `scanProgressReporter` 节流。
- 前端任何代码；本次不改线上契约。

## Further Notes

`CONTEXT.md` 补两条词条：**进度句柄**（今天在代码里被加粗 5 处却没有词条，还的是旧账）
与**扫描观察者**。两者关系紧密，一起写才说得清「谁交给谁」。
