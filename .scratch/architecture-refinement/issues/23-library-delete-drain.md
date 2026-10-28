# 23: 删库取消排空与失败补偿

**What to build:** 删除请求取消允许的本库运行并有界等待，失败恢复监听并报告已发生副作用。

**Blocked by:** 22: 空闲删库与占用拒绝。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 79、80、81、82、83、84、85、86、87、88、89。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 锁外取消和等待，仅取消符合真实覆盖的本库工作，不牵连其他库系统运行。
- [x] 已有读取/不可取消工作未退出则冲突，禁止用终态字符串代替访问退出。
- [x] 超时、请求取消及删库失败释放保留并按当前设置恢复监听；已取消工作不自动复活。
- [x] 结果表达提交和取消事实，真实 SQLite、任务屏障和界面验证补偿及必要失效。

## Comments

### Implementation and verification — 2026-10-28

- `LibraryReservation.Drain` uses the existing engine reservation and atomic upgrade, requests cancellation outside the access lock, and waits against actual claims. Cancellation eligibility combines task identity with the complete actual library coverage; library-local series runs are eligible, shared/global/system and noncancelable work are not. A released reservation cannot control newly admitted work.
- `libraries.Service.Delete` closes admission, pauses watching, bounds draining to two seconds, preserves request/failed-cancellation/observed-exit/timeout/commit facts, and restores watching from current settings on every uncommitted outcome before releasing admission. Already canceled work stays canceled. Committed deletion retires the old identity and applies existing cache/statistics/reading-path/derived invalidation even if the request ends after the database commit.
- HTTP returns the typed result on success, conflict, cancellation, and database failure. `DeleteLibraryDialog` consumes partial cancellation and compensation facts, including the explicit notice that canceled runs are not restarted. CHANGELOG and ADR 0013 describe the resulting behavior.
- TDD failures recorded before implementation: local work was not canceled (`.tmp/ticket23-red-drain.log`); a single-library series task was incorrectly ineligible (`ticket23-red-identity.log`); a released reservation canceled new work (`ticket23-red-released.log`); the UI omitted cancellation facts (`ticket23-ui-red.log`). Real temporary SQLite, task-body barriers, and the actual dialog provide the approved seams.
- Actual executions: full API `131.127s` (`ticket23-task-api.log`; its overall command failed only because the separate task executable was blocked); related HTTP `21.813s`; libraries/runtime/relocating `8.579s`/`10.855s`/`4.863s`; final libraries race `17.299s` including commit followed by request cancellation. Final task full suite `1.190s`, final released-reservation/eligibility tests `4.551s`, and their race execution `4.714s` passed after resumption (`ticket23-task-full-resume.log`, `ticket23-task-resume.log`, `ticket23-race-resume.log`).
- Frontend full suite passed 99 files / 934 tests (`ticket23-frontend.log`); actual dialog passed all four consumer tests. Frontend lint/build passed; Go 1.26.10 `golangci-lint` reported `0 issues.` and gofmt output was empty. `git diff --check` passed.
- Execution limit: API race executable remains blocked by Windows Application Control, including the resumed focused retry (`ticket23-api-race-resume-retry.log`). No race result is inferred from that failure; integration should rerun the focused delete HTTP race tests. Ordinary full API and both relevant owner/engine race seams above actually executed.
- Integration status remains ready-for-agent until merger validation and closure.
- Merged integration 82c4e01 into this implementation (7c6f110); resolved only CHANGELOG overlap and retained both collection/publication entries. After-merge actual delete HTTP suite passed 18.812s, Go 1.26.10 lint reported 0 issues, gofmt output remained empty, and diff whitespace validation passed (.tmp/ticket23-api-after-merge.log, ticket23-lint-after-merge.log, ticket23-format-after-merge.log).
