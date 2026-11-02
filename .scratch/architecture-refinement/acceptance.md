# Integrated acceptance — ticket 48 (OPEN)

## Target, provenance and acceptance discipline

- Target: `C:/Users/saber/dev/manga-refinement-48`, branch `codex/refinement-48`, HEAD `46c20cbc936c664eae6c246cb20ed59c1ac1f9e5`, tracked tree `813d019e7fc785ea2868e83f3f6d7f8df11cb348` (01–47 integrated/closed). No production changes in 48. Initial retry status had only the untracked partial `acceptance-evidence/`; **all prior `.tmp` artifacts and raw logs remain**, including failed cross-builds and doc timeouts. The initial retry did not reset, clean, stage or commit. The parent subsequently staged the acceptance paths, but its cached whitespace check failed; no commit was created. The evidence-integrity repair leaves the index untouched for parent restaging.
- Ground truth: AGENTS, CONTEXT, agent issue/domain/doc-style guides, architecture spec/design/queue/execution, issue 48 and ADRs 0001/0003/0005–0015. The external inventory is preserved as [input only](acceptance-evidence/inventory-input.md), not accepted evidence: it describes a pre-46/47 source snapshot and explicitly did not execute tests. All current row paths/symbols were checked; three inherited frontend suite/template pointers were replaced by concrete executed cases. The original partial mapping is retained as `stories-inherited.json`.
- Current gates below were completed by the interrupted 48 attempt on the same HEAD with no tracked source changes; retry inspected commands, logs and exit files, not just filenames. Fresh retry re-executed all 1024 frontend tests with JSON case results. Source digest and retired-boundary/import audit: [structure-audit.json](acceptance-evidence/structure-audit.json). `manifest.json` hashes retained evidence. Documentation-only reconciliation does not change tested production bytes.
- **Four distinct states:** observed source / test exists / actual scoped execution PASS / unexecuted or failed limitation. The matrix supplies source + consumer + actual executed witness + manual review; it never promotes ticket done, presence, package compilation, cross-compilation or a representative regression to complete story acceptance. Packages with no test files (accounts and several query owners) obtain behavioral evidence through real SQLite HTTP/use-case consumers, not package compilation.
- 48 stays **OPEN**. This artifact precedes the independent two-axis review: (1) behavior/spec/evidence and (2) architecture/ownership/current documentation. Neither independent review is claimed here. Findings must be fixed and reviewed before closure. A missing predecessor feature goes back to its originating ticket with a concrete reproducer; 48 does not implement it.

## Executed gates and exact evidence

