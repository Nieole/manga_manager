---
id: mm-runhandle
type: module-design
status: draft
title: 单次运行的执行资格
parent: mm-architecture
---

## 职责

封装交给单次任务体的执行资格：上报该次运行、过协作式暂停闸门、发起磁盘作业，并累计该句柄的 IO 实况。

## 公开契约

- `New(Writes, *Runner)` 注入写入通道，不校验全部回调非空；调用方须提供所用通道。无磁盘 Runner 仍可上报，但调用 `Disk` 在过闸门后会 panic。
- `Frame.Current/Total` 用指针区分未设置与零。`Report` 是整帧设置；`MergeParams` 合并运行入参，区别于文案参数；`AddMetrics` 累加增量。事件须显式调用 `ItemFailed`、`Warn`，不从日志推断。
- `Checkpoint` 经上下文检查暂停与取消。`Disk` 返回前吸收正常返回的等待实况；哈希计数只要求整文件读取工种且闭包确已执行，即使闭包返回错误也计数，不证明哈希成功。档位与卷键取最近非空值。
- `AdvanceLive` 表达未提交事务的临时计数，提交后仍须 `Advance`；`DisableCancellation` 由注入通道裁决提交边界。并发安全的写入依赖注入实现，句柄自身用锁保护 IO 累计值。

## 边界与限制

未确认：以下边界依据当前实现整理，待用户审阅。

不解释任务键、作用域与终态，不持有数据库或传输协议；资格绑定由构造方闭包决定。`taskcontrol` 的上下文闸门只作用于单次运行的可中断点，不是全局运行槽位，也不是卷 IO 暂停。跨包批循环宜只接收所需窄接口。

## 证据

`Handle`、`Writes`、`Frame`、`Handle.Disk`、`IOMetrics`；`handle_test.go` 的 `TestHandleCountsHashedFileOnlyWhenClosureRan`、`TestHandleDiskGateBlocksBeforeToken`、`TestHandleReportsToInjectedFunctions`、`TestHandleIsSafeForConcurrentUse`。
