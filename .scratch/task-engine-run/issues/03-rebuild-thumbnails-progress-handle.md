# 03 — 缩略图重建任务转换（证伪点）

**What to build:** 缩略图重建任务改走新的启动入口。

它的**进度由任务体之外写入**——写入者是扫描器的 goroutine，不在任务体的调用栈上。因此进度句柄由任务体交给缩略图重建聚合器，扫描事件转译处改为从聚合器取句柄再写进度，而不是靠拼一个固定的**任务键**字符串。

（票面原写「17 个任务里唯一一个」，票 02 的实现记录已更正：资料库扫描与系列扫描同样由任务体之外写入，只是走 `handleScannerProgressEvent` 按 scope+id 拼键那条路，属票 05。本票要证伪的是「聚合器持有句柄」这一形状。）

顺带的收益：那四处外部写入点今天各自都要先取聚合器快照、判断「重建是否在进行中」——「聚合器里有没有进度句柄」本身就是这个判定，四处重复的活性检查随之消失。

这张票**故意排在迁移批次的最前面，并且门控其余全部迁移批次**（05–10）：如果进度句柄的所有权模型不成立，要在这里暴露，而不是在转换完十几个启动点之后再推翻。这条门控是刻意牺牲并行度换来的——不要为了赶工把 05–10 提前开工。

用户可见行为不变：重建期间的**阶段**名、逐条目进度、可取消性都与今天一致。

**Blocked by:** 02

**Status:** ready-for-agent

- [x] 缩略图重建任务改用新入口，其各不相同的失败文案码通过 `TaskResult` 的文案覆盖机制表达（实为五个：`clear_cache_failed` / `mkdir_failed` / `clear_cover_index_failed` / `wait_queue_failed` 四个走覆盖，`failed` 是任务声明里的默认码不必覆盖；票面写的六个不准）
- [x] 进度句柄由任务体交给聚合器；扫描事件转译处不再出现该任务的键字符串
- [x] 四处外部进度写入点不再各自判断「重建是否在进行中」，改为以「有无进度句柄」为准
- [x] 用例覆盖句柄交接：启动一个任务、把句柄交给聚合器、驱动聚合器、断言投递出去的载荷——**不新增第二个 seam**，仍走启动入口这一个
- [x] 重建期间的**阶段**跃迁不被节流水位吞掉
- [x] 该任务的既有控制器级用例保持通过
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [x] **结论已记录**：进度句柄的所有权模型成立（或不成立、需要何种调整）。这条结论是放行 05–10 的前提

## Comments

### 结论：所有权模型成立，但需要一处接口调整（已随本票落地）

**成立**，放行 05–10。三条证据：

1. **交接是强制的，不是约定。** `initRebuildThumbAggregator` 现在要一个 `*TaskProgress`，而拿到它的唯一途径是任务体的第二个参数——漏交接是编译错误，不是运行期静默失效。
2. **四处活性判定归零。** `rebuildThumbSnapshot.Progress` 同时是写入资格与「重建是否在进行中」的判定，聚合器内部的 `active` 标志位随之删除。
3. **任务键字符串从扫描事件转译处彻底消失**，包括 `handleScannerMetricsEvent` 里那两处不属于「四处写入点」的合并调用。

**需要的调整**：`TaskProgress` 补一个原子上报入口 `Report(TaskFrame)`。

理由是实测出来的，不是推演。外部写入者把扫描器的一份报文翻成**一帧**进度，一帧里同时有指标、两阶段计数、当前条目、标签与阶段文案。只用 `Advance` / `Phase` / `Item` / `Metrics` / `Labels` 分五次报的话，投递水位会放行其中一条中间态、又把后面补齐的那条吞掉——水位只比对 status / phase / messageCode / message，而连续两份报文的 phase 与 code 往往一模一样。四份报文实测投递序列：

```
current=1 item=vol01 metrics.processed=1     ← 事件 1，完整
current=1 item=vol01 metrics.processed=2     ← 事件 2，指标是新的、计数与条目是旧的
current=2 item=vol02 metrics.processed=3     ← 同上
current=3 item=vol03 metrics.processed=4     ← 同上；最后一条永不补齐
```

即同一份载荷里指标已经走到第 N 条、进度条还停在第 N-1 条。补上 `Report` 之后每份报文恰好一条载荷、帧内自洽，与转换前一致。

这个调整**不复活九参数方法**：`TaskFrame` 用命名字段，计数用 `*int`，因此「别动总数」仍由不设值表达而非 `-1` 哨兵；`Advance` / `Phase` / `Item` / `Metrics` / `Labels` 全部退化为一字段的 `Report`，仍是日常主用法。

**给 05–10 的口径**（按**帧的形状**判，不按谁在调用）：一次上报只动一个维度就用对应的方法，那是主用法；一次上报天然是一整帧——多个维度同时变、拆开报就会被水位撕断——才用 `Report`。缩略图重建的任务体开工那一帧同时给出**阶段**与当前条目，因此它也用 `Report`。

### 实现记录

- `internal/api/rebuild_thumb_aggregator.go`：`active bool` → `progress *TaskProgress`；`begin` 收句柄；`rebuildThumbSnapshot.Active` → `Progress`。
- `internal/api/controller_scan_events.go`：四处写入点改判 `snap.Progress == nil`，并共用新的 `writeRebuildThumbProgress`——「一份快照怎么翻成一帧进度」因此只有一份实现。`handleScannerMetricsEvent` 里的重建那半段并入 `fixateRebuildThumbBaseline`。
- `internal/api/controller_maintenance.go`：`launchRebuildThumbnailsTask` 从「四次调用 + 手写九分支闭包」收成一份任务声明加一个任务体，返回值改为错误。
- `internal/api/task_run.go`：新增 `TaskFrame` / `Report` / `AddMetrics`；`taskProgressUpdate` 由 `TaskFrame` 取代。
- `internal/api/rebuild_thumb_progress_test.go`（新增）：六条用例。

