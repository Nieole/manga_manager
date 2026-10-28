# 11: 人工合集与智能快照完整用例

**What to build:** 人工成员修改及智能书架固化通过窄事务完成，用户看到原有所有权和稳定结果。

**Blocked by:** None (can start immediately)。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 31、32、39、56、57、58。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 名称、归属、成员校验与批内回滚由完整用例拥有，回调不暴露全量查询。
- [x] 智能书架保持动态，快照是一次动作且人工可编辑，作品群保持推导边界。
- [x] API 与页面实际消费结果，保留消失成员和冲突的准确分类。
- [x] 真实 SQLite、权限及页面回归验证，不用通用容器抹平差异。

## Comments

### Implementation verification (2026-10-28)

- Manual collection writes use `WithCollectionWrite`; smart shelf snapshots use `WithSmartSnapshot`. Private database wrappers expose only the required capabilities, without an embedded generated query handle or arbitrary SQL escape. Names, ownership, member existence and whole-batch rollback remain inside complete use cases.
- Snapshot preview and execution require an explicit reading subject. Saved shelf conditions stay dynamic; a created snapshot remains independently editable manual membership. Real SQLite tests cover missing members, disappearing members during a batch, rollback, name races and invalid subjects. HTTP regressions preserve not-found versus conflict classification and permissions.
- Red evidence: `.tmp/ticket11-disappearing-member-red-retry.log` records the raw foreign-key error before classification; `.tmp/ticket11-subject-red.log` records the subject boundary regression. Final organize suite executed successfully in `.tmp/ticket11-resume-go.log` (7.585s); full API executed from its package directory and ended PASS in `.tmp/ticket11-api-package-final.log`. Database capability regression executed successfully in `.tmp/ticket11-capability.log` (3.600s).
- Existing page consumers passed 8 files / 41 tests (`.tmp/ticket11-frontend-consumers.log`). No frontend production code changed. Go lint reported `0 issues.` (`.tmp/ticket11-lint-handoff.log`); gofmt and `git diff --check` were clean. Server suite executed successfully in `.tmp/ticket11-resume-go.log` (11.578s).
- Some additional ordinary test executions were blocked by Windows Application Control, including reading and database in the resume sweep. Those launches are not counted as passes; raw evidence remains in `.tmp/ticket11-resume-go.log`. Integration verification and ticket closure remain pending.
