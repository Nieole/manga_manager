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

02、09、13 正在独立工作树实施。首批原始日志汇总到本地 `.tmp/evidence/`，已完成工作树正在清理。
