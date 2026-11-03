---
id: mm-proposal
type: module-design
status: draft
title: 元数据提案生命周期
parent: mm-architecture
---

## Responsibility

持有元数据提案的入队、去重、读取、应用与拒绝生命周期，以及裁决写入的事务；把候选转为可审阅字段差异，而非直接接受整个候选对象。

## Contract

- `Queue` 要求非 nil 候选，接收调用方的系列快照；按锁过滤差异，返回新建、复用、无变化、全锁或曾拒绝等 `QueueStatus`，error 仅表示故障。
- 去重限定同一系列，比较字段当前值与建议值及 `SourceID`；文本只裁首尾空白、归一 CRLF，历史字段按当前锁过滤。先查待裁决，再查最近 20 条拒绝记录；`IgnoreRejected` 只跳过拒绝去重。签名并不额外包含 provider 名称。
- `Apply` 支持全部应用和只补空；后者按应用时读取的系列存值及集合成员判断，不用入队快照或目录名展示回落。重新过滤当前锁，全部跳过或无变化时保留待裁决状态。
- 真正写入的字段交给系列写入器；元数据、沿革、统计与提案处理处于本包发起的事务。部分应用删除已写字段行、保留剩余字段待裁决；全部完成才以 pending CAS 关单，抢先时回滚并返回 conflict。拒绝也只推进 pending，不刷新既有终态时间。
- `ListBySeries`、`Inbox` 与 `PendingCount` 提供读取；字段展示重算当前锁和系列当前值，不相信字段行的旧锁/旧值快照。字段行按批查询，分页总数不随当前页缩减。

## Boundary

未确认：以下边界依据当前实现整理，待用户审阅。

本包只认字段名，不拥有 HTTP 编解码、展示标签、候选请求或 AI 合集成员裁决。系列来源写入策略不是提案生命周期；共享边归根 `architecture.md`。

实现观察：部分应用分支没有终态 CAS，加载后遭并发拒绝仍可能写入；应用所用系列值和锁在事务外读取，不应宣称所有裁决都具备事务内重读保证。

## Evidence

- `queue.go`、`signature.go`、`resolve.go`、`read.go`：业务状态、去重、事务与读出契约。
- `queue_test.go`、`read_test.go`：去重范围、当前锁/当前值、分页及批量读字段。
- `resolve_test.go`、`fill_empty_test.go`、`replace_test.go`：部分应用、终态抢先、按存值补空、集合替换及关联失败整体回滚。
