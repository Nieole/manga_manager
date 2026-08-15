# 06 — 维护子域四个

**What to build:** 索引重建、缩略图清理、文件身份重建、低优先级哈希回填四个任务改走新的启动入口。

这批是最规整的一组——多数是系统**作用域**、无参数、终态文案码遵守常规约定。其中低优先级哈希回填是唯一一个由**另一个任务的任务体**发起的（资料库扫描成功后串联启动），转换后要确认这条串联仍然成立。

文件身份重建与哈希回填都会在循环里取**存储令牌**并检查**暂停闸门**——这套仪式本次不动，原样保留在任务体内。

用户可见行为不变。

**Blocked by:** 03（03 先证实进度句柄的所有权模型成立，本票才放行；02 由 03 传递依赖）

**Status:** ready-for-agent

- [x] 四个任务改用新入口
- [x] 低优先级哈希回填仍可由资料库扫描任务体在成功后串联发起
- [x] 四者的**存储令牌**取用与**暂停闸门**检查逻辑原样保留（本次不重构）
- [x] 缩略图清理与文件身份重建的**计数推进**与**阶段**播报分别走对应方法，不再传凑数的计数值
- [x] 四者的既有控制器级用例保持通过
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿

## Comments

**实现记录**

四个启动点各自收成「一份任务声明 + 一个任务体」：

- `internal/api/controller_maintenance.go`：`launchRebuildIndexTask`、`launchCleanupThumbnailsTask`、
  `launchRebuildFileIdentitiesTask`、`launchLowPriorityBookHashBackfillTask`。四个的返回值统一为错误
  （spec 的关键决定 9），回填那个的 `nil` 语义见下「回填的返回值」。
- 三个 HTTP 端点改判哨兵错误，口径与 `retryTask` 一致。`rebuildIndex` 此前用
  `strings.Contains(err.Error(), "already running")` 判定（票 05 点名留给本票），
  `cleanupThumbnails` 与 `rebuildFileIdentities` 此前把**任何**错误都翻成 409。
- **重启函数**注册表未动：`rebuild_index` 与 `rebuild_file_identities` 两条转发的正是本票转换的启动方法，
  启动入口的错误原样透传。`rebuild_book_hashes` 那条转发的是 KOReader 的 `launchRebuildBookHashesTask`
  （票 07），虽然低优先级回填也落成这个类型——重试一条回填任务因此会起一个 KOReader 重建，属既有现状。
- 删除 `taskEngine.startTaskMsg`。索引重建是它最后一个调用点，`golangci-lint` 的 `unused` 是 CI 门禁，
  不删则红——与票 04 删掉四个孤儿方法是同一处理。仍有调用点的
  `startPausableCancelableTaskMsg` / `updateTaskDetails(Msg)` / `completeTaskMsg` / `failTaskErrMsg` /
  `finishTaskMsg` / `setTaskMetadata` / `setTaskEffectiveLimit` / `newTaskContext` 原样保留。
- `mergeTaskParams` 在生产侧的最后两个调用点（票 05 点名交给本票）随之迁走，现在只剩
  `TaskProgress.MergeParams` 与一条快照竞态用例。

**新增的一处共用实现：`reportHashProgress`**

文件身份重建与低优先级哈希回填的进度回调此前逐行同形，只差文案码与任务键。合并成一份之后：

- 计数、**阶段**、指标与标签一帧报出（`Report`），不再拆成四次。按票 03 定下的口径判——
  这份报文里计数与指标同时变，拆开报会被投递水位撕断：`Advance` 那条放行、`Metrics` 那条被吞，
  于是同一份载荷里指标停在第 N-1 本、进度条已经到第 N 本。用例
  `TestHashProgressFrameIsPublishedWhole` 钉的正是这个（把它改回四次分报即变红，已实测）。
- IO 参数仍单独一次 `MergeParams`：那条通道去的是 `TaskStatus.Params`，存储 IO 面板按参数名读它。

`runRebuildFileIdentities` 与 `runBackfillFullHashesLowPriority` 的进度回调随之去掉那个恒为空串的
`message` 参数——展示文案已经全部由文案码承担。

**回填的返回值：`nil` 是「没有该做而没做的事」**

`launchLowPriorityBookHashBackfillTask` 是 spec 关键决定 9 里那 5 个布尔启动点的最后一个，本票把它
统一为错误。它的形状与另外几个不同，故把语义写死在符号 doc 上：三条前置条件（KOReader 未启用、
匹配模式不是二进制哈希、没有书缺哈希）返回 `nil`——不该发起不是错误；数缺口失败与**任务键**闸门
则各自返回错误，不再在门口被折叠成一个布尔值。

