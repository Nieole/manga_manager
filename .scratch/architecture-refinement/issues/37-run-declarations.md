# 37: 重启声明与有损合并事件

**What to build:** 完整落地输入/标签分离，让每次有损合并可追查，重启按行为输入执行。

**Blocked by:** None (can start immediately)。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 156、157、158、159、160、161、164。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 逐启动点区分重启输入、环境标签、上限、指标和文案参数，保持行为恢复。
- [x] 每次不同重启入参合并记事件，无损仍首次降噪，不改变先排运行执行选择。
- [x] 发起方不夹带行为，任务身份、槽位、IO 限额及上下文日志保持。
- [x] 任务内存/落盘、真实启动/恢复和观测消费者验证全部迁移，不留两套声明规则。

## Comments

### Implementation / resumed evidence (2026-11-02; left open)

Worktree `C:/Users/saber/dev/manga-refinement-37`, branch `codex/refinement-37`, source `16cfc9f`. The interrupted substantial dirty implementation was inspected and retained, not replaced. No commit, index/history mutation, merge, cleanup, or integration-tree edit was performed. Status remains **ready-for-agent** pending owner integration/acceptance; unchecked acceptance items above are not a claim of missing implementation.

Approved seams exercised: task Engine with memory store; Engine/taskstore with real temporary SQLite; feature Restart and runtime recovery registry; existing HTTP history/events/retry projections; TaskCenter and external-library live/reconnect consumers. Existing red logs were retained (`37-red-events.log`, `37-red-cover.log`); the web red attempt was a missing-npm command failure, **not** a behavioral red. The resumed registry regression is additional coverage, not a claimed new red/green cycle.

Implementation scope (all remains uncommitted):

- `internal/task/start.go`, `events_test.go`: compare persisted queued Args with incoming Args (nil/empty equivalent); retain first lossless event and **every** lossy event, JSON detail includes incoming trigger/args/labels/limits/lossy. Preserve queued declaration, trigger, body, limits, identity and scheduling. Memory seam covers lossless label-only repetition before repeated lossy input.
- `internal/taskstore/read_back_test.go`: real SQLite consumer reads discarded declarations via a second Engine/store adapter and verifies selected first body, immutable original Args/Labels and coalesced count. No SQL/schema change.
- `internal/application/scanning/{scanning,covers,restart}.go` and tests: force is replay input; environment is already in Limits; cover `backfill` is explicit and replayed, independent of Trigger. Absent old input retains legacy true; explicit false cannot discover work after process loss. Cover behavior regression checks missing covers, not only field names.
- `internal/application/runtime/runtime_test.go`: resumed run loaded from durable task history through production Registry/RecoverRuns retains explicit false **and** true discovery inputs. Existing scan registry recovery retains force. No runtime/controller/host production wiring changed.
- `internal/application/maintenance/jobs.go`, `maintenance_test.go`: foreground fingerprint rebuild consumes match_mode only; extension is a label there. Reconcile and matching refresh consume both match_mode and path_ignore_extension; both remain Args. Low-priority backfill consumes reason; fixed variant behavior/profile/mode is descriptive. IO counts/times only metrics; cross-library work does not claim a single disk. Existing SQLite calibration/reopen test exercises original matching options and reading facts.
- `internal/application/diskjobs/{comicinfo,diskjobs,transfer}.go`: remove redundant ComicInfo series input (scope owns identity); external session/count are labels, not restart inputs. Relocation already declares plan/operation as labels and was retained.
- `internal/api/{comicinfo_task,controller,external_tasks,koreader_tasks,maintenance_tasks,scan_coalesce,task_identity,task_retry_dispatch}_test.go`: migrate real observation/retry expectations, retain behavioral coverage, HTTP event projection exposes every discarded force declaration. No production HTTP family/controller/host edits.
- `web/src/components/tasks/TaskCenter{,.test}.tsx`, `web/src/i18n/task.ts`: consume Limits/Labels with old Params fallback; KOReader display combines Args and Labels; event detail displays discarded declaration. Cross-library metrics remain visible without fabricated disk identity.
- `web/src/features/external-library/useExternalLibrary.ts`, `useExternalLibrary.test.tsx`: live terminal and reconnect recovery match session Labels, retaining old Params fallback and committed-result behavior.
- `CONTEXT.md`, `docs/adr/0008-inputs-are-not-labels.md`, `CHANGELOG.md`: authoritative vocabulary, implemented declaration/compatibility contract, user-facing correction. Resume also corrected the obsolete glossary example that implied recovery always enables cover discovery.

Restart-read audit: scanning library/series force; covers backfill; intelligence scrape provider+locale and grouping locale; maintenance foreground match_mode, reconciliation/refresh match_mode+path_ignore_extension, backfill reason. Index/thumbnail/cleanup/file-identity maintenance has no replay input. `maintenance.Restart` parses matching fields before dispatch but only the relevant feature body consumes them; irrelevant old fields cannot change fixed backfill behavior. Scope/variant encode identity, Trigger records initiator, StartParams/MessageParams carry wording/results, environment is Labels or Limits and IO accumulators are Metrics. No new second declaration registry or HTTP response shape was introduced.

