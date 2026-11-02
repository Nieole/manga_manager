# 46: 配置账户与管理适配迁移

**What to build:** 设置、账户及其余管理路由按功能注入，安全与技术适配不持有全站业务对象。

**Blocked by:** 32: 设置分区编辑会话；39: 账户管理与最后管理员保护；40: 改密与会话资格交接；42: 独立装配根与新宿主扩展。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 4、5、8、9、13、137、146、152、184、192。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 广泛宿主迁移批次。

- [ ] 消费配置/资源/账户/供应商明确能力，校验、保存和会话后续不再由 HTTP 拼装。
- [ ] 健康、日志、连接与目录浏览等技术管理入口按实际职责收窄，避免遗漏 Controller 尾部能力。
- [ ] Cookie/CSRF、限流、密钥及版本冲突保持传输与功能的合理分工。
- [ ] 管理页面、认证协议、配置回归和公开类型检查通过，兼容批次保持可构建。

## Comments

### Implementation / evidence (2026-11-02; uncommitted, integration pending)

Worktree `C:/Users/saber/dev/manga-refinement-46`, branch `codex/refinement-46`, base `da6811ba011151d81d4bdb0605436945d576f06f`. Retry began clean. Ready for parent consumption as a dirty diff; no commit, merge, index mutation or main-tree edit. Ticket remains open until integration/acceptance.

Implementation:

- `administrationHTTP` receives the authoritative Manager, configuration publication owner, explicit account/session/diagnostic capabilities and KOReader service. No Controller, Runtime, full Store, Engine, SSE or content/cache dependency. Account initialization/management/password owners are bound once for this family and retain existing atomic revocation/last-admin rules.
- Production Host route composition directly mounts the family's auth gate and handlers: accounts, config/capabilities, directory browsing, health, client connections, logs, request-performance summary, LLM connection testing and KOReader administrative settings/accounts/diagnostics. HTTP retains Cookie/CSRF, proxy trust, rate limits, masking/localization and post-commit replacement-browser-session handling.
- `configuration.Service.SaveKOReaderSettings` owns section normalization, validation, optimistic version checking and section-only publication. Manager.Save remains authoritative; Reload/Coordinate compensation was not replaced or conflated. No JSON shape or localized error changed.
- Same single runtime/Engine/Manager, authState, Basic credential-cache generation, limiters, SSE and query/content cache leaves. Full Host/OPDS regression warms Basic credentials, resets via admin HTTP, observes old browser/Basic rejection, then forced-password-change and final-password eligibility.
- Left to siblings: KOReader reading protocol handlers (43); content/page-cache endpoints (43); maintenance launches including KOReader rebuild/matching/reconcile, runs/storage-IO/disk (45). No scanner/restoration cleanup or race fix attempted.

Approved real seams and TDD:

- `bootstrap.Open` + independent Host administration HTTP: `.tmp/46-red-config.log` fails compilation on absent `SetupAdministrationRoutes`; `.tmp/46-green-config.log` passes after implementation. Final consumers cover setup/session/CSRF, private config, required/stale ETag, masking with authoritative Manager publication and KOReader section save/conflict. Full Host + OPDS regression covers admin-reset committed acknowledgment, browser revocation and shared credential-cache handoff (`.tmp/46-auth-consumer.log`).
- `configuration.Service` settings: `.tmp/46-red-settings.log` fails compilation on missing KOReader command/input; `.tmp/46-green-settings.log` passes after extraction. Temporary files/Manager verify invalid input is not published, unrelated fields survive, saved/published facts and missing/stale versions. No internal mocks or production bypasses.

All Go commands: `GOTOOLCHAIN=go1.26.10`, shared `GOCACHE=C:/Users/saber/dev/manga_manager/.gocache`, own `GOTMPDIR=C:/Users/saber/dev/manga-refinement-46/.tmp`; lint uses own `.tmp/lint-cache`. Ignored main `web/dist` and `node_modules` copied here; final frontend build rebuilt this tree's dist.

Actual verification:

