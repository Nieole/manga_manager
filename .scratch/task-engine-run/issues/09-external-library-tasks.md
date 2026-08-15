# 09 — 外部库两个，并删掉手写节流

**What to build:** **外部库**扫描与外部库传输两个任务改走新的启动入口，同时删除这两条路径上手写的 500ms 进度节流。

这是本批次里唯一有**用户可见改善**的一张票：手写节流比引擎的节流水位更粗，而且丢掉了「**阶段**或文案跃迁必须立刻推送」这条规则。删除之后，传输任务的阶段名跃迁不会再被吞掉，任务气泡不会长时间停在过期的阶段上。

这两个启动点今天返回「(任务键, 是否已启动)」两个值——任务键本来就是调用方自己拼进任务声明的，转换后直接使用自己写进去的键，不需要引擎回传。

**本票的验证口径与其余迁移票不同。** 其余票的验收标准是「行为不变」，可以靠既有用例回归；本票**故意改变对外推送节奏**，既有用例若断言了旧节奏会红，那是预期结果而非回归。因此需要一条独立的判据：以「投递出去的载荷序列」为观测对象，断言**阶段**跃迁必定产生一条投递（即使距上次投递不足节流窗口），而纯计数推进在窗口内仍被吞。人工验收时对一次真实传输做转换前后的推送对比，确认阶段名不再滞留。

**Blocked by:** 03（03 先证实进度句柄的所有权模型成立，本票才放行；02 由 03 传递依赖）

**Status:** ready-for-agent

- [x] 两个任务改用新入口，返回值统一为错误
- [x] HTTP 层仍能拿到任务键回给前端（使用调用方自己写进任务声明的键）
- [x] 两处手写的 500ms 进度节流删除，进度节流只剩引擎水位一处
- [x] 传输任务的**阶段**跃迁不再被节流吞掉（用例覆盖：阶段变化时即使在时间窗口内也必须投递。见「对票面的偏离」1：本票让逐条目文案码恒定之后，任务内唯一的文案跃迁落在收尾那一帧上，用例钉的是它）
- [x] 传输任务「部分成功」「目标已全部存在」等变体**终态**文案码通过 `TaskResult` 的文案覆盖表达
- [x] 两者的既有控制器级用例（含传输原子性用例）保持通过
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿

## Comments

**实现记录**

两个启动点各自收成「一份任务声明 + 一个任务体」，返回值由 `(任务键, bool)` 改为 `error`：

- `internal/api/external_controller.go`：`launchExternalLibraryScanTask`、
  `launchExternalLibraryTransferTask`。两个 HTTP 端点改判哨兵错误（`writeTaskLaunchError`），
  任务键改为端点自己调那两个拼键函数取得——它本来就是调用方写进任务声明的，不必由引擎回传。
- `internal/external/manager.go`：`ScanSession` 的进度回调去掉 `message` 形参，那句硬编码中文
  （「已扫描 N / M 个外部资源文件」）随之删除。与票 06 对 `Scanner.CleanupThumbnails` 的处置相同：
  外部库会话管理器不该渲染用户可见文字，而新接口也没有地方接一句字面量文案。
- `internal/api/task_engine.go`：删除本票转换掉最后一个调用点的六个方法——
  `startPausableCancelableTaskMsg`、`updateTaskMsg`、`updateTaskCore`、`updateTaskDetailsMsg`、
  `finishTaskMsg`、`failTaskErrMsg`。`golangci-lint` 的 `unused` 是 CI 门禁，不删则红，
  与票 04 / 06 / 07 的处置一致。仍有调用点的 `startCancelableTask` / `updateTaskDetails` /
  `updateTaskDetailsCore` / `completeTaskMsg` / `finishTask` / `failTaskWithError` /
  `setTaskMetadata` / `newTaskContext` / `runBackgroundTask` 原样保留，全部只剩 ComicInfo 回写
  （票 10）一个调用方。

**本票唯一需要设计的地方：逐条目帧从两条收成一条**

转换前每传一本书报两次：拷贝前一条「正在传输 {{path}}」、拷贝后一条「已传输 {{done}} / {{total}} 本资源」。
把这两条原样搬到引擎水位上会得到**比不节流还糟**的结果，因此本票把它们收成一条。

