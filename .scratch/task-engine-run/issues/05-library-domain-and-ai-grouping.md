# 05 — 资料库子域其余两个 + AI 分组

**What to build:** 系列扫描、资料库清理、AI 分组三个任务改走新的启动入口。

三个都带**作用域**与元数据，是 02 里资料库扫描那一发曳光弹的同形推广——**任务键**含数字后缀、作用域为资料库或系列、启动时写入元数据与并发上限。

AI 分组还有一处特别之处：它的**重启函数**会从任务参数里读回语言设置，因此转换后必须确认元数据与参数仍按原样落盘，否则重试会回落到错误的语言。

用户可见行为不变。

**Blocked by:** 03（03 先证实进度句柄的所有权模型成立，本票才放行；02 由 03 传递依赖）

**Status:** ready-for-agent

- [x] 系列扫描、资料库清理、AI 分组三个任务改用新入口
- [x] 三者的**作用域**与作用域 ID 推导结果与转换前一致
- [x] 三者启动时写入的元数据、并发上限与转换前一致
- [x] AI 分组任务的语言参数仍按原样落盘，重试路径不受影响
- [x] 三者的既有控制器级用例保持通过，未新增该层用例
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿

## Comments

**实现记录**

三个启动点各自收成「一份任务声明 + 一个任务体」，返回值由 `bool` 改为 `error`：

- `internal/api/controller_library.go`：`launchSeriesScanTask`（两次重复的 `GetSeries` 合并成一次）、
  `launchCleanupLibraryTask`。
- `internal/api/controller_recommendations.go`：`launchAIGroupingTask`。
- `internal/api/controller_tasks.go`：三个**重启函数**原样透传启动入口的错误，不再把布尔值转换回哨兵错误。
- 三个 HTTP 端点改判哨兵错误，口径与 `retryTask` 一致（见下「顺带的两处收敛」）。

**本票必须连带做完的一件事：扫描事件转译处改走进度句柄**

票 02 把它列进「评审提出、本票不做的三条」，理由是需要票 03 的所有权模型；票 03 的偏离 3 又点名
「05 转换时那两个任务的句柄同样只能来自任务体的第二个参数，没有第二条路」。它确实只能落在本票：
`handleScannerProgressEvent` 是 `updateTaskDetailsMsg` 在扫描子域的最后一个调用点，而票 11 的
「九参数的详细进度方法删除」要求它零调用点——本票不做，11 就得替一个已关闭的票做设计。

落地形状与票 03 的聚合器同构，新增 `internal/api/scan_progress_handles.go`：

- `scanTarget`（作用域 + ID）是登记方与写入方共同的身份，取代双方各自拼一遍的任务键字符串。
- 任务体开工时 `track` 登记句柄、`defer` 交回；交回时只删**自己那份**，与 `newTaskContext` 同理。
- `handleScannerProgressEvent` / `handleScannerMetricsEvent` 改为 `lookup` 取句柄，取不到即无操作。
  守护扫描、watcher 触发的扫描与建库后的首扫本就不属于任何任务，此前表现为「任务表里查不到这个键」，
  现在表现为「没有句柄」，行为一致。

因此**资料库扫描**（票 02 的任务）也在本票登记句柄——不登记它就彻底没有进度了。

**新增的一个句柄方法：`TaskProgress.MergeParams`**

`handleScannerMetricsEvent` 那份报文写的是**任务参数**（`TaskStatus.Params`，存储 IO 面板按参数名读它，
见 `taskArchiveOpenRate`），不是文案占位参数，也不是指标增量——`Report` 与 `AddMetrics` 都不对口。
它是 `mergeTaskParams` 的搬家：该方法余下的两个调用点属票 06。
**票 11 的终值已同步改成 8 个方法**。

**对票面的四处偏离**

1. **新增了 7 条用例**（`internal/api/scan_progress_handle_test.go`）。票面说「未新增该层用例」，
   指的是控制器层；这 7 条全在 `newTaskEngine` 这一个 seam 上，无数据库 / 配置 / 扫描器，
   与票 03 的 `rebuild_thumb_progress_test.go` 同层。上一条说的句柄交接若没有覆盖，就只是换了个写法。
   既有的 `TestScannerMetricsUpdateTaskParams` 与 `TestScannerMetricsAggregateIntoRebuildThumbnailsTask`
   只改了 arrange（补上句柄登记），断言逻辑一字未动。
2. **AI 分组的 `taskcontrol.Wait` 收尾判定从「只认取消错误、其余落空」改为「非取消错误一律视为失败」。**
   与票 03 的顺带改变 5、票 08 票面写明的那条是同一形状：`Wait` 今天只返回 `nil` 或 `ctx.Err()`，
   因此这条改变现在观察不到；若将来给任务上下文加了超时，三处都要重新评估。
3. **`cleanup_library` 的任务体仍用 `context.Background()`**，任务声明因此没有取消文案码。
   它此前根本不建**运行时句柄**，停机取消不到它；换成任务体的 ctx 会让一次关服把这个没人取消过的
   任务写成**已取消**。这条耦合已写进 `launchCleanupLibraryTask` 的 doc：两者必须一起改。
4. **系列扫描的缓存预热与哈希回填串联挪到了收尾之前**（此前在 `finishTaskMsg` 之后）。
   与票 02 给资料库扫描定的形状一致：任务体干完活就返回，终态由引擎裁决。

**顺带的两处收敛**（评审两轴各自独立指到）

- `rebuildThumbFailure` → **`taskFailure`**，从 `controller_maintenance.go` 搬进 `task_run.go`。
  「专属失败文案必须挡在取消之外」这条规则本票新增了两个调用点（AI 分组的两处），
  票 07 的两阶段失败码与票 08 的按作用域分岔会再撞上。
- 抽出 **`writeTaskLaunchError`**。「只有哨兵错误是 409，其余 500」此前只写在 `scanLibrary` 里，
  本票要再抄三份。一并把 `rebuildThumbnails` 也接上——它把**任何**错误都翻成 409，
  正是票 02 评审在 `scanLibrary` 上指出的那处缺陷的孪生（今天不可达，启动入口只返回哨兵错误）。
  `rebuildIndex` 仍在用 `strings.Contains(err.Error(), "already running")` 判定，
  但它尚未转换（票 06），转换时一并接上即可。

**顺带修掉的一处**

资料库扫描与系列扫描进入**终态**后，迟到的扫描器报文不再改写它的进度与参数。
`mergeTaskParams` 不看任务状态，而封面队列在扫描主流程收尾后仍会上报——一条已完成的扫描
在任务面板上会继续变动。与票 03 给缩略图重建修掉的是同一处形状；已记进票 11 的 CHANGELOG 清单。

**验证**

- `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿。
- `go test -race -count=1 ./internal/api/` 全绿（本票动的是跨 goroutine 的句柄保管，额外跑一遍）。
- `go vet ./internal/api/`、`golangci-lint run ./internal/api/...`（0 issues）、`gofmt -l` 干净。
- `scripts/check-doc-style.sh` 通过。
- `cd web && npx tsc -b` 通过；未改动 `TaskStatus` / `TaskLimits`，前端契约未变。
- 本票引用的全部文案码在 `zh-CN.ts` 与 `en-US.ts` 两侧均已存在，未新增文案键。

**留给后续票的一条现状**

`web/src/api/generated.ts` 与 `go run ./cmd/tsgen` 的产物有一处头注释漂移，来自本票之前的
提交 39fcbd3（改了 `cmd/tsgen` 的模板但没重新生成）。本票未动它，以免把无关改动混进来。
