# 10 — ComicInfo 回写任务转换（含文案国际化）

**What to build:** ComicInfo 回写任务改走新的启动入口。

这是 17 个启动点里最后一个，也是唯一一个仍在使用**字面量文案**接口的生产任务——它的用户可见文案是硬编码中文（启动时的「正在写入 ComicInfo」、完成时的「已写入 N 本，跳过 N 本，失败 N 本」）。新接口只收 i18n 码，因此转换必须**同时为它补齐两个语种的文案键**：起始、逐条目进度、**完成**、已取消、失败。

这带来一处用户可见改善：英文用户此前在任务中心看到的是中文，转换后看到英文。

它还是唯一一个「可取消但不可暂停」的形状，转换后要确认任务中心不会给它显示暂停按钮。

**Blocked by:** 03

**Status:** ready-for-agent

- [x] ComicInfo 回写任务改用新入口
- [x] 该任务的可取消/不可暂停能力与转换前一致；任务中心不显示暂停控件
- [x] 新增该任务全部文案键，zh-CN 与 en-US 两处同步（该文案表无类型或用例保证两侧键齐全，须人工核对。见「对票面的偏离」2：是 7 个键不是 5 个）
- [x] 完成文案里的「已写入 / 跳过 / 失败」三个计数通过占位参数传递，不再由后端拼接成句
- [x] 逐条目进度改走**计数推进**方法，当前书名走条目方法（见「对票面的偏离」1：按票 03 的口径整帧报出，两条通道都在这一帧里）
- [x] 该任务的**存储令牌**取用逻辑原样保留（本次不重构）
- [x] 转换后，字面量文案接口在整个仓库的**启动点**中零调用（`grep` 可验证）。生产代码里还剩
  一处非启动点的调用：引擎自己的 panic 兜底 `runTaskGoroutine` 下发一句硬编码英文，属票 11
  （见下「票 11 的剩余范围」）
- [x] 该任务的既有控制器级用例保持通过（它在任何层本来一条都没有，见「对票面的偏离」3）
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [x] `cd web && npm run build` 通过

## Comments

**实现记录**

第 17 个、也是最后一个启动点收成「一份任务声明 + 一个任务体」：

- `internal/api/comicinfo_controller.go`：新增 `writeComicInfoTaskKey` 与
  `launchWriteSeriesComicInfoTask`。此前这个启动点内联在 handler 里（spec 关键决定 9 数的那个
  「第 17 个」），现在与其余 16 个同形。系列、书目、标签、作者仍由 handler 备齐后传入——任务声明
  要一次性落地，其中的**作用域**显示名来自系列。
- HTTP 端点改用 `writeTaskLaunchError`（票 05 定的收敛），任务键由端点自己调拼键函数取得。
- `internal/api/task_engine.go` 与 `controller.go`：删除本票转换掉最后一个调用点的八个方法
  （见偏离 4）。

**对票面的五处偏离**

1. **逐条目进度整帧报出（`Report`），不是 `Advance` + `Item` 两次分报。** 这一帧里计数、书名与
   三个结局计数同时变，按票 03 定下的口径（按**帧的形状**判，不按谁在调用）只能整帧报。
   **已实测**：拆成 `Advance` / `Phase` / `Item` / `Metrics` 四次分报后，两本书投递 8 条载荷而不是 2 条，
   且第一条的书名与指标都还是空的。用例 `TestWriteComicInfoFrameIsPublishedWhole` 钉的正是这个。
   与票 06 偏离 3、票 07 偏离 1、票 08、票 09 是同一形状。

2. **新增 7 个文案键，不是票面列的 5 个。** 票面列了起始 / 逐条目进度 / **完成** / 已取消 / 失败五类，
   但这个任务今天在界面上还有两处渲染成**裸键**：`task.type.write_comicinfo`（任务中心的任务类型
   标签，`getTaskTypeLabel` 无默认值）与 `settings.maintenance.taskPhase.writing`（阶段名，
   `writing` 这个阶段全仓库只有本任务在用）。两者同属「该任务全部文案键」，而且不补的话，
   本票兑现的「英文用户不再看到中文」会变成「英文用户改看到裸键」。阶段值本身仍是 `writing`，未改。
   **与 spec 的 Out of Scope 有一处张力**：那里为本票豁免的是「文案从硬编码中文改为按语种渲染」，
   而这两处此前是裸键、不是中文。它们仍是同一个任务的同一块界面文字，故一并补上，已记进票 11 的
   CHANGELOG 清单。

3. **新增 5 条用例**（`internal/api/comicinfo_task_test.go`）。票面说「既有控制器级用例保持通过」，
   而这个任务在**任何**层本来一条用例都没有（`comicinfo_controller_test.go` 只覆盖两个导出端点与
   单本回写）。5 条全在 `newTaskEngine` 这一个 seam 上，与票 03 / 05 / 06 / 07 / 08 / 09 的同层用例一致：
   无数据库、无扫描器（任务体本就不读库，系列与书目由启动点带进来），归档是真实的临时 cbz。

