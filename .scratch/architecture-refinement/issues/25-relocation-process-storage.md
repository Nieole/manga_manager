# 25: 迁移持久过程与恢复分工

**What to build:** 迁移明确持久操作、决策、存储适配和配置补偿职责，保留完整恢复与保护体验。

**Blocked by:** None (can start immediately)。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 75、76、136、142、197。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 划清过程状态、SQLite 适配及策略/配置协调，不让上层调用方重新拼配方。
- [x] 路径已提交、效果失败保留独占、配置补偿和恢复先于开放保持。
- [x] 身份、相对路径、异常保护及恢复解除判据保持，不提供文件搬运或猜测合并。
- [x] 既有真实迁移/恢复故障链复验，明确各提交阶段结果并保持运行交接。

## Comments

### 2026-11-02 — implementation / verification handoff

- 接续原有未提交实现，保留全部 dirty work 与失败日志。`relocation` 的私有 SQLite adapters 分别拥有操作日志、记录事实、路径原子提交、恢复保护 CAS 和策略意图；过程保留配置补偿、效果和独占释放的决策。公开构造器兼容既有装配，`RecordFacts` 不暴露 DB/Queries；结构人工核对，不用结构测试代替消费者验收。
- 生产 `application/relocating` 消费 `Execute` / `CommitOutcome`，在运行结果中保留 `paths_committed`、`operation_status` 和 `operation_completed`。真实 SQLite 用例覆盖效果失败后保留独占、配置写失败补偿及重试完成后的阶段事实。`startCheck` 返回独立 bound 值，避免修改任务体捕获的 plan。
- 已核对接续时保存的 red/green：`.tmp/ticket25-resume-summary-red.log` 中两个既有 HTTP 消费者实际失败（样本有记录但 summary 数量为零）；`.tmp/ticket25-resume-summary-green.log` 修复后通过。原因是提取记录事实后又把计数清零；`Check` 保留同一事实查询读到的 SeriesCount/BookCount，只重置派生计数。没有修改 HTTP 期待以掩盖回归。
- 本次实际重跑（`GOTOOLCHAIN=go1.26.10`，共享主树 `.gocache`，本树 `.tmp` 与独立 lint cache）：
  - `go test -count=1 ./...` PASS，API 158.446s；`.tmp/ticket25-handoff-all-test.log`。
  - `go test -race -count=1 ./internal/application/relocating ./internal/application/runtime` PASS，19.254s / 35.672s；`.tmp/ticket25-handoff-owner-race.log`。
  - `go test -race -count=1 -timeout=5m ./internal/api -run 'Relocation|Restoration'` PASS，269.645s；`.tmp/ticket25-handoff-http-race-retry.log`。此前同筛选命令在工具 240s 限制下无结果超时，`.tmp/ticket25-handoff-http-race.log` 保留，不算 PASS，也没有证据将其归为 Application Control。重试前确认本树无遗留测试进程。
  - `golangci-lint run --allow-parallel-runners` PASS，`0 issues.`；`.tmp/ticket25-handoff-lint.log`。
  - `go build ./...` PASS；`.tmp/ticket25-handoff-build.log`。`gofmt -l ./cmd ./internal` 无输出；`.tmp/ticket25-handoff-gofmt.log`。
  - 工作区及完整 baseline `git diff --check badc595541250189586b87e9d1c1c83ac79fd677` 通过，tracked 冲突标记扫描无命中；`.tmp/ticket25-handoff-diff-check.log`。
  - 文档检查器 `bash scripts/check-doc-style.sh` 在 30s 和 120s 工具上限均未完成，无输出，不计 PASS；保留 `.tmp/ticket25-handoff-doc-style.log`、`.tmp/ticket25-handoff-doc-style-retry.log`。本次改动注释已人工核对预算与归属。
- `HEAD` 与最新 `codex/architecture-refinement` 均为 `b913455f22f1b7eff431ffec2e555d1ea41987c1`，无需内容合并。执行环境的上级约束禁止 index/history 修改，因此未创建提交或运行 merge；交给父代理提交及独立 merger 验收，Status 保持 open（ready-for-agent）。`.lintcache/` 保留未跟踪，不纳入交付。
- 无前端、SQL 源或生成契约变化；未执行真实设备、外部供应商或其他平台运行。保留 Commit 兼容入口供现有调用者，生产运行入口已消费 Execute；不搬运实体文件，不改变身份、异常保护或管理员恢复政策。
