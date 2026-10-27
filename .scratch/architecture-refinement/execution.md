# 实施执行记录

集成分支：`codex/architecture-refinement`。固定基线：`badc595541250189586b87e9d1c1c83ac79fd677`。

规格与依赖以 [spec.md](spec.md)、[queue.md](queue.md) 和逐票验收为准。只有代码合入、验收满足且验证完成的票才标记 `done`；本记录不替代逐故事联合验收。

## 基线验证（2026-10-26）

- Go 工具链使用 CI 的 `go1.26.10`，共享仓库 `.gocache`，每树独立 lint 缓存。
- `golangci-lint run`：通过，`0 issues.`。
- 前端全量 Vitest：初次 912 通过、1 个超时；定向重跑 1/1 通过，降低并发后全量 94 文件、913/913 通过（`.tmp/refinement-baseline-web.log`）。前端 lint 与 build 通过。
- 后端全量测试：基线全量未通过。Application Control 拦截后，relocating/scanning/logger/organize/seriesmeta 定向重跑通过，metadata 仍被拦截。scanner 的 TestWatcherEventLoopSchedulesScanOnNewFile 停止时阻塞在 fsnotify.Remove，10 分钟超时；定向全包复跑通过（16.765s），留给 28 核对停止协调。原始输出见本地 `.tmp/refinement-baseline-go.log` 与重试日志。

## 实施状态

01、08、10 已合入且通过组合验证并关闭，关闭提交 `e0f7d1b`。组合 reading/proposal/API 完整测试通过（API 117.205s），人工元数据 SQLite/HTTP 定向及前端消费者 31 项通过；最终 gofmt 无输出、lint 0 issues。逐票记录含实际生产消费者和各自全量前端结果。

02 已合入并关闭（`20bc0b5`），主体列表/HTTP 与缓存消费者回归通过；13 已合入并关闭（`1871f05`），organize/runtime、API race、前端状态消费者通过，sqlc 无漂移、格式及 lint 通过。

03、09 已合入并关闭（`b6a701d`）：09 最终 owner 整包在主树实际通过，reading 整包、organize/API 定向及前端 52 项通过，gofmt/lint/diff 检查通过。

当前完成：01、02、03、08、09、10、13。04、14、16 在独立工作树实施。原始集成门禁日志位于主树 `.tmp/merge-*.log`，已完成工作树的日志汇总到 `.tmp/evidence/`；01、02、08、10 工作树已注销。10 残留空目录的删除被自动审批以 blocked by policy 拒绝，不影响分支或源文件。