配套新增 `chainBookHashBackfill`：两个调用点（资料库扫描与系列扫描的任务体）对返回值的处置完全相同，
而这处置不是「忽略」——`errTaskAlreadyRunning` 在本路径上是常态（回填跑得久，连着扫两个库时后一次
必然撞上），不记日志；其余错误必须记，否则数不清缺口这种真故障在任务体里没有别的地方能报。
收成一处是为了这条取舍只存在一份。

**对票面的五处偏离**

1. **索引重建补了 4 个文案键**（`cancelled` / `failed` / `series_failed` / `book_failed`，zh-CN 与 en-US 两侧）。
   它此前经字面量文案接口下发硬编码英文（「SQLite series search index rebuild failed: …」），
   而新接口只收 i18n 码。两步重灌各自的失败码是为了保住今天那条用户可见的区分——技术错误串两步一模一样。
   其中 `failed` 是任务声明里的默认码，今天走不到（两条失败都被覆盖、取消走取消码）：留着是因为
   `FailCode` 留空会让将来任何一条未覆盖的失败路径把**起始**文案渲染成失败态的文案。
   **附带一条给票 10 的更正**：票 10 称 ComicInfo 回写是「唯一一个仍在使用字面量文案接口的生产任务」，
   不成立——spec 的关键决定 6 自己列的就是 5 处：外部库扫描（票 09）、缩略图清理与本票转换的索引重建，
   以及 ComicInfo 回写三处。本票已修掉属于自己的那两处。
2. **缩略图清理的逐条目文案改为 i18n 码**，并把那两句硬编码中文从 `Scanner.CleanupThumbnails` 里删掉：
   它的进度回调不再收 `msg`，只收两个计数。扫描器不该渲染用户可见文字，而新接口也没有地方接一句字面量文案。
3. **文件身份重建与哈希回填的**阶段**没有单独一次播报**，而是随每一帧一起报出（见上 `reportHashProgress`）。
   票面那条「**计数推进**与**阶段**播报分别走对应方法，不再传凑数的计数值」在这两个任务上没有对象——
   它们此前传的是真计数，凑数的计数值只出现在缩略图清理的开工帧（`(0, -1)`），那一处已按票面改成
   只播阶段。这两个任务的阶段恒为 `hashing`、从不跃迁，单独报一次只会多一条投递。
4. **新增 9 条用例**（`internal/api/maintenance_tasks_test.go`）。票面说「既有控制器级用例保持通过」，
   这四个任务在那一层本来一条用例都没有。9 条全在 `newTaskEngine` 这一个 seam 上，
   与票 03 的 `rebuild_thumb_progress_test.go`、票 05 的 `scan_progress_handle_test.go` 同层：
   存储换成只实现被调到的那几个方法的替身，配置与扫描器按需拼装，无数据库。
5. **票面「低优先级哈希回填是唯一一个由另一个任务的任务体发起的」只对了一半**：系列扫描同样串联发起它。
   两条路都不变，用例 `TestHashBackfillStartsFromInsideATaskBody` 在任务体内部驱动启动入口，
   钉的是「启动入口在任务体内可用」这一条。

**顺带改变的行为**

1. **索引重建的失败与取消文案改为按语种渲染**，英文用户看到的文字不变，中文用户不再看到英文。
2. **索引重建在关服时落**已取消**而不是失败。** 引擎按任务体返回的错误裁决分支，而 `taskFailure`
   把取消挡在专属失败码之外。该任务不可取消，因此这条路径只在停机时走到。与票 03 给缩略图重建
   修掉的是同一形状。
3. **缩略图清理的逐条目进度文案改为按语种渲染**（此前对所有语种都是中文）。
4. **缩略图清理收尾那一瞬的文案不再单独一句。** 扫描器走完之后那次回调此前带的是与逐条目不同的
   一句「清理完成，共删除 N 个冗余缩略图」，现在与逐条目共用同一个文案码。它此前也只存在于收尾前的
   那一帧里，随即被**完成**文案覆盖，因此这条改变实际观察不到；删除数在两种写法下都不进完成文案。
5. **索引重建进入终态之后**，迟到的写入不会再改到它——它本来就没有外部写入者，
   这里只是随启动入口一并获得的性质。

**验证**

- `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿。
- `go test -race -count=1 ./internal/api/` 全绿。
- `go vet ./internal/api/ ./internal/scanner/`、`golangci-lint run ./internal/api/... ./internal/scanner/...`
  （0 issues）、`gofmt -l` 干净。
- `scripts/check-doc-style.sh` 通过。
- `cd web && npx tsc -b`、`npm run build`、`npm run test`（31 文件 / 266 条）全过；
  未改动 `TaskStatus` / `TaskLimits`，前端契约未变。
- 本票新增的 5 个文案键在 `zh-CN.ts` 与 `en-US.ts` 两侧均已核对；
  本文件引用的全部文案码逐条比对过两个语种文件，无缺失。