1. Final `go test ./...`: PASS; API 132.307s, bootstrap 2.399s, configuration 1.436s, config 3.893s; all packages passed (some unchanged packages cached). `.tmp/46-all-go-final.log`. Full suite naturally includes existing scanner/restoration tests; no suppression/competing cleanup.
2. `go test -race ./internal/api -run 'Test.*Password|Test.*Session|Test.*Auth|Test.*Admin|Test.*User|Test.*Setup|Test.*Config|Test.*KOReader|Test.*ClientConnection|Test.*Health|Test.*Cookie|Test.*CSRF|Test.*Proxy|Test.*ClientIP' -count=1 -timeout=8m`: PASS 296.478s, `.tmp/46-api-auth-config-race.log`.
3. After retaining protocol receivers for 43: `go test -race ./internal/bootstrap ./internal/application/configuration ./internal/api -run 'TestAdministration|TestKOReaderSettingsSave|TestPasswordCommitPreventsOldInFlightBasicCredentialsFromRepopulatingCache|Test.*KOReader.*|Test.*Config' -count=1 -timeout=4m`: PASS 16.791s / 1.069s / 48.021s, `.tmp/46-final-migration-race.log`. Complete bootstrap/configuration/config race also PASS 22.784s / 1.741s / 3.776s (`.tmp/46-owner-host-final-race.log`). Accounts has no standalone test files; real owner behavior runs via HTTP SQLite fixtures.
4. `go build ./...`: PASS (`.tmp/46-build-final.log`); `golangci-lint run`: `0 issues.` (`.tmp/46-lint-final.log`); `gofmt -l ./cmd ./internal`: empty (`.tmp/46-gofmt.log`); `git diff --check`: PASS. `bash scripts/check-doc-style.sh`: PASS, existing advisory history warnings only (`.tmp/46-doc-style-final.log`).
5. `go run ./cmd/tsgen`: 26 types, `git diff --exit-code -- web/src/api/generated.ts`: no drift (`.tmp/46-tsgen-final.log`, `.tmp/46-ts-drift.log`). No SQL/schema changes; sqlc not needed.
6. Node v24.16.0 via fnm: `npm test -- src/auth src/pages/settings src/utils/apiAuth.test.ts src/features/settings-actions src/features/settings-editor src/features/koreader-settings`: 10 files / 78 tests PASS (`.tmp/46-web-consumers.log`); `npm run lint` and `npm run build`: PASS (`.tmp/46-web-lint.log`, `.tmp/46-web-build.log`). No frontend production or generated-contract changes.

Unsuccessful attempts retained: initial broad race exceeded tool's 300s wall bound after bootstrap/configuration/config passed; no API result established (`.tmp/46-race.log`); longer explicit API race above passed. Initial lint found unused compatibility forwards; removed those and obsolete `requireConfigVersion`, no suppression (`.tmp/46-lint.log`). Initial npm lacked fnm PATH (`.tmp/46-web-tests.log`), actual fnm-enabled commands passed. First doc-style invocation exceeded 120s bound; longer final invocation passed. No external provider/device or non-Windows execution claimed.

Integration conflict sites / ticket 47 exits:

- `internal/api/host.go`: extend AdministrationDependencies, add family field/Bind construction, pass family into SetupRoutes. Preserve sibling bindings and sole shared leaves.
- `internal/api/controller.go`: route composition accepts administration adapter; family auth gate/mount replace only migrated registrations/helpers. Sibling migrations must not restore removed admin registrations.
- `internal/bootstrap/bootstrap.go`: only AdministrationDependencies literal adds Store/Manager/KOReader. ADR0014 adds current-state paragraph.
- `koreader_controller.go`: administrative receiver changes and settings command extraction only; reading protocol and three maintenance endpoints keep Controller receivers. `request_metrics.go` changes only performance-summary receiver.
- 47 removes `administration_compat.go`, Controller constructors/legacy full Store and family fields after remaining callers/tests migrate; finish moving route composition to Host. Reader/protocol callers of `currentUserID`, `clientIP`, `requireBasicAuth` must bind the SAME auth leaf, not another authState/cache/limiter. Embedded fixture forwarding binds the same state but is not the migrated production admin route path. Existing API/TS names and sole startup/shutdown owner remain intact.


### MERGER46 combined verification — 2026-11-02 (merge intentionally unfinalized)

