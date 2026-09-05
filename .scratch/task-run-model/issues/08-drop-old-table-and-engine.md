# 08 — 丢弃旧表与旧引擎

**What to build:** 手起刀落：`DROP TABLE tasks`，删掉旧引擎那套内存表、异步落盘、模型转换与
`params` 的编解码。零新功能，零行为变化——票 07 之后它们已经没有读者。

**Blocked by:** 07

**Status:** ready-for-agent

## 迁移就是丢弃（ADR 0004 关键决定）

不做转换。老表一个任务键只有一行，转过来也只是每个任务孤零零一条运行，而那段解析代码只跑一次
却要连测试一起维护。

**后果可自愈**：资料库的扫描模式与间隔不在这张表里，升级后第一个守护 tick 就会把身份重新建出来，
清单自己长回来。丢的只是「上次成功是什么时候」。**这条要写进 `CHANGELOG.md`。**

## 随之删除的

- `params` 的那对手工配对的编解码函数，以及 `metric.` / `label.` / `msgparam.` / `limit.` 四个前缀
- 旧的任务表、待落盘集合、落盘 goroutine 与它那把串行锁（若票 07 已判定不再需要）
- 旧的记录与状态之间的双向转换

## 验收

- [ ] `tasks` 表经迁移删除，`user_version` 递增
- [ ] 旧引擎的内存表、落盘链路与 `params` 编解码全部删除，无残留调用点
- [ ] `CHANGELOG.md` 记一条：升级后任务清单从空开始，第一个守护 tick 后自动长回
- [ ] 全量用例继续绿，无任何行为变化
- [ ] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [ ] `go vet ./...`、`golangci-lint run` 无 issue、`gofmt -l` 干净、`check-doc-style.sh` 通过