All links below are relative to [acceptance-evidence/](acceptance-evidence/). For a gate `name`, `name.command` is the exact invocation, `name.log` the captured output in canonical LF text (not necessarily byte-identical raw capture; see [canonicalization](acceptance-evidence/README.md#text-canonicalization-and-snapshot-identity)), and `name.exit` its captured exit. Scripts preserve working directory/env. Go used `GOTOOLCHAIN=go1.26.10`, shared main `.gocache`, own `.tmp/go` and `.tmp/lint`. Frontend used Node 24.15.0 and the existing ignored dependency copy; build generated this tree's ignored dist. No dependency upgrades.

| Gate | Observed result / scope | Evidence stems |
|---|---|---|
| Complete ordinary Go | PASS, uncached `go test -count=1 -timeout=30m ./...`; API 105.595s, scanner 17.104s | `go-all` |
| Complete Go race | PASS, uncached `go test -race -count=1 -timeout=30m ./...`; **complete API 723.912s**, plus all applicable packages (stronger than selected package shards) | `race-all` |
| Per-story exact Go witnesses | PASS; JSON has exact top-level `Action=pass` for every Go symbol in 204-row mapping, plus production routed SSE shutdown witness | `story-tests`, `story-command.json` |
| vet / formatting / lint / server build | PASS; `gofmt -l ./cmd ./internal` empty, lint **0 issues.**, native CGO Windows build | `vet`, `format`, `lint`, `build` |
| TS generation / drift | PASS; **26 types**, diff empty; final generated paths porcelain empty (also catches untracked outputs) | `tsgen`, `ts-drift`, `generated-status` |
| SQL generation / drift | PASS; PowerShell sqlc v1.31.1, nonfatal parser diagnostics retained, diff and final porcelain empty | `sqlc`, `sql-drift`, `generated-status` |
| Frontend full split | PASS **106 files / 1008** thread tests + **1 file / 16** heatmap fork tests = **107 files / 1024**. Original logs retained; fresh retry JSON identifies executed cases | `web-tests`, `web-heatmap`; `web-tests-json`, `web-heatmap-json`, `web-results.json`, `heatmap-results.json` |
| Frontend lint / TS / build | PASS, ESLint, `tsc -b`, Vite build; warnings retained | `web-lint`, `web-tsc`, `web-build` |
| Vulnerability scan | PASS, govulncheck v1.6.0 (database timestamp/toolchain in environment log); not a timeless security assurance | `vuln`, `environment` |
| Documentation checker | Original **exit 124 at 180s**, NOT green. Bounded traced retry **exit 0**, no blocking findings; history-word advisories retained | `doc-style`, `doc-style-retry` |
| CI-platform server cross-build attempts | **FAILED**, Linux/amd64 CGO: host MinGW lacks target POSIX headers/types; Darwin/arm64 CGO: `clang` unavailable. No target sysroot/compiler installed; do not substitute CGO-disabled release binary | `linux-build`, `darwin-build` |
| Historical worktree diff / generated status | PASS for then-tracked paths, empty outputs; original `diff-check` missed then-untracked evidence. Artifact committability requires full-file checks and parent restaging/cached check, not this historical gate | `diff-check`, `generated-status` |

Historical evidence is separate: `../manga-refinement-47/.tmp/47-contract-verified-*` records full ordinary Go and **complete API race 638.934s**, with `47-contract-web-*-final.log` recording 1024 frontend tests; `../manga-refinement-47/.tmp/merge47-*` records focused combination gates. These belong to the 47 worktree's dirty contraction over base `2744240`, later reconciled against docs-only `3d82d84`, **not a run on 48 HEAD**. They corroborate only; current-tree full Go/race/1024 evidence supersedes them. Historical files remain in their owning tree.

## Cross-feature joins / what witnesses establish

- **Subject → SQL → protocol/cache:** explicit `Subject` rejects zero/negative identities rather than silently selecting anonymous; catalogue binds before filter/count/order/page. R/Q/W rows join real SQLite reading and OPDS/Mihon consumers; unknown-page completion remains separate. Cache/session scope is manually checked, not inferred from one response.
- **Transaction → committed effects:** W/M/P/ACC join narrow atomic stores with real SQLite rollback/concurrency fixtures. Manual edit's required stats/read is atomic; bulk post-stats and position activity are soft warnings. Proposal rechecks pending/locks/fields, preserving prior partial commits. FX regressions verify failed authoritative refresh warns, invalidates known commits in finally, and read retry does not repeat adjudication.
- **Durable derived restart:** F uses persisted global desired/applied versions, transactional fact/cascade marking, stable identities, bounded retry and reopen tests. Older failure cannot overwrite newer success. Stale success vs first unavailable vs truly empty are separately consumed. RT/MOVE inspect recovery before background/HTTP opening, not just in-memory notifications.
- **Admission → drain → actual exit:** L/B join queue reservation, task identity plus actual cross-library coverage, cancellation outside Engine lock, real body/read-handle exit, locked upgrade and release/watcher compensation. Completed snapshot alone is not drain. Other libraries continue; cancelled work is not replayed.
- **Pause/cancel → physical publication → next reader:** X checks real staged copies, block boundaries, quota return, source/staging/parent revalidation and atomic no-clobber. PUB/B cover active borrows vs eviction, original-change/timeout protection, Windows rename, committed-file warning and next HTTP content read after fact-store failure. These are real temporary files, not all media/platforms.
- **Configuration → one assembly → shutdown:** CFG/RT/A bind the same Manager/Engine, content/cache and auth/SSE leaves before recovery/inputs. `bootstrap.TestShutdownClosesLiveHostSSEBeforeDrainingHTTPAndRetiringResources` passed in JSON, ordinary and race suites: authenticated routed SSE closes before real HTTP drain; ordinary request blocks retirement. Content/download lifetime is a separate B witness, not one all-in-one device scenario.
- **Auth → offline owner → late results:** ACC/OFF join real SQLite/protocol tests with AuthProvider/offline consumers. Expiry/network failure retain data; logout releases, new user reconciles first, original fact time/ID survives retry/late ACK. Browser mocks + backend fact/reopen regressions are a contract join, **not an executed physical-browser-to-restarted-server/device end-to-end test**.

## Majority-region disposition and old authority exit

The 25 capability records cover principal business/application/HTTP/frontend regions, with critical or selected reads labelled. Broader indexing/import/declaration searches are separate; generated files are audited through SQL/TS sources and drift, not read as independent rules. This is majority **region** coverage, not every file deeply read.

| Additional region | Disposition / review scope / execution |
|---|---|
| database / sql / taskstore | Retain SQLite/sqlc/private atomic adapters; query-shaped reads need no universal mapping. Real database/taskstore and consumer suites executed ordinary + race; regeneration clean. No full generated-code deep-read claim. |
| parser / images / bookcontent / runtimecfg | Retain format/size/memory guards and borrow/permit return to original resource generation; content owns cache eligibility/publication. Critical resource reads plus full ordinary/race; non-Windows file behavior unexecuted. |
| storageio / diskwork / taskcontrol / runhandle | Retain technical IO budget, pause gate and run-qualified work; no domain wrapping. Consumer boundary review plus full ordinary/race, not real-disk latency testing. |
| booksort / seriesmeta / seriescontext / catalogsearch / recommendation / dashboard | Retain purposeful sorting, source-specific policy and query/cache projections. Named consumers and actual API/scanner/owner tests; owners without local tests not counted as tested by compilation. |
| logger / task / taskstore / application runs | Retain identity, run state, durable history, event/sample/log distinctions. Context task/run logs and ADR 0008 startup/replay ownership retained; package and HTTP regressions executed. |
| external / koreader / metadata | Adjusted narrow capabilities, neutral identity evidence and root boundary; preserve protocol account/document-key semantics and normalization. Fixtures executed, live suppliers/devices not contacted. |
| frontend features/session/auth/pages/Layout | Adjusted offline/settings/library/task/metadata effects; keep local selection/scroll/dialog/draft state. All 1024 consumers/routes/pages/utilities executed, including stale/error/scope fences; not visual browser acceptance. |
| frontend theme / i18n / shared presentation, tooling | Retain technical/presentation responsibilities; indexed unchanged-boundary disposition, not deep business review. Relevant frontend tests, lint and build execute; redesign outside scope. |
| cmd/server / bootstrap / API tails / generators | Critical assembly/route/auth/lifecycle read; only bootstrap constructs runtime. Four families plus content/catalogue/cover/provider/SSE tails share one gate; API importers are composition/generation roots. Native build, full API/bootstrap and contracts executed. |

Exit check: no production `Controller`/`NewController` declarations, no production `*_compat*` Go files, sole `runtime.New` in bootstrap; API importers limited to cmd/server, cmd/tsgen, bootstrap (`structure-audit.json`). Manual Host/Transport review confirms no full Store/Runtime mirrors, capability family reconstruction or universal facade; reading public mutable projection/old bypass contracts retired per ADR 0010 and reading tests. Searches alone do not prove absence of all semantic duplication. Source-specific seriesmeta, anonymous protocol and ordered-list reread differences are deliberate retained rules.

Current docs: AGENTS already describes resulting ownership; ADRs 0008–0013/0015 record implemented boundaries. ADR 0014 and CONTEXT had stale 47 pending/uncommitted status; current wording corrected, historical ADR/execution entries retained. CHANGELOG already covers proposal/repair/drain/publication/pause/config/auth/offline/refresh corrections; no new user behavior in 48, so no invented changelog entry.

## Limitations, gaps and review handoff

- **No predecessor implementation defect newly established by this run.** Not proof none exists. Route any independent finding plus fixture/reproducer to its row's Origin (05/06 facts, 10 proposal, 13/14 repair, 18/20/21 files, 23 drain, 30/31 resources, 33 offline, 34 effects, 38–40 accounts, 42/47 assembly). Do not implement it inside 48.
- **US201 limited, not all CI green:** native Windows passes, Linux/macOS CGO cross-builds failed for missing target toolchains. Remote Ubuntu/macOS CI runtime/race and platform symlink/rename not executed here. Cross-build is not runtime validation. Windows skip branches are not Unix coverage.
- Doc-style diagnosis: per-file shell/head/awk/grep forks make MSYS slow. Original 180s timeout retained; bounded `timeout 600 bash -x scripts/check-doc-style.sh` reached end, **exit 0**. Trace retains progress, history-word advisories and actual final success. No historical code comments changed to manufacture green.
- Real KOReader/Mihon devices, real providers/credentials, physical-browser service worker/storage, removable/FAT/network media and OS-blocked IO interruption **not exercised**. Deterministic fixtures and Windows temporary files have narrower scope. Cross-process external resume explicitly not promised.
- Representative witnesses sometimes shared across structural stories. Source, consumer, execution and manual mapping cover **204/204**, but independent behavior/spec/evidence and architecture review remain pending. No closed-story count inferred from test count; 48 stays open until findings/limitations receive parent review disposition.

## Capability / manual structure catalogue

Each M-key resolves implementation **and actual production consumer** symbols for the matrix. “Critical” means the named high-risk boundary was read; “selected” is boundary/consumer inspection, not every file. These manual findings do not follow from passing tests.

### M-A

Implementation: `internal/bootstrap/bootstrap.go::Open`; `internal/api/host.go::Bind`; `internal/api/host_routes.go::SetupRoutes`

Consumer: `cmd/server/main.go::main`

Changed/critical read: sole production assembly; four bound families plus content/catalogue/cover/provider/SSE tails share the same leaves. Full Store only at assembly, not an HTTP field; no new universal facade. Source search and import audit supplement, not substitute, consumer tests.

### M-R

Implementation: `internal/reading/book_view.go::Subject`; `internal/reading/book_view.go::BookView`; `internal/reading/queries.go::Continue`

Consumer: `internal/api/reader_http.go::currentReadingSubject`

Changed/critical read: zero/negative subject is rejected, explicit anonymous remains separate. Facts are not mutable user projections; unknown completion does not invent page counts. Reader/OPDS/Mihon use the same bound queries.

### M-Q

Implementation: `internal/reading/catalogue_view.go::Search`; `internal/database/series_search.go::SearchSeriesPaged`

Consumer: `internal/api/reader_http.go::ReaderCapabilities`; `web/src/pages/library/hooks/useLibrarySeries.ts::useLibrarySeries`

Changed/critical read: subject enters SeriesListFilters before SQL filtering/count/order/page; retains query-shaped rows and batch SQL rather than per-book entity hydration. Cache invalidation is application-bound; browser scope includes reader/session.

### M-W

Implementation: `internal/reading/position.go::RecordPosition`; `internal/reading/mark.go::MarkBooks`; `internal/reading/bookmarks.go::Save`

Consumer: `internal/api/reader_http.go::ReaderCapabilities`; `web/src/reading/useReadStateMutation.ts::useReadStateMutation`

Changed/critical read: atomic write port exposes no DB/Queries escape. Committed book IDs survive later batch failure; soft activity error does not undo position. Original times/fact IDs and persisted clear basis survive retries/reopen; bookmark/time subject stays explicit.

### M-M

Implementation: `internal/application/seriesediting/manual.go::Save`; `internal/application/seriesediting/selection.go::Edit`; `internal/seriesmeta/manual.go::SaveManual`

Consumer: `internal/api/organization_http.go::MetadataEditor`; `web/src/features/series-metadata/useSeriesEdit.ts::useSeriesEdit`

Changed/critical read: manual save includes required stats/read in the narrow transaction; absent vs empty collections remain distinct. Selection commits metadata atomically but later statistics are warnings. Source-specific seriesmeta policies remain separate.

### M-P

Implementation: `internal/proposal/resolve.go::Apply`; `internal/proposal/resolve.go::Reject`

Consumer: `internal/api/metadata_review_controller.go`; `web/src/pages/series-detail/hooks/useSeriesMetadataReview.ts::useSeriesMetadataReview`

Changed/critical read: transaction rechecks pending state and current fields/locks/values, not just preflight. Partial fields/provenance roll back together; prior partial commit survives reject. Frontend waits authoritative reload, then finally publishes known commit; query retry never repeats adjudication.

### M-C

Implementation: `internal/organize/collections.go::AddMembers`; `internal/organize/smart_snapshot.go::Snapshot`; `internal/organize/reading_lists.go::Reorder`

Consumer: `internal/api/organization_http.go::ReadingListsCapability`; `web/src/pages/ReadingLists.tsx`

Changed/selected read: continuously derived members reject manual edits; snapshot/AI output stays manually owned. Ordered writes use a narrow transaction and preserve item identity; all-read list fallback differs intentionally from ordinary continue reading.

### M-AI

Implementation: `internal/organize/ai_grouping.go::ApplyCollection`; `internal/organize/ai_grouping.go::RejectReview`

Consumer: `internal/api/organization_http.go::GroupingCapability`; `web/src/pages/AIGroupingReviewInbox.tsx`

Changed/selected read: backend returns authoritative candidate and whole-review outcome. Existing applied members survive remaining rejection; frontend merges rather than guessing terminal state and retains selection/scroll.

### M-F

Implementation: `internal/organize/franchise_state.go::RepairDue`; `internal/organize/franchise.go::Rebuild`; `internal/application/runtime/lifecycle.go::repairFranchises`

Consumer: `internal/api/organization_http.go::RelationsCapability`; `web/src/features/organize/FranchiseRepairStatus.tsx::FranchiseRepairStatus`

Changed/critical read: global desired/applied versions, not per-library graph; facts/cascade triggers mark in the same transaction. Stable identities and version guarded completion/failure prevent old attempts overwriting new success. Reopen/backoff and first-unavailable vs stale-success are separate witnesses; retry retains durable responsibility.

### M-L

Implementation: `internal/application/libraries/delete.go::Delete`; `internal/task/library_reservation.go::ReserveLibrary`; `internal/task/library_drain.go::Drain`

Consumer: `internal/api/library_http.go`; `web/src/features/libraries/useLibraryManagement.ts::useLibraryManagement`

Changed/critical read: reserve closes starts/reads/queued admission, cancellation outside Engine lock, actual coverage and task identity determine eligibility. Upgrade checks real occupiers under same lock; defer releases/resumes queue and restores current watcher after failure. Cancellations are not automatically replayed. Create/update distinguish save from scan/watch/navigation effects.

### M-B

Implementation: `internal/bookcontent/file.go::ReadHandle`; `internal/bookcontent/page.go::Page`; `internal/bookcontent/source.go::List`

Consumer: `internal/api/content_http.go`; `internal/api/reader_http.go::serveBookFile`

Changed/critical read: same content source/cache/lease supports page and whole-book/protocol reads. Close ends file lease; materialized page bytes release archive borrow. Processing/pass-through and cache eligibility remain distinct; parser/images retain technical format, size and memory guards.

### M-PUB

Implementation: `internal/bookcontent/publication.go::Publish`; `internal/parser/comicinfo_writeback.go::PrepareComicInfo`; `internal/application/diskjobs/comicinfo.go::WriteComicInfoPrepared`

Consumer: `internal/api/library_http.go`; `internal/api/comicinfo_publication_test.go::TestComicInfoCommittedFileSurvivesFactStoreFailure`

Changed/critical read: stage first, return own source handle, bounded final borrow exclusion/revalidation, then replacement. File commit is recorded before bounded uncancelled fact/cache handoff. Warning is not write failure; the HTTP regression checks the next source read, not only a toast/run status.

### M-X

Implementation: `internal/external/copy.go::copyFile`; `internal/external/transfer.go::Transfer`; `internal/application/diskjobs/transfer.go::TransferPrepared`

Consumer: `internal/api/library_http.go`; `web/src/features/external-library/useExternalLibrary.ts::useExternalLibrary`

Changed/critical read: every registered library boundary is checked; each copy block passes pause and short IO lease. Resume checks source/staging identity, bytes/time, physical parent and destination eligibility. Atomic missing-target publication never clobbers a concurrent target. Committed IDs precede projection failure; cancellation removes only owned staging. No cross-process resume or forced OS IO interruption promised.

### M-S

Implementation: `internal/scanner/ingestion_writer.go::persist`; `internal/scanner/ingestion_coordinator.go::scanFinished`; `internal/application/scanning/scanning.go::ScanLibrary`

Consumer: `internal/application/runtime/lifecycle.go::watchLibraries`; `internal/api/library_http.go`

Changed/selected read: ingestion owns discovery/identity/commit, successful reassociation effects occur after commit. Watcher coordinates actual scan completion/generation before cleanup, failed/uncertain work requeues; stop joins handed-off work. Scanner remains outside protocol ownership; asynchronous covers retain their own run.

### M-I

Implementation: `internal/bookidentity/evidence.go::FingerprintFile`; `internal/bookidentity/rebuild.go::Rebuild`

Consumer: `internal/scanner/ingestion.go`; `internal/koreader/service.go::SaveProgress`

Changed/selected read: neutral byte/path evidence consumed by scan/maintenance, device document key and account remain protocol concerns. Retain size+mtime ambiguity and matching algorithms; evidence equality is not permission to merge book identity.

### M-MOVE

Implementation: `internal/application/relocating/service.go::Recover`; `internal/application/relocating/runs.go::Confirm`

Consumer: `internal/api/library_http.go::relocationHTTP`; `internal/application/runtime/lifecycle.go::Start`

Changed/critical boundary read: persistent domain owns path/compensation/protection; application owns Engine exclusivity and watcher/cache handoff. Recover precedes input opening. Post-path-commit effects are not a fictitious database rollback.

### M-CFG

Implementation: `internal/application/configuration/publication.go::SaveExpected`; `internal/application/configuration/publication.go::Reload`; `internal/config/effects.go::FieldEffects`

Consumer: `internal/api/administration.go::AdministrationStore`; `internal/bootstrap/bootstrap.go::Open`

Changed/critical read: API and reload share Manager but retain separate compensation; startup facts identify active database and pending restart. Published is not all fields live. Fixed operation snapshots and FieldEffects retain next-operation semantics.

### M-V

Implementation: `internal/metadata/provider.go::Fetcher`; `internal/metadata/provider.go::Searcher`; `internal/application/intelligence/intelligence.go::Sources`

Consumer: `internal/api/tail_http.go::providerHTTP`; `internal/application/intelligence/grouping.go::GroupLibrary`

Changed/selected boundary review: consumers request fetch/search/group/connection capabilities, not the provider superset. Adapters retain upstream format, paging, retry, cancellation and redaction; deterministic provider fixtures are not real-provider acceptance.

### M-RT

Implementation: `internal/bootstrap/bootstrap.go::Open`; `internal/application/runtime/lifecycle.go::Start`; `internal/application/runtime/lifecycle.go::Shutdown`

Consumer: `cmd/server/main.go::main`; `internal/api/host.go::Lifecycle`

Changed/critical read: bind leaves before Start; Recover and resources/RecoverRuns before background. StopEvents precedes HTTP drain; Close joins task/watcher/background before resources retire. Runtime is an assembly result, not a handler field. Real routed SSE shutdown is a separate final-tree witness.

### M-T

Implementation: `internal/task/start.go::Start`; `internal/task/start.go::coalesceLocked`; `internal/application/scanning/restart.go::Restarts`

Consumer: `internal/api/task_model.go::taskFilters`; `web/src/features/task-observation/useTaskObservation.ts::useTaskObservation`

Changed/selected boundary review: input Args, environmental Labels, Limits and metrics remain distinct. Lossy coalesces record each discarded input, lossless first-event suppression remains; restart does not infer behavior from trigger. Task/store/HTTP projections and context logs remain separate.

### M-OFF

Implementation: `web/src/features/offline/offlineReader.ts::queueOfflineProgress`; `web/src/features/offline/offlineReader.ts::reconcileOfflineOwner`; `web/src/features/offline/offlineSession.ts::endOfflineSession`

Consumer: `web/src/auth/AuthProvider.tsx::AuthProvider`; `web/src/pages/OfflineShelf.tsx::OfflineShelf`

Changed/critical consumer read: expiry/network failure clear auth but not owned bytes/queue; explicit logout uploads while authorized then releases ownership even on failure. New owner reconciles before new CSRF. Stable fact/time survives ACK/retry; browser mocks are not physical-device storage proof.

### M-EDIT

Implementation: `web/src/features/settings-editor/EditSession.ts::EditSession`; `web/src/features/koreader-settings/KOReaderProvider.tsx::KOReaderProvider`

Consumer: `web/src/features/settings-editor/SettingsProvider.tsx::SettingsProvider`

Changed/critical read: each partition retains baseline/version, prepare merges only that section with published server state, dirty receive does not rebase silently, acknowledgement retains edits after send. KOReader diagnostic request scope is separate from draft lifecycle.

### M-FX

Implementation: `web/src/session/ApplicationSessionProvider.tsx::useLibraryInvalidation`; `web/src/pages/series-detail/hooks/useSeriesMetadataReview.ts::useSeriesMetadataReview`

Consumer: `web/src/pages/series-detail/hooks/useSeriesContext.ts::useSeriesContext`; `web/src/reading/useReadStateMutation.ts::useReadStateMutation`

Changed/critical consumer read: plain query/retry does not assert mutation. Known commit invalidation happens in finally after local authoritative reload settles, including refresh failure/navigation. Scope fencing protects the new view without suppressing shared effects of an old commit.

### M-UI

Implementation: `web/src/pages/Dashboard.tsx::Dashboard`; `web/src/pages/Stats.tsx::Stats`

Consumer: `web/src/App.tsx::App`; `web/src/session/ApplicationSessionProvider.tsx::ApplicationSessionProvider`

Changed/selected consumer review: unavailable/first failure, actual empty/zero and stale success remain separate. Identity/target and request generation fence late replies. Supplementary panel degradation does not pretend a required query succeeded.

### M-ACC

Implementation: `internal/accounts/initialization.go::Setup`; `internal/accounts/management.go::Update`; `internal/accounts/management.go::Delete`; `internal/accounts/password.go::Change`

Consumer: `internal/api/administration.go::administrationHTTP`; `internal/api/auth_controller.go::userFromContext`; `web/src/auth/AuthProvider.tsx::AuthProvider`

Changed/critical read: fresh actor and last-admin guard inside private write capability, self-delete forbidden and usernames byte exact. Password/session revocation commit precedes cache invalidation; replacement browser session can fail independently. Cookie/CSRF/rate/protocol auth stays transport owned.

## Every-story evidence matrix

**204/204 mapped**, with executed regression witnesses; this is not 204 independently proven or accepted stories. Read each row together with its M-key above, the cross-feature joins and gate/limitation tables. Go references resolve to `story-tests.command` + JSON `story-tests.log` (`Action=pass`, exact package/Test); every selected top-level symbol actually passed. Web references resolve to `web-tests-json.command` + `web-results.json` (exact file/title/status). Full suites and race evidence are additional gates, not inferred symbol execution. Structural/delivery stories need manual/artifact evidence in addition to their representative behavioral witness. Original tickets are routing pointers for a future reproducer, not evidence of passing.

| Story / approved intent | Implementation + consumer + manual check | Actually executed regression symbols/cases | Scope / originating tickets |
|---|---|---|---|
| **US001** 作为维护者，我希望主要后端与前端功能都纳入审查，以便本轮改善覆盖整个项目而不只围绕 Controller。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 47. |
| **US002** 作为维护者，我希望审查区分文件索引、关键路径阅读和实际运行验证，以便能够判断结论的证据范围。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 47. |
| **US003** 作为维护者，我希望每个主要区域都有保留、调整或需验证的明确处置，以便未修改文件不被误认为遗漏。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 47. |
| **US004** 作为维护者，我希望业务规则、状态、事务和生命周期各有明确所有者，以便修改知识集中在所属能力。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 43, 44, 45, 46, 47. |
| **US005** 作为调用者，我希望通过完整用例表达意图，以便不必自己组合校验、写入、聚合与后续动作。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 43, 44, 45, 46, 47. |
| **US006** 作为维护者，我希望领域概念、查询视图、编辑输入、传输契约和持久化记录按含义区分，以便结构体归属可以从职责判断。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 43, 44, 47. |
| **US007** 作为维护者，我希望合理的技术辅助保留清晰职责，以便不会为采用 DDD 而增加无收益的领域包装。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 43, 44, 47. |
| **US008** 作为维护者，我希望模块通过所需能力建立依赖，以便不会把全站应用对象或全量 Store 变成默认入口。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 42, 43, 44, 45, 46, 47. |
| **US009** 作为维护者，我希望业务模块不反向依赖 HTTP 或页面，以便同一功能可以由不同输入和测试调用。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 42, 43, 44, 45, 46, 47. |
| **US010** 作为维护者，我希望读写各有合适的接口，以便写入一致性与查询效率都能得到保证。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 41, 43, 44, 47. |
| **US011** 作为维护者，我希望使用现有单一领域语境和部署方式，以便模块整理不演变成无依据的上下文或服务拆分。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 47. |
| **US012** 作为维护者，我希望有依据的业务差异继续保留，以便统一结构不会抹平不同来源和功能的意图。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 47. |
| **US013** 作为维护者，我希望每条迁移都有实际消费者且删除旧规则副本，以便不会同时维护两份权威实现。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 07, 43, 44, 45, 46, 47. |
| **US014** 作为维护者，我希望按规则集中和验证成本评估成果，以便文件数量、行数和层数不代替真实收益。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 47. |
| **US015** 作为阅读者，我希望书的藏书身份与个人阅读状态分开表达，以便另一位读者的进度不改变这本书的身份。 | [M-R](#m-r) | `internal/reading/book_view_test.go::TestBoundBookViewsKeepSubjectFactsNavigationAndUnknownCompletion` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 01. |
| **US016** 作为调用者，我希望阅读查询明确接收主体，以便结果属于谁不依赖隐含全局用户。 | [M-R](#m-r) | `internal/reading/book_view_test.go::TestBookViewRequiresExplicitValidSubject` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 01. |
| **US017** 作为维护者，我希望类型能区分藏书事实与已绑定读者的阅读视图，以便调用者不会忘记先做主体投影。 | [M-R](#m-r) | `internal/reading/book_view_test.go::TestBoundBookViewsKeepSubjectFactsNavigationAndUnknownCompletion` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 01, 07. |
| **US018** 作为阅读者，我希望详情、导航和继续阅读使用同一主体的事实，以便不同入口不会展示他人或全局兼容进度。 | [M-R](#m-r) | `internal/reading/book_view_test.go::TestBoundBookViewsKeepSubjectFactsNavigationAndUnknownCompletion` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 01, 03, 04, 41, 43. |
| **US019** 作为兼容客户端使用者，我希望无站点用户的路径继续保留已确认语义，以便升级不会丢失兼容阅读能力。 | [M-R](#m-r) | `internal/reading/recent_view_test.go::TestRecentBooksBindsReaderAndPreservesAnonymousCompatibility` — Go PASS (0.13s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 01, 04. |
| **US020** 作为维护者，我希望非法主体不能被静默当作匿名，以便输入错误不会变成另一种身份的读取。 | [M-R](#m-r) | `internal/reading/book_view_test.go::TestBookViewRequiresExplicitValidSubject` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 01. |
| **US021** 作为阅读者，我希望主体读态进入筛选、排序与计数后再分页，以便分页结果和总量真实反映自己的状态。 | [M-Q](#m-q) | `internal/reading/catalogue_view_test.go::TestCataloguePageBindsReaderBeforeFilteringCountingOrderingAndPagination` — Go PASS (0.18s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 02, 03, 41. |
| **US022** 作为维护者，我希望列表查询继续使用批量 SQL 和必要聚合，以便模型分离不会退化为逐书查询。 | [M-Q](#m-q) | `internal/reading/catalogue_view_test.go::TestCataloguePageBindsReaderBeforeFilteringCountingOrderingAndPagination` — Go PASS (0.18s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 02, 03, 41. |
| **US023** 作为阅读者，我希望书、系列和跨页面读态解释一致，以便已读、未读和阅读中不会因页面而变化。 | [M-R](#m-r) | `internal/reading/book_view_test.go::TestBoundBookViewsKeepSubjectFactsNavigationAndUnknownCompletion` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 01, 02, 04. |
| **US024** 作为阅读者，我希望未知页数的完成态与真实页数分开，以便不会把内部哨兵显示为已读页数。 | [M-R](#m-r) | `internal/reading/book_view_test.go::TestBoundBookViewsKeepSubjectFactsNavigationAndUnknownCompletion` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 01, 06. |
| **US025** 作为阅读者，我希望有效新位置继续允许前进和回退，以便结构调整不恢复旧的最远页策略。 | [M-W](#m-w) | `internal/reading/position_test.go::TestPositionAllowsNewerBackwardReadingAndRejectsDelayedReports` — Go PASS (0.05s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 01, 05. |
| **US026** 作为离线阅读者，我希望原时间、事实身份及清除后的拒旧规则继续有效，以便视图整理不破坏上一轮已交付的写入语义。 | [M-W](#m-w) | `internal/reading/mark_test.go::TestClearEvidenceSurvivesRestartForUserAndAnonymousReader` — Go PASS (0.16s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 01, 05. |
| **US027** 作为阅读者，我希望普通继续阅读与有序清单的重读回退各保留用途，以便全部已读后仍可从清单首书重读。 | [M-R](#m-r) | `internal/organize/reader_queries_test.go::TestOrganizedReaderQueriesBindSubjectsAndKeepListFallbackSeparate` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 01, 03, 15. |
| **US028** 作为客户端使用者，我希望既有 JSON 和 TypeScript 字段保持兼容，以便内部模型变化不迫使客户端同步改版。 | [M-R](#m-r) | `internal/reading/book_view_test.go::TestBoundBookViewsKeepSubjectFactsNavigationAndUnknownCompletion` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 01, 02, 03, 04, 43. |
| **US029** 作为维护者，我希望缓存区分主体及所依赖事实，以便不能把一个人的视图复用给另一个人。 | [M-Q](#m-q) | `internal/bootstrap/bootstrap_test.go::TestHTTPQueryCacheReceivesTheApplicationInvalidation` — Go PASS (0.13s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 01, 02, 41. |
| **US030** 作为维护者，我希望基本阅读规则可经完整查询和用例验证，以便无需构造整个 HTTP Controller。 | [M-R](#m-r) | `internal/reading/queries_test.go::TestReaderViewsIsolateSubjectAndReflectBackwardPositionAndClear` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 01, 04, 06, 43. |
| **US031** 作为维护者，我希望事务能力只提供完整一致性操作所需内容，以便窄接口不能通过回调穿透为全部 SQL。 | [M-W](#m-w) | `internal/reading/atomic_store_test.go::TestReadingWritesUseOnlyAtomicStorageCapabilities` — Go PASS (0.1s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 05, 06, 07, 11, 15. |
| **US032** 作为维护者，我希望事务归属按业务不变量确定，以便不会按一张表或一个 handler 机械划界。 | [M-W](#m-w) | `internal/reading/mark_test.go::TestMarkBatchFailureRollsBackProgressBasisAndAggregate` — Go PASS (0.12s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 05, 11. |
| **US033** 作为调用者，我希望人工保存元数据是完整动作，以便不必补上事务、系列统计和响应重读配方。 | [M-M](#m-m) | `internal/application/seriesediting/manual_test.go::TestManualSaveReturnsCommittedMetadataAndPreservesAbsentCollections` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 08, 44. |
| **US034** 作为管理员，我希望人工编辑的未提供字段与显式清空继续区分，以便既有编辑意图不被改变。 | [M-M](#m-m) | `internal/application/seriesediting/manual_test.go::TestManualSaveReturnsCommittedMetadataAndPreservesAbsentCollections` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 08. |
| **US035** 作为管理员，我希望批量编辑继续保留标量、标签添加与移除的既有次序和语义，以便抽取不会改变结果。 | [M-M](#m-m) | `internal/seriesmeta/bulk_test.go::TestSelectionPreservesRawScalarsAndRemovesAfterAdding` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 09. |
| **US036** 作为管理员，我希望扫描写入继续遵守标题和元数据锁定，以便摄入不会覆盖人工保护。 | [M-S](#m-s) | `internal/scanner/series_title_lock_test.go::TestScanKeepsUserEditedSeriesTitleAndLocks` — Go PASS (0.18s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 27. |
| **US037** 作为管理员，我希望提案应用继续遵守来源规则、锁定与字段选择，以便不会被统一成无差别覆盖。 | [M-P](#m-p) | `internal/proposal/resolve_test.go::TestApplyFiltersFieldsLockedAfterQueueing` — Go PASS (0.08s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 10. |
| **US038** 作为维护者，我希望元数据来源规则仍有单一归属，以便完整应用用例不会复制另一份规则。 | [M-P](#m-p) | `internal/proposal/resolve_test.go::TestApplyFiltersFieldsLockedAfterQueueing` — Go PASS (0.08s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 08, 09, 10, 27, 44. |
| **US039** 作为维护者，我希望存储适配避免形成包依赖循环，以便业务类型分离后仍可编译和独立验证。 | [M-M](#m-m) | `internal/application/seriesediting/manual_test.go::TestManualSaveRollsBackRequiredStatisticsAndConflictingVersion` — Go PASS (0.12s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 05, 06, 08, 11, 15. |
| **US040** 作为维护者，我希望只隔离有业务含义的存储形状，以便纯查询不产生大量无规则收益的转换。 | [M-M](#m-m) | `internal/reading/catalogue_view_test.go::TestCataloguePageBindsReaderBeforeFilteringCountingOrderingAndPagination` — Go PASS (0.18s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 02, 08, 41. |
| **US041** 作为操作发起者，我希望结果区分未提交、部分提交和已提交但后续未完成，以便可以安全判断是否重试。 | [M-W](#m-w) | `internal/reading/mark_test.go::TestLaterMarkBatchFailureRetainsAndInvalidatesCommittedRange` — Go PASS (0.58s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 05, 08, 09, 18. |
| **US042** 作为操作发起者，我希望前批已提交结果在后批失败后继续有效，以便错误报告不会掩盖已发生事实。 | [M-W](#m-w) | `internal/reading/mark_test.go::TestLaterMarkBatchFailureRetainsAndInvalidatesCommittedRange` — Go PASS (0.58s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 05, 09. |
| **US043** 作为维护者，我希望进度已保存而统计失败仍独立告警，以便不通过强事务回滚改变已批准的软统计语义。 | [M-W](#m-w) | `internal/reading/position_test.go::TestActivityFailureKeepsCommittedPositionAndAttemptsUserProjection` — Go PASS (0.05s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 05, 06. |
| **US044** 作为维护者，我希望退出无人使用且可绕过新规则的旧写入口，以便未来修改不会重新长出第二条写路径。 | [M-W](#m-w) | `internal/reading/atomic_store_test.go::TestReadingWritesUseOnlyAtomicStorageCapabilities` — Go PASS (0.1s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 05, 07, 47. |
| **US045** 作为审核者，我希望完整应用和部分应用都在写入事务中确认提案仍待裁决，以便终态之后不再写入字段。 | [M-P](#m-p) | `internal/proposal/concurrency_test.go::TestPartialApplyConflictsWhenRejectCommitsBeforeItsTransaction` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 10. |
| **US046** 作为审核者，我希望并发拒绝先完成时后来的部分应用返回冲突，以便抢先完成的裁决得到尊重。 | [M-P](#m-p) | `internal/proposal/concurrency_test.go::TestPartialApplyConflictsWhenRejectCommitsBeforeItsTransaction` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 10. |
| **US047** 作为审核者，我希望冲突时本次元数据、提案字段和来源沿革都不提交，以便审计与实际结果一致。 | [M-P](#m-p) | `internal/proposal/concurrency_test.go::TestPartialApplyFailureRollsBackMetadataFieldsAndProvenance` — Go PASS (0.09s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 10. |
| **US048** 作为审核者，我希望拒绝不撤销此前成功应用的字段，以便已确认事实不会被后续拒绝反向抹去。 | [M-P](#m-p) | `internal/proposal/concurrency_test.go::TestConcurrentPartialApplicationsDoNotReuseConsumedFields` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 10. |
| **US049** 作为审核者，我希望部分应用成功后保留剩余待裁决字段，以便可以继续处理未应用部分。 | [M-P](#m-p) | `internal/proposal/resolve_test.go::TestApplyFillEmptyKeepsUnappliedFields` — Go PASS (0.11s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 10. |
| **US050** 作为审核者，我希望应用依据事务内最新字段、锁定和系列事实选择，以便不使用加载后已过期的选择结果。 | [M-P](#m-p) | `internal/proposal/concurrency_test.go::TestApplySelectsUsingTransactionCurrentValuesAndLocks` — Go PASS (0.12s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 10. |
| **US051** 作为审核者，我希望两个部分应用交错时不会重复使用已移除字段，以便来源和剩余建议保持一致。 | [M-P](#m-p) | `internal/proposal/concurrency_test.go::TestConcurrentPartialApplicationsDoNotReuseConsumedFields` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 10. |
| **US052** 作为审核者，我希望完整应用仍能正确进入终态且不可重复裁决，以便原有状态语义保持有效。 | [M-P](#m-p) | `internal/proposal/resolve_test.go::TestApplyWritesEveryFieldAndClosesProposal` — Go PASS (0.09s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 10. |
| **US053** 作为审核者，我希望全部锁定或没有可写变化仍得到准确结果，以便不会把未写入误报为应用完成。 | [M-P](#m-p) | `internal/proposal/resolve_test.go::TestApplyReportsAllFieldsLockedWhenEverythingIsFiltered` — Go PASS (0.05s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 10. |
| **US054** 作为客户端使用者，我希望裁决冲突与系统故障可区分，以便页面可以重读当前状态而不是误报保存失败。 | [M-P](#m-p) | `internal/api/metadata_review_concurrency_test.go::TestPartialApplyHTTPClassifiesTransactionCurrentOutcome` — Go PASS (0.24s)<br>`web/src/pages/metadataReviewConcurrency.test.tsx::shows conflicts, no changes and faults separately after a partial-apply request` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 10. |
| **US055** 作为维护者，我希望真实并发事务覆盖裁决竞争，以便事务外预检不能被当作唯一正确性保证。 | [M-P](#m-p) | `internal/proposal/concurrency_test.go::TestConcurrentPartialApplicationsDoNotReuseConsumedFields` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 10. |
| **US056** 作为管理员，我希望人工合集、智能书架、快照、作品群和阅读清单保持不同成员语义，以便结构统一不改变编辑权限。 | [M-C](#m-c) | `internal/organize/collections_test.go::TestOnlyContinuouslyDerivedCollectionsRejectManualChanges` — Go PASS (0.25s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 03, 11, 12, 15, 44. |
| **US057** 作为管理员，我希望一次性快照和 AI 落地合集继续归人工所有，以便来源不会被误认为持续推导。 | [M-C](#m-c) | `internal/organize/smart_collections_test.go::TestSmartSnapshotIsOneTimeAndManuallyOwned` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 11, 12. |
| **US058** 作为管理员，我希望合集名称、归属和成员修改通过完整用例维护，以便输入不需要了解跨表约束。 | [M-C](#m-c) | `internal/organize/collections_test.go::TestMemberBatchIsAtomicForMissingSeries` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 11, 12, 15, 44. |
| **US059** 作为阅读者，我希望作品群身份和成员差集更新保持稳定，以便重建不使收藏入口或链接无故变化。 | [M-F](#m-f) | `internal/organize/relations_test.go::TestFranchiseMergeSplitKeepsUnaffectedIdentitiesAndTimes` — Go PASS (0.05s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 03, 13. |
| **US060** 作为管理员，我希望关系保存可以先成功而作品群随后更新，以便每次编辑不等待整个关系图重建事务。 | [M-F](#m-f) | `internal/organize/franchise_repair_test.go::TestRelationWriteRollsBackWhenDurableMarkFails` — Go PASS (0.1s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 13. |
| **US061** 作为维护者，我希望关系变化与待更新依据同事务提交，以便关系已变但修复责任不会丢失。 | [M-F](#m-f) | `internal/organize/franchise_repair_test.go::TestRelationWriteRollsBackWhenDurableMarkFails` — Go PASS (0.1s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 13. |
| **US062** 作为维护者，我希望派生更新依据覆盖全局关系图，以便跨资料库关系不会被错误分割。 | [M-F](#m-f) | `internal/organize/franchise_repair_test.go::TestRelationWriteRollsBackWhenDurableMarkFails` — Go PASS (0.1s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 13. |
| **US063** 作为维护者，我希望系列和资料库删除导致的级联变化也标记派生结果待更新，以便修复不会只覆盖显式关系编辑。 | [M-F](#m-f) | `internal/organize/franchise_facts_test.go::TestFranchiseCascadeSourcesRecoverAfterReopenWithStableIdentity` — Go PASS (0.28s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 14. |
| **US064** 作为维护者，我希望影响派生结果的系列事实变化也进入更新依据，以便成员名称等展示不会永久陈旧。 | [M-F](#m-f) | `internal/organize/franchise_facts_test.go::TestManualTitleEditPersistsFranchiseFactChange` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 14. |
| **US065** 作为阅读者，我希望重建失败后系统退避重试，以便一次暂时错误不会永久留下旧结果。 | [M-F](#m-f) | `internal/organize/franchise_repair_test.go::TestFranchiseFailureKeepsOldProjectionAndHonorsBackoffAcrossReopen` — Go PASS (0.1s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 13. |
| **US066** 作为阅读者，我希望服务重启后仍继续未完成重建，以便内存通知丢失不影响最终修复。 | [M-F](#m-f) | `internal/application/runtime/franchise_repair_test.go::TestRuntimeResumesDurableFranchiseRepairAndRetriesFailure` — Go PASS (2.18s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 13. |
| **US067** 作为维护者，我希望重建期间的新变化不会被旧成功清除，以便系统不会误以为已追上最新关系。 | [M-F](#m-f) | `internal/organize/franchise_repair_test.go::TestNewRelationChangeCannotBeClearedByOldRebuildCompletion` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 13, 14. |
| **US068** 作为维护者，我希望旧尝试的失败不能覆盖较新的成功状态，以便修复状态反映实际版本。 | [M-F](#m-f) | `internal/organize/franchise_repair_test.go::TestOldFranchiseFailureCannotReplaceNewerSuccess` — Go PASS (0.13s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 13, 14. |
| **US069** 作为阅读者，我希望已有成功作品群结果在失败期间仍可读且标明陈旧，以便保持可用性而不误信其已更新。 | [M-F](#m-f) | `internal/organize/franchise_repair_test.go::TestFranchiseUpgradePreservesExistingResultAsStale` — Go PASS (0.07s)<br>`web/src/features/organize/FranchiseRepairStatus.test.tsx::keeps old results identified as stale and retries immediately after a failure` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 13. |
| **US070** 作为阅读者，我希望首次没有成功结果时显示不可用而不是空结果，以便不会误判没有作品群。 | [M-F](#m-f) | `internal/organize/franchise_repair_test.go::TestFranchiseRepairResponsibilitySurvivesWithoutBackgroundNotification` — Go PASS (0.05s)<br>`web/src/features/organize/FranchiseRepairStatus.test.tsx::distinguishes a first unavailable projection from a successful empty projection` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 13. |
| **US071** 作为管理员，我希望可以立即重试派生更新且后台责任持续保留，以便可以主动恢复而不依赖下一次关系修改。 | [M-F](#m-f) | `internal/organize/franchise_repair_test.go::TestFranchiseRepairResponsibilitySurvivesWithoutBackgroundNotification` — Go PASS (0.05s)<br>`web/src/features/organize/FranchiseRepairStatus.test.tsx::keeps old results identified as stale and retries immediately after a failure` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 13. |
| **US072** 作为管理员，我希望创建、修改和删除资料库各有完整用例，以便 HTTP 与导航壳不再持有操作配方。 | [M-L](#m-l) | `internal/application/libraries/save_test.go::TestCreateLibraryKeepsCommittedResultWhenInitialScanAdmissionFails` — Go PASS (0.05s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 24, 45. |
| **US073** 作为管理员，我希望资料库路径、格式、间隔和已有路径占用有一致校验，以便不同输入不会绕开约束。 | [M-L](#m-l) | `internal/application/libraries/save_test.go::TestLibraryValidationRejectsEquivalentRegisteredRootAcrossCreateAndUpdate` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 24. |
| **US074** 作为管理员，我希望新建后的监听、首扫和刷新得到完整交接，以便资料库创建成功不被等同为扫描完成。 | [M-L](#m-l) | `internal/application/libraries/save_test.go::TestCreateLibraryKeepsCommittedResultWhenInitialScanAdmissionFails` — Go PASS (0.05s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 24. |
| **US075** 作为管理员，我希望有记录的资料库改路径继续使用保留身份的迁移流程，以便普通设置修改不会破坏记录。 | [M-L](#m-l) | `internal/application/libraries/save_test.go::TestUpdateLibraryPreservesIdentityAndRejectsOccupiedPathChange` — Go PASS (0.05s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 24, 25. |
| **US076** 作为管理员，我希望保护恢复约束继续限制自动处理和危险清理，以便迁移后异常记录不会被误删。 | [M-L](#m-l) | `internal/application/scanning/scanning_test.go::TestProtectedLibraryRejectsAutomaticScansAndEveryCleanup` — Go PASS (0.05s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 24, 25. |
| **US077** 作为管理员，我希望删库先关闭该库新工作和读取准入，以便等待期间不会不断增加占用。 | [M-L](#m-l) | `internal/task/library_reservation_test.go::TestLibraryReservationBlocksQueueAndReleasesOtherLibraries` — Go PASS (0s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 22. |
| **US078** 作为管理员，我希望已排队工作在删库保留期间也不能开跑，以便关闭准入不是只拦住新发起。 | [M-L](#m-l) | `internal/task/library_reservation_test.go::TestLibraryReservationBlocksQueueAndReleasesOtherLibraries` — Go PASS (0s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 22. |
| **US079** 作为管理员，我希望可取消的本库运行被请求取消并有界等待，以便删除不会无限挂起。 | [M-L](#m-l) | `internal/application/libraries/delete_test.go::TestDeleteCancelsLocalRunAndWaitsForItsActualExit` — Go PASS (0.08s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 23. |
| **US080** 作为管理员，我希望仍有阅读、不可取消或跨库工作占用时明确阻止删除，以便不会靠外键失败收拾晚到写入。 | [M-L](#m-l) | `internal/application/libraries/delete_test.go::TestDeleteBusyLibraryPreservesRecordAndWatcherThenDeletesWhenReadExits` — Go PASS (2.17s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 22, 23. |
| **US081** 作为管理员，我希望删一个库不自动取消覆盖其他库的系统运行，以便其他资料库工作不受牵连。 | [M-L](#m-l) | `internal/task/library_drain_test.go::TestLibraryDrainCancelsOnlyEligibleIdentityAndActualCoverage` — Go PASS (0.71s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 22, 23. |
| **US082** 作为维护者，我希望取消资格结合实际覆盖和任务身份判断，以便任务键前缀或单个库标识不造成误判。 | [M-L](#m-l) | `internal/task/library_drain_test.go::TestLibraryDrainCancelsOnlyEligibleIdentityAndActualCoverage` — Go PASS (0.71s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 22, 23. |
| **US083** 作为维护者，我希望等待的是访问者真实退出而不只是展示终态，以便资源仍在使用时不会删记录。 | [M-L](#m-l) | `internal/task/library_reservation_test.go::TestLibraryReservationWaitsForBodyExitDespiteTerminalSnapshot` — Go PASS (0s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 22, 23. |
| **US084** 作为维护者，我希望占用排空与升级独占之间没有重新准入窗口，以便并发访问不能穿过删除边界。 | [M-L](#m-l) | `internal/task/library_reservation_test.go::TestLibraryReservationUpgradeNeverReopensReadAdmission` — Go PASS (0s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 22, 23. |
| **US085** 作为管理员，我希望请求取消、超时或删除失败后准入被释放，以便仍存在的资料库可继续使用。 | [M-L](#m-l) | `internal/application/libraries/delete_test.go::TestDeleteHoldsEngineExclusivityUntilCommitAndCancellationReleasesIt` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 23. |
| **US086** 作为管理员，我希望删除失败后按当前设置恢复应有监听，以便资料库不会悄悄失去自动发现能力。 | [M-L](#m-l) | `internal/application/libraries/delete_test.go::TestDeleteFailureUsesCurrentWatchSettings` — Go PASS (0.05s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 23. |
| **US087** 作为管理员，我希望失败结果说明已取消的运行不会自动复活，以便可以有意识地重新发起工作。 | [M-L](#m-l) | `internal/application/libraries/delete_test.go::TestDeleteFailureRetainsQueuedCancellationFactsWithoutRestart` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 23. |
| **US088** 作为维护者，我希望删除释放后适用队列恢复放行且其他库不被阻塞，以便协调能力不会造成永久停滞。 | [M-L](#m-l) | `internal/task/library_reservation_test.go::TestLibraryReservationBlocksQueueAndReleasesOtherLibraries` — Go PASS (0s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 22, 23. |
| **US089** 作为管理员，我希望删除成功后统计、阅读路径、推荐和派生结果得到必要失效或修复，以便不继续看到已删资料库的旧内容。 | [M-L](#m-l) | `internal/organize/franchise_facts_test.go::TestFranchiseSeriesDeletionPersistsRepairResponsibility` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 14, 23. |
| **US090** 作为阅读者，我希望页清单、页读取和整书打开使用一致内容来源，以便缓存和访问资格不因入口不同而分叉。 | [M-B](#m-b) | `internal/bookcontent/page_test.go::TestPageMaterializesBytesAndReleasesBorrowBeforeReturning` — Go PASS (0.03s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 16, 17, 45. |
| **US091** 作为调用者，我希望内容能力隐藏来源定位、归档借还、加工和缓存资格，以便不必重新拼接读取配方。 | [M-B](#m-b) | `internal/bookcontent/page_test.go::TestPageMaterializesBytesAndReleasesBorrowBeforeReturning` — Go PASS (0.03s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 16. |
| **US092** 作为维护者，我希望内容事实与 HTTP 响应映射职责分开，以便技术读取能力不依赖请求对象或响应状态码。 | [M-B](#m-b) | `internal/bookcontent/page_test.go::TestPageMaterializesBytesAndReleasesBorrowBeforeReturning` — Go PASS (0.03s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 16. |
| **US093** 作为阅读者，我希望整书下载在句柄关闭前保留所需访问资格，以便删除、迁移或回写不会强行截断已开始的读取。 | [M-B](#m-b) | `internal/bookcontent/file_test.go::TestWholeBookHandleRetainsAccessUntilClose` — Go PASS (0.02s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 17. |
| **US094** 作为维护者，我希望已物化页字节及时释放归档借用，以便资源占用符合实际使用范围。 | [M-B](#m-b) | `internal/bookcontent/page_test.go::TestPageMaterializesBytesAndReleasesBorrowBeforeReturning` — Go PASS (0.03s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 16. |
| **US095** 作为阅读者，我希望图像透传、真实加工结果和缓存资格继续区分，以便没有实际加工的结果不会被错误缓存。 | [M-B](#m-b) | `internal/bookcontent/page_test.go::TestPassthroughAndUnavailableFilterStayOutOfProcessedCache` — Go PASS (0.14s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 16. |
| **US096** 作为阅读者，我希望图像尺寸、解压与内存保护继续有效，以便内容抽取不会扩大危险输入的资源影响。 | [M-B](#m-b) | `internal/images/processor_process_test.go::TestProcessImageRejectsDecodeBomb` — Go PASS (0s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 16. |
| **US097** 作为阅读者，我希望缓存包含影响字节的参数与来源依据，以便变更图像选项或替换归档后不会读取旧内容。 | [M-B](#m-b) | `internal/bookcontent/page_test.go::TestDerivedPageCacheTracksVariantAndArchivePath` — Go PASS (0.03s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 16. |
| **US098** 作为管理员，我希望 ComicInfo 临时归档构建尽量不阻塞阅读，以便耗时处理不占据整段写入窗口。 | [M-PUB](#m-pub) | `internal/parser/publication_test.go::TestComicInfoPublicationWaitsForEvictedBorrowAndBlocksNewReaders` — Go PASS (0.02s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 18. |
| **US099** 作为管理员，我希望最终替换前取得该书短期写入资格并阻止新借用，以便在稳定发布窗口处理原归档。 | [M-PUB](#m-pub) | `internal/parser/publication_test.go::TestComicInfoPublicationWaitsForEvictedBorrowAndBlocksNewReaders` — Go PASS (0.02s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 18. |
| **US100** 作为管理员，我希望发布有界等待已有读取结束，以便工作不会无限等待且不强制切断读者。 | [M-PUB](#m-pub) | `internal/parser/publication_test.go::TestComicInfoPublicationTimeoutCancelAndSourceChangePreserveOriginal` — Go PASS (0.05s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 18. |
| **US101** 作为管理员，我希望原文件已变化或读取仍占用时明确失败并保留原文件，以便不覆盖外部修改或正在使用的内容。 | [M-PUB](#m-pub) | `internal/parser/publication_test.go::TestComicInfoPublicationTimeoutCancelAndSourceChangePreserveOriginal` — Go PASS (0.05s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 18. |
| **US102** 作为维护者，我希望归档池淘汰与最后借用者退出被分别处理，以便移出缓存不会被误认为文件已经关闭。 | [M-PUB](#m-pub) | `internal/parser/publication_test.go::TestComicInfoPublicationWaitsForEvictedBorrowAndBlocksNewReaders` — Go PASS (0.02s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 16, 17, 18. |
| **US103** 作为管理员，我希望成功发布后内容事实和缓存得到统一交接，以便新请求不继续采用旧来源。 | [M-PUB](#m-pub) | `internal/bookcontent/publication_test.go::TestPublicationRetainsCommittedFactWhenFactStoreFails` — Go PASS (0.01s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 18. |
| **US104** 作为管理员，我希望发布后的后续失败保留文件已提交事实，以便不会因模糊失败无条件再次改写文件。 | [M-PUB](#m-pub) | `internal/bookcontent/publication_test.go::TestPublicationRetainsCommittedFactWhenFactStoreFails` — Go PASS (0.01s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 18. |
| **US105** 作为维护者，我希望真实平台文件系统验证替换和失败保留，以便不只用模拟 Rename 成功证明安全。 | [M-PUB](#m-pub) | `internal/parser/publication_test.go::TestComicInfoPublicationTimeoutCancelAndSourceChangePreserveOriginal` — Go PASS (0.05s) | Representative regression + named boundary check; not exhaustive clause proof. See platform/device/provider limits. Origin: 18. |
| **US106** 作为管理员，我希望外部路径不与任何已登记资料库重叠，以便其他资料库不会被隐式当作外部库。 | [M-X](#m-x) | `internal/external/boundary_test.go::TestExternalRootRejectsEveryRegisteredLibrary` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 19. |
| **US107** 作为管理员，我希望外部路径包含、位于或等于任一资料库目录时都被拒绝，以便祖先目录也不能绕过边界。 | [M-X](#m-x) | `internal/external/boundary_test.go::TestExternalRootRejectsEveryRegisteredLibrary` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 19. |
| **US108** 作为维护者，我希望扫描、传输和保留计划遵守同一资料库访问协调，以便路径改变后旧计划不会继续读写。 | [M-X](#m-x) | `internal/application/diskjobs/diskjobs_test.go::TestQueuedExportRetainsCoverageAndCancellationNeverCopies` — Go PASS (0.11s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 19, 20. |
| **US109** 作为管理员，我希望传输仍使用明确准备结果和实际运行，以便准入失败、无缺书和运行完成不是同一种结果。 | [M-X](#m-x) | `internal/application/diskjobs/diskjobs_test.go::TestTransferExternalCopiesAndRefreshesSession` — Go PASS (0.29s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 19, 20, 45. |
| **US110** 作为管理员，我希望取消可以在当前文件复制块之间生效并停止后续复制，以便不必等整本大文件传完。 | [M-X](#m-x) | `internal/external/transfer_test.go::TestTransferCancellationKeepsCommittedBooksAndRemovesCurrentStaging` — Go PASS (0.33s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 20. |
| **US111** 作为管理员，我希望取消清理本次未提交临时文件，以便不会留下可被误当成完整书的目标。 | [M-X](#m-x) | `internal/external/transfer_test.go::TestTransferCancellationKeepsCommittedBooksAndRemovesCurrentStaging` — Go PASS (0.33s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 20. |
| **US112** 作为管理员，我希望取消保留之前成功传输的文件，以便不会丢失已经完成的成果。 | [M-X](#m-x) | `internal/external/transfer_test.go::TestTransferCancellationKeepsCommittedBooksAndRemovesCurrentStaging` — Go PASS (0.33s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 20. |
| **US113** 作为管理员，我希望已有目标文件始终保留，以便复制竞争或重试不覆盖他人已经放好的文件。 | [M-X](#m-x) | `internal/external/transfer_test.go::TestTransferPreservesDestinationCreatedDuringCopy` — Go PASS (0.11s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 20. |
| **US114** 作为管理员，我希望暂停在复制块边界生效并保留临时文件及偏移，以便恢复时继续当前书。 | [M-X](#m-x) | `internal/external/transfer_pause_test.go::TestTransferPausesWithinCurrentBookAndResumesSameStaging` — Go PASS (0.33s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 21. |
| **US115** 作为阅读者，我希望暂停传输释放可重新取得的 IO 配额，以便暂停作业不会长期占用阅读所需资源。 | [M-X](#m-x) | `internal/external/transfer_pause_test.go::TestTransferPausesWithinCurrentBookAndResumesSameStaging` — Go PASS (0.33s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 21. |
| **US116** 作为管理员，我希望恢复前核对源文件和目标资格，以便不能确认安全时不拼接不一致内容。 | [M-X](#m-x) | `internal/external/transfer_pause_test.go::TestPausedTransferRejectsChangedSourceOrStagingAndCleansUp` — Go PASS (0.75s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 21. |
| **US117** 作为管理员，我希望不能安全续拷时明确失败并清理本次临时文件，以便原有文件与已完成成果仍受保护。 | [M-X](#m-x) | `internal/external/transfer_pause_test.go::TestPausedTransferRejectsChangedSourceOrStagingAndCleansUp` — Go PASS (0.75s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 21. |
| **US118** 作为管理员，我希望未提交文件不计为成功，以便暂停、取消和进度展示反映真实完成量。 | [M-X](#m-x) | `internal/external/transfer_test.go::TestTransferCancellationKeepsCommittedBooksAndRemovesCurrentStaging` — Go PASS (0.33s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 20, 21. |
| **US119** 作为管理员，我希望传输部分失败时保留已完成范围并刷新会话投影，以便页面不丢失已发生的成果。 | [M-X](#m-x) | `internal/external/transfer_test.go::TestTransferFailureKeepsSuccessfulRangeAndSessionProjection` — Go PASS (0.19s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 20. |
| **US120** 作为维护者，我希望临时复制、资源归还和最终提交由完整能力处理，以便调用方不依赖内部 CopyFile 步骤。 | [M-X](#m-x) | `internal/external/transfer_test.go::TestTransferCancellationKeepsCommittedBooksAndRemovesCurrentStaging` — Go PASS (0.33s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 20. |
| **US121** 作为管理员，我希望运行取消和暂停不承诺强行中断系统内阻塞的单次读写，以便界面与实际能力一致。 | [M-X](#m-x) | `internal/external/transfer_pause_test.go::TestTransferPausedWhileQueuedForQuotaDoesNotStartRead` — Go PASS (0.37s) | Representative regression + named boundary check; not exhaustive clause proof. See platform/device/provider limits. Origin: 20, 21. |
| **US122** 作为管理员，我希望本轮保持外部传输不跨进程自动续传，以便重启不会自动恢复可能有写盘后果的操作。 | [M-X](#m-x) | `internal/application/diskjobs/diskjobs_test.go::TestQueuedExportRetainsCoverageAndCancellationNeverCopies` — Go PASS (0.11s) | Representative regression + named boundary check; not exhaustive clause proof. See platform/device/provider limits. Origin: 21. |
| **US123** 作为维护者，我希望扫描内部按发现、解释、身份认领、入库与清理组织知识，以便复杂性不只从 Controller 移到另一个大实现。 | [M-S](#m-s) | `internal/scanner/ingestion_transaction_test.go::TestScanDoesNotReportRolledBackRenameAsCommitted` — Go PASS (0.12s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 27, 45. |
| **US124** 作为管理员，我希望扫描仍是完整摄入用例，以便输入不暴露 worker 与 SQL 刷写配方。 | [M-S](#m-s) | `internal/scanner/ingestion_transaction_test.go::TestScanDoesNotReportRolledBackRenameAsCommitted` — Go PASS (0.12s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 27. |
| **US125** 作为维护者，我希望技术监听交付文件变化，摄入协调负责扫描和清理资格，以便 fsnotify 不承担完整业务流程。 | [M-S](#m-s) | `internal/application/scanning/watch_queue_test.go::TestQueuedWatchCleanupRechecksInputAndChangeQualification` — Go PASS (0.28s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 28, 45. |
| **US126** 作为阅读者，我希望改名后的记录重连完成后才可能清理，以便阅读历史不因过早清理丢失。 | [M-S](#m-s) | `internal/application/scanning/scanning_test.go::TestWatchRenameCompletesBeforeCleanup` — Go PASS (0.09s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 27, 28. |
| **US127** 作为维护者，我希望扫描合并、排队、停发与真实成功继续区分，以便清理不把准入结果当作扫描完成。 | [M-S](#m-s) | `internal/application/scanning/watch_queue_test.go::TestQueuedWatchCleanupRechecksInputAndChangeQualification` — Go PASS (0.28s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 28. |
| **US128** 作为管理员，我希望失败扫描后的重新排期和安全清理保护保留，以便暂时故障不触发危险删除。 | [M-S](#m-s) | `internal/scanner/watcher_rename_order_test.go::TestCleanupSkippedWhenSameLibraryScanFails` — Go PASS (2.51s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 28. |
| **US129** 作为维护者，我希望扫描与封面交接保留独立运行和唯一准入，以便内部整理不制造第二套锁或隐式生成路径。 | [M-S](#m-s) | `internal/application/scanning/cover_test.go::TestApplicationScanCompletesWhileItsCoverRunStillWaits` — Go PASS (0.2s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 27, 28. |
| **US130** 作为维护者，我希望书身份、内容证据和设备文档键分别表达，以便它们不会因同名指纹被当作同一身份。 | [M-I](#m-i) | `internal/bookidentity/evidence_test.go::TestQuickAndPathEvidenceKeepCompatibleByteConventions` — Go PASS (0.01s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 26. |
| **US131** 作为维护者，我希望扫描和维护获取身份能力时不依赖整个 KOReader 协议模块，以便依赖方向符合实际用途。 | [M-I](#m-i) | `internal/bookidentity/rebuild_test.go::TestRebuildUsesEvidenceWithoutChangingBookIdentityAndSkipsUnreadableTail` — Go PASS (0.22s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 26. |
| **US132** 作为设备用户，我希望认证、账号、匹配与协议记录继续由设备协议能力处理，以便身份工具归位不改变同步协议。 | [M-I](#m-i) | `internal/api/controller_test.go::TestKOReaderSelfRegistrationCreatesAuthenticatableAccount` — Go PASS (0.11s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 26. |
| **US133** 作为阅读者，我希望现有哈希和路径匹配口径保持兼容，以便升级不会重新认错书或丢失关联。 | [M-I](#m-i) | `internal/koreader/fingerprint_extra_test.go::TestNormalizeDocumentForMatchModes` — Go PASS (0s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 04, 26. |
| **US134** 作为管理员，我希望 size+mtime 改名重连取舍和歧义保护继续有效，以便模块调整不静默改变身份算法。 | [M-I](#m-i) | `internal/scanner/rehome_test.go::TestRenameIndexRefusesAmbiguousMatch` — Go PASS (0.01s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 26, 27. |
| **US135** 作为管理员，我希望清理的根可达性、真实书与删除比例保护继续有效，以便整理不取消已有数据保护。 | [M-S](#m-s) | `internal/scanner/cleanup_guard_test.go::TestCleanupCircuitBreakerStopsMassDeletion` — Go PASS (0.12s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 27, 28. |
| **US136** 作为管理员，我希望资料库迁移的持久过程、决策和落盘职责明确且保留身份、保护、配置补偿与恢复独占，以便分工变化不破坏恢复能力。 | [M-MOVE](#m-move) | `internal/application/relocating/relocating_test.go::TestRuntimePolicyFailureKeepsAccessProtectedUntilRecovery` — Go PASS (0.28s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 25. |
| **US137** 作为维护者，我希望配置发布由明确能力协调 API 保存与文件热重载，以便两条生效路径不会分叉。 | [M-CFG](#m-cfg) | `internal/api/config_consistency_test.go::TestConfigUpdatesSerializeSaveAndReload` — Go PASS (0.09s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 30, 46. |
| **US138** 作为管理员，我希望设置区分即时、后续作业和重启后生效，以便保存成功不会被误认为所有行为已经变化。 | [M-CFG](#m-cfg) | `internal/config/effects_test.go::TestFieldTimingCatalogCoversConfigurationAndKeepsStartupFieldsExplicit` — Go PASS (0s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 30, 32. |
| **US139** 作为管理员，我希望一次操作的目标、来源和写入意图不因热更新半途改变，以便操作结果仍符合发起时的意图。 | [M-CFG](#m-cfg) | `internal/application/intelligence/configuration_snapshot_test.go::TestGroupingPinsSourceBeforeDeferredRunStarts` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 30. |
| **US140** 作为维护者，我希望资源预算在安全切换点更新，以便旧使用者能正常归还取得的资源。 | [M-CFG](#m-cfg) | `internal/runtimecfg/runtimecfg_test.go::TestOwnedResourcesKeepBorrowUsableAcrossUpdateAndRetirement` — Go PASS (0.02s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 31. |
| **US141** 作为阅读者，我希望正在使用的池和处理器不会被强制销毁，以便热更新不打断活跃读取。 | [M-CFG](#m-cfg) | `internal/runtimecfg/runtimecfg_test.go::TestOwnedResourcesKeepBorrowUsableAcrossUpdateAndRetirement` — Go PASS (0.02s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 31. |
| **US142** 作为维护者，我希望持久化、重新加载和跨参与者协调保留不同补偿语义，以便不能用通用配置实体掩盖文件与数据库边界。 | [M-CFG](#m-cfg) | `internal/runtimecfg/runtimecfg_test.go::TestRejectedConfigurationPreservesPublishedResources` — Go PASS (0s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 25, 30. |
| **US143** 作为管理员，我希望配置版本冲突、密钥占位和校验语义继续有效，以便结构整理不覆盖他人设置或泄露凭据。 | [M-CFG](#m-cfg) | `internal/config/config_extra_test.go::TestMaskAndRestoreAllSecrets` — Go PASS (0s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 30, 32. |
| **US144** 作为维护者，我希望进程资源有明确创建、替换与关闭所有者，以便全局状态不会成为隐含依赖。 | [M-CFG](#m-cfg) | `internal/application/runtime/resource_lifetime_test.go::TestShutdownRetiresResourcesAfterAdmittedReaderExits` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 31. |
| **US145** 作为维护者，我希望允许保留有依据的进程单实例资源，以便不会为形式一致增加无收益的多实例机制。 | [M-CFG](#m-cfg) | `internal/application/runtime/resource_lifetime_test.go::TestShutdownRetiresResourcesAfterAdmittedReaderExits` — Go PASS (0.07s) | Representative regression + named boundary check; not exhaustive clause proof. See platform/device/provider limits. Origin: 31. |
| **US146** 作为调用者，我希望元数据搜索、推荐、分组和连接测试只依赖所需能力，以便不被迫认识供应商超集接口。 | [M-V](#m-v) | `internal/api/metadata_capability_test.go::TestMetadataSearchConsumesSearchOnlySource` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 29, 46. |
| **US147** 作为维护者，我希望同一供应商可以实现多个真实消费接口，以便不为接口拆分增加无行为转发层。 | [M-V](#m-v) | `internal/api/metadata_capability_test.go::TestMetadataSearchConsumesSearchOnlySource` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 29. |
| **US148** 作为维护者，我希望上游 DTO、分页、重试和错误归一留在适配实现，以便供应商细节不进入业务模型。 | [M-V](#m-v) | `internal/api/metadata_capability_test.go::TestMetadataSearchConsumesSearchOnlySource` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 29. |
| **US149** 作为管理员，我希望外部调用继续遵守取消、预算及脱敏约束，以便配置工厂收窄不会改变保护能力。 | [M-V](#m-v) | `internal/metadata/provider_budget_test.go::TestProviderBudgetRespectsContextCancellation` — Go PASS (0.04s) | Representative regression + named boundary check; not exhaustive clause proof. See platform/device/provider limits. Origin: 29. |
| **US150** 作为维护者，我希望连接配置转换不要求构造整份全站配置，以便集成模块的实际依赖可以明确理解。 | [M-V](#m-v) | `internal/config/llm_connection_test.go::TestLLMConnectionNormalizesLegacyEndpointWithoutApplicationConfig` — Go PASS (0s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 29. |
| **US151** 作为维护者，我希望装配根独立创建应用、资源和宿主，以便 Controller 不再驱动整个进程构造。 | [M-RT](#m-rt) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 42, 47. |
| **US152** 作为维护者，我希望 HTTP 处理器按功能接收所需用例和查询，以便不会复制全部运行时引用或接受万能应用对象。 | [M-RT](#m-rt) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 42, 43, 44, 45, 46, 47. |
| **US153** 作为管理员，我希望重启能力登记与一致性恢复先于后台和请求开放，以便恢复失败时服务不会暴露半完成状态。 | [M-RT](#m-rt) | `internal/bootstrap/bootstrap_test.go::TestFailedRecoveryReturnsNoHostAndOpensNoInputs` — Go PASS (0.09s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 31, 42. |
| **US154** 作为管理员，我希望停机先排空输入并等待使用者退出，再释放资源，以便后台收尾不会使用已关闭的池。 | [M-RT](#m-rt) | `internal/bootstrap/bootstrap_test.go::TestShutdownWaitsForHTTPConsumerBeforeStoppingEngine` — Go PASS (0.06s)<br>`internal/bootstrap/sse_shutdown_test.go::TestShutdownClosesLiveHostSSEBeforeDrainingHTTPAndRetiringResources` — Go PASS (0.15s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 31, 42. |
| **US155** 作为维护者，我希望任务领域、任务落盘、传输投影、日志、事件和采样各保留含义，以便不会互相推导或混成万能事件。 | [M-T](#m-t) | `internal/application/runtime/runtime_test.go::TestInterruptedCoverRunReplaysDeclaredDiscoveryThroughRegistry` — Go PASS (0.13s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 36. |
| **US156** 作为维护者，我希望重启入参与声明标签严格分开，以便重启只重放影响行为的输入。 | [M-T](#m-t) | `internal/application/scanning/cover_test.go::TestCoverRestartReplaysBackfillIndependentlyOfTrigger` — Go PASS (0.2s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 37. |
| **US157** 作为管理员，我希望每次合并丢失不同重启入参都留下事件，以便强制等行为声明丢失时可以查明。 | [M-T](#m-t) | `internal/task/events_test.go::TestEveryLossyCoalesceRetainsIncomingDeclaration` — Go PASS (0s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 37. |
| **US158** 作为管理员，我希望无损合并继续控制重复事件噪音，以便监听与守护不会制造大量无意义记录。 | [M-T](#m-t) | `internal/task/events_test.go::TestRepeatedCoalescingLandsOneEvent` — Go PASS (0s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 37. |
| **US159** 作为维护者，我希望发起方只说明谁发起，以便重启行为不再藏在触发类型中。 | [M-T](#m-t) | `internal/application/scanning/cover_test.go::TestCoverRestartReplaysBackfillIndependentlyOfTrigger` — Go PASS (0.2s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 37. |
| **US160** 作为维护者，我希望每个启动点的参数、标签、上限与指标有唯一归属，以便同一事实不在多个通道重复报送。 | [M-T](#m-t) | `internal/application/runtime/runtime_test.go::TestInterruptedCoverRunReplaysDeclaredDiscoveryThroughRegistry` — Go PASS (0.13s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 37. |
| **US161** 作为管理员，我希望任务身份、队列、全局槽位与卷级 IO 限额继续各司其职，以便整理不改变运行控制语义。 | [M-T](#m-t) | `internal/task/pause_all_test.go::TestPauseAllHoldsTheQueue` — Go PASS (0s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 37, 45. |
| **US162** 作为客户端使用者，我希望运行 HTTP/SSE 契约和 TypeScript 生成结果保持兼容，以便类型归位不改变公开名称。 | [M-T](#m-t) | `internal/api/task_push_frames_test.go::TestSnapshotFrameCarriesTheWholeRun` — Go PASS (0.03s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 36. |
| **US163** 作为维护者，我希望传输模型与转换按功能组织，以便 Controller 中结构体减少对应明确归属而非污染领域。 | [M-T](#m-t) | `internal/application/runtime/runtime_test.go::TestInterruptedCoverRunReplaysDeclaredDiscoveryThroughRegistry` — Go PASS (0.13s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 36, 43, 44, 45, 47. |
| **US164** 作为维护者，我希望运行中的日志保留任务与运行上下文，以便迁移后仍可按运行查看故障与告警。 | [M-T](#m-t) | `internal/application/scanning/cover_test.go::TestCoverWorkerLogsUseItsOwnRunAfterTheScanFinishes` — Go PASS (0.09s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 37. |
| **US165** 作为维护者，我希望离线归属、下载、字节缓存和进度队列由完整离线功能拥有，以便鉴权与离线书架不反向依赖阅读器页面。 | [M-OFF](#m-off) | `internal/reading/sync_test.go::TestReceivedFactsAtSameTickRetainRetryIdentity` — Go PASS (0.14s)<br>`web/src/features/offline/readerOfflineConsumer.test.tsx::finishes a download for the original book after navigation without assigning it to the next book` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 33. |
| **US166** 作为阅读者，我希望断网与会话过期保留既有离线数据规则，以便暂时连不上服务不导致书目和进度丢失。 | [M-OFF](#m-off) | `internal/reading/sync_test.go::TestReceivedFactsAtSameTickRetainRetryIdentity` — Go PASS (0.14s)<br>`web/src/auth/sessionExpiryOffline.test.tsx::后端答「未登录」后，离线书目、缓存字节与进度队列原样还在` — web PASS<br>`web/src/auth/sessionExpiryOffline.test.tsx::任意请求收到 401 后，同样不动本机的离线数据` — web PASS<br>`web/src/auth/sessionExpiryOffline.test.tsx::会话过期后再断网，本人仍进得去自己下载的那本书` — web PASS<br>`web/src/auth/sessionExpiryOffline.test.tsx::状态探测连不上后端时，本机离线数据一样不动` — web PASS<br>`web/src/auth/sessionExpiryOffline.test.tsx::先把队列回传上去，再清掉书目、字节与队列` — web PASS<br>`web/src/auth/sessionExpiryOffline.test.tsx::B 登录后，A 的书目、字节与队列都不在了` — web PASS<br>`web/src/auth/sessionExpiryOffline.test.tsx::explicit logout releases offline ownership even when upload and server logout fail` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. See platform/device/provider limits. Origin: 33. |
| **US167** 作为阅读者，我希望显式登出、换人和首次认领继续遵守既有所有权规则，以便本机离线数据不会串给另一用户。 | [M-OFF](#m-off) | `internal/reading/sync_test.go::TestReceivedFactsAtSameTickRetainRetryIdentity` — Go PASS (0.14s)<br>`web/src/pages/book-reader/offlineOwner.test.ts::换成另一个用户时清掉上一个人的队列与书目` — web PASS<br>`web/src/pages/book-reader/offlineOwner.test.ts::换人时把已下载的缓存字节一并清扫掉` — web PASS<br>`web/src/pages/book-reader/offlineOwner.test.ts::同一个用户再次登录不清自己的离线数据` — web PASS<br>`web/src/pages/book-reader/offlineOwner.test.ts::升级后首次登录按当前用户认领，而不是把老数据删掉` — web PASS<br>`web/src/pages/book-reader/offlineOwner.test.ts::登出时同样清扫缓存字节` — web PASS<br>`web/src/pages/book-reader/offlineOwner.test.ts::显式登出把 owner 标记、队列与书目一并交还` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. See platform/device/provider limits. Origin: 33. |
| **US168** 作为离线阅读者，我希望重试、补传与回退保留真实时间及稳定事实身份，以便功能抽取不制造新阅读历史。 | [M-OFF](#m-off) | `internal/reading/sync_test.go::TestReceivedFactsAtSameTickRetainRetryIdentity` — Go PASS (0.14s)<br>`web/src/features/offline/readerOfflineConsumer.test.tsx::reconnect uploads original facts for both books and an old acknowledgement cannot remove a new owner fact` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. See platform/device/provider limits. Origin: 33. |
| **US169** 作为管理员，我希望配置编辑会话统一维护分区字段、草稿、保存基线和版本，以便分拆功能不会破坏并发保存。 | [M-EDIT](#m-edit) | `internal/api/config_consistency_test.go::TestConfigUpdatesSerializeSaveAndReload` — Go PASS (0.09s)<br>`web/src/pages/settings/configVersion.test.tsx::keeps edits made while saving and advances only to the acknowledged save baseline` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 32. |
| **US170** 作为管理员，我希望保存一个分区不保存其他分区草稿，以便未确认的编辑不会被顺带提交。 | [M-EDIT](#m-edit) | `internal/api/config_consistency_test.go::TestConfigUpdatesSerializeSaveAndReload` — Go PASS (0.09s)<br>`web/src/pages/settings/configVersion.test.tsx::saves only the selected section and retains unsaved credentials across the refresh` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 32. |
| **US171** 作为管理员，我希望刷新不替脏草稿换掉版本基线，以便冲突不会被悄悄掩盖。 | [M-EDIT](#m-edit) | `internal/api/config_consistency_test.go::TestConfigUpdatesSerializeSaveAndReload` — Go PASS (0.09s)<br>`web/src/pages/settings/configVersion.test.tsx::does not rebase a dirty section when an unrelated save refreshes an unchanged server section` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 32. |
| **US172** 作为维护者，我希望 KOReader 账户诊断和维护动作独立拥有生命周期，以便它们不与配置草稿共用一个功能总对象。 | [M-EDIT](#m-edit) | `internal/api/config_consistency_test.go::TestConfigUpdatesSerializeSaveAndReload` — Go PASS (0.09s)<br>`web/src/pages/settings/settingsLifecycle.test.tsx::keeps the latest device diagnostics when earlier requests finish late` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 32. |
| **US173** 作为管理员，我希望系列编辑草稿表达实际编辑意图，以便不在表单中构造数据库可空字段形状。 | [M-M](#m-m) | `internal/application/seriesediting/manual_test.go::TestManualSaveReturnsCommittedMetadataAndPreservesAbsentCollections` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 08. |
| **US174** 作为维护者，我希望列表和详情可以有各自所需视图，以便不为同名数据强行合成全站实体。 | [M-Q](#m-q) | `internal/reading/catalogue_view_test.go::TestCataloguePageBindsReaderBeforeFilteringCountingOrderingAndPagination` — Go PASS (0.18s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 02, 41. |
| **US175** 作为审核者，我希望 AI 分组动作返回后端权威候选与整单状态，以便前端不再复刻终态规则。 | [M-AI](#m-ai) | `internal/organize/ai_grouping_test.go::TestAIGroupingApplyCollectionOwnsIdentityAndAdjudication` — Go PASS (0.06s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 12, 44. |
| **US176** 作为审核者，我希望合并新审核结果保留滚动和选区，以便权威状态更新不打断操作。 | [M-AI](#m-ai) | `internal/organize/ai_grouping_test.go::TestAIGroupingApplyCollectionOwnsIdentityAndAdjudication` — Go PASS (0.06s)<br>`web/src/pages/aiGroupingReviewInbox.test.tsx::终态离开 pending 筛选后仍消费动作返回的权威候选内容` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 12. |
| **US177** 作为维护者，我希望查询、普通重试与写入影响声明分开，以便调用 reload 不隐含宣称发生跨功能变更。 | [M-FX](#m-fx) | `internal/reading/mark_test.go::TestLaterMarkBatchFailureRetainsAndInvalidatesCommittedRange` — Go PASS (0.58s)<br>`web/src/session/catalogConsumers.test.tsx::ordinary series reload and retry do not declare catalog writes` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 34. |
| **US178** 作为操作发起者，我希望写后刷新依据实际提交范围，以便部分失败也能看到已完成结果。 | [M-FX](#m-fx) | `internal/application/seriesediting/selection_test.go::TestSelectionRetainsCommittedMetadataWhenStatisticsFail` — Go PASS (0.08s)<br>`web/src/session/proposalConsumers.test.tsx::warns about committed apply with failed authoritative refresh, then retries only the query` — web PASS<br>`web/src/session/proposalConsumers.test.tsx::warns about committed reject with failed authoritative refresh, then retries only the query` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 08, 09, 12, 34. |
| **US179** 作为维护者，我希望任务、气泡和外部传输的功能类型由功能拥有，以便它们不反向导入具体页面或组件类型。 | [M-T](#m-t) | `internal/application/runtime/runtime_test.go::TestInterruptedCoverRunReplaysDeclaredDiscoveryThroughRegistry` — Go PASS (0.13s)<br>`web/src/features/task-observation/useTaskObservation.test.tsx::recovers expanded history and backend totals on gaps/reconnect while preserving filters` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 19, 36. |
| **US180** 作为管理员，我希望资料库表单提交与导航刷新归资料库功能，以便 Layout 主要持有入口和局部交互。 | [M-L](#m-l) | `internal/application/libraries/save_test.go::TestCreateLibraryKeepsCommittedResultWhenInitialScanAdmissionFails` — Go PASS (0.05s)<br>`web/src/features/libraries/useLibraryManagement.test.tsx::keeps drafts and browser location after rejected save, then distinguishes committed save from failed navigation refresh` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 24. |
| **US181** 作为阅读者，我希望零、空、未加载、失败和陈旧可区分，以便读取故障不冒充没有数据。 | [M-UI](#m-ui) | `internal/bootstrap/bootstrap_test.go::TestHTTPQueryCacheReceivesTheApplicationInvalidation` — Go PASS (0.13s)<br>`web/src/pages/QueryFailure.test.tsx::Dashboard shows genuine zero totals and explicitly empty lists after successful requests` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 35, 41. |
| **US182** 作为阅读者，我希望已有成功数据失败后可标为陈旧，首次失败提供重试，以便页面既可用又如实表达状态。 | [M-UI](#m-ui) | `internal/bootstrap/bootstrap_test.go::TestHTTPQueryCacheReceivesTheApplicationInvalidation` — Go PASS (0.13s)<br>`web/src/pages/QueryFailure.test.tsx::Dashboard preserves labelled stale overview while optional failures are retryable, then clears stale on recovery` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 35, 41. |
| **US183** 作为阅读者，我希望身份、资料库、系列、书或请求代次变化后迟到响应不能覆盖新视图，以便跨功能刷新不会破坏当前上下文。 | [M-UI](#m-ui) | `internal/bootstrap/bootstrap_test.go::TestHTTPQueryCacheReceivesTheApplicationInvalidation` — Go PASS (0.13s)<br>`web/src/pages/QueryFailure.test.tsx::recommendations in flight cannot populate the next identity` — web PASS | Representative regression + named boundary check; not exhaustive clause proof. Origin: 34, 35, 41. |
| **US184** 作为维护者，我希望初始化、角色修改、删账户和改密都有完整用例，以便不变量不散落在 HTTP 和 Store 调用之间。 | [M-ACC](#m-acc) | `internal/api/setup_atomic_test.go::TestSetupAtomicallyCreatesOnlyOneAdministrator` — Go PASS (0.13s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 38, 39, 40, 46. |
| **US185** 作为站点所有者，我希望首次管理员建立遵守初始化条件，以便并发请求不能绕过站点状态。 | [M-ACC](#m-acc) | `internal/api/setup_atomic_test.go::TestSetupAtomicallyCreatesOnlyOneAdministrator` — Go PASS (0.13s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 38. |
| **US186** 作为站点所有者，我希望最后一个管理员不能被删除或降级，即使请求并发也成立，以便不会失去管理能力。 | [M-ACC](#m-acc) | `internal/api/account_management_atomic_test.go::TestConcurrentAccountDemotionsPreserveAdministrator` — Go PASS (0.18s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 39. |
| **US187** 作为管理员，我希望禁止删除自身的既有规则继续有效，以便结构迁移不改变管理保护。 | [M-ACC](#m-acc) | `internal/api/account_management_atomic_test.go::TestAccountManagementFreshAuthorizationAndSelfDelete` — Go PASS (0.24s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 39. |
| **US188** 作为用户，我希望用户名继续按字节精确判定，以便已有独立账户不会被合并。 | [M-ACC](#m-acc) | `internal/api/setup_atomic_test.go::TestSetupSessionFailurePreservesCommittedAccountAndExactUsername` — Go PASS (0.19s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 38, 39, 40. |
| **US189** 作为用户，我希望密码、会话撤销和鉴权缓存得到一致交接，以便改密后旧资格不会继续被误用。 | [M-ACC](#m-acc) | `internal/api/password_transition_test.go::TestPasswordCommitPreventsOldInFlightBasicCredentialsFromRepopulatingCache` — Go PASS (0.44s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 40. |
| **US190** 作为站点所有者，我希望首次初始化时旧阅读数据与设备账户归属保留原有规则，以便完整用例抽取不遗失历史。 | [M-ACC](#m-acc) | `internal/api/setup_atomic_test.go::TestSetupClaimsLegacyActivityAndDeviceAccountWithoutMergingCredentials` — Go PASS (0.16s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 38. |
| **US191** 作为用户，我希望协议账号和站点用户身份继续区分，以便设备匹配不会越过账户边界。 | [M-ACC](#m-acc) | `internal/api/password_transition_test.go::TestPasswordResetAuthorizationAndProtocolEligibility` — Go PASS (0.69s) | Representative regression + named boundary check; not exhaustive clause proof. See platform/device/provider limits. Origin: 26, 38, 40. |
| **US192** 作为维护者，我希望 Cookie、CSRF、限流和协议认证保留传输安全职责，以便账户领域不吸收全部 HTTP 细节。 | [M-ACC](#m-acc) | `internal/api/security_p0_test.go::TestSetupRoutesEnforcesSession` — Go PASS (0.22s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 38, 40, 46. |
| **US193** 作为维护者，我希望账户规则使用真实事务验证，以便仅靠事务外计数不能被视为原子保证。 | [M-ACC](#m-acc) | `internal/api/account_management_atomic_test.go::TestConcurrentCrossAccountDeletesAndStaleActorKeepAdministrator` — Go PASS (0.18s) | Representative regression + named boundary check; not exhaustive clause proof. Origin: 38, 39, 40. |
| **US194** 作为维护者，我希望通过完整功能接口和真实消费者验收，以便测试不会只镜像内部辅助方法。 | [M-A](#m-a) | `internal/bootstrap/reader_http_test.go::TestBoundReaderPositionRatingAndProtocolConsumers` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 48 evidence/delivery. |
| **US195** 作为维护者，我希望真实临时 SQLite 覆盖提交、回滚、竞争和重启，以便模拟仓储不能掩盖事务错误。 | [M-A](#m-a) | `internal/organize/franchise_facts_test.go::TestFranchiseCascadeSourcesRecoverAfterReopenWithStableIdentity` — Go PASS (0.28s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 48 evidence/delivery. |
| **US196** 作为维护者，我希望文件和资源测试覆盖暂停、取消、占用、替换与后续失败，以便只测成功路径不能掩盖数据风险。 | [M-A](#m-a) | `internal/application/diskjobs/transfer_pause_test.go::TestTransferRunPauseResumeAndCancelKeepCommittedRange` — Go PASS (0.71s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. See platform/device/provider limits. Origin: 18, 21. |
| **US197** 作为维护者，我希望装配测试覆盖恢复前禁止开放与停机等待，以便模块化后承重时序仍可执行验证。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestShutdownWaitsForHTTPConsumerBeforeStoppingEngine` — Go PASS (0.06s)<br>`internal/bootstrap/sse_shutdown_test.go::TestShutdownClosesLiveHostSSEBeforeDrainingHTTPAndRetiringResources` — Go PASS (0.15s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 25, 42. |
| **US198** 作为维护者，我希望前端通过功能消费者、路由和必要页面验证可见结果，以便不以 hook 调用次数代替正确行为。 | [M-A](#m-a) | `internal/bootstrap/reader_http_test.go::TestBoundReaderPositionRatingAndProtocolConsumers` — Go PASS (0.21s)<br>`web/src/session/ApplicationRoutes.test.tsx::actual navigation routes share the session, refresh the library and release it in reader and graph` — web PASS | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 48 evidence/delivery. |
| **US199** 作为维护者，我希望保留必要 HTTP 和设备协议回归，以便模块测试通过时外部使用方式仍兼容。 | [M-A](#m-a) | `internal/bootstrap/reader_http_test.go::TestBoundReaderPositionRatingAndProtocolConsumers` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. See platform/device/provider limits. Origin: 17. |
| **US200** 作为维护者，我希望所有主要区域有规格到实现及执行证据映射，以便工单完成不能代替整个项目的归属验收。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 48 evidence/delivery. |
| **US201** 作为维护者，我希望格式、lint、生成契约和多平台门禁通过，以便移动符号和类型不会留下构建或漂移问题。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. See platform/device/provider limits. Origin: 48 evidence/delivery. |
| **US202** 作为维护者，我希望删除旧副本及无人调用桥并更新当前文档，以便交付后不会留下两份权威或过期实施状态。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 07, 47. |
| **US203** 作为使用者，我希望变更记录说明批准的行为纠正，以便升级时可以理解冲突、失败和恢复的新结果。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. Origin: 48 evidence/delivery. |
| **US204** 作为维护者，我希望区分未实现、测试存在、实际通过和未执行限制，以便规格就绪不会被当成运行成果。 | [M-A](#m-a) | `internal/bootstrap/bootstrap_test.go::TestOpenUsesOneManagerQueueAndServesBoundHTTP` — Go PASS (0.21s) | Structural/delivery: catalogue, joins and gates are required; behavioral witness alone insufficient. See platform/device/provider limits. Origin: 48 evidence/delivery. |
