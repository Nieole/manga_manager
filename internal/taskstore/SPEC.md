---
id: mm-taskstore
type: module-design
status: draft
title: 任务历史 SQLite 存储
parent: mm-architecture
references: [mm-adr-0006]
---

## 职责

持有任务与运行的 SQLite 表、迁移、侧数据、筛选分页、聚合与裁剪，实现领域声明的 `task.Store`，不保存另一份运行时状态。

## 公开契约

- `New(*sql.DB)` 仅包装已有连接，不建表；`Migrate` 建表并更新存量列与索引，检查 `foreign_keys`，未开启返回 `ErrForeignKeysDisabled`。连接使用方仍须确保各连接启用外键。
- `EnsureTask` 按身份四要素复用任务。活动运行与排队运行各有一条按任务的部分唯一索引；违例映射为 `ErrRunAlreadyActive`、`ErrRunAlreadyQueued`，允许同任务一条活动与一条排队共存。活动集合由 `ActiveStatuses` 生成，迁移时重建索引。
- 时间以 epoch 毫秒存储、按 UTC 读回，运行主要按序号定序；侧表随运行级联删除。指标的设值与累加分开，Args、Labels 按键合并；裁剪不选择活动或排队运行。

## 边界与限制

未确认：以下边界依据当前实现整理，待用户审阅。

不决定任务体、终态裁决、自动重发、全局槽位、暂停或推送；只执行端口表达的存取。侧表一次合并有事务，不等于整次引擎准入及全部侧数据共享一个事务；不得据此宣称完整声明原子落库。跨包依赖归架构节点。

## 证据

`Store` 的接口断言、`Migrate`、`admissionStatements`、`execTx`；`admission_test.go` 的 `TestActiveIndexRejectsSecondActiveRun`、`TestActiveRunAndQueuedRunCoexist`，`cascade_test.go` 的 `TestMigrateRefusesWhenForeignKeysAreDisabled`，以及 `runs_test.go`、`prune_test.go`。
