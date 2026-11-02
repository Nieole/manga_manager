# 47: 删除旧宿主与全量依赖兼容面

**What to build:** 所有路由完成迁移后删除旧生产构造入口、服务镜像与转发桥，完成 Controller 收敛。

**Blocked by:** 43: 阅读与目录 HTTP 依赖迁移；44: 元数据与藏书编排 HTTP 迁移；45: 资料库与运行磁盘 HTTP 迁移；46: 配置账户与管理适配迁移。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 1、2、3、4、5、6、7、8、9、10、11、12、13、14、44、151、152、163、202。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 广泛宿主迁移收缩。

- [x] 确认全部路由/协议/后台生产消费者后收缩，不能遗漏普通用户或管理尾部入口。
- [x] 不再由 HTTP 构造/恢复应用，无全量应用对象或 Store 换名传递。
- [x] DTO 与合理辅助按功能保留；仍有用途的故障/协议测试支持明确隔离于生产。
- [x] 服务构建、所有受影响回归和契约漂移通过，不能只报告删行收益。

## Comments


### 2026-11-02 contraction phase 1 — incomplete, uncommitted, review required

Tree: `C:/Users/saber/dev/manga-refinement-47`, branch `codex/refinement-47`, integration base `274424040a8503f065b7a47ca0593b801f1a4118`. Only this tree was changed. No commit, staging, merge, reset, clean, push or main-tree changes. 45/46 are integrated at this base; their earlier pending-family notes above are historical.

Delivered scope (NOT completion of 47):

- Removed exported production `api.NewController`; its sole actual consumer was the persisted-interruption regression. That regression now lives in `bootstrap/recovery_test.go` and uses the real task admission engine with a deterministic non-executing scheduler, closes/reopens SQLite, then calls production `bootstrap.Open` and authenticated Host HTTP. It checks the original run identity, interruption message and retained retry eligibility. No direct SQL seeding or new mirrored application fixture.
- Removed `taskEngine.runtime` and its Manager-policy fallback. Production and retained core assembly already bind `configSnapshot` plus the restart registry. Five retained SQLite recovery callers now explicitly call their application fixture's `RecoverRuns`; deleted the redundant test-only interruption/resume recipe in `task_recovery_adapter_test.go`. Historical display fallbacks, replay inputs and isolated dispatch/clock/scheduling seams are unchanged.
- Added a production `bootstrap.Open` + real HTTP server live `/api/events` shutdown witness in `bootstrap/sse_shutdown_test.go`. It authenticates through Host, reads the initial SSE frame, holds an ordinary HTTP request behind a barrier, and starts `Process.Shutdown`. Both SSE handler return and clean client EOF must occur before permitting HTTP drain. Engine cancellation and resource retirement are forbidden while the ordinary request remains active and required after drain. No sleeps or private broker access.
- Mutation red/green: temporarily removing the runtime's pre-drain `StopEvents` call makes that exact production consumer fail at the live-stream barrier (5s); restored production code passes 10 repetitions. The mutation was restored byte-for-byte and is not in the diff. This verifies existing lifecycle wiring rather than introducing new shutdown semantics.

Reinventory / remaining REQUIRED work (ticket stays open, every original acceptance box remains unchecked):

1. `Transport.legacy`, `Host.compatibility`, the production `Controller` type, `newControllerCore` and `assembleRuntime` still exist. The broad API fixture and relocation reopen fixtures still consume the core assembly. They have NOT been migrated to deliberate narrow family assembly; no claim of sole production composition is made for these retained source entrypoints.
2. `reader_http_compat.go`, `library_http_compat.go`, `administration_compat.go`, `Controller.httpFamilies()` and reader fallback reconstruction remain. Host still delegates route composition through Controller. The concrete relation/proposal assertions in `Bind` and full Store/mirrored services remain. These are not accepted final architecture.
3. The inspected production route body still consumes Controller for cover writes, catalogue tag/custom-field/bulk-favorite/open-directory operations, pages/thumbnail/cache/content transfer, SSE and scrape/provider tails. These need independent narrow owners and explicit single gated chi subtree composition, preserving protocol callbacks and content/read leases. Do not rename the residual graph to Host or move it wholesale into a test facade.
4. Actual callers checked beyond symbol search: server main uses `bootstrap.Open` and Process shutdown; Host Bind and all Setup methods; core/runtime construction; reader fallback binding; administration assembly; family composition; catalogue/cover guards; recovery callers and deterministic seed helper. This is a phase-specific deep read, NOT an exhaustive final consumer/ownership audit.

