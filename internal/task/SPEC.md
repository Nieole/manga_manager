---
id: mm-task
type: module-design
status: active
title: 任务身份、运行与统一准入
parent: mm-architecture
references: [mm-adr-0004, mm-adr-0005, mm-adr-0006, mm-adr-0007, mm-adr-0008]
---

## 职责

持有任务四要素身份与长期属性、单次运行状态机、统一准入、全局运行槽位、排队与合并，以及进度、事件、采样和历史保留策略。

## 公开契约

- `Engine.Start(RunSpec, Body)` 校验身份与发起方；槽位满、同任务已有活动运行或全部暂停时排队。已有排队运行时返回 `Launched.Coalesced`，后来交出的声明与任务体不执行。定时、监听发起受禁用与退避约束，手动发起复位连败。
- 控制按运行 ID，长期禁用按任务 ID；`RunSpec.Key` 仅供日志，不落盘、不反解身份。任务体经运行句柄上报，以返回错误裁决完成、取消或失败，panic 转失败；重启的 `MarkInterrupted` 转写活动与排队运行，仅交出本批符合白名单的恢复候选，不重建任务体。
- 排队不占运行槽位，暂停与取消中仍属活动态。`PauseAll` 同时拦队列放行；单次暂停是协作式的，要等任务体到可中断点，不表示磁盘操作已立即停止。`Shutdown` 等准入启动交接，任务体退出仍由后台所有者等待。

## 边界与限制

不持有 SQL、HTTP、具体任务类型词汇或文案翻译；落盘经 `Store`，快照与实况经注入函数。`taskcontrol` 只实现每次运行的上下文暂停闸门，不承担运行槽位或卷 IO 许可。落盘端口与临界区内回调不得回调引擎。

ADR 0008 已裁决但尚未完整落实：合并仍保留原排队声明，事件只记一次合并标记并另累计次数，尚非逐次有损事件；`Args` 仍混有部分环境描述。进度等写入失败存在只告警路径，不承诺所有快照与侧数据原子持久化。

## 证据

`Identity.Validate`、`Engine.Start`、`settle`、`MarkInterrupted`、`Store`；`slots_test.go` 的 `TestCoalescedRunOnlyRunsOnce`、`TestCoalescingIsDecidedByTheStore`，`events_test.go` 的 `TestRepeatedCoalescingLandsOneEvent`，以及 `state_machine_test.go`、`terminal_test.go`、`resume_test.go`。
