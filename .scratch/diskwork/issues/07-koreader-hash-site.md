# 07 — 漏掉的第九处：KOReader 指纹重建的无令牌读盘

**What to build:** KOReader 的书籍指纹重建任务逐本流式读完**整个文件**算 MD5，全程不取**存储
令牌**。它和 `diskwork` 管的 `identity_hash` 是同一种操作，却绕开了闸门与令牌。把它收进单一入口，
与另外八处同形。

**用户侧可观察的变化**：在慢盘（外置硬盘档／网络存储档）上，这个任务此后会为前台阅读让路，也会
遵守 idle-only。今天它不认这两条——用户一边翻页一边跑指纹重建，盘被两边一起抢。

**Blocked by:** 无（票 02 已把模块与入口建好）

**Status:** done

## 事实

`internal/koreader` **既不 import `diskwork` 也不 import `storageio`**。`RebuildBookIdentities`
在 `match_mode = binary_hash` 时对每一本缺身份的书调用 `FingerprintFileContext`，后者
`os.Open` 之后 `io.Copy(md5, …)` 读完整个文件。整个循环只有 `taskcontrol.Wait` 一道闸门，
没有令牌。

**两个任务都到这里**：`launchRebuildBookHashesTask`（书籍指纹重建）与
`launchRefreshKOReaderMatchingTask`（匹配刷新，它的第一阶段就是这次重建）。改一处，两个任务
一起受益。

它就是 `CONTEXT.md` 里**磁盘作业**词条描述的东西——「一次必须先经暂停闸门放行、再取得存储令牌
才能开始的磁盘操作」——只是当初数「全仓八个取用点」时没有算上它，因为它在 `koreader` 包里，
而清点是按 `scanner` 与 `api` 两个包做的。

### 危害的真实边界

- **不会超订。** 循环是严格顺序的（`for _, book := range books`），一次只有一本书在读盘，
  所以「并发额度被吃光」这一类问题不存在。
- **会抢前台。** 令牌承担的另外两件事它一件都没有：`PauseBackgroundWhenReading`（为阅读让路）
  与 `IdleOnlyHeavyTasks`（只在空闲时干重活）。慢盘档下这两项默认都是 `true`，而这个任务照跑
  不误。这才是本票要修的那一条。
- **观测也缺。** 它不产出等待与暂停耗时，票 02 那条「等令牌等了很久」的日志对它无效。

## 要注意的两件事

1. **不成环，直接 import 即可。** 已核实：`diskwork` 的依赖闭包只有 `config`、`storageio`、
   `taskcontrol` 三个叶子包，`koreader` 不在其中（`go list -deps` 查过）。因此
   `koreader` → `diskwork` 是一条新的、合法的边。

   `Runner` 经**构造期注入**，与另外两处同形：`koreader.NewService(store, cfg)` 加一个
   `*diskwork.Runner` 形参，唯一的生产构造点在 `api/controller.go`，那里 `c.diskWork` 已经建好。
   这会让 spec 关键决定 7 说的「生产构造点两个」变成三个——照实更新那句话。
2. **`Work.Path` 取那本书的路径**（`book.Path`），不是库根。这是票 03/05 已经统一过的口径：
   字节落在哪决定卷键。

## 验收

- [x] 书籍指纹重建的每本读盘走 `diskwork` 的 `Do`，工种为 `identity_hash`
- [x] 慢盘档下它为前台阅读让路、遵守 idle-only（今天两条都不认）
- [x] 等待与暂停耗时汇入该任务的指标，与另外八处同形
- [x] 算不出指纹仍只跳过这一本并推进分页游标，不中止整个任务；中止只由 `Do` 返回的错误决定
      （与票 05 两处回填同形——那里的游标推进有一条专门的守卫用例，本处照抄）
- [x] `CHANGELOG.md` 记一条对外行为变更：慢盘上书籍指纹重建此后为阅读让路
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [x] `go vet ./...`、`golangci-lint run` 无 issue、`gofmt -l` 干净、`check-doc-style.sh` 通过

## Comments

第九处收进单一入口，`koreader` → `diskwork` 这条新边如票面预判的那样不成环。两个任务
（书籍指纹重建、匹配刷新的第一阶段）共用同一份重建，一起受益。

