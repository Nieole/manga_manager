# 06: 书签与时长的完整命令

**What to build:** 书签与时长通过主体明确的功能接口处理，调用者无需知道存储形状或兼容统计步骤。

**Blocked by:** 01: 单书读者视图与导航。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 24、30、31、39、43。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 书签主体、书范围、页码约束及无阅读副作用保持，时长活跃性及上限保持。
- [x] 只收窄实际使用的数据能力，不把软统计改为事件账本或盲目重试。
- [x] 前端阅读器和 HTTP 实际消费完整接口，切书/身份变化不串写。
- [x] SQLite 和消费者回归验证，不为已清晰技术辅助额外造领域层。

## Comments


### Implementation evidence

- `reading.Bookmarks.List/Save/Delete` and `reading.TimeReport` require explicit `reading.Subject`; missing subjects fail before storage, while explicit anonymous bookmark scope and anonymous time no-op remain compatible. `reading.Bookmark` owns the public note result without exposing generated persistence types to HTTP callers; JSON fields and identity remain unchanged.
- Time reuses `AccumulateReadingTimeForBook` from ticket 05: receipt-day lifetime/daily counters commit together, seconds remain capped at 3600, and time failures remain independent of already committed positions. Removed the unused `AddUserBookReadingTime` business bypass after auditing all callers; its test seeds use the explicit technical adapter.
- Application-session activity guards prevent identity teardown from sending old seconds with a later session. Normal book switches still settle the old book. Bookmark generations reject stale loads, save/delete completions and retained write callbacks, including A -> B -> A navigation. Device/offline ownership reconciliation is unchanged; no retry or event ledger was added.
- TDD raw evidence: `.tmp/ticket06-red.log` (explicit-subject public command boundary fails to compile against old interface), `.tmp/ticket06-green.log` (SQLite command, bookmark and time invariants pass), `.tmp/ticket06-frontend-red.log` (old time cleanup submits 10 prior-reader seconds), `.tmp/ticket06-frontend-green.log`, `.tmp/ticket06-bookmarks-red.log` (late old bookmark overwrites returned-book state), `.tmp/ticket06-bookmarks-green.log`.
- CI toolchain Go 1.26.10: `go test -race ./internal/reading` passed (`.tmp/ticket06-race.log`); focused actual HTTP bookmark/time/invalid-subject tests passed (`.tmp/ticket06-http.log`). Full `go test ./...` passed all packages except API and runtimecfg test executable launches blocked by Windows Application Control (`.tmp/ticket06-go-tests.log`); these launch failures are not pass evidence. Only those packages were rerun: `go test ./internal/api ./internal/runtimecfg` passed (API 92.215s, runtimecfg 4.026s; `.tmp/ticket06-blocked-packages-retry.log`). Every Go package therefore has actual pass evidence; the initial full command itself failed due to the two launch blocks.
- Frontend `npm run test -- --maxWorkers=4`: 98 files / 929 tests passed (`.tmp/ticket06-frontend-tests.log`); `npm run build` and `npm run lint` passed. Final targeted reader/session consumer rerun: 4 files / 15 tests passed, including retained old write callbacks (`.tmp/ticket06-consumers-final.log`). Final `golangci-lint run --allow-parallel-runners`: `0 issues.` (`.tmp/ticket06-lint-final.log`); gofmt and diff whitespace checks clean.
- Based on integration `6b5e8f7` with ancestry verified; latest integration `a8109c5` merged before handoff. Status remains ready-for-agent until integration verification.
