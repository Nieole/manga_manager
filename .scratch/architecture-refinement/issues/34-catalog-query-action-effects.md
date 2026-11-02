# 34: 查询与已提交操作影响分离

**What to build:** 系列/资料库查询和重试只取数，读态及编辑动作按实际提交结果宣告刷新影响。

**Blocked by:** 05: 阅读位置与标记的窄事务；09: 批量元数据编辑的提交结果。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 177、178、183。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [ ] 消费已稳定阅读/批量元数据提交结果，明确成功、部分提交和后续告警。
- [x] 普通 reload/重试不隐含跨功能写入失效，写入影响由动作 owner 发出。
- [x] 保留页面筛选、选区、草稿，按目标及请求代次隔离迟到结果。
- [x] 功能消费者验证重试、部分失败与跨页面刷新，不引入万能事件总线。

## Comments

### Implementation evidence (implementation worktree; awaiting integration)

- Worktree: `C:/Users/saber/dev/manga-refinement-34`, branch `codex/refinement-34`. Started clean at `b913455f22f1b7eff431ffec2e555d1ea41987c1`; no pre-existing changes removed. Status remains open for independent merger acceptance.
- `useSeriesContext.reload/retry` and `useLibrarySeries.refreshLoadedSeries/retry/patchSeries` now only maintain query projections. List callbacks and requests are fenced by query target, page and enabled state, including unmount.
- Audited production callers before removing effects: detail progress, manual edit, proposal apply/reject, scrape and scrape-apply; library selection reading/favorite, card favorite, scrape success and bulk-edit success. Manual/bulk metadata and applied proposals publish catalog effects; queued/rejected proposals publish review effects; favorite effects moved out of projection helpers. Bulk metadata continues using the existing committed/error-response ID and refresh-warning owner, not a duplicate implementation. Rescan continues relying on completion events rather than pretending queue admission changed catalog data.
- Reading mutation consumes `committed_book_ids` from both success and partial HTTP failures. Unknown/uncommitted failures do not publish effects. Partial/uncertain writes retain their operation identity/time; complete responses release it. Successful no-change replays can still trigger local queries without declaring a new cross-feature commit. Committed writes and refresh warnings are separate; refresh failure does not resend the write. Local completion/selection changes are fenced by series/library target, while known commits still refresh session consumers.
- Added real session/API-adapter consumers for query retry purity, filtered library refresh/patch preserving selection, known partial reading commits with stable retry identity, uncommitted failure purity, committed reading plus failed refresh and late completion after switching series. Existing draft/version, stale save, BulkEditSeriesModal and query-generation consumers remain covered. No settings/locales, auth, offline, reader or shared request-lifetime files changed; no universal store/bus added.

Actual verification (all logs preserved under this worktree `.tmp/`):

- Red/green evidence: `34-red-query.log` (query revision incorrectly changed), `34-red-reading.log` (partial commit thrown away), `34-red-refresh.log` (committed write reported as ordinary success despite refresh failure); corresponding green logs retained. Initial environment failures and intermediate failures were retained, not counted as passes.
- Frontend package-script equivalents used the downloaded ignored `.tmp/node.exe` (v24.15.0), because `node`/`npm` were absent from PATH; dependencies copied from the existing integration worktree's ignored `web/node_modules`.
- From `web`: `../.tmp/node.exe node_modules/vitest/vitest.mjs run --maxWorkers=2`: **103 files / 961 tests passed**, 142.99s (`34-web-final.log`). Prior full run passed 103 / 960 (`34-web-full-retry.log`); first full launch exceeded the 120s tool timeout (`34-web-full.log`) and is not a pass.
- Focused query/edit/read consumers: **5 files / 28 passed** (`34-focused.log`). Session/read/BulkEdit modal consumers: **3 files / 17 passed** (`34-consumers-final.log`). These are supplemental, not additions to the final full-suite count.
- `../.tmp/node.exe node_modules/eslint/bin/eslint.js .`: **passed**, no output (`34-lint-final.log`). `../.tmp/node.exe node_modules/typescript/bin/tsc -b` followed by `../.tmp/node.exe node_modules/vite/bin/vite.js build`: **passed** (`34-build-final.log`).
- `GOCACHE=C:/Users/saber/dev/manga_manager/.gocache GOTMPDIR=C:/Users/saber/dev/manga-refinement-34/.tmp/go go run ./cmd/tsgen`: **26 types generated; no generated.ts drift** (`34-contract.log`). No Go/SQL contract or source change.
- Same cache/temp environment, `go test ./internal/reading -run 'Test(Mark|LaterMark)' -count=1`: **passed**, 0.684s (`34-reading-contract.log`); `go test ./internal/api -run 'Test(BulkMark|BulkMetadata)' -count=1`: **passed**, 0.953s (`34-http-contract.log`).
- `git diff --check`: **passed**, no output (`34-diff-check.log`). `bash scripts/check-doc-style.sh`: **not verified**; Windows fork failed with `0xC0000142` / resource temporarily unavailable and the tool timed out (`34-doc-style.log`).

