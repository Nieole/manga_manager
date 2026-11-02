# 32: 设置分区编辑会话

**What to build:** 配置草稿、基线及版本由编辑会话维护，KOReader 诊断和维护动作独立生命周期。

**Blocked by:** 30: 配置发布与生效范围。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 138、143、169、170、171、172。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [ ] 消费明确生效信息，单一分区字段归属用于脏判断与保存。
- [ ] 保存不顺带其他草稿，刷新不替脏草稿变更版本基线。
- [ ] 账户诊断和维护功能从草稿总装解耦，界面入口与必要状态保持。
- [ ] 消费者/页面验证并发、冲突、重试、切换分区和生效提示。

## Comments

### 2026-11-02 — implementation retry / verification (not merged)

保留接手时全部未提交实现，继续在 `manga-refinement-32` / `codex/refinement-32` 验证；未修改 33 离线或 34 目录功能文件。

- `features/settings-editor` 拥有唯一分区字段清单、草稿、服务端快照、逐分区基线/版本及冲突；页面消费保存、dirty、冲突和显式重载。保存期间继续编辑保留新值，其他分区草稿不会顺带提交，刷新不替 dirty 分区换版本。页面旧 `SettingsContext.tsx` 已由功能入口替代。
- `features/koreader-settings` 独立维护账户、状态、未匹配与设备诊断请求代次；`features/settings-actions` 独立维护连接测试和即时维护动作。挂载/会话生命周期保护迟到回复，不引入全站 store。
- 接手工作区已包含原 `device_count` / path 场景的后续修复及真实 `SettingsKOReaderPage` 消费测试；本次首次实际复跑 5 文件 / 34 项通过（`.tmp/ticket32-retry-consumers-node.log`），未删除或放宽失败覆盖。原失败日志 `.tmp/ticket32-editor-consumers.log` 保留。
- 增强已提交保存/刷新失败回归：同时失败 config 与 KOReader 状态读取时，诊断消费者仍应显示 POST 已确认的新路径。先实际 RED（旧路径 `/koreader` 而非 `/saved`，`.tmp/ticket32-ack-status-red.log`），再使 KOReader provider 消费保存返回的权威 status；GREEN 5 文件 / 34 项（`.tmp/ticket32-ack-status-green.log`）。保存成功、刷新告警、dirty 清除、下一次 If-Match 及在飞编辑断言全部保留。

实际门禁（日志均在本树 `.tmp/`）：

- `npm run lint`：exit 0，`ticket32-retry-lint.log`。
- `npm run build`（tsc + Vite）：exit 0，`ticket32-retry-build.log`。
- `npm run test`：103 文件通过、1 文件失败，966/967；`pages/library/adminActions.test.tsx` 等待“批量操作”超时，原日志 `ticket32-retry-full-test.log` 保留，未改该测试。
- `npm run test -- src/pages/library/adminActions.test.tsx`：3/3，`ticket32-retry-admin-actions.log`。
- `npm run test -- --maxWorkers=2`：全量 104 文件 / 967 项通过（201.59s），`ticket32-retry-full-test-workers2.log`。
- `GOTOOLCHAIN=go1.26.10 go run ./cmd/tsgen` + `git diff --exit-code -- web/src/api/generated.ts`：exit 0，26 类型无漂移，`ticket32-retry-tsgen-verified.log`。无 SQL/后端源变更，未重跑 Go 全量。
- 全基线到工作区 `git diff --check badc595541250189586b87e9d1c1c83ac79fd677` 无输出；tracked 源码/Markdown 冲突标记扫描无命中（`ticket32-retry-diff-check.log`、`ticket32-retry-markers.log`）。
- 全仓 `bash scripts/check-doc-style.sh` 两次分别在 120s/180s 命令时限内未完成，无完成结果，不计 PASS；日志 `ticket32-retry-doc-style.log`、`ticket32-retry-doc-style-verified.log` 保留。

