# 架构深化双线审查

集成分支：`codex/architecture-deepening`。固定比较为 `ec0d2175563c57c2979775d7971c2b37a0375c4f...44cd723`；完整提交列表随库外证据保留。两名独立审查者分别检查规范和规格，直接阅读四方向的生产代码与测试，不以工单状态或验收表代替源码判断。

## Standards

0 项硬规范违反，0 项值得报告的 possible smell。

规范来源包括 `AGENTS.md`、`docs/agents/`、`CONTEXT.md` 和相关 ADR。核对范围涵盖 reading 的主体、事务及活动边界，organize 的身份、事务和派生更新，前端会话与功能投影，以及应用发起、扫描封面交接、迁移、恢复和停机。

协议适配、应用能力组合及重启转发具有明确职责，不作为 Middle Man 报告。任务体使用上下文日志；普通常驻维护日志符合规范例外。Fowler smell 基线仅作判断性检查，仓库约定优先，工具已经强制的事项不重复报告。

## Spec

原审查发现 1 项 P2，已修复；未解决问题为 0。

US 69 要求保留成员顺序及进度回填，票 06 要求保留“全部已读或缺少可继续位置时的既有回退结果”。清单的 `Items`、`Page` 直接使用 `NextUnreadBookID`，在全部已读时得到零，导致页面禁用原有重读入口。实施注记和测试的新期待不构成产品批准。

修复提交 `daa519a` 已合入 `c5a15bf`：清单完整用例共用 `readingListTarget`，在共享主体投影上保留原清单的未完成优先、空排序号优先、排序号和名称顺序，全部已读回退首书，空系列保持零。全局 `Summarize` 的下一本未读语义不变。原 Spec 审查者复核确认该 P2 已解决，未发现修复新增的确切问题。

真实 SQLite 两轮红绿、页面按钮与导航、HTTP 目标书读取及协议回归支持该修复。协议没有新增字段。具体测试、当前验证结果和限制归 [联合验收](acceptance.md#清单首书回退审查修复证据)。

Standards 共 0 项，无最高严重项；Spec 原有 1 项，最高 P2，已解决。两轴不合并排序。

## 最终验证与交付范围

修复版本前端 913 项测试、构建、lint，以及 Go 格式和 lint 通过；文档检查器最终退出零。首轮完整 Go 运行中 27 包实际通过，API 完整包和新增导航断言通过；`application/diskjobs`、`images`、`task` 三包测试程序被 Windows Application Control 拒绝执行，完整命令退出非零。用户说明拦截可能有间歇性后，按原工具链和路径正常重试这三包，全部实际通过、命令退出零；未修改安全策略或绕过限制。最终代码的 30 包已分批验证通过，不将分批补验描述成一次完整命令退出零。补验原始日志为库外 `application-control-retry.jsonl`。清理前完整通过及原始日志覆盖限制另见联合验收。

主工作区已重建当前前端产物并完成 Go 构建。Ubuntu-only race、govulncheck 和远端多平台 CI 未在本机执行。所有 25 张工单为 done，112 条故事的实现与执行映射保留于联合验收；本次未推送或创建 PR。

## 工作树清理

已检查绝对路径、提交合入状态、干净状态及全部 reparse point，正常移除 21 个无链接风险的实现工作树。测试日志已保存在库外，分支仍可用于追溯。主工作区及无关 relocation 工作树未清理。

保留 `manga_manager-reading`、`manga_manager-collections`、`manga_manager-03`、`manga_manager-04`、`manga_manager-06`。其中 reading、03、04、06 含指向主树依赖的 junction；reading 的缓存、collections 的缓存及 06 的链接清理此前被自动审批以 `blocked by policy` 拒绝。未换工具或强制删除绕过拒绝。

库外原始审查结果、门禁日志、清理前链接审计和逐树清理结果统一位于 `C:/Users/saber/.codex/tmp/architecture-deepening/`；清理不删除这些证据。
