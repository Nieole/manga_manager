# 实施执行记录

集成分支：`codex/architecture-refinement`。固定基线：`badc595541250189586b87e9d1c1c83ac79fd677`。

规格与依赖以 [spec.md](spec.md)、[queue.md](queue.md) 和逐票验收为准。只有代码合入、验收满足且验证完成的票才标记 `done`；本记录不替代逐故事联合验收。

## 基线验证（2026-10-26）

- Go 工具链使用 CI 的 `go1.26.10`，共享仓库 `.gocache`，每树独立 lint 缓存。
- `golangci-lint run`：通过，`0 issues.`。
- 前端全量 Vitest：初次 912 通过、1 个超时；定向重跑 1/1 通过，降低并发后全量 94 文件、913/913 通过（`.tmp/refinement-baseline-web.log`）。前端 lint 与 build 通过。
- 后端全量测试：基线全量未通过。Application Control 拦截后，relocating/scanning/logger/organize/seriesmeta 定向重跑通过，metadata 仍被拦截。scanner 的 TestWatcherEventLoopSchedulesScanOnNewFile 停止时阻塞在 fsnotify.Remove，10 分钟超时；定向全包复跑通过（16.765s），留给 28 核对停止协调。原始输出见本地 `.tmp/refinement-baseline-go.log` 与重试日志。

## 实施状态

01、08、10 已合入且通过组合验证并关闭，关闭提交 `e0f7d1b`。组合 reading/proposal/API 完整测试通过（API 117.205s），人工元数据 SQLite/HTTP 定向及前端消费者 31 项通过；最终 gofmt 无输出、lint 0 issues。逐票记录含实际生产消费者和各自全量前端结果。

02 已合入并关闭（`20bc0b5`），主体列表/HTTP 与缓存消费者回归通过；13 已合入并关闭（`1871f05`），organize/runtime、API race、前端状态消费者通过，sqlc 无漂移、格式及 lint 通过。

03、09 已合入并关闭（`b6a701d`）：09 最终 owner 整包在主树实际通过，reading 整包、organize/API 定向及前端 52 项通过，gofmt/lint/diff 检查通过。

04 已合入并关闭（`4d49045`），reading/API 协议与鉴权回归通过；14 已合入并关闭（`f31aca1`），organize/scanner/API 级联、事实、回滚、重开及升级回归通过，sqlc 无漂移、格式及 lint 通过。

05、16 已合入并关闭（`ceda2d1`）：阅读写入、迁移/离线 fixture、API/封面组合回归通过，内容/解析完整 race、73 项前端消费者通过，格式及 lint 通过。05 的独立全仓记录保留分次执行的系统拦截限制。

06 已合入并关闭（`096b8cc`），书签/时长主体与会话消费者通过；17 已合入并关闭（`b4c80a0`），主树下载、Range、鉴权、真实断连与独占冲突回归通过，内容/解析完整 race 和格式/lint 通过。

07 已合入并关闭（`a078803`），读者模型收缩的查询/协议/看板/命令与生成契约验证通过；22 已合入并关闭（`8852105`），同Engine准入保留、队列、真实退出、失败释放、17下载阻删与UI回归通过，相关race/格式/lint通过。

18 已合入并关闭（`1e4c436`）：主树实际执行了文件提交范围与RunEvents断言，发布/原文件保护/下载占用/删库准入相关回归、runtime/parser整包、diskjobs/content完整race通过，格式及lint通过。普通API启动拦截保留原始失败记录，最终API-race实际通过。

11、23、19 已依次合入并关闭：11 合集/快照及权限 API race 86.945s、organize 8.175s、reading 9.227s、数据库窄能力 race 7.237s 与 41 项页面消费者通过；23 删库 HTTP race 30.846s、libraries 全量 race 19.331s、drain/准入保留 race 4.762s 与 5 项删除界面消费者通过；19 external 全量 race 15.517s、diskjobs 全量 race 15.561s、外部/删除/下载组合 HTTP race 45.801s 与 16 项界面消费者通过。最终前端 lint/build、Go lint 0 issues、gofmt 和 diff 检查通过；原始日志为 `.tmp/merge-wave9-*.log`。本轮部分启动被 Application Control 拦截，重跑实际通过才计入；数据库普通全包在主树仍被拦截，实现树全包实际通过 13.472s，主树新窄能力 race 已实际通过。Windows 跳过的 Unix symlink 测试不计其他平台执行。

当前完成 25/48：01、02、03、04、05、06、07、08、09、10、11、12、13、14、15、16、17、18、19、20、22、23、24、29、36。21、26 在独立工作树实施，41 将继续推进。原始集成门禁日志位于主树 `.tmp/merge-*.log`，已完成工作树的日志汇总到 `.tmp/evidence/`；01、02、08、10 工作树已注销。10 残留空目录的删除被自动审批以 blocked by policy 拒绝，不影响分支或源文件。

12 已合入并关闭：主树 AI 分组 owner race 13.910s、数据库窄能力 race 7.706s、集成后全前端 99 文件 / 937 测试及 lint/build 通过，Go lint 0 issues、格式和 diff 检查通过。主树 API 多次被 Application Control 拦截，稳定二进制启动也未确认执行，已停止精确启动器并保留限制；API 实际通过依据为实现树完整 104.921s、focused race 17.835s 和合并集成后 focused 普通 7.710s。日志位于 .tmp/merge-wave10-*.log。

20 已合入并关闭：主树 diskjobs 全量 race 16.903s、外部传输 HTTP race 24.134s、13 项前端消费者及 Go lint/格式通过。主树 external 全量普通/race 启动仍被 Application Control 拦截，未计为通过；实现树合并当前集成后的完整普通 3.803s、完整 race 14.845s 已实际通过。其他平台仅 cross-compile，不计运行验证。15 已合入并关闭：主树 organize/reading 相关 race 12.167s/11.937s、数据库窄能力 race 7.067s、阅读清单 HTTP race 19.770s、3 项页面消费者通过；最终 Go lint 0 issues、gofmt 和 diff 检查通过。数据库/API 首次启动被拦截后，记录的重跑实际执行通过。日志为 .tmp/merge-wave11-*.log。

36 已合入并关闭：主树 task/taskstore 完整 race 2.301s/10.198s、Task/Run/Push/SSE 组合 API race 148.976s、124 项前端消费者实际通过；tsgen 24 类型无漂移，lint/格式通过。24 已合入并关闭：主树 libraries/runtime 完整 race 22.509s/15.478s、14 项功能消费者及 lint/格式通过。24 主树库 HTTP 普通与 race 启动仍被系统拦截，未算通过；实际完整 API 根据实现树稳定二进制 PASS 记录，数据库 race 超时也保留为未通过。日志为 .tmp/merge-wave12-*.log。

本波修复此前合并时误保留的 CHANGELOG 冲突标记，并独立清理 queue/types 两处格式；以完整基线 git diff --check badc595...HEAD 和全 tracked 冲突标记扫描验证，补足此前只检查工作区差异的遗漏。

29 已合入并关闭：主树 config/metadata/intelligence 完整 race 6.398s/6.628s/10.949s、搜索/抓取/连接配置 HTTP race 35.510s 实际通过，lint 0 issues、gofmt、完整基线 diff 检查与 tracked 冲突标记扫描通过。无前端、SQL 或生成契约变化。日志为 .tmp/merge-wave13-*.log。
