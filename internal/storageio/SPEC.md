---
id: mm-storageio
type: module-design
status: active
title: 卷级 IO 调度
parent: mm-architecture
---

## 职责

发放卷级 IO 许可，协调阅读与后台申领、后台整体暂停及空闲窗口，并提供等待耗时与诊断快照；本包不实际读写磁盘。

## 公开契约

- `Scheduler.Acquire(Request)` 返回需释放的 `Lease`，`Release` 幂等。非正上限或空卷键直接返回空许可；有效申领等待支持上下文取消，返回总等待及暂停等待。
- 卷键仅去首尾空白，大小写原样保留。计数桶键是「卷键 + Limit」：同卷不同上限各自计数，不是全卷单一硬上限。
- 阅读请求不受后台暂停约束；后台依据 `PauseWhenReading`、`IdleOnly` 与读者空闲窗口让路。阅读可在有可暂停后台占用时按阅读上限旁路，不能把快照 Active 必不超过 Limit 当成保证。
- `PauseBackground` 拦后续有效后台申领，不抢占已发出的许可；`ResumeBackground` 唤醒等待者。`Snapshot` 按卷汇总观测，不承诺固定顺序或 FIFO 公平性。包级 `Default` 是共享实例。

## 边界与限制

不解析路径或配置、不认识书与资料库、不负责 HTTP 控制端点；调用方提供工种、卷键与上限。这里暂停的是卷调度中的后台 IO 放行，不是任务执行槽位，也不是 `taskcontrol` 的单次运行协作式暂停闸门。

## 证据

`Acquire`、`limiterKey`、`pauseReasonLocked`、`readerMayBypassBackgroundLocked`；`scheduler_edge_test.go` 的 `TestDifferentLimitsCreateSeparateBuckets`、`TestReleaseIsIdempotent`，`scheduler_test.go` 的 `TestSchedulerLetsReaderBypassPauseableBackground`、`TestSchedulerPauseBackgroundKeepsReaderAvailable`。
