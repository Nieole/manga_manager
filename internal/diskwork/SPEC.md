---
id: mm-diskwork
type: module-design
status: active
title: 后台磁盘作业策略执行
parent: mm-architecture
---

## 职责

把一次后台磁盘作业的暂停检查、策略解析、工种并发选择、令牌申领与归还收在 `Runner.Do`，将等待实况作为返回值交给调用方。

## 公开契约

- `NewRunner` 的调用方须提供非空配置读取函数与调度器，构造函数不做空值校验。`Work.Path` 决定卷键；`PolicyFrom` 仅在同卷时改变策略来源，不改变实际字节所在卷。
- `Do` 先过上下文的协作式闸门，再申请令牌；取消或申领失败不执行闭包。令牌以 defer 归还，闭包返回错误或 panic 都释放；panic 不被转换或吞掉。
- 元数据扫描取归档打开上限，哈希取哈希上限，封面构建取归档与封面上限的最小正值，缓存写取封面上限。未知工种、非正上限或空卷键跳过令牌申领，仍执行闭包。
- `Stats` 提供策略档位、卷键、申领总等待与其中的 IO 暂停等待；不包含前置单次运行暂停闸门的等待时长。慢申领记录日志。

## 边界与限制

不持有任务身份、运行槽位或指标累计，也不读取闭包内部业务。`taskcontrol` 只提供前置暂停检查；不得把单次运行闸门与卷调度的后台整体暂停混同。前台阅读取页不走此后台工种策略入口。跨兄弟依赖由架构节点维护。

## 证据

`Runner.Do`、`resolvePolicy`、`concurrencyLimit`；`runner_test.go` 的 `TestRunnerPauseGateBlocksBeforeToken`、`TestRunnerReleasesTokenOnEveryExit`、`TestRunnerSkipsTokenWhenUnlimited`、`TestRunnerPolicyFromDecidesPolicyOnSameVolume`。