水位的判定是「时间窗口内**且**展示态一字未变才吞」，而展示态包含文案码。两个文案码逐本交替，
水位每次都判「展示态变了」而放行——等于这条路径上根本没有节流。这不是理论风险：一次传输最多
2000 个系列（`maxExternalTransferSeries`），全部命中「目标已存在」时循环只做两次系统调用就过一本，
投递会以数万条的规模冲进 SSE broker；它的背压处置是**断开客户端连接**（`sseBroker.run` 的
`default` 分支），于是所有人的任务中心当场停止更新。转换前那层 500ms 节流恰好挡住了这一幕。

收成一条之后：逐条目只在**拷贝之前**报一帧（计数报的是**已完成**数，条目名报的是正在传的那本），
文案码恒定，水位因此真正生效。代价是「已传输 k / N 本资源」这句话在逐条目阶段消失——它与进度条
本就重复（`SidebarTaskBubble` 直接渲染 `current/total`），而拷贝一本要几分钟，那几分钟里用户
需要看到的是**正在传哪一本**，不是上一本传完时的旧计数。

那条文案码没有变成孤儿：循环结束后补的收尾帧用的正是它，报「传成了几本 / 共几本」。这一帧不是
为凑用例加的——**终态只把 `Current` 拉到 `Total`，不动指标**，少了它「传输文件」这个指标会永远
停在倒数第二本上。它的计数与指标刻意答两个问题：`Current` 是「走完了几本」（失败的也走过了，
进度条因此在部分失败时也走满），`transferred_files` 是「传成了几本」。它同时是本票唯一一处任务内的文案跃迁，用例
`TestExternalTransferClosingFrameSurvivesThrottle` 钉的就是「换了文案码就必须投递，哪怕时钟一动没动」。

**对票面的六处偏离**

1. **票面「传输任务的阶段名跃迁」在这个任务上没有对象，而那条改善只兑现了一半。** 它的**阶段**
   恒为 `transferring_files`、从不跃迁；转换前会滞留的是**文案**——一本大书拷几分钟，其间那条
   「正在传输 X」若落在上一次投递的 500ms 窗口内就被丢掉，用户盯着的是上一本传完时的「已传输 k / N」。
   **票面把这条滞留归因于「手写节流丢掉了跃迁豁免」，只对了一半**：上一节把逐条目文案码收成恒定
   之后，逐条目帧同样换不到那条豁免——水位只比对 status / phase / messageCode / message，
   `CurrentItem` 与占位参数都不在其内。上一本传得快、下一本是几百 MB 时，后者的开工帧仍会被吞，
   气泡在那几分钟里停在上一本书上。**净改善因此是量的（窗口 500ms → 200ms）而非质的**。
   要兑现成质的，得让水位比对 `CurrentItem`，而节流水位的判定规则是 spec 关键决定 12 明确排除的。
   已写进 `launchExternalLibraryTransferTask` 的 doc。
   任务内的文案跃迁只剩收尾那一帧，用例 `TestExternalTransferClosingFrameSurvivesThrottle`
   因此钉它——它守的是水位规则本身在这条路径上成立，不是逐条目帧的滞留。
2. **新增 9 条用例**（`internal/api/external_tasks_test.go`）。票面只要求既有用例保持通过，
   而这两个任务在控制器层只有一条端到端流程用例（`TestExternalLibraryScanAndTransferFlow`），
   它靠轮询等真实 goroutine，钉不住投递序列。9 条全在 `newTaskEngine` 这一个 seam 上，
   与票 03 / 05 / 06 / 07 / 08 的同层用例一致：存储替身只实现被调到的三个方法（它同时充当
   `external.Manager` 的窄接口），资料库与外部库是两个真实临时目录——传输任务真的在拷文件。
3. **扫描任务的任务参数（`session_id`）现在无条件落地。** 转换前它与**作用域**显示名同挂在一次
   `setTaskMetadata` 上，读库失败时两者一起丢。任务声明把它们分开：`session_id` 不依赖数据库，
   显示名取不到就留空。
4. **传输任务的首帧现在带齐总数、任务参数与作用域显示名。** 此前总数是 0、参数是启动之后的第二次
   写入，中间有一个「任务已在列表里、进度条分母却还是 0」的窗口，任务列表接口能观察到。
   与票 02 / 07 / 08 修掉的是同一处形状。
5. **票面「两处手写的 500ms 进度节流」少数了一处**：传输任务体里是两处（拷贝前后各一），
   加上扫描那一处共三处。三处现在都没了，`internal/` 里不再有第二个按墙上时钟计时的进度节流。