Fresh resumed checks (Go `GOTOOLCHAIN=go1.26.10`, shared `manga_manager/.gocache`, this tree's `.tmp` and lint cache):

- `go test ./... -count=1 -timeout=8m`: **PASS**, `.tmp/37-resume-go-full.log`, EXIT=0. Final `go test ./... -timeout=8m` after registry regression: **PASS**, `.tmp/37-resume-go-final.log`, API 140.822s, runtime 3.628s, EXIT=0.
- `go test -race ./internal/task ./internal/taskstore ./internal/application/{scanning,maintenance,runtime,intelligence,diskjobs,relocating} -count=1 -timeout=4m`: **PASS**, `.tmp/37-resume-race.log`, all eight packages actually ran (2.081s–30.980s), EXIT=0.
- Focused affected HTTP event/declaration/retry/external consumers, `go test -race ./internal/api -run 'Test(ForcedRescan|OnlyAManualForced|WriteComicInfoDeclaration|RebuildThumbnailsTask|KOReaderTasksCarry|RebuildBookHashes|HashProgressFrame|RebuildFileIdentitiesCompletes|BackfillRetry|External|TransferExternal|Retry|Resume|TaskIdentity)' -count=1 -timeout=3m`: **PASS**, 26.187s, `.tmp/37-resume-api-race.log`, EXIT=0.
- Full API race attempt `go test -race ./internal/api -count=1 -timeout=6m`: **NOT PASS**, package timeout 360.829s, `.tmp/37-resume-api-full-race.log`, EXIT=1. Alarm reported `TestDeleteLibraryHTTPPreservesCancellationFactsOnEveryOutcome/request-cancel`; captured stacks include fixture SQLite FTS migration and fsnotify shutdown. No DATA RACE report or assertion failure preceded timeout. Root cause not established or fixed in 37. Earlier `.tmp/37-race-final.log` timed out at `TestLibraryExclusiveSeesSeriesAndSystemWork/series`; both are retained, not hidden by focused success.
- `gofmt -l ./cmd ./internal`: empty output (`37-resume-gofmt.log`); `golangci-lint run`: **0 issues**, `37-resume-lint.log`; `go build ./...`: **PASS**, `37-resume-build.log`.
- `go run ./cmd/tsgen`: generated 26 types, **no generated.ts diff**, `37-resume-tsgen.log`. No transport struct/schema migration.
- Frontend equivalent package scripts via this tree's `.tmp/node.exe` (npm absent from shell PATH): `node node_modules/vitest/vitest.mjs run --maxWorkers=2`: **104 files / 960 tests PASS**, `37-resume-web-full.log`; ESLint **PASS**, `37-resume-web-lint.log`; `tsc -b` + `vite build` **PASS**, `37-resume-web-build.log`, all EXIT=0. Build regenerated ignored web/dist locally (not counted as source).
- `git diff --check`: **PASS**. `bash scripts/check-doc-style.sh`: completed with “文档风格检查通过。” and informational historical-word hints, `.tmp/37-resume-doc-style.log` (slow command outlived initial tool timeout; complete output subsequently inspected).

Historical logs remain exact: `37-go-full-final.log` contains pre-migration API assertion failures; `37-targeted.log` an outdated maintenance assertion; `37-sql-maintenance.log` a missing RunBackground test-fixture panic subsequently corrected. `37-go-full-pass.log`, `37-race-full.log` and frontend logs record earlier passing runs but are not substituted for the fresh outcomes above. Real external devices/providers were not exercised; no cross-platform runtime claim.

Integration/47 follow-up:

- Inspected `da6811b` (#42 bootstrap) **read-only**, including bootstrap.Open, host run dependencies, taskEngine Registry dispatch and runtime construction changes. Registry source is identical to this source base; restart maps pass unchanged through production Registry. #42 does not alter the affected feature RunSpec declarations or DTO shape. This is source compatibility inspection, **not** a merged integration build/test: no #42 changes were consumed into this worktree.
- Main was independently observed at `0b7e2e7` during this session; it was not edited. Other 43–46 family/controller/host wiring is untouched. Owner must integrate 37 with #42/current main and run the new bootstrap seam/combined gates before closing.
- #45 should preserve Args->Params and Labels/Limits projection and string Event.Detail through its HTTP family migration, including external-session terminal/reconnect correlation. #47 should remove only truly unused old Controller/dispatch/seed compatibility after consumer migration; retain legacy persisted Params display fallback and missing-backfill compatibility unless an explicit data migration replaces them. Preserve the single production Registry and no Trigger-derived cover behavior. Full API race timeout remains a visible integration follow-up, not an acceptance pass.
