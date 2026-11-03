---
id: mm-runtime
type: submodule-design
status: active
title: 进程能力装配与生命周期
parent: mm-application
---

## 责任与顺序

`Runtime.New` 构造共享配置、任务引擎、存储、IO 执行器、功能服务和重启注册表，先于任何恢复或发起输入。传入的 `Effects` 仅提供发布、缓存和身份投影叶操作，不把生命周期配方交回 API。

`Recover` 在自动输入启动前恢复资料库迁移一致性。`RecoverRuns` 将遗留运行转成中断历史，再通过各能力真实重启入口和已登记策略发起安全续跑，不把旧行改成新运行。配置变更可重新释放排队工作，但不替换共享的 `config.Manager`。

`Close` 停止配置监听和事件输入，通知后台循环，停止 watcher，关闭任务准入并等待已登记工作，再释放 scanner 和运行资源。关闭必须幂等；仍可能登记工作的阶段不能提前封闭后台等待组。

## 边界

这里只拥有装配、启动恢复与资源寿命，不拥有阅读规则、任务状态机、数据库事务或 HTTP 解释。能力通过窄端口注入，测试可替换时钟、调度和叶效果而不建立第二套实现。

核对入口：`Runtime.New`、`Recover`、`RecoverRuns`、`Close`、`Registry` 与本目录生命周期测试。
