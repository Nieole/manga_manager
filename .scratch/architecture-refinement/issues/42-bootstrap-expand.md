# 42: 独立装配根与新宿主扩展

**What to build:** 生产服务通过独立装配根创建应用与 HTTP 宿主，新宿主能力入口与旧适配兼容共存。

**Blocked by:** 24: 资料库创建修改与导航功能；31: 资源热切换与关闭所有权；36: 任务观测功能与传输契约归属；38: 站点初始化与账户用例入口；41: 目录组合查询与评价用例。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 8、9、151、152、153、154、197。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 广泛宿主迁移扩展。

- [ ] 使用已稳定资料库、供应商、资源、传输投影、账户及查询能力构造图。
- [ ] 同一 Engine/Manager，先登记恢复再开放，停机排空顺序不变。
- [ ] 生产启动消费新装配入口，旧宿主形式只为尚未迁家庭提供短期兼容，不再次启动应用。
- [ ] 生产等价启动/恢复失败/队列/停机测试与服务构建验证，为后续机械迁移保持绿色。

## Comments

### Implementation / verification handoff (2026-11-02, retry)

Production `cmd/server` consumes `bootstrap.Open`. The root builds resources, transport leaves and one Runtime/Engine with the supplied Manager, binds four capability families, and only returns HTTP after Start registers/reconciles/replays before opening inputs. HTTP and application effects share authentication, SSE, dashboard/recommendation and content cache leaves. Process shutdown retains config/SSE stop, actual HTTP drain, Engine/task-body and background joins, scanner close, then resource retirement. Bootstrap tests cover shared Manager queue admission and authenticated HTTP task projections, recovery failure returning no Host/opening no inputs, HTTP cache invalidation, borrowed ZIP lifetime and real net/http drain.

`Host`'s compatibility Controller is deliberately temporary. 43 owns reading/catalog, 44 metadata/organization, 45 library/runs/disk, 46 administration/config/accounts; 47 must delete legacy Controller constructors, Runtime references and whole-Store compatibility after those consumers migrate. None may create another Runtime, Engine, Manager, auth/SSE or cache instance. ADR 0014 and API package documentation describe this expansion state. A focused race run exposed an SSE fixture depending on another test's key registration; the fixture now registers its own declared identity (red log `ticket42-final-api-focused-race.log`, green owned suite below).

All commands ran in this worktree with `GOTOOLCHAIN=go1.26.10`, `GOCACHE=C:/Users/saber/dev/manga_manager/.gocache`, own `GOTMPDIR=.tmp` (absolute path) and own `GOLANGCI_LINT_CACHE=.tmp/lint-cache` (absolute path). Logs below are in this worktree's `.tmp/`; successful commands have adjacent `.exit` files containing 0.

- `go test -count=1 -timeout=8m ./cmd/server ./internal/bootstrap ./internal/application/runtime ./internal/api`: PASS, 0.091s / 0.540s / 2.738s / 131.219s (`ticket42-final-consumers-v2.log`). Earlier same consumers passed too (`ticket42-final-consumers.log`); neither substitutes for the race failures below.
- `go test -race -count=1 -timeout=10m ./cmd/server ./internal/bootstrap ./internal/application/runtime`: PASS, 2.769s / 11.080s / 15.374s (`ticket42-final-race.log`).
- `go test -race -count=1 -timeout=6m ./internal/api -run='^Test(Task|SSE|ServerEvents|StopAllRuntimes|Auth|Config|Interrupted|Queued)'`: PASS, 85.773s (`ticket42-final-owned-api-race.log`), including the independent SSE fixture.
- `gofmt -l ./cmd ./internal`: PASS, empty output (`ticket42-final-gofmt.log`). `golangci-lint run --allow-parallel-runners`: PASS, `0 issues.` (`ticket42-final-lint.log`). `go build ./...` and `go build -o .tmp/ticket42-server.exe ./cmd/server`: PASS (`ticket42-final-build.log`, `ticket42-final-server-build.log`). Existing untracked root `server.exe` is preserved as an excluded build artifact, not source for a commit.
- Full baseline-to-working-tree `git diff --check badc595541250189586b87e9d1c1c83ac79fd677`: PASS (`ticket42-final-diff-check.log`); tracked conflict marker scan has no matches (`ticket42-final-conflict-markers.log`, grep exit 1). No SQL, generated response contract or frontend source changes in this ticket.

Open limitations (not counted as PASS):

- Full API race launching via `go test -race -count=1 -timeout=12m ./internal/api` produced no confirmed test output before the 760s tool bound (`ticket42-final-api-race.log`); no claim of execution or diagnosed Application Control rejection. Compile to stable `ticket42-final-api-race.exe` succeeded. Running it from `internal/api` with `-test.timeout=10m` actually executed, then timed out in `TestReaderTargetSizeSnapsToBucket` while PNG/flate work was runnable (`ticket42-final-api-race-stable.log`, exit 2). Prior 4m timeout log is retained too. This is NOT a full API race pass or a proven deadlock.
- Broad API race regex `Test.*(Task|SSE|Shutdown|Auth|Config|Interrupt|Recovery|Queued)` after the fixture fix failed with the preexisting captured `plan.RunID` race in `relocating.startCheck`, observed by restoration consumers (`ticket42-final-api-focused-race-green.log`). Latest integration already fixes that exact capture by returning a separate bound plan; do not duplicate its fix in this ticket. It must be consumed and rerun by the parent.
- `scripts/check-doc-style.sh` exceeded tool bounds of 30s and 180s with no final result (`ticket42-final-doc-style.log`, `ticket42-final-doc-style-retry.log`). Not counted as passing. No remote Linux/macOS, real device or provider execution claimed.

Delivery remains UNCOMMITTED and ticket remains open for independent merger. Worker policy prohibits index/history mutation, so this retry did not merge or commit despite the requested delivery sequence. Actual consumed HEAD remains `d117e5000f9903dcd59c75d9d61a2b668d132d34`; latest integration reviewed, NOT merged/consumed, is `16cfc9f834f4507b262553541d90206bb72918a0` (includes 25 and 33). Parent must merge the latest integration into this branch without discarding dirty work, rerun combined gates including the now-fixed relocation race, and commit scoped source/docs (excluding `server.exe`) before independent integration acceptance. No main-worktree edits, push or tree deletion performed.
