# 06 — `taskstore` 实现落盘端口

**What to build:** 新建落盘包，建起新表并实现票 05 定义的端口。**准入从此由数据库保证**：
两条部分唯一索引分别兜住「同一任务只有一次活动运行」与「最多一条排队中的运行」，而不再靠内存表
判定。旧 `tasks` 表本票一行不动。

**Blocked by:** 05

**Status:** done

## 表形状（形状来自本次盘问，故内联；不含类型细节与索引全集）

```
tasks     id, type, scope, scope_id, variant,
          disabled, last_success_at, fail_streak, backoff_until
          UNIQUE(type, scope, scope_id, variant)

runs      id, task_id, trigger, nth_run, status,
          phase, current_item, current, total,
          paused_at, control_paused_ms, coalesced_count,
          message_code, error, started_at, updated_at, finished_at, sequence
          UNIQUE(task_id) WHERE status IN ('running','paused','cancelling')
          UNIQUE(task_id) WHERE status = 'queued'

run_events    run_id, at, kind, payload      kind ∈ {phase, item, control, warn}
run_samples   run_id, at, current, rate_per_minute
run_metrics   run_id, key, value
run_limits    run_id, + 11 列并发上限
run_args      run_id, key, value
```

**不留 JSON 堆**（ADR 0004）。今天 `params` 里挤着的五类语义各回各家。代价是新增一个上限字段
要改 schema——这是明知的交换。

## 迁移与排序

- 走仓里既有的 `PRAGMA user_version` 迁移机制，先例是 KOReader 账号与书签用户隔离那两次。
- **排序主键取序号，不要取时间列。** 时间列今天由两个写入方写成两种文本格式，而 SQLite 比的是
  文本：同一秒的两种写法既比不出相等、短的还总排在前面。新表沿用单调序号，不要退回时间列。

## 本票要测的（也只测这些）

只测**只有 SQL 才答得出**的东西，规则类的判定属于票 05：

- 两条部分唯一索引真的拒绝了第二条活动运行、第二条排队运行
- 同一任务可以有任意多条**终态**运行（这正是「重试不覆盖历史」的底座）
- 事件、采样、指标、上限、入参随运行级联删除
- 分层保留的 DELETE 选中的正是该选的行：每任务留最近 20 次、终态 ≤90 天，
  **活动态与排队中永不被选中**
- 指标的聚合查询能回答「这个任务最近十次运行的平均耗时」

## 验收

- [x] 六张新表建起，迁移经 `user_version` 落地，旧 `tasks` 表未被改动
- [x] 两条部分唯一索引各有一条用例证明它拒绝了第二条
- [x] 级联删除有用例；保留 DELETE 的选中集合有用例，含「活动态与排队中不被选中」
- [x] 聚合查询有用例
- [x] 排序主键是序号，不是时间列
- [x] 用例对真 SQLite 跑（临时目录建库），先例见 `internal/database` 既有用例
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [x] 若改动 `sql/query.sql` 或 schema，已在 PowerShell 下重跑 `sqlc generate` 并确认产物一致
- [x] `go vet ./...`、`golangci-lint run` 无 issue、`gofmt -l` 干净、`check-doc-style.sh` 通过