4. **删除八个成为孤儿的方法与一条守卫用例**——这是票 11 前四条的一部分，被 CI 门禁提前。
   `golangci-lint` 的 `unused` 在本票转换掉最后一个调用点后一次报出 8 条，不删则红，
   与票 04 / 06 / 07 / 09 的处置一致。删掉的是：
   `startCancelableTask` / `startTaskWithOptionsCore` / `updateTaskDetails` / `updateTaskDetailsCore`
   （**九参数详细进度方法及其 `-1` 哨兵随之消失**）/ `setTaskMetadata` / `finishTask` /
   `completeTaskMsg`，以及 `Controller.runBackgroundTask`（它的 doc 本就写着「待启动点迁到引擎的
   启动入口后即可删除」）。
   `TestTaskLaunchersUsePanicGuardedBackground` 一并退休：它扫的是「函数里同时出现
   `taskEngine.start*` 与 `runBackground`」，而 `start*` 这一族已经一个不剩，守卫从此永远绿；
   它的失败提示还指着刚被删掉的 `runBackgroundTask`。票 01 预告的正是这一刻。
   **票 11 的剩余范围见下节。**

5. **删掉任务体里那条冗余的取消判定。** 转换前每本书前有两条：`taskcontrol.Wait(ctx)` 与
   `ctx.Err() != nil`。未暂停时 `Wait` 返回的**就是** `ctx.Err()`，两条完全同义。现在只剩一条。
   这不算碰 spec 划出 Out of Scope 的「**存储令牌** / **暂停闸门**的取用仪式」：闸门调用本身一字未动，
   删掉的是它旁边那条同义判定；票面第 6 条护的**存储令牌**取用逻辑原样保留。

**顺带改变的行为**

1. **起始、逐条目、**完成**三处文案改为按语种渲染**（此前对所有用户固定为中文，且完成那句由后端
   `fmt.Sprintf` 拼好），**取消文案从通用的「正在取消任务...」改为本任务专属的已取消文案**——
   一条**终态**此前在说自己正在取消。任务类型标签与阶段名也不再是裸键（见偏离 2）。
   本票已把这四条逐条写进票 11 的 CHANGELOG 清单（此前那里只有一句概括）。
2. **首帧现在带齐总数、任务参数与作用域显示名。** 此前元数据是启动之后的第二次写入，中间有一个
   「任务已在列表里、却还不知道是哪个系列」的窗口，任务列表接口能观察到。投递帧数因此从 2 变 1。
   与票 02 / 07 / 08 / 09 修掉的是同一处形状。
3. **逐条目进度现在会更新显示文案。** 转换前那次详细进度传的 `message` 是空串，`applyTaskMessage`
   对空串无操作，于是整个回写期间显示的一直是启动文案。节流节奏不变：文案码在循环内恒定，
   只有首条逐条目帧因为码从起始跃迁到进度而必定放行。
4. **收尾判定从「只认取消错误、其余落空」改为「非取消错误一律视为失败」。** 与票 03 顺带改变 5、
   票 05 偏离 2、票 08 票面写明的、票 09 顺带改变 3 是同一形状：任务上下文无 deadline，
   **暂停闸门**只返回 `nil` 或 `ctx.Err()`，因此这条改变现在观察不到。
5. **端点不再有第二条 409 来源。** 启动入口只返回哨兵错误，`writeTaskLaunchError` 的 500 分支
   今天不可达；409 的响应体一字未变。

**作用域的一处既存别扭（未动）**

`inferTaskScope` 按任务类型判作用域、按任务键末段取 id：类型 `write_comicinfo` 里没有 series 二字，
于是这个任务是**系统**作用域、却带着系列 id。转换前后是同一份推导、同样的入参，因此结果一致。
改它会挪动这个任务在界面上的挂点，不属于本票；用例
`TestWriteComicInfoDeclarationLandsWhole` 已把这个组合钉住。

**票 11 的剩余范围（本票提前做掉了前四条）**

- 已做：字面量文案版的启动 / 进度 / 完成方法与其核心方法、九参数详细进度方法及其 `-1` 哨兵、
  `Controller.runBackgroundTask`、`TestTaskLaunchersUsePanicGuardedBackground`（均见偏离 4）。
  `setTaskMetadata` 也已随之删除，因此票 11 第 4 条「建上下文、写元数据两个方法降为引擎内部」
  **只剩 `newTaskContext` 一个**——而它今天已只有 `Run` 一个调用方。
- 未做，仍属票 11：**字面量文案在生产的最后一处残留**是引擎自己的 panic 兜底——
  `runTaskGoroutine` 调 `failTaskWithError(key, "Background task panicked", …)` 下发一句硬编码英文，
  `finalizeTaskCore` / `failTaskCore` / `failTaskWithError` 三处的 `message` 形参也因它而留着。
  它不是启动点的残留（本票的验收口径说的是启动点），但票 11 的「无任何遗留的兼容垫片」要收掉它，
  届时要为 panic 补一个文案码。
