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

当前完成 35/48：01、02、03、04、05、06、07、08、09、10、11、12、13、14、15、16、17、18、19、20、21、22、23、24、26、27、28、29、30、31、36、38、39、40、41。32、42 在独立工作树实施，25 将按依赖继续推进。原始集成门禁日志位于主树 `.tmp/merge-*.log`，已完成工作树的日志汇总到 `.tmp/evidence/`；01、02、08、10 工作树已注销。10 残留空目录的删除被自动审批以 blocked by policy 拒绝，不影响分支或源文件。

12 已合入并关闭：主树 AI 分组 owner race 13.910s、数据库窄能力 race 7.706s、集成后全前端 99 文件 / 937 测试及 lint/build 通过，Go lint 0 issues、格式和 diff 检查通过。主树 API 多次被 Application Control 拦截，稳定二进制启动也未确认执行，已停止精确启动器并保留限制；API 实际通过依据为实现树完整 104.921s、focused race 17.835s 和合并集成后 focused 普通 7.710s。日志位于 .tmp/merge-wave10-*.log。

20 已合入并关闭：主树 diskjobs 全量 race 16.903s、外部传输 HTTP race 24.134s、13 项前端消费者及 Go lint/格式通过。主树 external 全量普通/race 启动仍被 Application Control 拦截，未计为通过；实现树合并当前集成后的完整普通 3.803s、完整 race 14.845s 已实际通过。其他平台仅 cross-compile，不计运行验证。15 已合入并关闭：主树 organize/reading 相关 race 12.167s/11.937s、数据库窄能力 race 7.067s、阅读清单 HTTP race 19.770s、3 项页面消费者通过；最终 Go lint 0 issues、gofmt 和 diff 检查通过。数据库/API 首次启动被拦截后，记录的重跑实际执行通过。日志为 .tmp/merge-wave11-*.log。

36 已合入并关闭：主树 task/taskstore 完整 race 2.301s/10.198s、Task/Run/Push/SSE 组合 API race 148.976s、124 项前端消费者实际通过；tsgen 24 类型无漂移，lint/格式通过。24 已合入并关闭：主树 libraries/runtime 完整 race 22.509s/15.478s、14 项功能消费者及 lint/格式通过。24 主树库 HTTP 普通与 race 启动仍被系统拦截，未算通过；实际完整 API 根据实现树稳定二进制 PASS 记录，数据库 race 超时也保留为未通过。日志为 .tmp/merge-wave12-*.log。

本波修复此前合并时误保留的 CHANGELOG 冲突标记，并独立清理 queue/types 两处格式；以完整基线 git diff --check badc595...HEAD 和全 tracked 冲突标记扫描验证，补足此前只检查工作区差异的遗漏。

29 已合入并关闭：主树 config/metadata/intelligence 完整 race 6.398s/6.628s/10.949s、搜索/抓取/连接配置 HTTP race 35.510s 实际通过，lint 0 issues、gofmt、完整基线 diff 检查与 tracked 冲突标记扫描通过。无前端、SQL 或生成契约变化。日志为 .tmp/merge-wave13-*.log。

26、21 已合入并关闭。26 主树 bookidentity/KOReader/maintenance/runtime 完整 race 4.845s/46.562s/16.574s/20.573s、scanner 身份重联 race 15.011s、设备 HTTP race 21.118s 全部实际通过。21 主树 diskjobs/diskwork/storageio 完整 race 20.742s/6.009s/5.424s、external 普通全包 4.675s、14 项前端消费者通过。external race 启动被拦截，未算通过；实际 race 依据为实现树 34.446s。外部 HTTP 广泛匹配初次命中迁移测试，Windows rename Access is denied 失败保留；外部及该失败测试的 race 定向重跑实际通过 10.430s。lint 0 issues、gofmt、完整基线 diff 检查及 tracked 冲突标记扫描通过。10/29 恢复核对已结束原始日志，没有重复已通过整套门禁；日志为 .tmp/merge-wave14-*.log。

41 已合入并关闭：主树完整owner SQLite和HTTP搜索/上下文/推荐/看板/评价消费者普通测试实际通过 4.754s，3 项系列上下文页面消费者通过，lint 0 issues、gofmt、完整基线 diff 和 tracked 冲突标记扫描通过。主树 race 两次启动被 Application Control 拦截，不计通过；实际 race 依据为实现树合并后的 16.036s。五个 owner 包没有独立测试文件，完整用例由上述真实 SQLite API fixture 调用验证；包编译不代替行为测试。日志为 .tmp/merge-wave15-*.log。

