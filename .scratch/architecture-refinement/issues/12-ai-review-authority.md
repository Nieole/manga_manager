# 12: AI 分组的权威裁决结果

**What to build:** AI 分组动作返回后端权威候选与整单终态，前端合并结果而不复刻收尾规则。

**Blocked by:** 11: 人工合集与智能快照完整用例。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 56、57、58、175、176、178。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 建合集和裁决使用已收窄的合集/事务能力，名称、成员及 CAS 原子保证保持。
- [x] 动作返回准确候选及整单结果，混合终态和已落地合集不被后续拒绝覆盖。
- [x] 前端移除终态业务推导，保留选区、滚动及必要刷新。
- [x] 真实 SQLite、HTTP 与审核页面覆盖并发、部分应用和旧 pending 筛选离开。

## Comments


### Implementation verification (2026-10-28)

- `AIGrouping` uses separate narrow adjudication and generation transaction capabilities. The private database wrappers expose no generated query or database escape; adjudication reuses collection creation capabilities without manual collection deletion. Name checks, membership writes and candidate CAS retain one transaction.
- Apply/reject actions return the candidate set and whole review result captured inside their write transaction, including display facts and created collection identities. No committed action needs a later database read to construct its response. Mixed applied/rejected candidates preserve the applied whole-review status and existing manual collection identity. Existing public action methods now return this result; redundant legacy action entry points are absent.
- HTTP preserves existing success/count/identity response fields and adds the authoritative review. The page merges that review without inferring terminal status, preserving loaded rows and active selection when a settled review leaves the pending filter. Collection refresh still follows actual successful application.
- Red: `.tmp/ticket12-http-red-race.log` executed the missing-review assertion; `.tmp/ticket12-capability-red.log` caught the absent narrow capability; `.tmp/ticket12-ui-red.log` caught ignored authoritative candidate content after pending-filter departure. Ordinary HTTP red launches blocked by Application Control were not counted as test failures or passes.
- Green: full organize 6.519s, database 17.805s, API 104.921s (`.tmp/ticket12-full-go.log`). Final owner/API AI grouping race regressions executed 9.328s / 17.835s (`.tmp/ticket12-final-race.log`), including mixed results, concurrency, rollback and CAS. Capability suite executed with related consumers (`.tmp/ticket12-outcome-green.log`).
- Frontend full suite: 99 files / 933 tests, lint and production build passed (`.tmp/ticket12-full-ui.log`, `ticket12-ui-lint.log`, `ticket12-ui-build.log`). Final Go lint: `0 issues.` (`.tmp/ticket12-final-lint.log`); gofmt and diff whitespace checks clean. SQL and generated contract targets were unchanged. Integration closure remains pending.

- Merged integration f76b470 before handoff; retained both changelog entries. Post-merge focused database / organize / API regressions actually passed 5.718s / 5.248s / 7.710s (.tmp/ticket12-merged-consumers.log); page tests passed 4/4 (.tmp/ticket12-merged-ui.log). The 933-test full UI sweep predates merging tickets 19/23 and was based on 82c4e01; this ticket deletes no frontend test and adds one test to the existing three. It is not a post-merge full-suite count.
