# 24: 资料库创建修改与导航功能

**What to build:** 创建、编辑、校验、监听首扫及导航刷新成为完整资料库功能，Layout 消费入口。

**Blocked by:** 22: 空闲删库与占用拒绝。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 72、73、74、75、76、180。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 复用已建立的资料库管理 owner 和访问协调，创建/修改仍各有明确用例。
- [x] 路径、格式、间隔及占用校验一致，非空改路径与迁移保护继续阻止错误操作。
- [x] 首扫准入、监听后果、存储结果和刷新分别表达，前端保留表单与目录状态。
- [x] 真实路径/数据库、HTTP 及资料库表单验证，不把首扫发起当作扫描完成。

## Comments

### Implementation and verification — 2026-10-28

- `application/libraries.Service.Create` and `Update` own complete persistence, validation, watching, initial scan admission and refresh handoff. Runtime injects the shared relocation root guard, scan use case and effects; HTTP only decodes, localizes validation codes and maps outcomes. Existing interval/format defaults and KOReader omission semantics are preserved. Canonical native path comparison rejects registered-root aliases. An empty-root change must obtain existing engine exclusivity; populated roots require relocation, protected roots cannot enable automatic processing, and delete/relocation reservations reject saves.
- `SaveResult` embeds the existing library JSON fields and adds committed `saved`, watcher outcome, initial scan admission/run identity, refresh request and warnings. Failed watching or initial scan admission does not undo a saved library. Occupied path changes preserve the existing 503 `library_unavailable` and Retry-After contract while adding actual structured conflicts. Existing post-save cache invalidation and publication are runtime-owned.
- `features/libraries` owns navigation queries, create/edit drafts, directory browsing, recent paths, save submission and refresh consumption. Layout consumes this entry and the modal. Existing consumers of directory selection and library types point to their owner. Failed validation or occupancy retains the draft and browser location; committed saves followed by navigation failure receive a separate message. Navigation responses use the existing latest-request guard, and application refresh does not reset drafts. Scan admission is never presented as scan completion.
- TDD evidence: the complete owner interfaces were absent before the create/update slices; relative aliases bypassed registered-root checks before canonicalization (`.tmp/ticket24-red-path-corrected.log`); actual occupied-update HTTP omitted conflicts (`ticket24-red-http-occupancy-stable-fixed.log`) before the structured result. Real temporary SQLite and paths, public engine access barriers, HTTP and feature consumers verify the resulting behavior. Legacy scan visibility assertions now follow the returned actual run identity rather than a global test-only key registry; the defaults fixture waits for actual public engine conflicts to disappear before changing an empty root.
- Actual passing executions: owner suite 5.396s and owner race 22.891s; runtime suite 6.904s; initial HTTP compatibility 4.123s; full API stable test executable completed PASS from its package working directory (`ticket24-http-green-stable.log`), and the occupied-update consumer explicitly passed in 0.08s (`ticket24-http-occupancy-green.log`). Canonical-path owner tests passed 3.923s. Ordinary database full stable executable completed PASS (`ticket24-database-full-normal.log`). Backend `go build ./...` passed.
- Frontend full suite passed 100 files / 937 tests (`ticket24-ui-full-final.log`); final form and navigation consumers passed nine tests (`ticket24-ui-final-consumers.log`). Frontend lint/build passed. Earlier Go 1.26.10 lint reported 0 issues; final delivery checks follow below.
- Preserved limitations: the initial broad HTTP race run failed on the two obsolete fixture assumptions described above, and its raw 171.285s failure remains in `ticket24-http-race.log`; later API race executable attempts were blocked by Windows Application Control and are not passing evidence. A broader database race run exceeded its two-minute package budget while constructing the existing SQLite query-plan fixture (`ticket24-database-race.log`); it is not counted as a pass. The complete owner race and ordinary database/API executions above actually ran. Integration should retry the focused final API race seam.
- Status remains ready-for-agent until the merger verifies and closes the ticket.
- Final delivery checks: frontend consumer tests 9/9, frontend lint/build, Go 1.26.10 lint 0 issues, gofmt empty output, and git diff whitespace validation all passed (ticket24-ui-final-consumers.log, ticket24-ui-lint-delivery.log, ticket24-ui-build-delivery.log, ticket24-lint-final.log, ticket24-format-final.log).