Actual gates on delivered source:

- `GOTOOLCHAIN=go1.26.10`, shared main `.gocache`, own `.tmp/go` and `.tmp/lint`. Final uncached `go test ./... -count=1 -timeout=10m` PASS (`.tmp/47-go-all-final.log`).
- Complete API (not a filtered shard), bootstrap, runtime, task, taskstore and bookcontent race PASS: `go test -race ./internal/api ./internal/bootstrap ./internal/application/runtime ./internal/task ./internal/taskstore ./internal/bookcontent -count=1 -timeout=20m`; API 617.426s, bootstrap 25.079s (`.tmp/47-api-lifecycle-race-final.log`). Covers actual family/auth/protocol/recovery consumers without timeout-as-green. Earlier full API race also passed 642.944s; retained separately.
- Final gofmt/diff-check empty, lint `0 issues.`, build PASS, tsgen 26 types and no generated.ts drift: `.tmp/47-{format,lint,build,tsgen,ts-drift}-final.log`. No schema/SQL/DTO/frontend implementation changes.
- Copied ignored main dist/dependencies to this tree. Node v24.21.0: full split frontend PASS, threads 106 files/1008 tests (`.tmp/47-web-threads.log`) plus forks heatmap 1 file/16 tests (`.tmp/47-web-heatmap.log`); ESLint and tsc/Vite build PASS (`.tmp/47-web-{lint,build}.log`). Total 107 files/1024 tests.
- SSE original pass, expected mutation failure, restored ×10: `.tmp/47-sse-{initial,red-missing-stop-events,green}.log`. Initial new recovery test compilation failed on an incorrect handle type and was corrected; original failure remains `.tmp/47-bootstrap-recovery-initial.log`, actual pass `.tmp/47-bootstrap-recovery-green.log`. Final constructor-exit regression `.tmp/47-constructor-exit-green.log` PASS. No test expectations were weakened.

Limitations: whole-repository race, doc-style script, real devices/providers and non-Windows runtime verification were not executed here. This tree is a compilable, verified partial contraction, not an all-47 ready-for-closure tree. Independent review and the remaining implementation above are required.

### 2026-11-02 continuation — implementation complete, UNCOMMITTED, pending independent review

The phase-1 remaining implementation list above is superseded by this continuation. All four acceptance items are implemented and verified in `C:/Users/saber/dev/manga-refinement-47`, branch `codex/refinement-47`, still at base `274424040a8503f065b7a47ca0593b801f1a4118`. Acceptance ticks describe this worktree, not shipment: **keep ticket OPEN pending independent review/integration**. No main-source edits, index/history operations, commits or merges. Inherited dirty changes and `.tmp/47-*` evidence remain; `.tmp/47-contract-baseline.diff` records the inherited diff.

Completed contraction / actual caller audit:

