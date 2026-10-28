# 15: 阅读清单顺序与窄写用例

**What to build:** 清单创建、成员及重排操作拥有完整规则，结果与主体明确的阅读目标一致。

**Blocked by:** 03: 藏书编排的读者查询。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 27、31、39、56、58。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 顺序、成员身份、限制和批内回滚由清单用例维护，不依赖全量 sqlc。
- [x] 读态复用已迁查询，保留全部已读首书重读及空系列结果。
- [x] API 和清单页面保持公开契约、选区及写后刷新。
- [x] 真实 SQLite 和页面导航回归验证，不与合集模型合并。

## Comments


### Implementation verification (2026-10-28)

- `ReadingListStore` exposes `WithReadingListWrite` instead of full-query transactions or direct writes. Its private database wrapper does not embed generated Queries or expose a SQL/database escape. Create, Update, Delete, Add, Remove and Reorder use this capability, with the existing name, membership identity, ordering limits and rollback rules owned by complete list use cases.
- Lists remain independent of collections. Existing explicit `reading.Subject` queries and continuation logic are unchanged, including all-completed first-book fallback and empty series results. Pure catalogue query shapes remain technical data; no extra domain wrapper was added without an invariant.
- Capability test red: `.tmp/ticket15-capability-red.log` shows the unavailable narrow capability. Green capability/lifecycle/HTTP consumers actually executed database 4.278s, organize 4.757s and API 7.427s (`.tmp/ticket15-first-green.log`). Existing real SQLite and HTTP cases cover duplicate member identity, partial and invalid reorder rollback, request limits, reader isolation and continuation navigation.
- Full database 15.751s and organize 4.466s passed (`.tmp/ticket15-full-go.log`). Related owner/API race regressions actually passed 8.943s / 19.355s (`.tmp/ticket15-race.log`). Existing reading-list page navigation, selection and response-generation regressions passed 3 tests (`.tmp/ticket15-page.log`). Frontend production code and public contracts were unchanged.
- Go lint reported `0 issues.` (`.tmp/ticket15-lint.log`); gofmt and whitespace checks were clean. SQL and generated targets were unchanged. Integration closure remains pending.