- Resumed the existing merge in `C:/Users/saber/dev/manga-refinement-46`, `codex/refinement-46`: HEAD `21756e8`, actual MERGE_HEAD `7589b15` (integrated library/runs/disk families). Preserved existing index, merge state, working files and prior logs; no staging, commit, new merge, reset, clean, abort, main-tree edit or push. Parent must stage the four marker-free resolutions and finalize the existing merge.
- `controller.go` combines direct administration routes with ReaderHTTP, organization and library families under one auth gate; shared books/series chi subtrees retain all migrated readers, edits and disk routes. Administration owns directory browsing and performance/config/accounts/KOReader settings; library owns storage/run/maintenance routes. No duplicate mounts or admin registrations. `host.go` binds both families and administration with the existing authState, Engine/run projection, Manager, caches and SSE. `bootstrap.go` preserves reader/organization/library narrow inputs and adds administration Store/Manager/KOReader to the same graph. `controller_system_config.go` keeps the administration directory-browser receiver.
- Minimal actual combination corrections: remove duplicate library `Controller.browseDirs` forwarding (administration already owns it); supply newly required administration capabilities in the real bound library Host fixture; remove the now-unused `Controller.currentUserID` forward after reader migration. Extended the production config publication test to run both standalone and full combined Host routing, preserving all secret-masking, missing/stale version and section-only KOReader save assertions. No auth-generation, password/session reset, limiter, protocol/query, declaration/replay, content read-guard or application behavior was rewritten.
- Reviewed auto-merged `koreader_controller.go`: administrative receivers and configuration-owned section save remain; the three maintenance launches retain `maintenanceHTTP`, reading protocol receivers remain intact. Performance remains `administrationHTTP`; content guards remain shared. Reviewed ADR0014 and updated current-state language for all four families sharing the auth subtree. Ticket 37 declarations and proposal fixes are preserved.

Actual gates (`GOTOOLCHAIN=go1.26.10`, shared main `.gocache`, own `.tmp` and `.tmp/merge46-lint`):

- Combined API/bootstrap/cmd suite PASS: API 99.646s, bootstrap 1.521s (`.tmp/merge46-consumers.log`). Final uncached `go test ./... -count=1 -timeout=10m` PASS: API 103.058s, bootstrap 2.595s, all packages passed (`.tmp/merge46-all-go-final.log`).
- Broad four-family focused API race PASS 570.082s (`.tmp/merge46-four-family-race.log`): reading/catalogue/protocols, organization/reviews, libraries/disk/maintenance/runs, accounts/auth/config/KOReader. Not claimed as whole-repository race.
- Full bootstrap plus configuration/config, task/taskstore, scanning/diskjobs/maintenance races PASS (`.tmp/merge46-bootstrap-owners-race.log`); bootstrap 23.056s. Final complete bootstrap race after the combined config consumer extension PASS 24.748s (`.tmp/merge46-bootstrap-race-final.log`).
- Explicit password/session/auth/reset/credential/config races PASS: bootstrap 13.406s, API 109.757s, configuration 1.106s, config 1.571s (`.tmp/merge46-auth-config-race.log`), including in-flight Basic credential generation and production reset/OPDS handoff regressions.
- Final lint PASS `0 issues.` (`.tmp/merge46-lint-final2.log`), build PASS (`.tmp/merge46-build-final.log`), gofmt/diff-check clean. TS generation 26 types and no generated.ts drift (`.tmp/merge46-tsgen.log`). No SQL/schema changes.
- Targeted combined frontend consumers PASS 33 files / 276 tests (`.tmp/merge46-web-consumers.log`): auth/settings/KOReader, session/catalog/proposal consumers, reading, task observation/center, external library and collection/review consumers. ESLint and TypeScript/Vite build PASS (`.tmp/merge46-web-lint.log`, `.tmp/merge46-web-build.log`); Node v24.21.0 via fnm. Final Go build uses this tree's rebuilt assets.
- Preserved unsuccessful attempts honestly: `.tmp/merge46-initial.log` records unresolved-marker compilation after Python's default Windows encoding rejected UTF-8 before edits; `.tmp/merge46-initial2.log` records the real missing-administration fixture panic, corrected by binding actual consumed capabilities, not a production fallback. `.tmp/merge46-lint.log` records the unused currentUserID forward before removal. No gate timeout occurred in this merge verification.

47 exit readiness: integration gates above are green; contraction is not implemented here. The external `manga-refinement-47-exit-inventory.md` is still a pre-merge reconnaissance snapshot and needs a fresh merged symbol/caller inventory. Retain live Controller constructors/runtime assembly and reader/library/administration forwarding only until 47 converts their callers; remaining cover/catalogue/cache/SSE/protocol leaf composition and concrete relation/proposal compatibility binding remain explicit 47 obligations. Preserve single auth/cache/SSE/Engine/Manager, persisted declarations and protocol compatibility. No external device/provider or other-platform verification claimed; ticket remains open pending parent integration.