- `host.go` constructs technical leaves without a Store argument, Runtime pointer or application graph mirror. External provider client factories are independent of feature queries. Bind installs the single run projection, shared cache/content effects, adapters and protocol callbacks before bootstrap recovery/opening. Coverage closures capture only the narrow series query, not complete assembly inputs.
- `Host.SetupRoutes` in `host_routes.go` directly composes all four families under one `/api` authentication gate and shared series/books/tags subtrees. OPDS/Mihon share the bound organization, content and Basic-auth adapters; KOReader protocol routes belong to administration. The ordered 81 explicit method/path/subtree registrations in the main composition match the integrated base; mounted-family routes retain their existing owners.
- Tails have independent owners: `catalogMaintenanceHTTP` for tags/custom fields/bulk favorite/open-directory; `coverHTTP` for custom covers; `contentHTTP` for pages, thumbnails/covers, page cache and whole-book transport; `providerHTTP` for search/scrape/queueing; `sseBroker` for filtered events. Inputs are narrow `ContentQueries`, `CatalogueMaintenanceStore`, `ProposalQueue` and existing capabilities. Content leases, DTOs, private-path filtering and persisted display/replay fallbacks remain.
- Deleted production Controller/core/runtime HTTP assembly, concrete relation/proposal extraction, `reader_http_compat.go`, `library_http_compat.go`, `administration_compat.go`, `runtime_application.go`, `Controller.httpFamilies` and reader fallback reconstruction. Production API has no full Store field/parameter or Runtime pointer; bootstrap is the sole production `runtime.New` caller. Checked real consumers: server startup/shutdown, bootstrap binding/recovery/start, all Host Setup methods, family/protocol mounts and callbacks, source/cache effects, run/admission/coverage, cover/catalogue/provider tails, reopen/fault/scheduling fixtures. Evidence: `.tmp/47-contract-caller-audit.log`, `.tmp/47-contract-injection-{inventory.log,review.diff}`.
- `_test.go`-only `newAPIFixture` explicitly assembles application resources and narrow Host ports, not compatibility-forwarding handlers. SQLite/crash/recovery/reopen and controlled clock/dispatch/scheduler coverage remains. Callers invoke owning adapters; faults target consumed reader queries/context/search, account use-case/session ports, source resolvers or explicitly rebound resources. Config changes retain the same Manager; Host family pointers are not duplicated as stale fixture mirrors.

Behavioral red/green:

- New production-Open `bootstrap/tail_routes_test.go` crosses real PNG/CBZ bytes and session/CSRF/role gates, catalogue writes/queries, custom fields/tags, cover page/upload, pages/file/thumbnail ETag, page cache, organization/run/library coexistence and non-admin path filtering. Omitting the real bulk-favorite registration fails with POST 405 (`.tmp/47-contract-tail-red.log`); restored routes pass ten repetitions (`.tmp/47-contract-tail-green.log`). Mutation absent from diff.
- Phase-1 production SQLite reopen/interruption and live-SSE/ordinary-drain shutdown witnesses remain and pass final races. Original StopEvents mutation evidence is untouched.
- First complete API race exposed the transfer test treating file visibility as completion before session coverage refresh (no race-detector warning): `.tmp/47-contract-api-race.log`, API 757.758s, failure retained. The test now awaits that admitted run's terminal outcome before retaining both file and complete-coverage assertions; no expected result was weakened. Twenty race repetitions pass (`.tmp/47-contract-transfer-race-green.log`). Subsequent entire API runs pass; final-source gate below is authoritative.

Final gates (this continuation, not borrowed phase-1 results):

- Go **1.26.10**, shared content-addressed Go cache, own `.tmp/go` and `.tmp/lint`.
- `go test ./... -count=1 -timeout=10m` PASS, API **105.258s**, bootstrap **3.662s**: `.tmp/47-contract-verified-go-all.log`.
- **Entire API**, uncached: `go test -race ./internal/api -count=1 -timeout=20m` PASS, **638.934s**: `.tmp/47-contract-verified-api-race.log` and matching exit 0 `.exit`. Not a filtered shard or timeout-as-success. Earlier complete passes (756.459s, 686.808s) are separately retained.
- `go test -race ./internal/bootstrap ./internal/application/runtime ./internal/task ./internal/taskstore ./internal/bookcontent -count=1 -timeout=5m` PASS, **30.973s/13.398s/2.190s/8.423s/2.347s**: `.tmp/47-contract-verified-lifecycle-race.log`. Final focused family/auth/recovery/capture races also pass: `.tmp/47-contract-narrow-capture-race.log`.
- gofmt/diff-check empty, Go lint **0 issues**, `go build ./...` PASS: `.tmp/47-contract-verified-{format,diff-check,lint,build}.log`.
- `go run ./cmd/tsgen` **26 types**, generated.ts diff empty: `.tmp/47-contract-verified-{tsgen,ts-drift}.log`. No SQL/schema/DTO or frontend implementation changes.
- Node **v24.22.1**, full split frontend PASS: threads excluding `src/utils/heatmap.test.ts`, **106 files/1008 tests**, plus forks heatmap **1 file/16 tests**, total **107 files/1024 tests**. `.tmp/47-contract-web-{threads,heatmap}-final.log`; ESLint and tsc/Vite build PASS: `.tmp/47-contract-web-{lint,build}.log`. Initial wrong exclusion reproduced timezone-sensitive heatmap failure in threads; that failure is retained, not counted green.