集成限制：本次执行器的上级约束只允许 git 只读操作，禁止提交、索引或历史变更，故未执行用户要求的 merge/commit。本树基线仍为 `feb2e6b5c91c3cdfe936b3a4f1ee5432ef8f9aad`；已核对最新 integration `b913455f22f1b7eff431ffec2e555d1ea41987c1`，两 tip 的 `web/` 差异为空（`ticket32-retry-integration-web-diff.log`），但这不等于已合并或已验证集成后的工作树。独立 owner/merger 必须先 merge 当前 integration，再复核适用门禁、提交本票源/文档；本票保持 open，不能据此记录为最终交付或已 consumed integration。


### 2026-11-02 — independent blocking-review fixes (dirty / open)

保留全部接手时未提交实现；本次只补两项 blocking findings，没有提交、合并、暂存、清理或修改 main。

- `SettingsEditorProvider.tsx`：连接协议保存返回 `{ committed: boolean }`，缺配置、重复请求或失效生命周期明确未提交；`SettingsConnectionsPage.tsx` 消费确认后才提示成功，connections 分区在飞期间两个协议开关都禁用。
- 显式 KOReader discard/reload 成功后，编辑会话发布 `koreaderReloadVersion`；`KOReaderProvider.tsx` 据此重新请求已发布 status，使用既有 latest-request / session / mount fences。配置重载确认与诊断刷新失败分开，刷新失败只报 refresh 告警，不冒充保存成功或保存失败。
- 真实消费者 RED/GREEN：协议页第二开关原为可点击（`ticket32-review-protocol-red.log` → `ticket32-review-protocol-green.log`）；真实 KOReader 页发生 409 后显式重载，表单及 conflict 已更新而 setup URL 原仍旧值（`ticket32-review-reload-red.log` → `ticket32-review-reload-green.log`）。保留所有旧断言，尤其 POST 已确认而 config/status 后续读取均失败的回归；新增未提交结果、重载后旧 status 迟到、重载确认后 status 刷新失败覆盖。
- 新增测试使用现有消费者 seam，不直测 EditSession 内部。协议页测试等待配置 Ready 后点击，避免把配置尚未加载误当成并发保存。

实际门禁（exit 0，日志在本树 `.tmp/`；node 不在默认 PATH，使用已安装 `C:/Users/saber/dev/manga-refinement-34/.tmp/node.exe`，未修改该树）：

- `node node_modules/vitest/vitest.mjs run src/pages/settings --maxWorkers=2`：5 文件 / 39 项，`ticket32-review-consumers.log`。
- `node node_modules/vitest/vitest.mjs run --maxWorkers=2`：104 文件 / 972 项，232.02s，`ticket32-review-full-test-workers2.log`。
- `node node_modules/eslint/bin/eslint.js .`：`ticket32-review-lint.log`（空输出）。
- `node node_modules/typescript/bin/tsc -b && node node_modules/vite/bin/vite.js build`：`ticket32-review-build.log`。
- `GOTOOLCHAIN=go1.26.10 go run ./cmd/tsgen && git diff --exit-code -- web/src/api/generated.ts`：26 类型无漂移，`ticket32-review-tsgen.log`。无后端/SQL变更，不重复 Go 全量。
- `git diff --check`：无输出，`ticket32-review-diff-check.log`。编辑的文本保留 LF，避免 Windows 写入造成全文件 CRLF diff。

本次新增修改范围：上述两个 provider、连接页、`settingsLifecycle.test.tsx`、本评论、`design.md` 当前契约说明、`CHANGELOG.md`。其余 dirty 内容原样保留。原 evidence logs 均保留。

源 tip 仍为 `codex/refinement-32` / `feb2e6b5c91c3cdfe936b3a4f1ee5432ef8f9aad`，修复在其 dirty 工作树。当前 integration tip 为 `codex/architecture-refinement` / `16cfc9f834f4507b262553541d90206bb72918a0`（已集成 offline 归属）；相对源 tip 的 web 差异为 22 文件（`ticket32-review-integration.log`），不再是前次评论所述空差异。尚未消费该 tip，owner/merger 必须保留本树全部未提交源文件及文档，按允许流程集成并复跑门禁；不能仅合并源分支 tip 而遗漏 dirty 实现。本票保持 open / ready-for-agent，不记录已交付。
