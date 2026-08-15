# 07 — KOReader 三个

**What to build:** 书籍**指纹**重建、KOReader 进度对账、匹配刷新三个任务改走新的启动入口。

这三个今天是结构上最接近的一组——三段各约 30 行的代码除了字符串键与所调用的同步方法之外完全一致，是手抄仪式最直白的证据。

其中匹配刷新是本批的重点：它是**两阶段**任务（先重建指纹、再对账进度），两个阶段各有自己的失败文案码，且两阶段共享一条取消文案码。它验证 `TaskResult` 的文案覆盖机制在「同一个任务、不同失败原因」下成立。

用户可见行为不变。

**Blocked by:** 03（03 先证实进度句柄的所有权模型成立，本票才放行；02 由 03 传递依赖）

**Status:** ready-for-agent

- [x] 三个任务改用新入口，三段近乎一致的样板消失
- [x] 匹配刷新的两个阶段各自的失败文案码通过 `TaskResult` 的文案覆盖表达
- [x] 匹配刷新在任一阶段被取消时都进入已取消**终态**并使用同一条取消文案码
- [x] 三者启动时写入的匹配配置元数据与转换前一致
- [x] 三者的**阶段**播报（指纹计算 / 进度对账）与**计数推进**分别走对应方法（见「对票面的偏离」1：四处上报里只有一处对得上，其余按票 03 的口径整帧报出）
- [x] 三者的既有控制器级用例保持通过
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿

## Comments

**实现记录**

三个启动点各自收成「一份任务声明 + 一个任务体」：

- `internal/api/koreader_controller.go`：`launchRebuildBookHashesTask`、
  `launchReconcileKOReaderProgressTask`、`launchRefreshKOReaderMatchingTask`。三段各约 30 行、
  除字符串键与所调用的同步方法外完全一致的样板随之消失。三处共同的匹配配置元数据抽成
  `koreaderMatchMetadata`，两个进度帧抽成 `koreaderFingerprintFrame` / `koreaderReconcileFrame`
  ——它们各有两个上报方（单干那个任务，以及匹配刷新的对应阶段）。
- 三个 HTTP 端点改判哨兵错误，口径与 `retryTask` 一致（见偏离 4）。
- **重启函数**注册表未动（ADR-0001）：三条转发的正是本票转换的启动方法，启动入口的错误原样透传。
- 删除 `taskEngine.setTaskEffectiveLimit`（见偏离 6）。

**匹配刷新的进度条数的是阶段，不是条目**

这是本票唯一需要设计的地方。它的总数在任务声明里就是 2，进度条走的是 0/2 → 1/2 → 2/2；
两个阶段内部复用的是上面那两个进度帧，而那两个帧带着自己的逐条目计数——原样报进去，
用户会看到「40 / 共 2」。`holdingStepCount` 把一帧的**计数推进**摘掉、其余照报，收在一处。

方向刻意是「默认整帧、显式摘掉」而不是反过来：帧默认完整时，单干的两个任务是一行；
忘了摘的后果（进度条乱跳）看得见，而忘了补的后果（进度条一动不动）看着像任务卡住。

**对票面的偏离**

1. **「阶段播报与计数推进分别走对应方法」四处上报里只对上一处。** 对上的是匹配刷新第一阶段的
   开工帧——它只换阶段与文案，因此是一次纯 `Phase`，不必编造凑数的计数值。其余三处：两个单干
   任务的逐条目帧里计数、阶段、指标与占位参数同时变，拆开报会被投递水位撕断（票 03 的口径）；
   匹配刷新的第二阶段跃迁同时动阶段计数、阶段名与文案，同理。这与票 06 偏离 3 是同一形状。
   用例 `TestRebuildBookHashesFrameIsPublishedWhole` 钉的正是这个——把它改回分次上报即变红，
   已实测：载荷里指标停在第 1 本、进度条已经到第 2 本。
2. **新增 1 个文案键** `task.msg.refresh_koreader_matching.failed`（zh-CN 与 en-US 两侧）。
   它今天走不到：两步的失败各被 `TaskResult` 覆盖，取消走取消码。留着的理由同票 06 偏离 1——
   `FailCode` 留空会让将来任何一条未覆盖的失败路径把**起始**文案原样渲染成失败态的文案。
   已写进 `launchRefreshKOReaderMatchingTask` 的 doc。
3. **新增 7 条用例**（`internal/api/koreader_tasks_test.go`）。票面说「既有控制器级用例保持通过」，
   这三个任务在那一层本来一条用例都没有。7 条全在 `newTaskEngine` 这一个 seam 上，与票 03 的
   `rebuild_thumb_progress_test.go`、票 05 的 `scan_progress_handle_test.go`、票 06 的
   `maintenance_tasks_test.go` 同层。
