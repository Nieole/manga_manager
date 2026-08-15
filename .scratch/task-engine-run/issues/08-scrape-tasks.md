# 08 — 刮削两个

**What to build:** 全库刮削与单库刮削两个任务改走新的启动入口。

这批的特别之处：两者的**终态**文案码按**作用域**分岔（全库一套、单库一套），因此它们是 `TaskResult` 文案覆盖机制的第二个验证场景——与 07 的「同任务不同失败原因」互补，这里是「同形任务不同作用域」。

刮削任务体内今天有三处**暂停闸门**检查，每处后面跟着一段相同的取消收尾。转换后这些收尾统一交给引擎，任务体只需把取消错误返回上去。

刮削的**重启函数**走的是一条自有的重试路径（不是简单转发启动方法），转换后要确认它仍然工作。

用户可见行为不变。

**Blocked by:** 03（03 先证实进度句柄的所有权模型成立，本票才放行；02 由 03 传递依赖）

**Status:** ready-for-agent

- [x] 两个任务改用新入口
- [x] 按**作用域**分岔的终态文案码通过 `TaskResult` 的文案覆盖表达（见「对票面的偏离」1：分岔落在**任务声明**里，`TaskResult` 只留完成文案的占位参数）
- [x] 任务体内三处重复的取消收尾消失，改为把取消错误返回给引擎
- [x] 收尾判定从「只认取消错误、其余落空」改为「非取消错误一律视为失败」——**这是行为等价的搬运，不是修正**：任务上下文由 `WithCancel(Background())` 建立、无 deadline，而**暂停闸门**只返回 `nil` 或 `ctx.Err()`，因此该上下文的错误只可能是取消。若将来给任务上下文加了超时，这条等价即失效，届时需重新评估
- [x] 刮削的重试路径仍然工作（**重启函数**注册表本身不改动，见 ADR-0001）
- [x] 两者的既有控制器级用例保持通过
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿

## Comments

**实现记录**

两个启动点各自收成「一份任务声明 + 一个任务体」：

- `internal/api/scrape_controller.go`：`launchBatchScrapeAllSeriesTask`、`launchLibraryScrapeTask`。
  两者本就返回错误，签名不变；单库那个取**作用域**显示名的 `GetLibrary` 挪到了闸门之前
  （任务声明必须一次性备齐），代价是 409 那条路上多一次读。
- `runScrapeTask` 的形参从 9 个收到 5 个：**任务键**、两条终态文案码、`providerName` 与
  `providerKey` 全部消失。任务键的消失是本票最实的一处——spec 用户故事 6 点名「今天某个任务体里
  出现 8 次」的正是这个任务体，它现在一次都不出现，进度写入资格改由句柄授权。
- 三处**暂停闸门**加速率限制里的 `ctx.Done()`，四条出口一律把错误返回上去，由引擎裁决终态。
  票面写的「三处」只数了闸门。
- 两个 HTTP 端点改用 `writeTaskLaunchError`（票 05 定的收敛），不再用
  `strings.Contains(err.Error(), "task already running")` 判定。
- **重启函数**注册表未动（ADR-0001）：`scrape` 那条转发的 `retryScrapeTask` 是一条自有的重试路径，
  它从终态任务的**任务参数**里读回刮削源，本票为它补了守卫用例。
- 没有方法因此成为孤儿：`startPausableCancelableTaskMsg` / `updateTaskDetailsMsg` /
  `completeTaskMsg` / `finishTaskMsg` / `setTaskMetadata` / `newTaskContext` / `runBackgroundTask`
  在外部库（票 09）与 ComicInfo 回写（票 10）都还有调用点，本票一个都不删。

**新增的一个任务声明字段：`TaskSpec.Labels`**