### 对票面的三处偏离

1. **失败文案码是五个，不是六个**，而且只有四个需要覆盖：`clear_cache_failed`、`mkdir_failed`、`clear_cover_index_failed`、`wait_queue_failed` 经 `TaskResult.Code` 覆盖，第五个 `failed` 是任务声明里的 `FailCode` 默认值——扫描本身失败时任务体返回零值 `TaskResult` 即可，这正是「常规任务不必为收尾写任何代码」那条决定的样子。
2. **新增 `TaskProgress.AddMetrics`**。`handleScannerMetricsEvent` 里那两处字面任务键（`mergeTaskParams` + `mergeRunningTaskMetricSums`）合并成一次经句柄的累加。`mergeRunningTaskMetricSums` 全仓库只有这一个调用方，所以这是搬家而不是加接口。它与 `Metrics` 是「加」与「设」之别——跨库任务的每份报文只覆盖一个库，全局总量只能累加得出——两处 doc 互相点名。存储 IO 面板按参数名读这些累计值（`taskArchiveOpenRate`），因此这条通道不能省。
3. **票 02 给本票的更正已核实**：资料库扫描与系列扫描确实也由任务体之外写入，但它们走的是 `handleScannerProgressEvent` 按 scope+id 拼键那条路，属票 05 范围，本票未动。本票证实的是「聚合器持有句柄」这一形状成立；05 转换时那两个任务的句柄同样只能来自任务体的第二个参数，没有第二条路。

### 顺带改变的行为

1. **取消发生在「清空封面索引」这一步时，此前落失败态并显示「清空封面索引失败」，现在落已取消并显示取消文案。** 引擎按任务体返回的错误裁决分支，而 `TaskResult` 的文案覆盖对三条分支一视同仁，因此专属失败文案必须挡在取消之外——`rebuildThumbFailure` 把这条规则收成一处。新口径与同一任务体里 `WaitForCoverQueue` 那步既有的判定一致。
2. **系列作用域的扫描报文不再污染重建任务的指标。** 那两次合并此前写在 `report.Scope != "library"` 判定之前，重建期间并发的系列扫描会把自己的指标并进去。
3. **重建任务进入终态之后，后续扫描的指标报文不再往它的参数里写。** `mergeTaskParams` 不看任务状态，此前会持续改写一条已完成任务的参数。
4. **`trackLibrary` 不再「在 begin 之前被调用就自行进入激活态」**——没有句柄可以自行发明。该分支在生产不可达：`runGlobalScan` 的进度回调只在重建任务体内注册，而任务体第一件事就是交接句柄。
5. **潜在（今天不可达）**：`taskcontrol.Wait` 返回的非取消错误此前会被略过、任务继续往下跑，现在一律落失败态并用默认失败码。`Wait` 今天只返回 `nil` 或 `ctx.Err()`，因此这条改变现在观察不到；与票 08 记的那条等价搬运是同一形状，若将来给任务上下文加了超时，两处都要重新评估。

### 仍然存在的一处撕帧（非本票引入）

`fixateRebuildThumbBaseline` 会先 `AddMetrics`（累加通道，自带一次投递）再写一帧，因此逐库报文那条路仍是两次投递，中间那条带的是上一帧的计数与阶段。它按库触发（低频），且随后的**阶段**跃迁或终态必定补齐；转换前那条路是三次投递，所以只减不增。要收成一次，得让 `TaskFrame` 同时承载「指标增量」与「任务参数」两条语义不同的通道，不在本票范围。

### 用例

`internal/api/rebuild_thumb_progress_test.go` 六条，全部在 `newTaskEngine` 这一个 seam 上，无数据库 / 配置 / 扫描器。`newRebuildThumbTestController` 只是把引擎与聚合器装进一个 Controller 壳（聚合器的 doc 已载明白盒测试会这么拼），不引入第二个注入点；驱动用的是生产的扫描器回调入口本身。

- `TestRebuildThumbProgressFlowsThroughHandedOverHandle` — 票面要求的那条交接覆盖。
- `TestRebuildThumbMetricsReportAccumulatesThroughHandle` — 跨库累计经句柄落地。
- `TestRebuildThumbWritersAreInertWithoutHandle` — 任务在跑、任务键人尽皆知，但句柄没交出去，四处写入点全部写不进去。这条是所有权模型的反面。
- `TestRebuildThumbFramesArePublishedWholeAndOnce` — 一份报文一条载荷、帧内自洽。它钉住的正是上面「需要的调整」那一节里的实测缺陷。
- `TestRebuildThumbPhaseTransitionsSurviveThrottle` — 阶段跃迁不被水位吞掉。
- `TestRebuildThumbCountsHoldStillBeforeAnyDenominator` — 两阶段都还没有分母时不乱报总数（原 `-1` 哨兵那条路）。

既有的 `TestScannerMetricsAggregateIntoRebuildThumbnailsTask` 只改了 arrange（补上句柄交接），断言逻辑一字未动——它因此顺带成了「Controller 侧确实从聚合器取句柄」的守卫。

### 验证

- `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿。
- `go test -race -count=1 ./internal/api/` 全绿。
- `go vet ./...`、`golangci-lint run ./internal/api/...`（0 issues）、`gofmt -l` 干净。
- `scripts/check-doc-style.sh` 对本票新增/重写的文件头无预算超支。
- 未改动 `TaskStatus` / `TaskLimits`，前端契约未变。