Integration/commit restriction: the agent's governing instructions prohibit all index/history-mutating git commands and require leaving changes uncommitted. Therefore no commit or merge was attempted, despite the ticket request. Verification above consumes **b913455f22f1b7eff431ffec2e555d1ea41987c1**, not the later integration tip **16cfc9f834f4507b262553541d90206bb72918a0** observed at final checks. The independent owner must merge that tip into this implementation branch, rerun final gates, commit these scoped changes and review/close the ticket. No main-worktree source edits, integration merge, push or worktree deletion performed.

### Blocking review follow-up: own invalidation versus authoritative refresh

- Preserved all 17 previously dirty implementation files; additional edits only in `useSeriesEdit.ts`, `useReadStateMutation.ts`, `useSeriesProgress.ts`, `catalogConsumers.test.tsx`, this ticket, the owning design, and CHANGELOG. Ticket remains **ready-for-agent**, pending independent integration/acceptance.
- Real session/context/edit and reading consumers now hold adapter responses instead of immediately rejecting them. RED (`.tmp/34-review-red.log`): edit lost `savedRefreshFailed`; reading emitted ordinary success after both competing context requests failed (2 failures / 12 passes). This reproduces the generation-guard ordering defect rather than just a mocked reload failure.
- Manual save awaits local refresh before publishing shared effects in `finally`. Reading mutation accepts an optional local-refresh continuation, also completed before unconditional committed invalidation in `finally`; detail progress uses it and owns its refresh warning. Queries remain pure. No session/store/event-bus change. Known partial commits still publish; no-change responses still allow local refresh without declaring a commit. No write is retried because refresh failed.
- Added nine deferred real-consumer cases: editing, complete reading and partial reading refresh failures; all three switching targets during either pending write or pending refresh. They assert committed effects, retained new-target drafts, no stale toast/error, and no ordinary-success toast on authoritative refresh failure. Existing retry-identity and partial-selection consumers remain intact. GREEN: `.tmp/34-review-green.log` (2 files / 16 tests); `.tmp/34-review-switches.log` (1 file / 20 tests).
- Full frontend suite: **103 files / 970 tests passed**, 127.19s (`.tmp/34-review-web-full.log`), using existing `.tmp/node.exe` and `--maxWorkers=2`. ESLint **passed** (`.tmp/34-review-lint.log`); TypeScript + Vite build **passed** (`.tmp/34-review-build.log`). Commands are package-script equivalents documented above.
- Contract gates rerun with the same Go cache/temp paths: tsgen **26 types, no generated.ts drift** (`.tmp/34-review-contract.log`); reading Mark/LaterMark contracts **passed**, 0.490s (`.tmp/34-review-reading-contract.log`); API BulkMark/BulkMetadata contracts **passed**, 0.448s (`.tmp/34-review-http-contract.log`). No Go, SQL or wire contract edits.
- Verification is still on worktree base `b913455`, **not** latest main `72cf8b2` (including tickets 25/32/33/42). Governing policy forbids commits/merges/index mutation; parent must integrate and rerun gates. No main source edits, reset/clean/push, commit, merge or tree deletion. Shared invalidation follows local refresh settlement, so other consumers wait for that request; a permanently pending network request remains subject to existing request timeout behavior. Independent concurrent invalidations retain existing generation semantics; this fix coordinates the action's own invalidation only.