38 已合入并关闭：主树初始化窄数据库能力 race 11.072s、并发 setup/claim/session/login/auth HTTP race 60.133s、3 项初始化恢复前端消费者实际通过。Go lint 0 issues、gofmt、完整基线 diff 检查和 tracked 冲突标记扫描通过。账户 owner 完整用例由真实 SQLite HTTP fixture 调用验证，包本身无独立测试文件。日志为 .tmp/merge-wave16-account-*.log。

27 已合入并关闭：主树扫描身份/重联/锁定/清理/封面 race 42.501s、新增两项提交边界 rollback 回归单独 race 6.663s、scanning/runtime race 10.302s/8.866s、扫描封面 HTTP race 15.572s 实际通过。database 的筛选命令没有选中测试，不计行为证据；窄摄入事务由真实 SQLite scanner 回归验证。lint 0 issues、gofmt、完整基线 diff 与 tracked marker 扫描通过。完整扫描 outcome 与 cleanup 资格仍由 28 接续，27不声称改变旧 warn+nil 语义。日志为 .tmp/merge-wave17-*.log。

39 已合入并关闭：主树账户管理窄能力 race 1.538s、最后管理员并发/当前actor权限/自删与级联 HTTP owner race 15.721s、4 项管理页消费者实际通过。Go lint 0 issues、gofmt、完整基线 diff 和 tracked 冲突标记扫描通过。改密与会话资格交接继续由40承接。日志为 .tmp/merge-wave18-*.log。

30 已合入并关闭：主树 config/publisher/intelligence/maintenance 全 race 3.761s/1.759s/5.959s/6.925s、配置 HTTP race 11.510s、明确发布与实际 DB 目标 HTTP race 2.692s、5 项设置消费者实际通过；tsgen 26 types 无漂移，lint 0 issues、格式/完整基线检查通过。runtime 全 race 首次实际失败于 shutdown handoff fixture 的 completed/cancelled 期待，整包重跑实际 8.822s 通过；初次失败日志保留，不声称已修复时序不稳定，31需核对真实关闭边界而非20ms假定。日志为 .tmp/merge-wave19-*.log。

40 已合入并关闭：主树密码提交与会话撤销、替换会话失败、旧 Basic 在飞回填、设备/强制改密协议及密码探测成本 HTTP race 实际通过 38.782s，私有事务能力隔离 race 1.524s、2 文件/10 项认证恢复与重置界面消费者通过。Go lint 0 issues、gofmt、完整基线 diff 和 tracked 冲突标记扫描通过。实现树合并后完整 API/database 108.123s/8.018s、前端 103 文件/954 项及 lint/build 的真实证据保留在逐票记录。主树日志为 .tmp/merge-wave20-*.log。

31、28 已依次合入并联合关闭：主树资源及内容完整 race、配置/页读取/整书下载 HTTP race 通过；两票合并后 scanner/application/scanning/runtime/task/runtimecfg 完整 race 80.762s/2.134s/26.288s/13.738s/1.912s/1.268s、坏归档事件与嵌套库监听及资源 HTTP 组合 race 36.565s 实际通过。Engine 停止准入与取消通知、实际 body 退出、watcher join 和最终资源退休分别验证；31 用真实 Stopping 边界替代旧 20ms 假定，28 保留坏归档失败及明细覆盖。最终 lint 0 issues、gofmt、完整基线差异与 tracked 冲突标记扫描通过；本波无前端生产或生成契约变更。原始证据为主树 .tmp/merge-wave21-*.log。


## 44 resumed worktree evidence (2026-11-02; not integrated)

`codex/refinement-44` at base `da6811b` retains uncommitted organization/metadata HTTP migration. Production routes consume a standalone narrow capability family under the existing auth gate; no second runtime/Engine/Manager/SSE/cache. Full ordinary Go tests, focused family/owner races, Go lint/build/format, TS generation drift and 64 frontend consumers plus web lint/build passed. Full API race exceeded a 6m deadline; whole-repo doc-style script hit MSYS fork/resource limits and a tool deadline. Exact commands, logs and 47 compatibility exit obligations are appended to ticket 44. Ticket remains open; this is worktree evidence, not an integration/closure claim.
