# 架构深化集成记录

集成分支：`codex/architecture-deepening`。固定基线：`ec0d2175563c57c2979775d7971c2b37a0375c4f`。

规格及测试 seam 以 `spec.md` 为准，各票验收以独立 issue 文件为准。代码合入不等于门禁全部通过；未通过的验收项保持未勾选。

## 环境与门禁

- 基线全量 `go test ./...` 在 CGO 编译阶段受阻：Windows Application Control policy 阻止 Scoop `gcc.exe` 执行。纯 Go 包可运行；依赖 `chai2010/webp` 的包无法用 `CGO_ENABLED=0` 替代验证。不得将编译受阻记成测试通过。
- 系统 Go 1.27 与 CI 固定 golangci-lint 的 export data 不兼容；使用已验证可用的 CI 同款 `GOTOOLCHAIN=go1.26.10` 执行相关门禁。lint 使用 v2.12.2，每工作树独立缓存。
- 基线日志：`C:/Users/saber/.codex/tmp/architecture-deepening/baseline-go-test.log`。

## 集成测试进展

- 在 `9658dac` 上启动的全量 Go 测试实际进入 API 断言，服务入口、reading/database、organize、application/task/taskstore、scanner 等包通过。
- API 唯一失败为旧 `TestUpdateBookProgressThrottlesRepeatedWrites`，其 spy 只拦截已替换的写入方法，已交 04 用公开行为回归替换。
- booksort 与 koreader 测试可执行文件仍被 Application Control 拦截，因此该次全量门禁失败；不能用其他包通过代替完整门禁。
- 日志：`C:/Users/saber/.codex/tmp/architecture-deepening/integration-go-test.log`。后续按原工具链正常验证，不修改编译器路径或系统策略。

## 已合入

| 票 | 实现提交 | 集成提交 | 验证结果 |
| --- | --- | --- | --- |
| 01 | `c630bd7` | `6ddeb64` | reading/database 真实 SQLite 测试及定向 lint、前端测试构建 lint 通过；集成最小回归通过。HTTP/全量门禁受环境阻碍，票未关闭。 |
| 03 | `905414b` | `52aa2a2` | reading/database 全包、清除拒旧 HTTP 回归及前端最小回归通过；前端完整测试构建 lint 与局部 Go lint 通过。等待全量验收。 |
| 09 | `aac6601` | `a41f201` | 人工合集真实 SQLite 测试、organize lint、全仓 gofmt 通过。API/全量门禁受环境阻碍，票未关闭。 |
| 14 | `1252c8f` | `b17df4e` | 前端全量 869 测试、构建与 lint 通过；集成后 8 项会话/路由回归通过。后端 SSE 权限回归已在集成分支补验通过，票已关闭。 |
| 15 | `f282819` | `9658dac` | 全前端 876 测试、build/lint 通过，合入后 25 项会话及消费者回归通过；票已关闭。 |
| 17 | `b926ad6` | `1aa92c5` | application/task/taskstore 真实 SQLite 测试与局部 lint 通过，合入后测试通过。API/全量门禁受环境阻碍，票未关闭。 |

## 联合验收

25 票尚未执行。全部 112 条故事的最终实现与验证映射需在联合验收阶段逐条核对。
