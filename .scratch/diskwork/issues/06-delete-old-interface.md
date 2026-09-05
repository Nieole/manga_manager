# 06 — 删除旧接口，收口

**What to build:** 八处**磁盘作业**全部改走新入口之后，把旧的那一套删干净：两个包里各一份的取令牌辅助方法、两份逐字相同的「取正最小」上限函数，以及那条守着旧形状的用例。

删除必须与迁移落在**同一个 PR** 内。理由沿用任务引擎那次：本仓接口里那条「i18n 码版／字面量版」的疤，就是一次停在半路的迁移冻结进签名的产物；用同样的方式收尾只会再造一条相同的疤。

**Blocked by:** 04, 05（03 由它们传递）

**Status:** done

- [x] 扫描器与 api 各自的取令牌辅助方法一并删除
- [ ] 两份逐字相同的「取正最小」上限函数一并删除；api 那份里没有任何调用点的封面构建死分支随之消失
  —— 死分支已随辅助方法消失；两份函数**保留并改名为 `minPositive`**，见 comment 1
- [x] 删除那条从扫描器内部、伸手进未导出方法、靠固定时长 sleep 去测同卷排队语义的用例——它守的是 `storageio` 自己的语义，而该包已有一条等价用例
- [x] 扫描器包内不再有任何用例触碰取令牌的内部
- [x] 全仓再无第二处「解析存储策略并挑选并发上限」的代码；前台阅读取页仍直接向调度器申领，不受影响
- [x] `CHANGELOG.md` 记两条对外行为变更：每工种的并发上限改由工种单独决定（只影响手工把四项并发配成不同正值的库，默认档与慢盘档结果不变）；按下暂停后不再继续往盘上写缩略图
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [x] `go vet ./...`、`golangci-lint run` 无 issue、`gofmt -l` 干净

## Comments

八处**磁盘作业**的迁移与旧接口的删除落在同一个 PR 内，收口完成。`golangci-lint run` 从票 05 起
红着的那条 `unused` 随本提交转绿。

1. **两份「取正最小」函数保留并改名，只删了它们服务的取令牌辅助方法。** 票面第二条无法按字面
   执行——票 05 已就 api 那份提前预警，扫描器那份同理，两者都还有活调用点：

   - `scanner.minPositive`（原 `storageIOLimit`）被 `scanWorkerCount` 用来算这次扫描起几个
     **worker**；
   - `api.minPositive`（原 `minPositiveStorageLimit`）被 `taskLimitsForPath` 用来算任务面板
     上报的**有效并发数**。

   **改名的理由**：旧名字是被删掉的那套东西起的，`storageIOLimit` / `minPositiveStorageLimit`
   听起来仍管着存储令牌，而它们现在只算 worker 数。初版是给两者各加一句「它不是磁盘作业的限流」
   的 doc——那句话是在纠正函数自己的名字（该改名而不是加注释），且把一个属于 `diskwork` 的事实
   抄进另外两个包，违反 AGENTS.md 的唯一**归属**。改名后两句都不必写，各自只留一行「全非正返回
   0」这个非显然的约定。三个包因此有三个同名的 `minPositive`：同一段算术叫同一个名字不制造歧义，
   叫三个名字才会——而歧义的来源本来就是「storage IO」这个前缀，不是重名。

   **没有合并成一份。** 两条合并路各自的否决理由：

   - **从 `diskwork` 导出**（两个调用方都已 import 它，是最短的一条路）：会给一个 spec 明写
     「对外只暴露**一个**入口」「接口小到可以完整读进上下文」（user story 18）的模块加上第二个
     导出符号，而这个符号与磁盘作业毫无关系——`scanWorkerCount` 会为了一段算术去 import 一个
     管令牌的模块。收口票不该反手把刚收窄的接口撑开。
   - **放进 `config`**：给一个所有包都依赖的叶子包加公共面，去消一个本 spec 从未认领的重复。

   这里真正的重复也不是那 10 行算术，是 `api.taskLimitsForPath` 整段重抄了
   `scanner.scanWorkerCount` 的公式（已核实两者**当前等价**：`computesFullHash` 忽略它的 `cfg`
   形参，于是 `computesQuickHash() || computesFullHash(cfg)` 就是 `Identity || Repair`）。等价是
   巧合而非约束——改一处、另一处静默报旧数。只合并算术会把这条盖住，因此它单独立了票：
   `.scratch/task-limits-report/issues/01-task-limits-reimplements-scan-worker-formula.md`
   （`needs-triage`——那里还牵出一个要拍的决定：面板给四个非扫描任务报的 worker 数本就与它们
   无关，是钉住现状还是改报真正管住它们的那一项）。

   **第五条（「全仓再无第二处解析存储策略并挑选并发上限」）按字面不成立，按口径成立。** 字面上
   还剩三处「解析策略 + 挑上限」：`scanWorkerCount`、`taskLimitsForPath`，以及封面队列按
   `CoverConcurrency` 收窄 worker 数那处（后者在 spec 的 Out of Scope 里）。三处挑的都是**起几个
   goroutine**，不是磁盘作业的限流。按它要守的那个口径——向调度器申领令牌——则成立：全仓 `Acquire`
   只剩 `diskwork` 与前台取页两处，前者一张按工种的表，后者按票面不动。

2. **`CHANGELOG.md` 记了四条对外变更，不是票面的两条。** 多出的两条由前票明确点名要求或直接
   产生：

   - **存储策略的解析源统一成「字节落在哪」**——票 03 comment 1 与票 05 comment 2 各提出过一次
     同样的要求。五处：扫描器的归档打开（原按库根）、扫描器与维护子域的四处指纹（原按书所属库的
     路径）。只有配了「比库根更深」的策略条目时生效档位才会变。
   - **「等令牌等了很久」的日志扩到八处**——它是 spec user story 21，用户在日志里直接看得见。
     CHANGELOG 这条的口径比 spec 严一点：spec 关键决定 8 写的是「今天这条日志只存在于封面构建
     一处」，实际上**缓存写入那处也有**（`Queued thumbnail cache write completed`，落盘耗时或
     等令牌任一 ≥250ms 即触发，票 04 保留了它）。写「两处」而不是照抄「一处」。

   `internal/api` 的文件头随之改写：它不再推导存储 IO 令牌。

3. **未新增用例。** 本票只做删除，被删的那条用例守的是 `storageio` 的语义，该包的
   `TestSchedulerSerializesSameVolume` 逐条等价且不碰扫描器内部。八处迁移各自的守卫已由票 02–05
   建立（含票 03/04/05 三批此前零覆盖的失败出口）。

一条与本票无关、核对时顺手查实的事实：`cmd/storageiobench/main.go` 里另有一个双参版
`minPositive`。它属于一个独立的压测命令，不是那八处磁盘作业，本次不动——顺带说明 `minPositive`
本就是本仓给这段算术起的名字，改名不是新造词。
