# 架构深化集成记录

集成分支：`codex/architecture-deepening`。固定基线：`ec0d2175563c57c2979775d7971c2b37a0375c4f`。

规格及测试 seam 以 `spec.md` 为准，各票验收以独立 issue 文件为准。代码合入不等于门禁全部通过；未通过的验收项保持未勾选。

当前交付：01—25 已完成适用本机验收；逐条故事、当前 owner、联合场景、门禁及未执行范围集中见 [acceptance.md](acceptance.md)。本文件保留各批提交和过程证据；集成分支的最终双线审查由后续独立执行，不将它写成本票已完成结果。

## 环境与门禁

- 基线全量 `go test ./...` 在 CGO 编译阶段受阻：Windows Application Control policy 阻止 Scoop `gcc.exe` 执行。纯 Go 包可运行；依赖 `chai2010/webp` 的包无法用 `CGO_ENABLED=0` 替代验证。不得将编译受阻记成测试通过。
- 系统 Go 1.27 与 CI 固定 golangci-lint 的 export data 不兼容；使用已验证可用的 CI 同款 `GOTOOLCHAIN=go1.26.10` 执行相关门禁。lint 使用 v2.12.2，每工作树独立缓存。
- 基线日志：`C:/Users/saber/.codex/tmp/architecture-deepening/baseline-go-test.log`。

## 集成测试进展

- 在 `9658dac` 上启动的全量 Go 测试实际进入 API 断言，服务入口、reading/database、organize、application/task/taskstore、scanner 等包通过。
- API 唯一失败为旧 `TestUpdateBookProgressThrottlesRepeatedWrites`，其 spy 只拦截已替换的写入方法，已交 04 用公开行为回归替换。
- booksort 与 koreader 测试可执行文件仍被 Application Control 拦截，因此该次全量门禁失败；不能用其他包通过代替完整门禁。
- 日志：`C:/Users/saber/.codex/tmp/architecture-deepening/integration-go-test.log`。后续按原工具链正常验证，不修改编译器路径或系统策略。

- 2026-10-25：已安装 CI 固定 govulncheck v1.6.0，原路径正常执行被 Windows Application Control 明确拦截，漏洞扫描未运行；未改名、改路径或绕过策略。该项在 CI 仅 Ubuntu 执行，Windows 本机不将其列为交付阻塞；验收保留未执行记录。

## 已合入

