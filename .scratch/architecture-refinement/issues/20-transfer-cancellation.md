# 20: 复制中的取消与提交结果

**What to build:** 外部传输在复制块间响应取消，丢弃未提交暂存并保留已完成文件。

**Blocked by:** None (can start immediately)。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 108、109、110、111、112、113、118、119、120、121。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 完整复制会话持暂存、目标、进度和提交结果；取消停止后续文件。
- [x] 已有目标及复制期间出现的目标不覆盖，未提交暂存不计成功且被清理。
- [x] 部分成功范围、会话刷新及运行状态正确，阻塞单次 IO 限制如实表达。
- [x] 真实文件与可控 IO 屏障验证取消、目标竞争和失败；API/消费者结果可见。

## Comments

### Implementation handoff — 2026-10-28

- `external.Manager.Transfer` owns the prepared-plan copy lifecycle, staging, byte progress, atomic non-overwriting publication and session projection. Cancellation is checked between IO calls and prevents later books. The return value retains committed and existing book IDs independently of failures; cancellation after publication preserves the committed file and result.
- Windows uses `MoveFileEx` without replacement, Linux `RENAME_NOREPLACE`, and Darwin `RENAME_EXCL`; unsupported atomic publication fails rather than falling back to a check-then-overwrite race. A live-boundary check also runs immediately before publication. Cancellation of boundary validation retains the existing session; actual invalidation still removes unsafe sessions.
- The application consumes that complete capability, publishes the committed book IDs and actual counts into the run, emits per-item failures and refreshes the session on cancellation or failure. Uncommitted bytes do not advance successful-book counts. The transfer dialog explains that cancellation awaits the current read/write returning, and completed files remain.
- Red/green evidence: `.tmp/ticket20-run-red.log` captured a transfer finishing as completed/added=1 without observing a mid-book cancellation; `.tmp/ticket20-run-green.log` records cancelled/added=0 and refresh passing. `.tmp/ticket20-aftercommit-red.log` captured the lost cancellation result after a physical commit; its green counterpart retains the commit with cancellation. `.tmp/ticket20-beforecommit-red.log` captured a new registered root bypassing publication safety, fixed in the matching green log. `.tmp/ticket20-session-cancel-red.log` captured cancellation discarding a retained session, fixed with the complete external suite in the matching green log. `.tmp/ticket20-ui-red.log` and its green counterpart cover the visible cancellation contract.
- Real temporary files and SQLite verify mid-book cancellation before all bytes are staged, no further copying after cancellation, preservation of earlier commits, removal of uncommitted staging, a racing destination remaining byte-for-byte unchanged, partial failures retaining the exact successful IDs and coverage, and late cancellation retaining the actual commit. The HTTP regression verifies 202 launch separately from failed run outcome, committed scope and the refreshed session projection; its fixture correction was setup repair, not a product red.
- Actual gates: `.tmp/ticket20-api-full-final.log` records full API passing (134.779s). After merging integration `44f261d`, `.tmp/ticket20-after-merge.log` records external 3.803s, diskjobs 5.503s and related API 8.649s passing, including the new real SQLite HTTP partial-result assertion. `.tmp/ticket20-race.log` records external race 21.058s; `.tmp/ticket20-external-final-race.log` records strengthened cancellation coverage under race 14.845s; `.tmp/ticket20-diskjobs-race-retry.log` records full diskjobs race 14.107s. AppControl-blocked attempts are retained as failed execution attempts, not counted as passes.
- Frontend full suite passed 100 files / 938 tests in `.tmp/ticket20-ui-full.log`; the 13 transfer/dialog consumers passed after merge in `.tmp/ticket20-after-merge-ui.log`. Frontend lint/build passed before and after merge. Go 1.26.10 lint returned `0 issues.` after merge in `.tmp/ticket20-after-merge-lint.log`; gofmt and diff whitespace checks were empty.
- Linux and Darwin external test binaries cross-compiled successfully with `CGO_ENABLED=0` in `.tmp/ticket20-cross-linux-nocgo.log` and `.tmp/ticket20-cross-darwin-nocgo.log`; these are build checks, not other-OS runtime evidence. The initial Linux attempt inherited the Windows CGO compiler and failed; the corrected environment is the passing build. Existing Unix-only symlink/FIFO tests do not execute on Windows.
- Pause/resume offset and IO-quota release are reserved for ticket 21. No forced interruption of a blocked operating-system IO call or cross-process auto-resume is claimed. Pending merger verification and closure; status remains ready-for-agent.