这是「用户可见行为不变」的必需品，不是顺手加的。刮削是第一个在**启动时**就带展示标签的任务：
它此前把 `label.provider` / `label.provider_name` 塞进 `setTaskMetadata` 的任务参数里，靠
`hydrateTaskStatusDerivedFields` 解码成 `TaskStatus.Labels`。而 `claimTaskSlot` 不做这层解码
（解码只发生在读盘那条路），照搬进 `Metadata` 的话活动期 `Labels` 全空——`TaskCenter` 读的是
`labels.provider_name || labels.provider || params.provider`，用户会在整个刮削期间看到刮削源的
**键**而不是显示名。

补上字段之后：`Metadata` 只留 `provider`（重启函数读的那个），`Labels` 走自己的通道，
落盘仍由 `taskParamsWithDerivedFields` 编码回 `label.*`，线上契约与落盘结构均未变
（关键决定 12）。逐条目那两个标签仍由任务体补，两条路都是按键合并。
**票 11 的终值不受影响**：那条数的是进度句柄的方法数，`TaskSpec` 的字段不在其内。

**对票面的四处偏离**

1. **按作用域分岔的终态文案码落在任务声明里，不走 `TaskResult` 覆盖。** 关键决定 10 把两者分工
   写死了：「终态文案的默认值写在任务声明里、而变体（如「部分成功」…）由任务体在返回时指定」。
   全库与单库是**两份任务声明、两个任务键**，它们的差异是默认值之别，不是同一个任务内的变体；
   走覆盖的话，两条常规完成路径都要为收尾写代码，正撞上同一条决定里「常规任务不必为收尾写
   任何代码」。`TaskResult` 因此只承载完成文案的两个占位参数。
   **代价要说清**：票面「`TaskResult` 文案覆盖机制的第二个验证场景」这条立票理由随之落空——
   本票没有验证覆盖机制，覆盖机制的验证仍只有票 07 的两阶段失败码那一处。
2. **新增 2 个文案键** `task.msg.scrape.failed_all` / `failed_library`（zh-CN 与 en-US 两侧）。
   刮削此前没有失败分支，因而也没有失败文案码。今天仍走不到（四条出口的非取消错误在当前上下文
   形状下不可达），留着的理由同票 06 偏离 1 与票 07 偏离 2——`FailCode` 留空会让将来任何一条
   未覆盖的失败路径把**起始**文案原样渲染成失败态的文案。
3. **新增 5 条用例**（`internal/api/scrape_tasks_test.go`）。票面说「既有控制器级用例保持通过」，
   指的是控制器层；这 5 条全在 `newTaskEngine` 这一个 seam 上，与票 03 / 05 / 06 / 07 的同层用例
   一致，无数据库 / 无配置。既有的 `TestLibraryScrapePauseResumeStopsNewProviderRequests`
   与两条 409 用例一字未改，仍绿。
4. **票面「任务体内三处重复的取消收尾」少数了一处**：速率限制的 `select` 里还有第四条同形出口。
   四条现在都只是 `return TaskResult{}, err`。

**顺带改变的行为**

1. **两个任务的首帧现在带齐任务参数、作用域显示名与刮削源标签。** 此前是启动之后的第二次写入，
   中间有一个「任务已在列表里、却还不知道用的哪个源」的窗口，任务列表接口能观察到。
   与票 02 给资料库扫描、票 07 给三个 KOReader 任务修掉的是同一处形状。
2. **`PublishEvent("refresh")` 挪到了收尾之前**（此前在 `finishTaskMsg` 之后）。与票 05 偏离 4
   同形：任务体干完活就返回，终态由引擎裁决。该事件不带任务载荷，前端收到就重取数据，观察不到差别。
3. **完成那条日志不再带 `task_key`**——任务体已经不知道任务键了。两个入口各自的逐条目日志
   （`Scraping series metadata` / `Scraping library series metadata`）原样保留，仍能区分是哪一条路。
4. **两个端点不再把任何错误都翻成 409。** `ListLibraries` / `ListSeriesByLibraryLite` 失败此前会被
   报成「已有刮削在跑」，用户去等一个不存在的任务；现在是 500。这是票 02 评审在 `scanLibrary` 上
   指出的那处缺陷的孪生，与票 05 / 06 / 07 的处置一致。