| 票 | 实现提交 | 集成提交 | 验证结果 |
| --- | --- | --- | --- |
| 01 | `c630bd7` | `6ddeb64` | reading/database 真实 SQLite 测试及定向 lint、前端测试构建 lint 通过；集成最小回归通过。后续完整 Go 与 HTTP 门禁补验通过，票已关闭。 |
| 02 | `f9936ae` | `b37240b` | 批量/回退真实 SQLite 与队列/身份切换回归、前端900测试及构建lint通过；无时间事实ID边界已修复并全量补验通过，票已关闭。 |
| 03 | `905414b` | `52aa2a2` | reading/database 全包、清除拒旧 HTTP 回归及前端最小回归通过；前端完整测试构建 lint 与局部 Go lint 通过。时钟修复后全量补验通过，票已关闭。 |
| 04 | `ecb7882` | `cb8fab1` | 全量 Go、lint/格式/SQL与TS生成检查通过；前端 893 测试、build/lint 通过，合入定向回归通过；票已关闭。 |
| 05 | `08a94ab` | `021676b` | 完整 Go、合并后设备协议/缓存/reading回归、lint/格式/SQL与TS生成检查通过；票已关闭。 |
| 06 | `a1792aa` | `b8d7ca0` | 完整 Go、主体投影/继续阅读/上下本/协议及页面回归、lint/格式通过；票已关闭，06树junction保留禁止递归清理。 |
| 07 | `4d4e2f3` / `bc666f9` | `245b563` | 时长真实SQLite/HTTP/前端907测试、完整Go、build/lint/生成检查通过；独立fixture排空修复及合入focused回归通过，票已关闭。 |
| 08 | `a35be7b` / `1b378f8` | `99abb19` | 真实SQLite/HTTP/页面、完整Go及910前端测试、build/lint/格式通过；合入页面回归通过，后端复验exe被策略拦，既有绿证据保留，票已关闭。 |
| 09 | `aac6601` | `a41f201` | 人工合集真实 SQLite 测试、organize lint、全仓 gofmt 通过。相关 API/协议/授权回归与全量 lint 已补验通过，票已关闭。 |
| 10 | `659adad` | `d038411` | 智能书架/快照真实 SQLite、完整 API、协议及分页回归、lint/格式/生成契约通过；合入focused回归通过，时钟修复后全仓补验通过，票已关闭。 |
| 11 | `53028c8` | `f809dec` | 关系/作品群真实 SQLite、Graph/OPDS/Mihon、完整 Go、lint/格式/生成检查通过；合入focused回归通过，票已关闭。 |
| 12 | `2b401ad` | `5cd4cec` | AI 裁决真实 SQLite、并发/回滚/授权/协议身份、完整 Go、lint/格式/生成检查通过；合入focused回归通过，票已关闭。 |
| 13 | `0fd41c8` / `7ccda2c` | `8a38cf0` | 清单真实SQLite/身份/顺序/主体及协议页面回归、完整Go/前端/格式/lint/生成检查通过；合入focused回归通过，票已关闭。 |
| 14 | `1252c8f` | `b17df4e` | 前端全量 869 测试、构建与 lint 通过；集成后 8 项会话/路由回归通过。后端 SSE 权限回归已在集成分支补验通过，票已关闭。 |
| 15 | `f282819` | `9658dac` | 全前端 876 测试、build/lint 通过，合入后 25 项会话及消费者回归通过；票已关闭。 |
| 16 | `f19f7bf` | `2d3eeb7` | 全前端 890 测试、build/lint 通过，合入后 10 项消费者回归通过；票已关闭。 |
| 17 | `b926ad6` | `1aa92c5` | application/task/taskstore 真实 SQLite 测试与局部 lint 通过，合入后测试通过。后续完整 Go 与 API 门禁补验通过，票已关闭。 |
| 18 | `615f52d` | `0a3fa49` | 应用扫描真实 SQLite、scanner 全包及定向 API 回归通过；全 lint 0 issues，格式及生成契约通过。时钟修复后完整 Go 门禁通过，票已关闭。 |
| 19 | `be1d728` | `89d6943` | 全量 Go 除已知旧阅读回归外全部通过，lint/格式/生成契约通过；合入后封面编排及日志回归通过，时钟修复后整体 Go 门禁通过，票已关闭。 |
| 20 | `b527b3f` | `2e4acec` | 迁移/恢复应用 SQLite、配置故障与 API 组通过，lint/格式/生成契约通过；读态时钟边界修复后完整 Go 门禁通过，票已关闭。 |
| 21 | `8e54d0a` | `19c7f29` | 完整Go/vet/build/lint/生成/文档检查、维护重启及清除重放回归通过；合入focused通过，票已关闭。 |
| 22 | `8ee6352` | `c7331ff` | 完整 Go、lint/格式/生成检查、外部与ComicInfo应用/协议回归、相关前端验证通过；合入focused回归通过，票已关闭。 |
| 23 | `ca544b0` | `bf1628d` | 完整 Go、合并后intelligence/API与17项前端回归、build/lint及生成检查通过；票已关闭。 |
| 24 | `50e5208` / `b6f4b4d` | `21253f8` / `cb85378` | 生产等价应用图、恢复/停止/Manager/历史重放回归及完整本机门禁通过；独立 Release/Claims 夹具身份遗漏修正后，本轮分别精确独跑退出 0，并在 25 全量复验通过。 |
| 25 | `14483e1` 及联合验收提交 | 待集成 | 阅读器日界三出口红绿、设备偏好保持、112 故事逐条执行映射及结构/ADR核对完成；94 文件913项前端、清理前30包Go及静态/生成门禁通过。清理后受影响 API/reading 等25包通过，额外完整运行5包系统拒执行。库外 `acceptance-25/` 的日志覆盖限制见联合验收，不宣称最新全量全绿。 |

## 联合验收

25 票的 [联合验收](acceptance.md) 对全部 112 条故事给出真实 owner、生产入口、直接测试与当前执行界限。196 条 Go 故事/测试引用逐个对应本轮同名 pass，前端索引按精确标题核对 JSON passed；结构/阶段故事另有人工核对，没有从工单 done 推导行为。

## 补验状态

04 完整门禁已在包含 19 的集成内容上通过，01、03、17、18、19 的待补验项据此补齐并关闭。表内早期受阻和等待描述记录各批合入时结果，最终状态以各票最新集成补验为准。

20 集成暴露的同刻兼容操作回归由 04 的 `4e68d8d` 修复并合入 `9b55d36`；固定时钟的用户/匿名、按书/系列及跨位置/标记日界操作红绿验证，完整 Go、lint 与格式补验通过（`post-clock-go-test.log`），03、04 恢复 done，10、20 关闭。25 进一步以真实前端 hook 复证并纠正独立的合成毫秒日界问题，不放宽后端显式历史拒旧。

24 的旧整体通过没有覆盖独立 Release fixture 未登记身份的遗漏。`b6f4b4d` 修正 fixture，原树绿复验被系统策略拦截；25 在合入 `cb85378` 后分别精确运行 Release 四分支和 Claims，均实际通过退出 0，再做完整 Go 门禁。早期断言失败、系统阻断和随后绿证据分别记录，不互相混同。

旧执行受阻/回归发现记录保留为证据，当前状态以票据 Status 和联合验收为准；此前库外只读草图是定位材料，未作为最终验收。ADR 0008 的独立计划未完整实施，按源码保留明确状态；不将该范围外工作记作本批完成。

## 本机验收范围

以 AGENTS.md 的本地 Go 测试、gofmt、golangci-lint，以及受影响前端测试/构建/lint、SQL 与 TypeScript 生成检查为必需门禁。已核对 .github/workflows/ci.yml：govulncheck 与 race 仅在 Ubuntu job 执行；本 Windows 实施不宣称这些检查通过，如实列为未执行，不要求更改安全策略或作为额外交付阻塞。其他平台 CI 结果也不由本机结果代替。
