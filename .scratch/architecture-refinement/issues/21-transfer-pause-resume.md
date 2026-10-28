# 21: 块边界暂停与安全续拷

**What to build:** 在复制会话上实现块边界暂停，释放可再取得的 IO 配额并经资格核对恢复同一文件。

**Blocked by:** 20: 复制中的取消与提交结果。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 114、115、116、117、118、121、122、196。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 暂停保留偏移与暂存但不计成功，释放配额使其他读取可推进。
- [x] 恢复核对源和目标，无法确认安全则失败并清理本次暂存，保留原有成果。
- [x] 取消可唤醒暂停并归还资源，不新增进程重启自动续传。
- [x] 真实文件及运行控制/消费者回归验证暂停、变化、恢复与取消交错。

## Comments

### Implementation handoff — 2026-10-28

- `Manager.Transfer` retains the current file handles, copied offset and staging identity across block-boundary pause. Reads and writes use separate short `TransferWork` leases, supplied by the existing run handle; same-volume copies never nest leases and cross-volume directions cannot hold one lease while acquiring the other. The transfer stream shares the configured hash/stream concurrency budget without counting copies as identity hashes.
- Each continuation checks source identity, size and modification time, staging identity/size/modification time, physical destination parent and the live session/library boundary. Unsafe continuation returns a failure and removes only this copy's owned staging file. Physical staging paths keep cleanup attached to the original parent if an alias changes. Existing destinations and earlier committed files remain protected by the atomic non-overwriting publication introduced in ticket 20.
- `Runner.Do` also handles pause arriving while quota acquisition is queued: a newly granted lease is returned before waiting for resume, then reacquired. It never waits on a task pause while retaining that quota. Cancellation wakes paused or queued work through the existing context; no new restart registration, persisted copy plan or cross-process continuation is introduced.
- Real-file red/green evidence: `.tmp/ticket21-pause-red.log` captured copying from 1 MiB to 2 MiB despite pause, fixed in `.tmp/ticket21-pause-green.log`. `.tmp/ticket21-queued-pause-red.log` captured an actual source read after pause arrived while quota was queued; the fixed path passed in the complete external race suite and final pause regression. `.tmp/ticket21-staging-time-red.log` captured continuation after staging modification, then passed with staging fact validation. `.tmp/ticket21-ui-red.log` and `.tmp/ticket21-ui-green.log` cover the visible pause/resume/restart contract.
- Real SQLite/files, PauseGate and actual Scheduler leases verify stable staging offsets, same-file resumed contents, zero held quota during pause and a reader actually reading a book while the copy is paused. Source size/time, staging size/time and a newly registered target root reject unsafe continuation; paused cancellation wakes and cleans staging. The final queued-pause/file-guard regressions passed 2.405s in `.tmp/ticket21-final-pause.log`.
- The real Engine consumer regression uses `Pause`, `Resume`, `Cancel`, snapshots and terminal results with a previously committed first book. Safe resume commits the second book; changed-source failure and cancellation preserve only the first book's exact committed scope and coverage. It passed under race 8.102s in `.tmp/ticket21-run-control-race.log`; after integration, exact committed-ID equality for all three outcomes passed 6.075s in `.tmp/ticket21-final-run-scope.log`. Its first fixture attempt used a throttled published frame as a barrier; the fixture was corrected through the pre-existing controllable clock seam, not counted as a product red.
- Actual backend gates: full API passed 122.239s in `.tmp/ticket21-api-full.log`; full external race passed 34.446s and storageio race 5.633s in `.tmp/ticket21-final-race.log`; diskwork race passed 5.452s in `.tmp/ticket21-owners-race.log`; full diskjobs ordinary passed 4.556s and taskcontrol 3.740s in `.tmp/ticket21-owners-final.log`. AppControl-blocked packages in mixed commands are explicitly not passing evidence.
- After merging integration `ff58410`, `.tmp/ticket21-after-merge.log` records relevant external 5.654s, diskjobs 5.736s, diskwork 4.398s and API 8.004s actually passing. Its storageio executable was blocked; that unchanged package had actually passed the full race suite above. Go 1.26.10 lint returned `0 issues.` after merge in `.tmp/ticket21-after-merge-lint.log`; gofmt and diff whitespace checks were empty.
- Frontend after-merge full suite passed 101 files / 942 tests in `.tmp/ticket21-after-merge-ui-full.log`, including pause → running → completed coverage consumption and prior failure/cancellation recovery. After-merge frontend lint and real build passed. The transfer dialog explains retained progress, safety checks, cancellation latency and the absence of automatic continuation after restart.
- A final merge also incorporated ticket 26. `.tmp/ticket21-last-integration.log` records related external 2.744s, diskjobs 1.942s and API 8.013s actually passing afterward; `.tmp/ticket21-last-integration-lint.log` records `0 issues.`.
- No Linux/Darwin runtime execution or forced interruption of a blocked single OS IO call is claimed. Existing Unix-only fixtures remain platform-qualified. Pending merger verification and closure; status stays ready-for-agent.