**不给票 11 的 CHANGELOG 添条目**：上面四条没有一条是用户能指着说出来的变化——首帧空窗以毫秒计，
refresh 事件无载荷，日志不面向用户，而 409→500 那条只在数据库读失败时可达。

**用例**

`internal/api/scrape_tasks_test.go` 五条，全部在 `newTaskEngine` 这一个 seam 上。刮削源替身一律
返回「没找到」或「源报错」——两条路都在写入审阅队列之前折返，因此用例既够不到数据库，也不必等
那 500ms 的速率限制（它只在成功入队之后才走到）。前三条与最后一条按作用域表驱动跑两遍。

- `TestScrapeCompletionCodeSplitsByScope` — **完成**码按作用域分岔，成功计数走占位参数。
- `TestScrapeCancellationCodeSplitsByScope` — 取消码同上，且取消由引擎裁决、任务体不写终态。
- `TestScrapeTaskDeclarationLandsWhole` — 首帧就带齐作用域显示名、刮削源参数与标签。
  把 `TaskSpec.Labels` 去掉即变红，那正是上面那节说的用户可见回归。
- `TestScrapeFrameIsPublishedWhole` — 一次上报一条载荷、帧内自洽。**已实测**：改回
  `Advance` / `Phase` / `Item` / `Metrics` / `Labels` 五次分报即变红，载荷里的当前条目停在上一个
  系列（`Alpha`）、占位参数已经是下一个（`Beta`）。
- `TestScrapeRetryReadsProviderFromTaskParams` — 重试从终态任务的参数里读回刮削源。
  把 `Metadata` 里的 `provider` 换成显示名即变红（`getProvider` 会静默回落到默认源）。

**评审修正**

`/code-review` 两轴各自跑完后改了五处，都不涉及行为：`runScrapeTask` 那个已无读取点的
`providerKey` 形参删除（标签搬进任务声明后它就没人读了）；`scrapeFrame` 收成
`scrapeMetrics.frame` 方法（它只读 `m.total` 与 `m.toMap()`）；符号 doc 里会腐坏的出现次数
（「三处」）与复述自 `newTaskContext` 的上下文构造细节各自改掉；新用例文件头的「资源库」
按 CONTEXT.md 改回**资料库**（`zh-CN.ts` 里那 105 处面向用户的「资源库」是仓库级既存偏离，
本票新增的文案键与紧邻的同族键保持一致，不单独改）。

**留给票 11 的一条**

`claimTaskSlot` 直接存下 `spec.Labels` 与 `spec.Metadata` 的 map，而 `applyTaskProgress` /
`mergeTaskParams` 就地写它们——任务帧因此会写穿调用方那份 map。今天无害（每个启动点都现造一份），
但这是引擎层面的别名隐患，`Metadata` 那半边在本票之前就是这个形状。票 11 收缩时一并克隆即可。

**验证**

- `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿。
- `go test -race -count=1 ./internal/api/` 全绿。
- `go vet ./internal/...`、`golangci-lint run ./internal/api/...`（0 issues）、`gofmt -l` 干净。
- `scripts/check-doc-style.sh` 通过。
- `cd web && npx tsc -b`、`npm run test`（31 文件 / 266 条）、`npm run build` 全过。
- 本票涉及的 12 个 `task.msg.scrape.*` 文案码逐条比对过 `zh-CN.ts` 与 `en-US.ts`，两侧各一份、无缺失。
- `go run ./cmd/tsgen` 的产物差异只有下面那条既有的头注释漂移，类型本身无变化
  （本票未动 `TaskStatus` / `TaskLimits`，`TaskSpec` 不是 tsgen 目标）。

**留给后续票的一条现状（承自票 05、07，本票仍未动）**

`web/src/api/generated.ts` 与 `go run ./cmd/tsgen` 的产物有一处**头注释**漂移，来自提交 39fcbd3
（改了 `cmd/tsgen` 的模板但没重新生成）。类型本身无差异，CI 的漂移校验因此是红的。
本票已核实它与刮削改动无关，仍另行处理。
