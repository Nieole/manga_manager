# 36: 任务观测功能与传输契约归属

**What to build:** 任务中心和气泡消费功能自有投影，HTTP/SSE 契约及转换有明确传输归属。

**Blocked by:** None (can start immediately)。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 155、162、163、179。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 任务观测不反向导入具体 TaskCenter/气泡组件的类型，组件消费功能接口。
- [x] 运行 HTTP/SSE DTO 与转换从混合总装归位，领域不吸收公开 JSON 模型。
- [x] 公开名称/字段、生成 TS、权限、重连及后端实况权威保持。
- [x] 后端契约与前端任务消费者回归验证；移动类型的收益按依赖清晰度报告。

## Comments


### Implementation verification (2026-10-28)

- Task observation owns task actions, filters, navigation targets, expanded history, run details and bubble projections in `features/task-observation/types.ts`. Hooks, task components and their page consumers import the feature contract; component type exports and reverse imports are removed. Generated wire types remain the transport source of truth and are exposed through the feature entry where its consumers need them.
- Six HTTP/SSE run contract types now live in `run_contract.go` rather than controller assembly. `run_transport.go` owns domain snapshot/live conversions and SSE envelope serialization/publish adaptation. Engine sequence linking and mutable state remain in the engine; the domain does not import public JSON models. Event/sample contracts retain their existing dedicated HTTP adapters. Public Go symbols, JSON names, permissions and generated TypeScript shapes are unchanged.
- This is an ownership refactor with no new product behavior; existing consumer regressions verify unchanged behavior rather than mirrored static file-location tests. Focused task observation hooks, TaskCenter and bubbles passed 121 tests in 4 files (`.tmp/ticket36-consumers.log`). Full frontend passed 100 files / 939 tests, lint and production builds (`ticket36-full-ui.log`, `ticket36-lint-ui.log`, `ticket36-build-final.log`).
- Full API executed successfully in 132.965s (`.tmp/ticket36-go.log`). Taskstore race executed successfully in 12.672s (`ticket36-race.log`). Final SSE/push/live/snapshot consumer regressions after the publish-adapter move actually passed 9.018s (`ticket36-final-transport-retry.log`). Task package executables and some intermediate API/tsgen launches were blocked by Windows Application Control and are not counted as passes.
- The compiled generator actually executed successfully (`.tmp/ticket36-tsgen-final.log`, 24 types); `web/src/api/generated.ts` has no diff. Final Go lint reported `0 issues.` (`ticket36-lint-handoff.log`), gofmt and whitespace checks were clean. Integration closure remains pending.

### Integration verification (2026-10-28)

- Merged through c5c182c713dcb660b169c119667ab9e2093dd791. Main-tree full task race passed 2.301s, full taskstore race passed 10.198s, and combined Task/Run/Push/SSE HTTP race actually passed 148.976s. Core task-observation, TaskCenter, bubble and reconnect consumers passed 5 files / 124 tests.
- Go lint reported 0 issues and gofmt emitted no files. The actual tsgen execution generated 24 types with no generated.ts drift. Logs: .tmp/merge-wave12-owner.log, merge-wave12-api.log, merge-wave12-web.log and merge-wave12-lint.log. Whole-baseline whitespace/conflict-marker verification is tracked separately in this integration wave; working-tree checks alone are not treated as that gate.
