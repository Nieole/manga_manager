# 38: 站点初始化与账户用例入口

**What to build:** 首次管理员建立成为原子完整用例，保持旧阅读数据和设备账户归属以及登录响应。

**Blocked by:** None (can start immediately)。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 184、185、188、190、191、192、193。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 初始化条件在真实事务/并发下成立，用户名继续字节精确。
- [x] 旧数据认领、会话建立及已提交后续失败保持原有规则并如实表达。
- [x] HTTP 保留 Cookie/CSRF 和认证入口，账户功能暴露明确主体及输入。
- [x] 真实 SQLite、并发及初始化/登录页面回归验证，供后续管理用例复用。

## Comments


### Implementation verification (2026-10-29)

- `accounts.Initialization` owns first-admin creation through `WithAccountInitialization`: the site-empty decision and administrator insert share a real SQLite write transaction. The private transaction wrapper exposes only count and administrator creation, with no sqlc handle, arbitrary SQL or database escape. SetupInput and Credentials make the site-user inputs explicit; device-account authentication remains separate.
- Setup and login preserve username bytes. Initialization validates required input and password length, keeps the existing display-name trimming rule, and uses the shared site-password minimum. Credential lookup preserves the single real/dummy KDF comparison path while HTTP retains cookies, CSRF, rate limits, Basic challenges, session creation and credential-cache responsibilities.
- Legacy progress, device ownership and activity claims preserve their existing best-effort and individual rollback/idempotency boundaries. The administrator remains committed when a later claim fails; SetupResult carries step warnings. HTTP reports account_committed on session failure, and the actual auth pages lead to login instead of repeating setup. Successful initialization keeps claim warnings visible after entering the application.
- Red evidence: `.tmp/ticket38-setup-red.log` actually executed two successful concurrent initializations (200/200). `.tmp/ticket38-ui-red.log` captured the three real consumer failures: trimmed username, retrying setup after committed/session failure, and lost claim warning. Green SQLite/HTTP tests cover exactly one concurrent administrator, claim rollback/retry, committed account after session failure, exact username login, preserved orphan device identity and separate credentials, and migrated historical activity.
- Full API/database suites executed 99.800s / 10.566s (`ticket38-full-go.log`). Related setup/auth/transaction race regressions executed API 32.693s and database 5.067s (`ticket38-final-race.log`). Final focused consumer verification after extracting the shared password constant passed API 7.095s/database 0.741s (`ticket38-handoff-consumers.log`). Capability escape checks actually executed; earlier policy-blocked launches were not counted as passes.
- Frontend full suite passed 102 files / 945 tests, lint and build (`ticket38-full-ui.log`, `ticket38-ui-lint-final.log`, `ticket38-ui-build.log`). The final real setup-to-login flow and claim-warning consumers passed 3 tests (`ticket38-ui-login-final.log`). No generated target or SQL schema/query source changed.
- Final Go lint reported `0 issues.` (`ticket38-handoff-lint.log`); gofmt, whole-baseline whitespace checks and tracked conflict-marker scans were clean. The branch is based on integration 48527f2; ticket closure remains pending merger verification.
