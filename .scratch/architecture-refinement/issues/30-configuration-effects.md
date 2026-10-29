# 30: 配置发布与生效范围

**What to build:** API 保存和文件重载共享发布能力，设置明确即时、后续作业或重启后生效。

**Blocked by:** None (can start immediately)。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 137、138、139、142、143。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] Manager 为事实源，版本冲突、密钥占位、校验及不同补偿政策保持。
- [x] 逐字段说明生效范围，操作目标/来源/写意图在适当发起边界固定。
- [x] 保存结果与实际生效状态可区分，设置页面消费明确结果。
- [x] 配置持久化、重载、并发和 UI 回归验证，不将所有设置假装立即生效。

## Comments

### Implementation handoff — 2026-10-29

- `application/configuration.Service` owns settings saves and file reload publication through the same `config.Manager`; Manager remains the only mutable configuration source. It retains the distinct Save, Reload and cross-durable-participant compensation policies. Settings version preconditions, masked-secret restoration and deep validation remain; reload keeps its pure-value validation and leaves invalid edited files for correction.
- `config.FieldEffects` supplies the exhaustive field timing catalog, exposed by the configuration query and referenced from the configuration template. Persistence/Manager publication are separate from immediate, subsequent-operation and pending-restart effects. Restart status compares immutable startup resource facts rather than inventing a second live configuration. GET returns document, ETag and effect status from the same snapshot generation. The Settings consumer shows those results and the field catalog; a successful save followed by failed refresh is reported as saved plus refresh failure.
- Grouping fixes its external provider at launch, even before a deferred body runs. Thumbnail rebuilding keeps the cleanup target and protected page-cache directories from the same launch snapshot. A pending `database.path` setting cannot redirect the active database's trash/log fallback locations. Resource retirement and the complete editor-session extraction remain reserved for tickets 31/32.
- Actual red/green: `.tmp/ticket30-save-effects-red.log` captured the old generic save reply without effect facts; the configuration HTTP regressions passed in `.tmp/ticket30-api-config.log`. `.tmp/ticket30-external-edit-red.log` captured both settings/reload publishing an outdated request when an external editor changed the file during successful apply; publication now verifies the observed file again and restores runtime effects without overwriting the editor. `.tmp/ticket30-intent-red-race.log` captured a deferred grouping using the replacement source, fixed in `.tmp/ticket30-source-green.log`. `.tmp/ticket30-cleanup-intent-red-stable.log` captured deletion of the original cache's protected page after hot update, fixed in `.tmp/ticket30-cleanup-intent-green-stable.log`. `.tmp/ticket30-settings-red.log` captured ignored restart facts and a refresh failure misreported as save failure; `.tmp/ticket30-settings-consumers.log` records the 15 configuration consumers passing.
- Real temporary configuration files, fsnotify, SQLite-backed consumers and external-effect barriers verify file reload, version conflict, secret masks, invalid-file retention, external-editor interleaving and operation target/source preservation. Full config/publication race suites passed 5.267s/3.027s in `.tmp/ticket30-config-race.log`; operation race suites passed intelligence 8.648s and maintenance 11.391s in `.tmp/ticket30-operation-race.log`. `.tmp/ticket30-db-target.log` confirms the real HTTP trash operation remains at the active database while the new path awaits restart.
- Full API executed and passed 122.057s in `.tmp/ticket30-backend-full-related.log`; the same mixed command actually passed publication 2.197s, intelligence 2.244s and maintenance 3.467s, while config/runtime executables were OS-blocked. The runtime package subsequently genuinely executed its complete stable test binary to exit 0 with recovery/scan test logs and final PASS in `.tmp/ticket30-runtime-final.log`; this is execution evidence, not just compilation. AppControl-blocked attempts are retained as blocked executions, never counted as passing tests.
- Frontend full suite passed 101 files/944 tests in `.tmp/ticket30-ui-full.log`; lint and actual build passed. After merging account initialization and composite-query work, 18 settings/auth consumers and frontend lint/build passed. Go lint returned `0 issues.` after those merges. `go run ./cmd/tsgen` regenerated the two added contract types; no SQL generation was required.
- After incorporating scan ingestion and the final snapshot-consistency fix, `.tmp/ticket30-final-combination.log` records publication 0.645s, intelligence 0.801s, maintenance 0.225s and API 1.148s actually passing; `.tmp/ticket30-final-lint.log` records `0 issues.`. Full-baseline whitespace checks from `badc595541250189586b87e9d1c1c83ac79fd677` and tracked conflict-marker scans were empty, as was gofmt.
- The final account-administration merge preserved both sets of CHANGELOG/localization additions; `.tmp/ticket30-last-main-api.log` records the real settings/active-database-target HTTP tests passing 0.247s afterward, and `.tmp/ticket30-last-main-ui-build.log` records the successful final real build.
- No remote multi-platform execution or resource-retirement acceptance is claimed. Status remains ready-for-agent pending merger verification and closure.
