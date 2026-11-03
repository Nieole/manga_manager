---
id: mm-database
type: module-design
status: active
title: 藏书数据访问与统一迁移
parent: mm-architecture
references: [mm-adr-0003]
---

## 职责

持有藏书与用户数据的 SQLite 形状、生成查询、手写事务、搜索索引及聚合读模型；统一 `Migrate` 负责建表、存量升级与按版本门控的一次性回填。

## 公开契约

- `NewStore` 打开连接并返回组合 `Querier` 与手写操作的 `Store`；迁移由独立 `Migrate(dbPath)` 入口执行。`DB` 暴露底层连接用于装配，`Close`、`PingContext` 管连接生命周期与探测。
- `SqlStore.ExecTx` 在事务绑定的 `Queries` 上执行回调，失败或 panic 后回滚，panic 继续传播；不是所有 Store 调用自动共享事务。一般 SQL 查询归生成绑定，跨表操作由手写方法收口。
- 搜索具备 FTS、2-gram 与子串回退路径，分页提供 offset 与游标方法；用户进度、账号会话、书签及统计由各自查询明确用户作用域。用户名身份按字节精确匹配，不在持久层做大小写折叠。
- `Migrate` 重放必要 DDL，昂贵回填成功后才更新 `user_version`；它不是包住所有升级步骤的全库原子事务，不应承诺中途失败完全无变更。

## 边界与限制

不执行藏书文件遍历、归档解码、网络抓取或任务调度；持久化操作可以维护派生数据，但不接管阅读与编排用例裁决。任务表的专属 DDL 不并入 sqlc 模型；跨包迁移安装关系归架构节点。旧 `doc.go` 中任务引擎和作品群计算的 API 归属已漂移，不能据此扩张本包责任。

## 证据

`Store`、`SqlStore`、`ExecTx`、`Migrate`、`schemaSQL`；`store_exectx_test.go` 的 `TestExecTxAlwaysFinishesTransaction`，`migration_test.go` 的 `TestMigrateSetsSchemaVersionAndSkipsRebackfill`，以及 `users_case_test.go`、`pagination_total_order_test.go`、`store_search_dualpath_test.go`。