- Follow-up `bash scripts/check-doc-style.sh` again exceeded the 120s tool timeout; not counted as passing (`.tmp/34-review-doc-style.log`). `git diff --check` passed (`.tmp/34-review-diff-check.log`).

### Independent plan-review blocker rc_d9a20c81: proposal consumer follow-up

- Isolated worktree `C:/Users/saber/dev/manga-refinement-34-proposal`, branch `codex/refinement-34-proposal`, base `2b28bbe`. Only proposal-detail consumer/hook, its two locale messages, consumer tests, owning design, CHANGELOG and this evidence changed. Main worktree and independent HTTP tickets 43–46 were not edited. Ticket status is deliberately unchanged; parent owns reopening/integration and independent plan re-review.
- Reproduced the actual production swallower using the real `ApplicationSessionProvider`, `useSeriesContext` and `useSeriesMetadataReview`, with deferred Axios adapter responses (not a rejecting mock reload). `.tmp34-proposal-red-swallower.log` runs the focused new regression against the original base hook: POST committed, context GET failed, context error was visible, but the toast was `metadataReviewApplied / success` rather than `savedRefreshFailed / error`. The original hook was temporarily restored through read-only `git show` for this baseline replay, then the implementation was restored; no git index/history operations.
- Apply and reject now explicitly await `reload(true)` and publish their known committed shared effects in `finally` only after that refresh settles. Failure or target switch does not cancel committed effects. Apply uses the existing semantically correct metadata-saved refresh warning; reject has a dedicated bilingual already-rejected refresh warning. Write failures alone retain write-failure messaging and publish nothing. `applied:false` publishes nothing and uses a query-failure message if its refresh fails, never a claim of saved metadata. No UI layout/CSS redesign, transport contract, session engine, context swallower or unrelated action changes.
- Target/mount scope fences both actions' late success/error toasts, stale callback invocation and busy cleanup. A per-scope pending guard blocks duplicate submissions while the action is waiting for either write or refresh. New-target actions can proceed independently; old completion cannot clear their busy state. Query retry does not replay writes. No automatic retry of committed operations was added.
- RED→GREEN slices: `.tmp34-proposal-red-refresh.log` proves premature catalog invalidation; `-green-refresh.log` passes the first real refresh-failure consumer. `-red-scope.log` proves old busy state leaking into a new series; `-green-scope.log` passes scoped apply cases. `-red-reject.log` proves premature review invalidation and stale rejection toasts; `-green-reject.log` passes. `-red-no-change.log` proves a no-change response incorrectly claimed saved metadata. Final focused `.tmp34-proposal-green-final.log`: **16/16 passed**, including the direct swallower regression replay. Baseline/intermediate logs are failures/evidence, not counted as gates.
- Final frontend verification (package-script equivalents; existing Node `C:/Users/saber/dev/manga-refinement-34/.tmp/node.exe`; ignored `web/node_modules` and `web/dist` copied from main before tests):
  - `node node_modules/vitest/vitest.mjs run --pool=threads --maxWorkers=2 --exclude=src/utils/heatmap.test.ts`: **106 files / 1008 tests passed**, 178.82s (`.tmp34-proposal-web-threads-final.log`).
  - `node node_modules/vitest/vitest.mjs run src/utils/heatmap.test.ts --pool=forks --maxWorkers=1`: **1 file / 16 tests passed** (`.tmp34-proposal-web-heatmap-final.log`). Combined split suite: **107 files / 1024 tests**.
  - `node node_modules/eslint/bin/eslint.js .`: **passed**, empty output (`.tmp34-proposal-lint-final.log`).
  - `node node_modules/typescript/bin/tsc -b && node node_modules/vite/bin/vite.js build`: **passed** (`.tmp34-proposal-build-final.log`), including a fresh production dist.
- No Go/SQL/generated contract changes, so no contract regeneration or backend suite was needed for this frontend-only follow-up. Logs are retained as requested at worktree root `.tmp34-proposal-*.log` (untracked evidence, not source changes). All source/doc changes remain uncommitted; no commit, merge, reset, clean, push or tree deletion performed. Parent must integrate and independently re-review; this evidence does not claim approval or closure.
