# 40: 改密与会话资格交接

**What to build:** 改密和会话撤销通过账户能力完成，鉴权缓存与协议资格跟随既有安全政策更新。

**Blocked by:** 38: 站点初始化与账户用例入口。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 184、188、189、191、192、193。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 密码写入、撤销会话与缓存失效的提交边界和后续结果明确。
- [x] 站点用户、设备账号、Cookie/CSRF、限流及协议挑战继续区分。
- [x] 前端本人改密/管理员重置及过期恢复保持既有离线所有权规则。
- [x] 真实 SQLite、认证协议及用户消费者验证旧资格和失败结果。

## Comments

### Implementation handoff — 2026-10-30

- `accounts.Passwords.Change/Reset` owns current actor/credential validation, password hashing, password replacement and old-session revocation through the private `AccountPasswordWrite` transaction. A revocation failure rolls back the password; successful commit invalidates Basic eligibility. Site users and KOReader device credentials retain distinct ownership.
- HTTP keeps Cookie/CSRF, rate limits and protocol challenges. A failed replacement browser session returns `password_committed` and `login_required`; the browser preserves offline ownership and requests login with the new password. Admin reset consumes the authoritative user and reports later session-refresh failure separately.
- Real SQLite HTTP red tests observed successful password writes despite revocation failure, and an ambiguous failure after committed password/session creation failure. Both are green. The reset protocol matrix verifies revoked sessions, stale/current passwords, forced-change Basic challenges and unchanged device credentials/ownership.
- A controlled concurrent credential lookup reproduced an old in-flight Basic verification refilling the cache after password commit (`204` on a later old-password request). Cache publication and invalidation now share an epoch and short mutex, preventing late cache refill. The real HTTP race regression is green.
- UI red/green covers committed-password login recovery with preserved offline books, authoritative reset state and a distinct failure after committed self-reset. The first reset consumer attempt had two incorrect translated selectors; those fixture errors were corrected before recording green results.
- Before merging the latest configuration publication, complete API/database tests executed in 107.240s/11.190s; password/capability race executed in 22.023s/1.502s. The final cache-handoff plus Basic/protocol race executed in 27.832s. The complete frontend executed 103 files/952 tests, lint and build. The merged auth/account/config consumers executed 15 tests successfully. Logs are in `.tmp/ticket40-*.log`.
- Code commit `5f21200`; integration `cb7b737` merged in `1215c69`, preserving both configuration-publication and password CHANGELOG entries. Final merged verification is recorded below. Status remains `ready-for-agent` until merger verification and closure.
- Final merged complete API/database executed successfully in 108.123s/8.018s; merged frontend executed 103 files/954 tests. Frontend lint/build, Go build, Go lint (`0 issues.`), empty `gofmt -l ./cmd ./internal`, full-baseline `git diff --check` and all-tracked conflict-marker scan passed. No SQL source, schema or generated-contract change was needed by this ticket. No remote device or other-platform execution is claimed.