4. **三个 HTTP 端点改用 `writeTaskLaunchError`。** 此前三处都把**任何**错误翻成 409 并把错误串
   原样回给前端，正是票 02 评审在 `scanLibrary` 上指出的那处缺陷的孪生（今天不可达，启动入口
   只返回哨兵错误）。前端三处调用点都用自己的本地化文案、不读这个串，因此响应体里那句英文的
   变化观察不到。
5. **`koreader` 服务的两个进度回调去掉那个恒为空串的 `message` 参数**
   （`RebuildBookIdentities`、`ReconcileProgress`）。展示文案已经全部由文案码承担，与票 06
   对 `runRebuildFileIdentities` / `runBackfillFullHashesLowPriority` 的处置相同。
6. **删除 `taskEngine.setTaskEffectiveLimit`。** 本票转换掉它最后两个调用点，`golangci-lint`
   的 `unused` 是 CI 门禁，不删则红——与票 04 删四个孤儿方法、票 06 删 `startTaskMsg` 同一处理。
   它是票 11 那条「建上下文、写元数据、写并发上限三个方法降为引擎内部」的三分之一，
   **11 对账时只剩 `setTaskMetadata` 与 `newTaskContext` 两个**。

**顺带改变的行为**

1. **三个任务的首帧现在带齐匹配配置元数据与并发上限。** 此前元数据是启动之后的第二次写入
   （中间有一个「任务已在列表里、却还没有索引类型标签」的窗口，任务列表接口能观察到），
   而并发上限那次写入被自己刚写下的节流水位吞掉。与票 02 给资料库扫描修掉的是同一处形状。
2. **进入终态之后，迟到的进度写不进去了。** `applyTaskProgress` 一律忽略非活动态任务，
   而旧的 `updateTaskDetailsMsg` 也已有同样的判定，因此这三个任务上观察不到差别。

**不给票 11 的 CHANGELOG 添条目**：上面两条都不是用户能指着说出来的变化，
偏离 4 的响应体变化被前端自己的文案挡住。本票没有用户可见改动。

**用例**

`internal/api/koreader_tasks_test.go` 七条，全部在 `newTaskEngine` 这一个 seam 上，无数据库
（存储替身只实现被调到的那几个方法）。匹配模式取**路径**而非二进制哈希：那条路只拼字符串、
不读书文件，用例因此不必造临时文件，而任务引擎这一侧走的是同一条路径。

- `TestRefreshMatchingNamesTheFailedStage` — 票面要求的两阶段专属失败码（表驱动，两步各一）。
- `TestRefreshMatchingCancellationSharesOneCode` — 两步取消都落同一条取消码。
- `TestRefreshMatchingWalksBothPhases` — 进度条数的是阶段：0/2 → 1/2 → 2/2，两步内部的逐条目
   回调一次都不动它。去掉 `holdingStepCount` 即变红。
- `TestKOReaderTasksCarryMatchConfigMetadata` — 三个任务的首帧都带齐匹配配置，
   并发上限的有无与转换前一致（表驱动，三个任务各一）。
- `TestRebuildBookHashesFrameIsPublishedWhole` — 一次进度回调一条载荷、帧内自洽（见偏离 1）。
- `TestReconcileProgressCompletesWithCounts` — 一条都没对上不是失败，那是这个任务最常见的结果。
- `TestRebuildBookHashesCancellationLandsCancelled` — 单阶段任务的取消由引擎裁决。

**验证**

- `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿。
- `go test -race -count=1 ./internal/api/ ./internal/koreader/` 全绿。
- `go vet`、`golangci-lint run ./internal/api/... ./internal/koreader/...`（0 issues）、`gofmt -l` 干净。
- `scripts/check-doc-style.sh` 通过。
- `cd web && npx tsc -b`、`npm run test`（31 文件 / 266 条）、`npm run build` 全过；
  未改动 `TaskStatus` / `TaskLimits`，前端契约未变。
- 本文件引用的 18 个文案码逐条比对过 `zh-CN.ts` 与 `en-US.ts`，两侧各一份、无缺失。

**留给后续票的一条现状（承自票 05，本票仍未动）**

`web/src/api/generated.ts` 与 `go run ./cmd/tsgen` 的产物有一处**头注释**漂移，来自提交 39fcbd3
（改了 `cmd/tsgen` 的模板但没重新生成）。类型本身无差异。CI 的漂移校验因此是红的，
但修它与本票无关，仍另行处理。