- 未做，仍属票 11：`claimTaskSlot` 直接存下 `spec.Labels` / `spec.Metadata` 的 map 而不克隆
  （票 08 留下的那一条）、CHANGELOG，以及全仓库「不存在绕过启动入口创建任务的路径」的终检。

**用例**

`internal/api/comicinfo_task_test.go` 五条，全部在 `newTaskEngine` 这一个 seam 上。三种结局各由
一本书代表：真实 cbz（写成）、`.cbr`（格式在打开之前就被判为不可写，算**跳过**）、盘上不存在的
`.cbz`（打开失败，算失败）。

- `TestWriteComicInfoDeclarationLandsWhole` — 任务声明一次落地（投递恰好 1 条），带齐总数、
  元数据、作用域名与作用域推导；并钉住**不可暂停**（`can_pause` 为假 + 引擎拒绝 `pause`）——
  票面「任务中心不显示暂停控件」的后端一半，前端的暂停控件正是由这个字段决定的。
- `TestWriteComicInfoFrameIsPublishedWhole` — 一本书一条载荷、帧内自洽（见偏离 1）。
- `TestWriteComicInfoCompletionCountsGoToParams` — 三个计数走占位参数、终态不带后端拼好的文案，
  且那本可写的书真的被写进了 ComicInfo。
- `TestWriteComicInfoCancellationLandsCancelled` — 取消由引擎裁决、落本任务专属取消码，
  且取消之后没有再动用户的归档。
- `TestWriteComicInfoRejectsSecondLaunchOnSameKey` — **任务键**闸门与哨兵错误。

`steppingClock` / `frozenClock` / `windowSteppingClock` 从 `external_tasks_test.go` 搬进
`task_engine_seam_test.go`——它们是这个 seam 的共用装置，留在某一组用例的文件里会让后来者跨文件
消费（票 01 评审已就此提过一次，票 02 已照此搬过一批）。同理，「按任务键 + 文案码过滤快照」此前
有三份实现（seam 的取最后一条、外部库用例的计数、本票新写的取全部），合并成 `publishedTasksWithCode`
一份，另两处按它派生。

**评审修正**

`/code-review` 两轴各自跑完后改了六处：两轴独立指到同一处——**票 11 的 CHANGELOG 清单里只有一句
概括**，而本票声称「全部已记进」，于是把四条用户可见改动逐条补了进去。另外五处：新用例文件里三处
带转换叙事的注释按 `docs/agents/doc-style.md` 的三段判定改写成约束（历史归 CHANGELOG）；
`TestWriteComicInfoFrameIsPublishedWhole` 的 doc 说「拆开报会被水位吞掉」而该用例的时钟每帧都跨窗口，
改为分别说清用例的红信号与生产的代价；`absentBook` → `bookWithoutFile`（原名读起来像「书目不存在」，
实际是书目在、文件不在）；上面那处三份过滤实现的合并；以及「字面量文案零调用」那条验收标准
补上口径（是**启动点**零调用，引擎的 panic 兜底那处属票 11）。

两条判断题**未采纳**：`(series, books, tags, authors)` 这个四元组在本文件里第 5 次同行同游
（Data Clumps），抽一个「回写素材」类型要连带改 `buildComicInfoForBook` /
`buildSeriesComicInfoArchive` / `buildBookComicInfo` 三个导出路径，属 ComicInfo 子域的重构而非
任务引擎的收口；任务类型串与 `len(books)` 各在几处手写（Primitive Obsession），与其余 16 个启动点同形。

**验证**

- `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿。
- `go test -race -count=1 ./internal/api/` 全绿。
- `go vet ./...`、`golangci-lint run ./internal/api/...`（0 issues）、`gofmt -l` 干净。
- `scripts/check-doc-style.sh` 通过。
- `cd web && npx tsc -b`、`npm run test`（31 文件 / 266 条）、`npm run build` 全过；
  未改动 `TaskStatus` / `TaskLimits`，前端契约未变。
- 两个语种文件按机器比对：各 1952 个键、集合完全相同，本票新增的 7 个键两侧齐全。

**留给后续票的两条现状**

1. `web/src/api/generated.ts` 与 `go run ./cmd/tsgen` 的产物有一处**头注释**漂移，来自提交 39fcbd3
   （改了 `cmd/tsgen` 的模板但没重新生成）。类型本身无差异，CI 的漂移校验因此是红的。
   承自票 05 / 07 / 08 / 09，本票仍未动。
2. **前端那句写回完成提示读的是一份后端不再返回的响应体**：
   `handleWriteSeriesComicInfo` 按 `{written, skipped, failed}` 渲染 `series.header.writeComicInfoDone`，
   而这个端点早已改成 202 + `{status, task_key, total}`，于是用户点完立刻看到「写入完成： 成功 ·  跳过 ·  失败」
   三个空格。这不是本票引入的（后端异步化时前端没跟上），修它要先定「一个刚发起的异步任务该提示什么」，
   属前端改动，本票未动。