6. **本票的用户可见改动超出 spec 明确豁免的那一条。** Out of Scope 只为本票豁免了「推送节奏变准」，
   而逐条目文案由两条收成一条、外部库扫描的逐条目文案改按语种渲染都是用户看得见的。
   两条都已记进票 11 的 CHANGELOG 清单；理由分别见上一节与「顺带改变的行为」1。

**顺带改变的行为**

1. **外部库扫描的逐条目进度文案改为按语种渲染**（此前对所有语种都是中文）。已记进票 11 的 CHANGELOG 清单。
2. **两个端点不再把任何错误都翻成 409。** 与票 05 / 06 / 07 / 08 的处置一致（今天不可达，
   启动入口只返回哨兵错误）。
3. **传输任务收尾判定从「只认取消错误、其余落空」改为「非取消错误一律视为失败」。**
   与票 03 顺带改变 5、票 05 偏离 2、票 08 票面写明的那条是同一形状：**暂停闸门**今天只返回
   `nil` 或 `ctx.Err()`，而任务上下文无 deadline，因此这条改变现在观察不到；给任务上下文加超时
   会让这条等价失效，届时四处都要重新评估。
4. **传输进入终态之后，迟到的写入不会再改到它。** `applyTaskProgress` 一律忽略非活动态任务；
   这两个任务本就没有任务体之外的写入者，此处只是随启动入口一并获得的性质。

**用例**

`internal/api/external_tasks_test.go` 九条，全部在 `newTaskEngine` 这一个 seam 上，无数据库、
无配置、无扫描器。

- `TestExternalTransferProgressObeysEngineWaterLevelOnly` — 本票的核心断言。同一段传输跑两遍：
  不动的假时钟只放行首帧，每读一次就跨过一个窗口的假时钟放行全部三帧。**已实测**：把那层
  500ms 墙上时钟节流加回任务体，后半段立刻变红（三本书的拷贝在真实时间里连 1ms 都用不到，
  推假时钟撬不动它）。
- `TestExternalTransferClosingFrameSurvivesThrottle` — 换了文案码的收尾帧即使在窗口内也必须投递，
  且「传输文件」指标落在终值上。
- `TestExternalTransferDeclarationLandsWhole` — 首帧带齐作用域、会话、系列数与总数。
- `TestExternalTransferAllExistNamesItsOwnVariant` / `...PartialFailureNamesItsOwnVariant` —
  两条变体各自的文案码；后者还断言一本失败不中止整批，且技术错误串留在 `Error` 里。
- `TestExternalTransferCancellationLandsCancelled` — 取消由引擎裁决，任务体不写终态。
- `TestExternalScanReportsWholeFramesAndCompletes` — 扫描的一份报文一条载荷、帧内自洽。
- `TestExternalScanEmptyNamesItsOwnVariant` — 一个文件都没扫到不是失败，但也不用常规完成文案。
- `TestExternalTasksRejectSecondLaunchOnSameKey` — **任务键**闸门在两条路径上仍生效，
  且返回的是哨兵错误（HTTP 层据此才分得清 409 与 500）。

既有的 `TestExternalLibraryScanAndTransferFlow`、`TestExternalLibraryScanIgnoreExtensionOption`
与两条传输原子性用例一字未改，仍绿。

**验证**

- `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿。
- `go test -race -count=1 ./internal/api/ ./internal/external/` 全绿。
- `go vet`、`golangci-lint run ./internal/api/... ./internal/external/...`（0 issues）、`gofmt -l` 干净。
- `scripts/check-doc-style.sh` 通过。
- `cd web && npx tsc -b`、`npm run test`（31 文件 / 266 条）、`npm run build` 全过；
  未改动 `TaskStatus` / `TaskLimits`，前端契约未变。
- 本票涉及的 14 个 `task.msg.*_external_library.*` 文案码逐条比对过 `zh-CN.ts` 与 `en-US.ts`，
  两侧各一份、无缺失；本票未新增也未删除文案键。

**留给后续票的一条现状（承自票 05、07、08，本票仍未动）**

`web/src/api/generated.ts` 与 `go run ./cmd/tsgen` 的产物有一处**头注释**漂移，来自提交 39fcbd3
（改了 `cmd/tsgen` 的模板但没重新生成）。类型本身无差异，CI 的漂移校验因此是红的。
本票已复核它与外部库改动无关，仍另行处理。