Limitations / review obligations: optional whole-repository doc-style script exceeded the 120s tool deadline without output (`.tmp/47-contract-doc-style-timeout.log`); **not** claimed as passed. Whole-repository race, non-Windows runtime, real devices and live metadata providers were not exercised. Remaining material implementation work for 47: **none identified**. Independent review of the broad caller/fixture migration and integration remain mandatory; do not close automatically or treat checked worktree acceptance as shipment.


### 2026-11-02 independent combination preparation — approved dirty tree, OPEN pending parent integration

- Independent review of the actual complete dirty tree was reported approved with no findings by the delegating parent. This supersedes the continuation's pending-review wording; it does not constitute shipment. All inherited tracked/untracked implementation and prior `.tmp/47-*` evidence were preserved. No git index/history mutations, merge/reset/clean/abort, main-tree edits, push or deletion were performed.
- Latest main `3d82d84` differs from this tree's base `2744240` **only** in ticket 43–46 closure records and two execution-summary paragraphs. No production/test/frontend source delta exists to combine. Execution now includes main's combined-gate evidence and current **46/48 closed, 01–46** facts, retaining the phase-1/continuation history. 47 is implementation-complete, reviewed and open pending parent integration; 48 is next after that integration. Ticket 43–46 files themselves remain untouched here: parent integration must take their main versions.
- Semantic combination audit PASS (`.tmp/merge47-semantic-audit.log`): sole production `runtime.New` in bootstrap; no production API full Store/Runtime/Controller/core assembly; ordered 81 explicit route registrations unchanged. Manual binding/fixture inspection confirms one auth gate and shared auth/SSE/cache/run projection, narrow protocol callbacks and consumed-port injection; Bind precedes recovery/opening and SSE ends before ordinary HTTP drain, cancellation and retirement after drain. No implementation adjustment identified.
- Fresh combined focused race PASS with Go **1.26.10**, main `.gocache`, own `.tmp/go` and `.tmp/lint`: bootstrap **26.600s**, API **29.141s**, uncached `-count=1 -timeout=4m`. Selection covers real production Host families/tails, auth/Basic/password/CSRF, config publication, catalogue atomicity, shared admission, persisted recovery and SSE/drain consumers. Exact invocation/results: `.tmp/merge47-focused-race-command.log`, `.tmp/merge47-focused-race.log`, exit 0 `.exit`. This is a focused gate, not a fresh complete API race.
- Fresh gofmt empty and diff-check clean; lint **0 issues**; `go build ./...` PASS; tsgen **26 types**, generated.ts byte-identical: `.tmp/merge47-{format-diff,lint,build,tsgen,ts-drift}.log`. An initial execution-document CRLF write was caught by diff-check and corrected to LF; original diagnostic retained as `.tmp/merge47-format-diff-initial-crlf.log`.
- Source is unchanged from reviewed final gates: full Go PASS, **complete API race 638.934s**, lifecycle races, frontend **107 files/1024 tests**, frontend lint/build and generated-contract checks remain applicable (`.tmp/47-contract-verified-*`, `.tmp/47-contract-web-*`). No unnecessary full-suite repeat. Existing doc-style timeout, whole-repository race, other-platform runtime and live device/provider limitations remain unclaimed; 48 owns joint acceptance, not missing 47 implementation.
- Parent finalization: commit the entire inherited dirty tracked/untracked 47 implementation plus these doc reconciliations; integrate with main `3d82d84`, preserve main ticket 43–46 closures and this reconciled execution summary/history. The execution paragraph changed on both sides is the only predicted overlapping region; retain the reconciled 46/48 facts and all 47 evidence. Parent owns commits/merge/closure. Keep 47 open until integrated, then close on acceptance and proceed to 48. No material implementation work remains identified.
