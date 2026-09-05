# 挂账清单 · task-limits-report

跑票过程中冒出的非阻塞选择。流程不为它们停；由用户挑时机逐条处理。

## D1 · 票 01 · 存储策略在一次徽章拼装里解析两遍

- **问：** `taskLimitsForPath` 自己调一次 `config.ResolveStoragePolicy` 取徽章上那几项（档位、卷键、
  四项并发、三个开关），而它调的 `scanner.WorkerCount` 内部又解析一次同一条路径。两次解析的入参
  完全相同，结果也必然相同（`ResolveStoragePolicy` 是纯函数），只是多跑一趟。
- **选项：** A 各解析各的（推荐，因为「worker 数怎么算」整个留在扫描器里，api 只知道调它；
  合并就得让扫描器导出一个同时吐策略与 worker 数的复合返回值，把徽章的形状漏进扫描器）｜
  B 让扫描器导出 `func WorkerCountFor(policy config.ResolvedStoragePolicy, cfg config.Config, opts ScanOptions) int`，
  api 解析一次策略后把它传进去
- **不处理会怎样：** 已按 A 落地；每次拼徽章多一次纯函数解析（每个任务起跑时一次，不在扫描热路径上）。
- **状态：** open

## D2 · 票 01 · 缩略图重建的 `storage_profile` / `cover_concurrency` 元数据标签留着

- **问：** 本票撤掉了缩略图重建的并发徽章，理由是那个数取自 `path=""` 的全局默认策略而它逐库扫。
  同一个任务声明的 `Metadata` 里还挂着 `storage_profile`、`volume_key`、`cover_concurrency`——
  同样取自全局默认策略，同样在有按库策略条目时对不上。票据只点名了徽章。
- **选项：** A 只撤徽章，标签留着（推荐，本票的判据是「有没有一个并发上限真的管得住它」，
  而封面并发**这一项**确实管得住缩略图重建的封面读盘——`diskwork.concurrencyLimit` 给
  `WorkKindCoverBuild` 与 `WorkKindCacheWrite` 判的正是它；只是那边按作业路径逐库解析策略，
  标签上钉死的仍是全局默认那一份，所以问题在**值取自哪条路径**而不在这一项该不该报）｜
  B 一并撤掉，理由与徽章同款
- **不处理会怎样：** 已按 A 落地；配了按库策略的用户在缩略图重建详情里看到的档位是全局默认那一档。
- **状态：** open

## D3 · 票 01 · CHANGELOG 另起一节还是并进 9-05 那批

- **问：** 本票是一条对外行为变更（五个任务不再显示并发徽章），而 `CHANGELOG.md` 顶部已经堆着九节
  同为 v1.6.1 / 2026-09-05 的条目。
- **选项：** A 另起一节，日期写 2026-09-06（推荐，与现有形状一致——每节各答一个用户能复述的症状，
  同版本多节本就是这份 CHANGELOG 的常态）｜ B 并进 9-05 任务编排那一节，因为都动任务面板
- **不处理会怎样：** 已按 A 落地；v1.6.1 下多一节。
- **状态：** open

## D4 · 票 01 · `TestScanWorkerCountUsesExternalHDDPolicy` 跟着改名

- **问：** `scanWorkerCount` 提成自由函数并导出为 `WorkerCount` 之后，那条用例名里的
  `ScanWorkerCount` 不再指向任何符号。
- **选项：** A 跟着改名为 `TestWorkerCountUsesExternalHDDPolicy`（推荐，用例名指向被测符号是这份仓库
  既有的写法，且新增的两条用例同前缀，三条排在一起）｜ B 保留旧名，只改函数调用
- **不处理会怎样：** 已按 A 落地；`git log --follow` 之外，按旧用例名搜不到它。
- **状态：** open

## D5 · 票 01 · 「两处扫描仍带 Limits」没有用例守着

- **问：** code-review（Spec 轴）指出：全仓没有任何用例断言 `launchLibraryScanTask` /
  `launchSeriesScanTask` 的 `TaskSpec` 仍带 `Limits`。本票刚撤掉四个兄弟调用点的同一行，
  第五处被误删不会有用例变红——绿的两条守的都是 `taskLimitsForPath` 这个函数本身。
  这是既有缺口，票据未点名。
- **选项：** A 本票不补（推荐，补它要造一套能跑通**资料库扫描**任务体的装置——真扫描器、真库路径、
  等首帧发布，而本票承诺不动那两处；且后面 `task-run-model` 那 19 张要重生整份任务声明，
  现在造的装置多半白造）｜ B 当场造装置，顺带把「五个任务不报」也一并按调用点守住
- **不处理会怎样：** 已按 A 落地；那两处的徽章靠人读代码守着。
- **状态：** open