本票新增四条用例。`koreader` 三条：两条守令牌——**此前零覆盖**，因为此前根本没有令牌可守——
一条把哈希并发压到 1 并预先占满那把限流器，一条用外置硬盘档让前台取页占住这块盘；两条都断言
重建拿不到令牌就**不读盘**（书的 `file_hash` 仍为空），且第一条在归还令牌后重跑一遍确认同一本
算得出来，否则「没读盘」可以靠「重建根本不工作」满足。第三条守指纹算不出来时的分页游标推进，
照抄票 05。`api` 一条：守实况从 `koreader` 的批循环流回本包任务指标、并落到**任务参数**那条通道
上的接线。

第一条用例同时守住三件事，靠的是把限流的那一档配在**比库根更深**的系列目录上、库根那一档配成
不限流的 SSD 档：工种挑错字段（其余三项并发都更宽）、策略解析源改回库根（会落到 SSD 档一路
放行）、以及根本不取令牌，三种回退都会让它变红。守卫有效性逐条实测：摘掉 `Do` 那一层，两条
令牌用例立即失败；`Work.Path` 改成 `book.LibraryPath`，第一条失败；摘掉失败分支里的
`afterID = book.ID`，游标用例挂到 5 秒超时后失败；摘掉 `MergeParams`，api 那条失败。

五处取舍：

1. **指标的接缝是一个 `absorbDiskWork func(diskwork.Stats)` 形参，而不是把 `taskIOMetrics` 穿过
   包边界。** 另外八处的批循环与指标类型同包，一行 `metrics.absorbDiskWork(stats)` 即可；这一处
   的循环在 `koreader`、指标在 `api`，跨包只能交出一个沉降口。两个任务体各自持有一个
   `taskIOMetrics`，把方法值交给重建，进度回调读的就是同一个它。`nil` 在入口处换成空实现，
   取用点上不留守卫——同关键决定 5。
2. **计数搭着实况一起回来。** 沉降口收的是 `absorbHashedFile`：这个工种的一次**磁盘作业**就是
   把一本书读一遍，因此除等待与暂停外「计算哈希」也记一笔。批循环在别的包里，这个计数没有第二
   条路可走。
3. **档位与卷键只走任务参数通道，不进帧的标签。** IO 那几项指标与参数在**路径**匹配模式下恒为
   零值，而面板只显示大于零的指标与非零的参数，于是它们自己不会露面；标签则是有一个显示一个，
   写进去等于把「没有这回事」显示成「实况为空」。两处哈希上报共用的 `frameMetrics` 顺手把 IO
   那三个键收成一份，免得第三处各自抄一遍键名。
4. **生产构造点从两个变成三个**（spec 关键决定 7 已照实更新）。KOReader 服务由控制器构造并转交
   控制器已建好的那个 `Runner`，因此 `koreader.NewService` 的调用必须从结构体字面量里挪到
   `c.diskWork` 建立之后。
5. **逐本的 `taskcontrol.Wait` 保留**，尽管二进制哈希模式下它与 `Do` 的闸门重复：**路径**模式
   走不到 `Do`，那道闸门是那条路上唯一的取消检查。

这次抹平的重复——`koreader.RebuildBookIdentities` 与 `api.runBackfillFullHashesLowPriority`
至此完全同形——由**票 08** 合掉，两票落在同一个 PR 内。

顺带更正 `CHANGELOG.md` 里两处按「八处」写的计数：慢等待日志现在覆盖九处，模块的共用方由两个包
变成三个。「全仓八个取用点各一份仪式」那句不动——它说的是旧代码里手抄的份数，而这一处从来就没抄
过那套仪式。

## 出处

票 06 收口后核对 `taskLimitsForPath` 的调用点时查出（见
`.scratch/task-limits-report/issues/01`）。spec 的「八处」与「全仓再无第二处」都是按 `scanner`
与 `api` 两个包清点的，这一处在 `koreader` 包里，因此没有进入那次清点——**spec 的计数写作
「八处」时就已经漏了它**，不是本批迁移弄丢的。
