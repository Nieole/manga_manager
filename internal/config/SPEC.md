---
id: mm-config
type: module-design
status: draft
title: 配置快照、更新与持久化
parent: mm-architecture
---

## 职责

定义 YAML 配置值与默认值，负责加载、归一化、两档校验、密钥掩码、路径存储策略、原子文件写入及热重载；`Manager` 持有共享运行时配置快照与串行更新边界。

## 公开契约

- `LoadConfig` 可在文件缺失时生成默认配置；`LoadConfigFile` 用于读取既有文件。`ValidateConfigValues` 只检查值，`ValidateConfig` 另检查文件系统目标。配置持久化使用 `AtomicWriteFile` 与 `ConfigFilePerm`。
- `Snapshot`、`Replace` 和构造时通过 `CloneConfig` 隔离当前引用字段；修改快照不会更新 Manager。`Version` 是不暴露密钥的预期状态令牌，作用域限于该 Manager 生命周期。
- `Save`、`Reload` 串行处理校验、持久化与应用回调；检测预期状态或已观察文件冲突，应用失败尝试补偿并返回错误，不保证补偿必成功。回调可读取快照，不得重入更新方法；`Replace` 只替换内存，不代替保存或应用资源。
- `Coordinate` 提供同一串行边界下的读取、校验、写入与发布步骤，外部持久参与者自己决定补偿，`Publish` 不回滚磁盘。`StartWatcher` 去抖后走重载，文件消失时不自动重建。
- `ResolveStoragePolicy` 按最具体路径策略解析并返回卷键，不改入参；这里只产出策略，不发放 IO 许可。

## 边界与限制

未确认：以下边界依据当前实现整理，待用户审阅。

不导入兄弟包、不拥有 HTTP 设置端点或资料库数据库行。`runtimecfg.Apply` 负责配置派生的进程资源效果，本包通过注入回调协调，不自行重建解析池、图片并发闸或日志资源；不得把快照副本当成第二份可写运行时配置。

## 证据

`Manager`、`UpdateSession`、`CloneConfig`、`ResolveStoragePolicy`；`config_extra_test.go` 的 `TestSnapshotIsDeepCopy`、`TestResolveStoragePolicyDoesNotMutateInput`，`runtime_test.go` 的 `TestResolveStoragePolicyUsesMostSpecificPathOverride`，以及 `watcher_test.go`。
