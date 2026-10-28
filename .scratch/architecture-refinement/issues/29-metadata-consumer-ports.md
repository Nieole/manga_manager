# 29: 元数据与 AI 消费能力

**What to build:** 搜索、推荐、分组和连接测试按实际能力消费供应商，保持上游兼容和调用保护。

**Blocked by:** None (can start immediately)。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 146、147、148、149、150。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 完整生产调用改为所需能力，具体供应商可以实现多个接口。
- [x] 连接输入明确，不为解析 endpoint 构造全站配置，也不添加逐接口空转发。
- [x] 上游 DTO、分页/重试、取消、预算、错误和脱敏保持适配职责。
- [x] 可控供应商加 HTTP/应用消费者验证搜索、分组及连接测试的现有结果。

## Comments


### Implementation verification (2026-10-28)

- Metadata sources implement separate Fetcher/Searcher capabilities; recommendation, grouping and connection checking have dedicated consumer interfaces. HTTP candidate search uses a Searcher port, enrichment and background intelligence require only Fetcher, and recommendation/connection callers hold their own narrow capabilities. Concrete provider implementations remain shared; no per-capability forwarding adapters were introduced. The controlled intelligence source no longer implements an unused search method.
- `config.LLMConnection` provides explicit endpoint routing/timeout input. Its normalization is the single rule used by full configuration and connection testing; endpoint construction in the metadata factory no longer builds an empty application Config. Legacy endpoint splitting, default paths, provider fallback and timeout behavior are preserved.
- Upstream DTOs, pagination, retries, cancellation, provider budgets and redacted errors remain in the existing adapters. The AI grouping transaction/generation capability boundary is unchanged. No frontend, generated contract target or SQL changed; this is an ownership refactor without a new product behavior claim.
- Red evidence: `.tmp/ticket29-connection-red.log` and `.tmp/ticket29-search-red.log`. The search-only HTTP consumer actually passed with pagination clamp and unchanged results; connection conversion also executed successfully (`ticket29-capability-green.log`, config 2.881s/API 9.134s).
- Full metadata race executed 7.188s (`ticket29-race.log`); full config executed 3.020s (`ticket29-consumer-retry.log`). The full intelligence suite actually ran from its package cwd using the saved test executable and ended PASS (`ticket29-intelligence-binary.log`). Full API/runtime suites executed 120.109s/7.688s (`ticket29-full-api.log`). Intermediate test launches blocked by Windows Application Control are preserved in the logs and not counted as passes.
- Go lint reported `0 issues.` (`ticket29-lint.log`); gofmt, whole-baseline whitespace checks and tracked conflict-marker scans were clean. Integration closure remains pending.
