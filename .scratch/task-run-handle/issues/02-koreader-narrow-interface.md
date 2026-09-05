# 02 — KOReader 收下句柄（证伪点，唯一的对外行为变更）

**What to build:** `internal/koreader` 的两个批循环改成收下一个窄接口，替掉今天那个
`progress func(current, total int)` 形参与 `RebuildOptions.AbsorbDiskWork` 字段；
`api` 侧用嵌入**任务句柄**的小适配器承载各任务自己的帧构造；包内用例改用手写假体。

**这一票排第二位是刻意的。** 它是整套形状的证伪点：若嵌入式适配器装不下帧构造、或跨包读 IO
实况不成立，要在这里暴露，而不是转完四处之后。它门控票 03 与 04。

**Blocked by:** 01

**Status:** done

## 唯一的对外行为变更

`hashed_files` 今天在这两处口径不一致：

| 取用点 | 今天 | 本票之后 |
| --- | --- | --- |
| 文件身份重建（`api`） | 错误检查**之后** +1 | 不变 |
| **指纹**重建（`koreader`） | 错误检查**之前** +1 | 与上面一致 |

也就是说今天被**暂停闸门**或**存储令牌**挡下、一个字节都没读的书，在 KOReader 这一侧被计入
「已哈希文件数」。票 01 已把规则钉成「工种是 `identity_hash` 且闭包真的执行了」，本票让它生效。

**慢盘上这个数字会比今天小**，小掉的那些本来就没读。这条要进 `CHANGELOG.md`（由票 05 统一记）。

## 窄接口

`koreader` **自己声明**它需要的那几个方法，**不** import `internal/taskrun`——`*Handle` 结构化
满足它即可。这是 `external.Store` 已经用过的路子。接口里只放批循环真正用到的三样：
**计数推进**、过**暂停闸门**、发起**磁盘作业**。

接口的 doc 要写明它为什么是这三个，以及为什么**计数推进**只报两个数字不带展示文案——
理由与 `ScanSession` 那条相同（本包渲染的话英文用户会看到中文），照抄即可，不必重新论证。

随之删除的两样：

- `RebuildOptions.AbsorbDiskWork func(diskwork.Stats)` 字段，以及包内那个 `nil` 时兜底的空实现
- 两个方法上的 `progress func(current, total int)` 形参

包内 5 处手抄的闸门调用改成经句柄过闸门；1 处磁盘作业改成经句柄发起，实况由句柄自己吸收，
包内不再出现 `diskwork.Stats`。

## `api` 侧适配器

各任务自己的帧构造（**阶段**、i18n 码、要不要带 IO 指标）留在 `api`。做法是声明嵌入
`*taskrun.Handle` 的小类型，**只遮蔽计数推进那一个方法**，过闸门与发起磁盘作业由嵌入白拿。
写法与 `proposalDB` 遮蔽内嵌 `Store` 的 `ExecTx` 同款。

`koreader` 的 5 处调用点分布在 4 个任务体上（**指纹**重建、进度对账、匹配刷新——它两样都用、
低优先级哈希回填），因此**适配器按帧构造分，不按任务分**：今天已有的三个帧构造器
（指纹帧、对账帧、哈希进度那份两处共用的）各对应一个适配器。

`taskIOMetrics` 在这几处的声明与穿参数随之消失，实况改从句柄读。**注意**：适配器读实况与批循环
写实况在同一个 goroutine 上，但句柄已整体加锁，不必额外处理。

## 包内用例改假体

`internal/koreader` 的相关用例改用手写假体驱动窄接口，于是**不再需要** `storageio.NewScheduler()`
与真 `diskwork.Runner`。借此把两条今天没有直接用例的批循环不变量补上：

- **指纹**算不出来只跳过这一本、分页游标仍推进。游标停住即死循环——批次按 id 递增切，
  坏书排在末尾时同一本会被永远切回来。
- 中止只由磁盘作业入口返回的错误（闸门与令牌的错误）决定，不由指纹算不出来决定。

## 会变红的既有用例（逐条确认而不是逐条改绿）

- `api.TestRebuildBookHashesReportsDiskWorkIO` —— 断言 IO 实况进了整帧。形状不变，
  但实况来源从手卷的累加器变成句柄，需跟着改造；**它断言的行为必须保持**。
- `api.TestRebuildBookHashesFrameIsPublishedWhole` —— 守「整帧一次报出」，适配器必须保持这条：
  计数、阶段、指标同属一次事件，拆开报会被投递水位撕帧。
- `internal/koreader` 里今天穿过真调度器的那组磁盘作业用例 —— 改为假体驱动；
  它们原本断言的「取了令牌」属于 `diskwork` 自己的契约，已由票 01 覆盖，不必在这里重测。

## 验收

- [ ] `internal/koreader` 声明了自己的窄接口，且**不** import `internal/taskrun`
- [ ] `RebuildOptions.AbsorbDiskWork` 与两处 `progress func(current, total int)` 形参已删
- [ ] 包内不再出现 `diskwork.Stats`；5 处手抄闸门与 1 处磁盘作业均已改走句柄
- [ ] `api` 侧适配器按帧构造分，每个只遮蔽计数推进；i18n 码与**阶段**字面量仍在 `api`
- [ ] `hashed_files` 不再计入被闸门/令牌挡下的书；票 01 的计数规则用例即是它的守卫
- [ ] `internal/koreader` 用例不再构造调度器；两条批循环不变量各有一条用例
- [ ] 上述三组既有用例按「会变红的既有用例」一节各自处置
- [ ] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [ ] `go vet ./...`、`golangci-lint run` 无 issue、`gofmt -l` 干净、`check-doc-style.sh` 通过
