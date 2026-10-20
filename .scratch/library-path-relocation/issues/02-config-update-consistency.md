# 02: 统一配置保存与热重载的一致性边界

**What to build:** 对现有配置更新做行为保持的预重构，让设置保存和文件热重载共享一致的更新规则，避免后续迁移策略时把并发设置改动覆盖。用户继续通过原有设置与配置文件操作，后续目录策略迁移可接入同一可靠边界。

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

规格覆盖：用户故事 34、36、46；实施决策 14、15、16、23。

- [x] 梳理并接入设置保存、配置文件热重载及其他现有持久化配置修改入口，保留 config.Manager 为运行中配置事实来源，不在 Controller 另藏配置快照。
- [x] 配置读取、版本验证、持久化和生效之间具有可协调的更新边界；基于过期快照的写入不能静默覆盖已经接受的新设置，失败可被调用方识别。
- [x] 配置热重载与 API 更新遵循相同的校验、生效和冲突规则；不能只串行文件写入而让 Manager.Replace 或派生资源更新在外层交错。
- [x] 为后续目录策略变更提供按预期状态更新及失败恢复的接缝，允许保持无关设置不变；本票不执行策略迁移，不虚构数据库与配置文件已具备联合事务。
- [x] 既有配置无效拒绝、内容一致时跳过、不泄露密钥、配置文件权限及派生资源更新语义保持；配置失败不能误报成功。
- [x] 既有保存后的队列放行受协调边界约束，不能在更新尚未一致时触发依赖旧配置的工作；不改变正常配置更新的用户流程。
- [x] 主测试通过已有 Controller、临时真实配置文件和 config.Manager 观察磁盘与运行中结果，参考 TestPersistConfigRebuildsDerivedResources；不只断言私有同步原语。
- [x] 用可控屏障覆盖两个设置保存、保存与热重载交错，以及持久化或生效失败，证明无丢失更新、无错误成功结果，错误后仍可进行后续有效保存。
- [x] 通过现有配置与热重载回归，未改变用户无关配置；本票可在未实现路径迁移时独立验收。

本票不依赖资料库访问协调，也不直接修改迁移规格或任务身份。遵循父规格的实现门禁；跨介质操作状态、补偿及启动恢复归 08。

## Implementation evidence

- `config.Manager.Save` / `Reload` coordinate expected-state checks, disk writes, runtime application, rollback and post-publication queue release. Mutation callbacks can compare only relevant policies and retain unrelated settings; no database/config joint transaction is claimed.
- System config and KOReader GET expose opaque `ETag`; POST requires `If-Match` (428 missing, 409 stale). Settings section drafts retain the original version when a refetch discovers changed server fields. CORS permits both headers.
- `internal/api/config_consistency_test.go` covers stale and overlapping HTTP saves, completed-update browser staleness for both settings endpoints, Save/Reload barriers, pending external edits, persistence/application failures, reload rollback and queue publication ordering. `web/src/pages/settings/configVersion.test.tsx` covers version submission, conflict drafts and a dirty library draft after an unrelated save reveals moved policies.
- Verified: full config/API tests; targeted config/API race run before the final added queue test; Go formatter empty; compatible Go 1.26.5 golangci-lint `0 issues.`; frontend lint/build; full frontend 821 tests followed by targeted 3 interaction tests after the section-version fix.
- Final full `go test ./...` passed API/config/server and most packages, but Windows Application Control blocked booksort/logger/proposal test executables. Final race rerun passed config but the OS blocked api.test.exe. No security settings were changed; integration must repeat the final suite/race on its compatible toolchain.
