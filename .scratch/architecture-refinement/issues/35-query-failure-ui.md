# 35: 看板与统计的失败陈旧呈现

**What to build:** 看板、统计及相关列表区分真实零/空、失败、未加载和旧结果，并可重试。

**Blocked by:** None (can start immediately)。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 181、182、183。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [ ] 首次失败显示不可用和重试，已有成功结果可以保留并明确陈旧。
- [ ] 主干失败不假装成功，附属降级按功能定义，不强套通用页面状态机。
- [ ] 切身份、目标或请求代次不保留错误主体的旧结果。
- [ ] 页面/消费者验证真零、真空、失败、陈旧及恢复，保留当前可用布局。

## Comments

### 实施与证据（2026-11-02，待独立合并验收）

- 实现树：`C:/Users/saber/dev/manga-refinement-35`，分支 `codex/refinement-35`；实际消费集成基线 `b913455f22f1b7eff431ffec2e555d1ea41987c1`。票保持 `ready-for-agent`，未宣告关闭。
- `Dashboard.tsx`：资料库查询成功且确实为空才呈现引导；空库的后续刷新失败也标旧。概览、最近阅读、管理员审核数、热力图与延迟推荐独立显示加载/不可用/重试，已有同目标数据保留并标旧；成功空列表有独立提示。热力图不再依赖概览成功，也不把失败画成全零活动。
- `Stats.tsx`：连续阅读、时长与期间回顾独立失败/恢复，未取得结果显示破折号而非零；回顾失败不再无限转圈。切换年月清除旧期间值，同期间刷新失败保留标旧值。
- `pages/insights/useInsightPanel.ts` 和 `InsightNotice.tsx` 仅服务这两页，无全站查询 store；两页以认证主体为 keyed 生命周期，URL 作用域与每次 effect 清理阻止旧期间/旧代次迟到成功或失败。未修改设置、离线、目录/session provider 或 32 所有的生命周期 helper。
- 已确认测试 seam 为实际页面消费者 + HTTP 客户端边界。先执行首次失败消费者 RED，再接入实现 GREEN；后续空库陈旧、成功空列表另有 RED/GREEN 日志。14 项页面回归覆盖真零/空、附属降级、旧值/恢复、身份切换、推荐在飞换人、期间/热力图范围竞争及旧代次失败不覆盖较新成功。移除身份 key 与 active fence 的敏感性运行 7 项失败，恢复实现后全部通过；这份故意失败证据不是生产门禁失败。
- 最终全前端：`cd web && ../.tmp/node.exe node_modules/vitest/vitest.mjs run --maxWorkers=2`，104 文件 / 968 项全部通过（137.51s），日志 `.tmp/35-full-tests-final.log`；定向 14/14 日志 `.tmp/35-consumers.log`。
- 最终 lint：`cd web && ../.tmp/node.exe node_modules/eslint/bin/eslint.js .`，退出 0，日志 `.tmp/35-lint-final.log`。
- 最终 build：`cd web && ../.tmp/node.exe node_modules/typescript/bin/tsc -b && ../.tmp/node.exe node_modules/vite/bin/vite.js build`，退出 0，日志 `.tmp/35-build-final.log`。环境 PATH 无 npm/node，以本树 `.tmp/node.exe` 直接执行 package.json 相同工具；未更改依赖。
- `git diff --check` 退出 0，日志 `.tmp/35-diff-check.log`。原始 RED、GREEN 和 fence 敏感性日志均保留在本树 `.tmp/35-*.log`。无后端改动，未跑 Go 门禁。
- 可供父代理 visualize 的自包含静态示意图：`.thinkrail/context/mockups/ticket35-before.html` 与 `.thinkrail/context/mockups/ticket35-after.html`；只含内联 CSS，无脚本、表单、外链图片，保留当前深色/紫色卡片语言。它们是状态 mockup，不冒充浏览器截图；本轮未做真实浏览器/服务器联调。
- 所有改动未提交、未暂存。上级约束禁止任何 index/history 修改，故未 merge/commit；最终观察集成分支已到 `16cfc9f834f4507b262553541d90206bb72918a0`，该 tip 未消费，须由独立合并者集成并重跑门禁。locale 仅新增 `insights.*` 键；CHANGELOG 同步当前行为，合并时留意 32 的 locale/CHANGELOG 相邻内容。
