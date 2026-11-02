# 44: 元数据与藏书编排 HTTP 迁移

**What to build:** 编辑、提案、合集、作品群与清单路由注入所需完整能力，传输类型按功能归位。

**Blocked by:** 09: 批量元数据编辑的提交结果；10: 提案部分应用与终态竞争；12: AI 分组的权威裁决结果；14: 级联变化与作品群修复覆盖；15: 阅读清单顺序与窄写用例；42: 独立装配根与新宿主扩展。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 4、5、6、7、8、9、10、13、33、38、56、58、152、163、175。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 广泛宿主迁移批次。

- [ ] 依赖已完成元数据/裁决/派生/清单能力，不再拼事务与收尾配方。
- [ ] 生成契约、权限、来源差异及审核权威结果保持。
- [ ] 各路由和前端真实消费者迁移，旧兼容仅服务其他未迁家庭。
- [ ] SQLite/HTTP/页面回归及类型生成检查通过，批次独立可构建。

## Comments


### 2026-11-02 resumed implementation evidence (uncommitted; ticket remains open)

- Worktree `C:/Users/saber/dev/manga-refinement-44`, branch `codex/refinement-44`, base `da6811b`. Inspected existing dirty diff and `.tmp/44-{red-collection,green-collection,go-full}.log` before continuing; retained all dirty tracked/untracked implementation. No commits, index/history changes, merge, clean, tree deletion or main-worktree edits.
- `organizationHTTP` consumes collections/shelves/relations/ordered lists/metadata selection and save/proposals/AI adjudication plus purpose-limited queries and leaf effects. It holds no Controller, runtime or full Store. Production bootstrap binds the existing application proposal/relation owners and same Manager/SSE/cache invalidation leaves; no new Engine, auth state, runtime or query cache. The actual family routes now mount through `setupRoutes` inside the existing host auth gate, rather than merely relying on promoted handler methods.
- Metadata request DTOs moved beside the metadata HTTP adapter. Existing JSON/source semantics, optimistic versions, partial commit classifications, collection permissions, ordered-list transactions, batch query hydration and authoritative AI candidate snapshots remain unchanged. Existing frontend consumers require no source changes.
- Updated four fault-injection test files to bind the capability actually consumed after migration, rather than swapping the now-unrelated compatibility Store/proposal field. The original `.tmp/44-go-full.log` failures are retained; no production fallback to the compatibility Store was added to satisfy them.
- Approved seams used: real SQLite + HTTP router and production bootstrap/Host; existing owner/use-case and frontend consumer tests. Prior red/green collection logs retained. New route red: `go test ./internal/api -run '^TestOrganizationHTTPCollectionWithoutController$' -count=1` failed with missing `setupRoutes` (`.tmp/44-red-routes.log`), then real routed create/list plus transaction-current race/failure regressions passed (`.tmp/44-green-routes.log`, 0.816s). Standalone fixture creates only SQLite + organization adapter, not a Controller. Production Host test verifies committed collection members, metadata save/selection, all family query entrypoints, and shared session/CSRF (`.tmp/44-production-http.log`, 0.239s).
- Environment: `GOTOOLCHAIN=go1.26.10`, shared `GOCACHE=C:/Users/saber/dev/manga_manager/.gocache`, own `GOTMPDIR=C:/Users/saber/dev/manga-refinement-44/.tmp` and `GOLANGCI_LINT_CACHE=.../.tmp/lint-cache`. Existing ignored dist retained; copied ignored main `web/node_modules` for consumer gates. Node initialized with `eval "$(fnm env --shell bash)"; fnm use default` (v24.21.0).
- PASS `go test ./...`: `.tmp/44-go-all.log` (API 115.370s; every package passed).
- PASS owner/production race: `go test -race ./internal/api ./internal/bootstrap ./internal/organize ./internal/proposal ./internal/application/seriesediting -count=1 -timeout=6m` passed bootstrap 12.890s, organize 56.785s, proposal 55.243s, seriesediting 6.517s, but full API exceeded the 6m package deadline (`.tmp/44-race-retry.log`). This command is NOT claimed wholly passed. Earlier parallel invocation was terminated by tool timeout and has empty `.tmp/44-race.log`.
- PASS focused HTTP family race: `go test -race ./internal/api -run 'Test(OrganizationHTTP|.*Collection|.*SmartFilter|.*ReadingList|.*AIGrouping|.*Metadata|PartialApplyHTTP|.*Franchise|.*Relation|.*ReviewInbox|.*Organized)' -count=1 -timeout=10m` (142.795s, `.tmp/44-race-family.log`). Additional optimistic-save/locked-status race `-run 'Test(UpdateSeriesInfo|ApplyReports)' -count=1 -timeout=3m` passed 14.074s (`.tmp/44-race-manual-status.log`). Known integration race/cleanup work was neither duplicated nor suppressed.
- PASS `golangci-lint run`: final `.tmp/44-lint-final.log`, `0 issues.`; `go build ./...`: `.tmp/44-build-final.log`; `gofmt -l ./cmd ./internal`: empty `.tmp/44-gofmt.log`; `git diff --check`: empty `.tmp/44-diff-check.log`.
- PASS `go run ./cmd/tsgen`: 26 types (`.tmp/44-tsgen.log`), then `git diff --exit-code -- web/src/api/generated.ts` (empty `.tmp/44-contract-drift.log`). No SQL changes.
- PASS web consumers: `npm run test -- src/pages/collections src/pages/aiGroupingReviewInbox.test.tsx src/pages/metadataReviewBulk.test.ts src/pages/metadataReviewConcurrency.test.tsx src/pages/organizeAdminActions.test.tsx src/pages/organizeLibrarySwitchRace.test.tsx src/pages/readingListsSwitchRace.test.tsx src/pages/reviewInboxStaleFailure.test.tsx src/features/organize/FranchiseRepairStatus.test.tsx`: 16 files / 64 tests (`.tmp/44-web-consumers.log`). `npm run lint` and `npm run build` passed (`.tmp/44-web-{lint,build}.log`).
- Open verification limitation: full API race did not finish within 6m; focused family and owner races above actually passed. Whole-repo doc-style script was tool-timed-out amid MSYS fork/resource errors (`.tmp/44-doc-style.log`), not claimed passed.
- 47 exit obligations: remove legacy `Controller` construction/`assembleRuntime` duplicate compatibility bindings, embedded family method compatibility used by older test/protocol callers, and concrete relation/proposal compatibility fields/type assertions in `Transport.Bind` once 43/45 consumers are migrated. Keep the existing proposal/relation owners, shared authentication/SSE/query caches and unique Engine/Manager; do not reassemble an application from this adapter. Coordinate protocol query helpers (`loadCollectionViews`, static/smart members, `readingLists().Page`) with 43 rather than deleting live consumers. `controller_series.go` changes only remove metadata-save declarations/handler and their now-unused imports; catalogue/context sections are untouched.
- Ready for parent review/integration as dirty scope; ticket intentionally remains `ready-for-agent` and unchecked pending integration/combined acceptance.
