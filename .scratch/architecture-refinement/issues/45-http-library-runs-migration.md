# 45: 资料库与运行磁盘 HTTP 迁移

**What to build:** 资料库、扫描、任务控制、传输、回写和迁移路由通过所需能力运行，HTTP 只适配结果。

**Blocked by:** 18: ComicInfo 回写的安全发布；19: 外部库全资料库边界；21: 块边界暂停与安全续拷；23: 删库取消排空与失败补偿；25: 迁移持久过程与恢复分工；28: 文件监听与安全清理协调；37: 重启声明与有损合并事件；42: 独立装配根与新宿主扩展。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 4、5、8、9、13、72、90、109、123、125、152、161、163。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 广泛宿主迁移批次。

- [ ] 使用已完成访问/文件/扫描/声明及运行投影能力，不复制运行体或取消等待配方。
- [ ] 各类维护、清理、刮削及磁盘入口按同一职责判据迁移或明确保留技术职责。
- [ ] 恢复与真实运行结果、上下文日志、公开控制语义和权限保持。
- [ ] 真实应用、HTTP/协议及相关页面回归通过，旧宿主仅保留未迁家庭兼容。

## Comments


### Implementation evidence — 2026-11-02 (uncommitted; keep open)

- Tree `C:/Users/saber/dev/manga-refinement-45`, branch `codex/refinement-45`, inspected clean at `da6811b`; no commits, merges, index/history mutation, or main-tree edits. The base already contains 42/25. Integration and ticket closure remain with the parent; 37's declaration work and the separately owned restoration slotLimit/scanner-cleanup fixes are not duplicated or suppressed here.
- `Host` binds independent library/scan, maintenance/scrape, disk/ComicInfo/external, relocation/restoration, storage-diagnostics, run-control/history, and dedup-removal adapters. Each receives application capabilities and its own limited query/write projection, never a Controller, Runtime or full database.Store. Production routes address these adapters directly. No second Engine, Manager, auth, SSE broker, run projection or query cache is created. Binding no longer mirrors the library application graph onto Controller.
- Existing run admission vs activation, cancellation, restart lookup, event/stage/sample projection, library protection, archive publication, transfer and removal partial outcomes are unchanged. Directory browsing and derived-initial maintenance remain technical operations but now have independent adapter receivers. Dedup removal retains its existing bounded record/file workflow and partial counters; this is a dependency migration, not a new deletion algorithm. Cover/page/cache technical routes stay with reader/config families (43/46); franchise rebuilding, AI grouping and single-series metadata proposal handling stay with organization (44). Three KOReader *maintenance launch* handlers migrate here, not configuration/accounts/protocol semantics (46).
- Red → green at the approved real HTTP seam: a bound Host with compatibility feature references removed first panicked in `Controller.startLibraryScanRun` (`.tmp/45-red.log`); the bound-family implementation passed (`.tmp/45-green.log`). The retained regression additionally removes Controller's run projection/external/relocation references and checks authenticated+CSRF requests, paused queue admission, queued cancellation projected back through HTTP, events/samples, library listing, ComicInfo export/write admission, storage diagnostics, search-index maintenance, restoration readiness/conflict and external-session admission. Existing real SQLite/file/HTTP regressions now route through the new families as well.

Verification (Go environment: `GOTOOLCHAIN=go1.26.10`, `GOCACHE=C:/Users/saber/dev/manga_manager/.gocache`; final checks use this tree's `.tmp/go` and `.tmp/lint`):

- `go test ./... -timeout 240s`: PASS, API 133.886s; `.tmp/45-go-all-final.log`. First attempt failed only because ignored `web/dist` was absent (`.tmp/45-go-all.log`); copied main's ignored built assets into this tree before the successful rerun.
- `go test -race ./internal/api -run 'HostLibrary|Library|Relocation|External|ComicInfo|Task|Run|Scan|StorageIO|Maintenance|Thumbnails|FileIdentit|Dedup|RemoveBooks|Scrape' -timeout 480s -count=1`: PASS, 416.384s; `.tmp/45-api-focused-race.log`. Includes restoration and declaration/retry regressions without editing their semantics.
- `go test -race ./internal/bootstrap ./internal/application/... ./internal/task ./internal/taskstore ./internal/diskwork ./internal/external ./internal/bookcontent ./internal/relocation -timeout 180s -count=1`: PASS (relocation has no standalone test files); `.tmp/45-owners-race.log`. Bootstrap 11.437s, libraries 22.085s, relocating 10.203s, scanning 28.821s, diskjobs 17.308s, external 31.603s. No whole-repository race-pass claim: the initial whole-repo race invocation exceeded the 600s tool budget (`.tmp/45-race-all.log`); a whole API retry with a too-short 240s package limit also timed out while running unrelated dashboard tests (`.tmp/45-api-race.log`). CI uses 30m for bcrypt/SQLite race suites. These failures/timeouts are retained, not hidden or treated as passes.
- `golangci-lint run --allow-parallel-runners`: PASS, `0 issues.`; `.tmp/45-lint.log`. Parallel-runner flag avoids other tickets' global linter lock; own cache retained. `gofmt -l` on changed/new Go files and `git diff --check`: clean. `go build ./...`: PASS; `.tmp/45-build.log`.
- Optional `bash scripts/check-doc-style.sh` exceeded the 180s tool budget before producing a report (`.tmp/45-doc-style.log`); its own surviving script process was stopped. Not claimed as passed. Final linter result above is the completed independent run; the command chained after this optional script did not run.
- `go run ./cmd/tsgen`: 26 types, no `web/src/api/generated.ts` drift; `.tmp/45-tsgen.log`. No JSON/TS/SQL schema contract changes.
- Related frontend consumers via Node v24.15.0 and this tree's linked existing dependency install: Vitest 11 files / 171 tests PASS (library form/management/delete, external transfer, relocation entry/modal, task observation/center/bubbles); `.tmp/45-web-consumers.log`. ESLint and `tsc -b && vite build` pass (`.tmp/45-web-lint.log`, `.tmp/45-web-build.log`). No frontend production changes.

Conflict sites / 47 obligations:

- Shared surgical edits: `api/host.go` family binding + Host field; `api/controller.go` `setupRoutes` signature and migrated route receiver expressions plus `getLibraries` receiver; `bootstrap/bootstrap.go` four narrow storage bindings. Reconcile these with 43/44/46 rather than restoring Controller delegation or mirroring feature state.
- Receiver-only overlaps: `controller_system_config.go:browseDirs`, `controller_recommendations.go:rebuildInitials`, `scrape_controller.go:batchScrapeAllSeries/scrapeLibrary`, `koreader_controller.go` three maintenance launches; `library_access.go` shared content/read guard extraction. No changes to the neighboring owners' decisions.
- 47 must remove `library_http_compat.go` and `Controller.httpFamilies()` after converting retained direct-method/constructor fixtures, then retire the old constructor/runtime-assembly fields and unused LibraryDependencies scanner/watcher/disk-work compatibility inputs. `taskEngine` remains the one shared transport projection; its legacy runtime/dispatch/testing fields are not cleaned up in 45. Reader cover writes still need the Scanner capability in 43, organization AI launch still needs Intelligence in 44; account/config/protocol binding and HTTP leaf ownership finish in 46/47. Do not create a replacement full-application transport object during contraction.
