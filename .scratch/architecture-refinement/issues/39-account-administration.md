# 39: 账户管理与最后管理员保护

**What to build:** 账户角色变更与删除经完整用例保证并发下仍保留管理员，页面获得准确结果。

**Blocked by:** 38: 站点初始化与账户用例入口。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 184、186、187、188、193。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 使用已建立账户应用能力与窄事务，最后管理员保护在并发下原子成立。
- [x] 禁止删除自身及既有数据级联政策保持，业务主体/授权不被入口解析代替。
- [x] 角色变更、删除与冲突结果在 HTTP 和管理界面准确呈现。
- [x] 真实 SQLite 并发及账户页面回归，不引入新权限系统。

## Comments


### Implementation verification (2026-10-29)

- `accounts.Management` owns List/Create/Update/Delete through explicit Actor identity and a private `AccountManagementWrite` transaction capability. Each command reloads the actor's current site-user role and the target inside its transaction. The last-administrator count and role/delete writes are atomic; self-deletion is rejected. Username bytes, default roles, display-name trimming and initial must-change-password behavior are preserved. Password reset/session qualification remains assigned to ticket 40.
- No sqlc handle or SQL/database escape is exposed by the private transaction wrapper. Deletion uses the existing user DELETE policy, retaining session/progress foreign-key cascades and the owned-device cleanup trigger. HTTP keeps authentication parsing, Cookie/CSRF, rate limiting and existing Basic-cache policy; no new permission system is introduced.
- The account page consumes the authoritative create/profile result and removes rows only after committed deletion, retaining the prior role on command conflict. Self role changes refresh the authentication view with a distinct post-save refresh warning. Usernames are submitted without trimming.
- Genuine red evidence: `.tmp/ticket39-red.log` captured concurrent 200/200 demotions and a regular actor successfully managing a user without route middleware. `.tmp/ticket39-ui-red.log` captured username trimming and a successful role command being replaced by stale state after list-refresh failure. Green tests include cross-account concurrent deletes, current-role authorization, stale actors, self-deletion, exact usernames and preserved session/device cascades.
- Full API actually passed 116.370s after an Application Control blocked launch (`ticket39-api-retry.log`); database full suite passed 18.833s (`ticket39-full-go.log`). Related API/database race regressions passed 16.610s/7.946s (`ticket39-race.log`). Final owner/cascade consumers passed 0.835s (`ticket39-owner-final.log`). Blocked executables were not counted as passes.
- Frontend full suite passed 103 files / 948 tests with lint/build (`ticket39-full-ui.log`, `ticket39-ui-lint.log`, `ticket39-ui-build.log`). A subsequent deletion-consumer regression joins the three earlier management tests; the final four tests passed (`ticket39-ui-final-consumers.log`), without implying a newer full-suite count. Go lint reported `0 issues.` (`ticket39-lint.log`). SQL source/schema and generated contract targets were unchanged. Integration closure remains pending merger verification.

- Merged integration bb300a8 before handoff; resolved the changelog and Store-interface additions by retaining both sides. Post-merge account/auth/capability consumers actually passed API 1.748s/database 0.067s (`ticket39-merged-consumers.log`), final Go lint remained `0 issues.` (`ticket39-merged-lint.log`), and final frontend lint passed (`ticket39-handoff-ui-lint.log`). Gofmt and whole-baseline diff/marker checks remained clean.

### Integration verification (2026-10-29)

- Merged without conflicts through c1e1ac8b560b1a4741f53a4579a3bc7ae876e71d. Main-tree private account-management capability race passed 1.538s; administrator/actor authorization, concurrent demotion/deletion and session/device cascade owner+HTTP race consumers actually passed 15.721s. Administration page consumers passed all 4 tests.
- Go 1.26.10 lint reported 0 issues and gofmt emitted no files. Whole-baseline whitespace and tracked conflict-marker scans passed. Raw logs: .tmp/merge-wave18-owner.log, merge-wave18-api.log, merge-wave18-web.log and merge-wave18-lint.log. Password/session qualification remains the separate ticket 40 scope.
