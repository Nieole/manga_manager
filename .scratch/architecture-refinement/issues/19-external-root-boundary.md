# 19: 外部库全资料库边界

**What to build:** 外部会话拒绝与任一资料库重叠的路径，页面明确解释冲突并由功能持有传输投影类型。

**Blocked by:** None (can start immediately)。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 106、107、108、109、179。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 相等、祖先及后代重叠均对全部登记资料库检查，沿用平台路径语义。
- [x] 保留访问协调与会话失效，旧计划不能绕过路径/保护变化。
- [x] 外部功能拥有自身类型与完整会话契约，页面和组件消费，不反向定义接口。
- [x] 真实临时路径、API 和外部库界面验证跨库、正常路径与失败分类。

## Comments

### Implementation handoff — 2026-10-28

- `external.Manager` checks all registered roots for equality and both containment directions, using platform path comparison plus resolved existing ancestors. An unresolvable boundary fails closed with a distinct error. Retained sessions capture physical boundaries; preparation, queued admission, scan execution and each transfer operation recheck live paths. Existing Engine coverage and protection admission remain in force.
- The external-library feature owns session, coverage and transfer types and `useLibraryTransfer`; pages and confirmation UI consume these contracts. The external-library consumer renders separate overlap and unresolvable-boundary messages.
- Red/green: `.tmp/ticket19-boundary-red.log` records cross-library roots and stale-session preparation being accepted before the fix; `.tmp/ticket19-boundary-green.log` records the completed real SQLite/path regression passing. Existing normal-path, platform-path, scan, transfer, queue and cancellation regressions remain.
- Actual execution: `.tmp/ticket19-ui.log` contains all 99 frontend files / 934 tests passing; `.tmp/ticket19-ui-final.log` contains 11 final external-library/transfer consumer tests passing. `.tmp/ticket19-ui-build.log` and `.tmp/ticket19-ui-lint.log` record the successful real build and lint. Frontend was unchanged after these gates.
- Final full Go execution: `.tmp/ticket19-resume-full.log` records external 4.695s, diskjobs 5.837s and API 105.657s all passing with `-count=1`.
- Final added HTTP failure-classification assertion passed in `.tmp/ticket19-resume-api.log` (`TestExternal`, 7.731s). Go 1.26.10 final lint returned `0 issues.` in `.tmp/ticket19-resume-lint.log`; `gofmt -l ./cmd ./internal` and `git diff --check` were empty.
- `.tmp/ticket19-final-race.log` records diskjobs race passing (8.527s), while external/API race executables were blocked by Application Control; those blocked executions are not passing evidence. Existing Unix symlink tests skip on Windows; no other-OS runtime execution is claimed.
- Pending integration merge and verification; status remains ready-for-agent until the merger closes the ticket.

### Integration verification (2026-10-28)

- Merged through acfe487bb974f3b7aaccfe54d4303917d08b3b0c after the collection and library-drain integrations. Main-tree full external race passed 15.517s, diskjobs full race passed 15.561s, and combined external/delete/download HTTP race passed 45.801s. Initial external and API race launches were blocked by Application Control; the recorded retries actually executed and passed.
- External/transfer/delete frontend consumers passed 3 files / 16 tests, with real frontend lint and build passing. Final Go 1.26.10 lint reported 0 issues, gofmt emitted no files, and diff whitespace checks passed. Logs: .tmp/merge-wave9-external-owner.log, .tmp/merge-wave9-external-retry.log, .tmp/merge-wave9-external-api-retry.log, .tmp/merge-wave9-external-web.log, .tmp/merge-wave9-web-lint.log, .tmp/merge-wave9-web-build.log and .tmp/merge-wave9-final-lint.log. Platform-specific Unix symlink skips remain explicit; no other-OS execution is inferred.
