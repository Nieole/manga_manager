# 挂账清单 · address-by-object

跑票过程中冒出的**非阻塞岔口**。流程不为它们停；由用户挑时机逐条处理（走 `/settle`）。
它不是[产品缺口清单](../GAPS.md)——那份装产品缺口、永不清空；这份装实现期的岔口、一批批清空。

## 一条条目要过三关

1. **另一条路站得住。** 写得出替代方案，且不是稻草人。只有一条路可走就不是岔口，那属于提交信息。
2. **翻案不出这张票。** 用户日后改选另一边，代价是一次编辑，不是拆掉已落地的活。翻案要伸出这张票——那是**停线**，不是条目。
3. **你能推荐一个。** 走过一遍才有的上下文，交出去时要带着结论。

## 停线只有三种

1. 照做会写下错的产物，且撤销要伸出这张票（典型：票的指令与上游仍然生效的决定相抵触）。
2. 动作不可逆，且不止一个方向站得住（典型：删或改一个被到处引用的标识、字段、文件）。
3. 前提不存在，票根本走不下去（典型：「在 X 上建 Y」而没有 X）。

其余全部：记一条、走最站得住的那条路、继续。**票里的数与你量到的对不上、验收条读着含糊、
票没预料到的第三种情形、顺路撞见的无关缺陷**——都属于这一类。

## 编号

本文件一条序列，`D<n>`。并行派工时由工头按票**分块**预留，谁都不用问号。
条目标题自带来处（`票 NN`），因为 id 里不带票号。

## 一条条目长什么样

```markdown
## D<n> · 票 NN · <一句话：岔口是什么>

- **问：** 岔在哪里，为什么票答不了。带上可跳转的位置（路径 + 符号名）。
- **已做：** 走了哪条路。**必填**——合并抽检看的就是这一格。
- **选项：** A …（推荐，因为…）｜ B …
- **不处理会怎样：** 不裁定的话，留在代码里的是什么。
- **归谁裁：** 用户 / 票 NN / 下一次结算
- **状态：** open
```

## Open

<!-- 往下追加，不重排、不删除 -->

## D51 · 票 06 · 改名扫到哪：三个不是任务体的函数也带着同一个运行句柄形参

- **问：** 票面写的是「任务体的**运行句柄**形参」，而 `internal/api` 里带这个形参的函数有三个不是任务体：
  `Controller.runGlobalScan`、`reportHashProgress`（均在 `internal/api/controller_maintenance.go`）与
  `Controller.runScrapeTask`（`internal/api/scrape_controller.go`）。它们是任务体把句柄传下去的辅助函数，
  类型同为 `*runhandle.Handle`，但签名上不是 `taskBody`。票答不了它们算不算「任务体的形参」。
- **已做：** 一并改成 `handle`。理由是票的立论是「同一个东西在两个包里叫两个名字，读的人要认两次」——
  留下三个 `tp` 等于在同一个包里还得认两次，而这三处正是任务体调用栈往下的第一跳。
- **选项：** A 全包扫干净，含这三个辅助函数（推荐，因为立论针对的是「同一个东西一个名字」，
  按签名是不是 `taskBody` 切一刀，会在同一条调用链上留下两个名字）｜
  B 严格按票面只改 `taskBody` 形参，这三处留 `tp`。
- **不处理会怎样：** 无论选哪边代码都能跑；分歧只在包里还剩不剩 `tp`。选 B 则 `internal/api` 里
  `tp` 与 `handle` 长期并存，后面 `address-by-object/05` 再扫这批文件时会再撞一次同样的问题。
- **顺带一记：** 票面的两个数一个准一个不准。`约 74 处` 与实测一致；`17 个启动点` 与实测不符——
  `taskEngine.Run` / `taskEngine.start` 的调用点比票面多，其中还有一个（`rebuild_index`，
  在 `internal/api/controller_maintenance.go`）的任务体压根不用句柄、形参写作 `_`，不在改名之列。
  按实测做的，不按票面那个数。
- **归谁裁：** 下一次结算
- **状态：** open

## D52 · 票 06 · 用例里外层捕获变量已经占着 `handle` 这个名字

- **问：** `internal/api/task_run_test.go` 的 `TestTaskProgressIgnoredAfterTerminal` 里，
  外层有 `var handle *runhandle.Handle` 把句柄捕出闭包，好在任务进**终态**之后再调它一次。
  形参直接改名会得到 `handle = handle`：闭包形参把外层变量遮住，外层变量始终为 nil，
  编译得过、用例在终态那一断言前先空指针。票面只说「用例全绿即是验收」，没说这一格怎么让。
- **已做：** 把外层捕获变量改名为 `captured`，形参照常改成 `handle`。
- **选项：** A 外层改 `captured`（推荐，因为它说的是「捕出来留到终态之后用」这件事，
  比 `handle` 更贴，且形参名在全包保持一致）｜
  B 这一个闭包的形参保留 `tp`，外层不动——代价是全包唯一的例外，读的人得停一下想为什么。
- **不处理会怎样：** 选哪边用例都绿；分歧只在这一个用例里读到的是哪个名字。
  选 B 的话包里会剩下最后一个 `tp`，`grep tp` 不再是零。
- **归谁裁：** 下一次结算
- **状态：** open

## D53 · 票 06 · 新工作树里 `go build ./...` 过不去，卡在前端产物而不是 Go 代码

- **问：** `web/web.go` 的 `//go:embed all:dist` 要求 `web/dist` 存在，而 `dist` 是前端构建产物、
  被 `web/.gitignore` 忽略，因此**任何新建的工作树里它都不存在**：`go build ./...` 报
  `pattern all:dist: no matching files found`，`cmd/server` 跟着编不过。这与本票的改动无关，
  一张纯后端改名票却因此跑不完自己的门禁。派工时给的「基线 `go build ./...` 退 0」是在主工作区量的，
  那里有一份历史构建产物。
- **已做：** 从主工作区 `cp -R` 了一份 `web/dist`（6.5 MB / 65 个文件）到本树，门禁得以跑完。
  它被 `web/.gitignore` 忽略，`git status --untracked-files=all` 里一条都不出现，进不了提交。
  本票不动任何前端源码，因此这份产物与本树该构建出的那份没有差别。
- **选项：** A 从主工作区拷一份产物（推荐，6.5 MB、几秒钟，且拷来的与该构建出的一致）｜
  B 在每棵新树里跑一次 `npm run build`（分钟级，且为一张不碰前端的票构建整个前端）｜
  C 把「新树先补 dist」写进工作树的开树步骤，让每个 agent 不必各自撞一次。
- **不处理会怎样：** 每一张开新工作树的纯后端票都会在收尾门禁上撞一次这堵墙，
  各自现场发明一个绕法；更糟的是有人把 `go build ./...` 的这条失败当成自己改坏了而去查 Go 代码。
- **已成事实：** 工头核过这条之后，**选项 C 已经落地**——开树流程上补了「建树后从主仓
  `cp -a web/dist`」这一步，本批同时在跑的 `abo-07` 那棵树也补上了。因此这条挂账要裁的
  不再是「要不要做 C」，而是「C 这个绕法是不是长久之计」：产物仍然是拷来的而不是本树构建的，
  真要动前端的票必须自己重新构建，不能吃这份拷贝。
- **归谁裁：** 用户（C 那条要动开树流程，出了单票范围）
- **状态：** open

## D61 · 票 07 · 采样表上 `rate_per_minute` 这一**列**要不要跟着改名

- **问：** 票面第二条写「领域侧、落盘侧与 api 侧的对应字段名一起改」，但落盘侧那一格是一条真实
  的 SQL 列（`internal/taskstore/schema.go` 的 `createStatements` 里 `run_samples` 那张表），
  改列名要一条 DDL 迁移，而票面一个字都没提迁移（同批 `03-drop-task-key-column.md` 提到迁移时是明写的）。
- **已做：** 选了 B。列改名为 `throughput_per_minute`，并在 `schema.go` 加了 `renamedColumns` 与
  幂等的 `ensureRenamedColumn`（先查后改，`RENAME COLUMN` 原地改、不搬数），配一条迁移用例
  `TestMigrateRenamesTheThroughputColumn`（存量形状 → 迁移两遍 → 点还在、旧列名不在）。
- **选项：** A 只改 Go 符号与 JSON 字段，SQL 列名留着，写入与读回两处 SQL 继续写旧列名
  （代价：落盘那一层的名字与其余各层对不上，但那是纯内部、无人读）｜ B 连列一起改，补一条
  幂等 DDL 与迁移用例（**推荐**，因为：票面明写落盘侧一起改；而且只改 Go 与 JSON、却让写入面
  按新名字插入的话，存量库上会撞「没有这一列」——采样写不进去只告警，症状是曲线毫无声息地
  一直空着，所以「列留着、SQL 也不改」才是 A 真正的样子）。
- **不处理会怎样：** 不裁定的话，留在代码里的是一条本票之外没人要求的迁移路径（约 20 行 DDL 逻辑
  加一条用例）。翻案就是删掉 `renamedColumns` 那一项、把四处 SQL 与建表语句改回旧列名。
- **归谁裁：** 用户
- **状态：** open

## D62 · 票 07 · CHANGELOG 记进哪一节：票面说 v1.6.2 已发布，仓库惯例说它还在攒

- **问：** 票面第五条写「这是一次对外 JSON 字段变更（v1.6.2 已发布）」，但 `CHANGELOG.md` 顶部
  连着十几节都挂在 `v1.6.2` 下、按日期排（最新一节是 2026-09-08），而提交 3ce7686 明说「归到
  v1.6.2，不再挂在**已发布的 v1.6.1** 下」——按仓库现有惯例，v1.6.2 才是正在攒的那一节。
- **已做：** 按仓库惯例走：在顶部新开一节 `v1.6.2 — 2026-09-09`，与其余未发布条目并列。
- **选项：** A 记进 v1.6.2（推荐，因为惯例与提交历史都指着它，且被改名的这个字段本身就是
  v1.6.2 里那条「吞吐曲线」带进来的，它与那条一起发布，对外没有任何一个版本见过旧名字）｜
  B 另开 v1.6.3 一节，把它当成对已发布契约的破坏性变更来记。
- **不处理会怎样：** 不裁定的话，留在文件里的是一节 `v1.6.2 — 2026-09-09`。翻案是改一行标题。
- **归谁裁：** 用户
- **状态：** resolved
- **已决（用户裁定 2026-09-09）：** 选 **B**，另开 `v1.6.3`。本条的前提被证伪了——
  `v1.6.2` 是 **2026-09-08 21:07** 打在 `7b07a07` 上的附注 tag，**已发布**，票面写的是对的，
  「仓库惯例说它还在攒」这个判断只看了 CHANGELOG 没查 tag。因此这次改名确实是对**已发布契约**
  的破坏性变更（`v1.6.2` 出厂时带的就是旧名 `rate_per_minute`），选项 A 里那句「对外没有任何
  一个版本见过旧名字」不成立。本轮队列写下的六节 `v1.6.2 — 2026-09-09` 已全部改标为 `v1.6.3`。

## D63 · 票 07 · `TestStorageIOCoverRateComesFromTheCoverRun` 间歇性红，与本票无关

- **问：** 全量 `go test ./...` 有相当高的概率红在这一条（命中率随机器负载浮动，两次实测见下）：`expected recent storage IO rates,
  got scan=3.000000 cover=0.000000 writes=50`。根因在 `internal/api/storage_io_controller.go`
  的 `taskArchiveOpenRate`：`duration_ms` 缺席时它退回 `time.Since(*task.StartedAt).Milliseconds()`，
  而用例里那条封面运行从开跑到断言不足 1 毫秒，毫秒化之后是 0，于是被判成「这一段量不出」、
  速率返回 0。跑得快就红，跑得慢反而绿。
- **已做：** 没有动它——它不在本票范围内（本票只改采样那一格的名字，一行都没碰这条路径）。
  用 `git archive 1208a05` 把基点解到临时目录、跑同一条用例 `-count=50`，**基点上同样红、
  同样的消息**，因此确认它是继承来的。本票自己的门禁除这一条之外全绿（前端 75 文件 814 用例、
  build 与 lint 干净；`go build`/`go vet` 干净；契约无漂移；doc-style 通过），而完整的
  `go test ./...` 也确实绿过一整轮。据此把这个判断交回给工头。工头独立复现：在集成分支
  `e17bcc1`（不含本票任何改动）上跑 `-count=30`，**30 中 6 红**、消息一字不差，据此判定
  与本票无关、票面那条门禁算绿——票据因此勾满并改成 `done`，本条留在挂账里等人处置。
- **选项：** A 改用例：给那条封面运行显式上报一个 `duration_ms`（或把时钟注进去），
  让断言不再对一个不足 1 毫秒的真实窗口有期待（**推荐**，因为被测的产品判据「量不出就不报速率」
  本身是对的，坏掉的是用例——它要求墙上时间在两行代码之间走满 1 毫秒）｜ B 改产品代码：
  把窗口量到亚毫秒再判（`time.Since(...).Seconds()`），让极短的运行也报得出速率
  （代价：面板上会出现由一次几百微秒的观测外推出来的巨大速率，而那个数没有任何意义）。
- **不处理会怎样：** 留在仓库里的是一条掷硬币的用例：每个人、每条 CI 每跑一次全量都有可观的
  概率见红，而它红起来与一个真缺陷长得一模一样——下一个撞上的人还要再查一遍。
- **归谁裁：** 用户
- **状态：** open
## D1 · 票 01 · 显示名进了匹配串，而 SQLite 的 `LOWER` 只折 ASCII、Go 侧折的是整个 Unicode

- **问：** 两处谓词（`taskstore.runFilterClause` 与 `taskstore.taskFilterClause`）把查询串在 Go 侧经
  `strings.ToLower` 折过一遍，列侧用 SQLite 的 `LOWER`。改口径之前匹配的三格全是本仓自己生成的
  ASCII 标识串，两种折法看不出差别；换进来的作用域显示名是**用户起的名字**，差别就露出来了。
  实测：显示名 `Éditions Manga` 之下，`éditions`、`ÉDITIONS`、`Éditions`、`editions` 四种打法**一个都不中**
  （`Éditions` 也不中：Go 把查询折成 `éditions`，而列侧的 `É` 原样留着）；同一行搜 `manga` 正常命中。
  中日文没有大小写，不受影响；带重音的拉丁文、西里尔、希腊字母的库名会踩到。票面没提这一格。
- **已做：** 按现状落地，并在 `taskstore.keywordClause` 的 doc 里写明这是**单边折叠**：
  「SQLite 的 LOWER 只折 ASCII、调用方折的是整个 Unicode，显示名里的大写非 ASCII 字母两边对不上，
  照着界面原样打也不中」。没有为它加用例——加了就等于把这个行为钉成规格，而它正是待裁的那一边。
- **选项：** A 接受，只把边界写进注释（推荐，因为本仓主要语种是中日英，三者都不受影响；
  而真修要么加一列要么改驱动，两条都比这张票大得多）｜
  B 给 modernc.org/sqlite 注册一个 Unicode 版 `LOWER`，让列侧与 Go 侧同一套折法｜
  C 写入时另存一列 `scope_name_folded`，谓词判那一列——查询快，但多一列要跟着运行行一起迁移与维护。
- **不处理会怎样：** 名字里有大写非 ASCII 字母的资料库，用户按名字搜不到自己的运行，
  而界面上那个名字明明就写着。没有报错，只是搜不着。
- **归谁裁：** 下一次结算
- **状态：** open

## D2 · 票 01 · 显示名是运行落库那一刻的快照：库改名之后，历史运行搜不到新名字

- **问：** `runs.scope_name` 在启动点由 `lib.Name` / `series.Name` 一次性写死（见
  `internal/api/controller_library.go` 与 `internal/api/cover_run.go` 的 `ScopeName` 赋值），此后不再刷新。
  搜索改判这一列之后，把资料库从「旧名」改成「新名」，搜「新名」找不到改名前的运行、搜「旧名」
  反而找得到。改口径之前判的是任务键，键同样是快照，所以这不是新问题——但它第一次变得**用户看得见**，
  因为用户现在打的是界面上那个（已经改过的）名字。票面只说「打库名筛得到该库的运行」，
  没说改过名的库算哪一种。
- **已做：** 按快照落地，不在查询时解析。理由是 `task.Run.ScopeName` 的 doc 明写它是**过渡期**字段、
  最终归属未定（身份行上的一列，还是渲染时按作用域解析），这张票不该替那个决定选边。
- **选项：** A 保持快照（推荐，因为它与显示名在界面上的来源一致——任务中心那一行显示的也是这个快照，
  搜得到的与看得见的是同一个串）｜
  B 查询时按 `t.scope` + `t.scope_id` 连库表/系列表取当前名字来判——搜的是最新名字，
  但与那一行**显示**的名字对不上，且给两处谓词各加一次连表。
- **不处理会怎样：** 改过名的资料库，其历史运行只认旧名。选 B 则相反：只认新名，而界面上写的是旧名。
- **归谁裁：** 下一次结算
- **状态：** open

## D3 · 票 01 · 顺路撞见：`TestStorageIOCoverRateComesFromTheCoverRun` 整包连跑时偶发失败

- **并入 D63。** 同一件事，以 **D63** 为准：那条带着根因（`taskArchiveOpenRate` 在 `duration_ms`
  缺席时退回 `time.Since(StartedAt).Milliseconds()`，用例里那条封面运行不足 1 毫秒就被断言）
  与工头在集成分支 `e17bcc1` 上 `-count=30` 的实测（30 中 6 红），比本条那一次观察扎实。
  本条不删、只降为指针，免得已经引用过 D3 的地方悬空。由票 01 的实现者提出合并，工头在合并时执行。
- **已做：** 工头在合并票 01 的分支时把本条正文降为指向 D63 的指针，不删 id。
- **归谁裁：** 用户（随 **D63** 一并裁，本条不单独成议题）
- **状态：** open

## D11 · 票 02 · 按**任务键**取运行的那两个函数：留在生产里还是搬进用例

- **问：** 票面第八条明说 `RunStatus.Key` 与 `runs` 上那一列本票不动、归票 03。但重试撤走之后，
  `internal/api/task_engine.go` 的 `latestRunFilterFor` 与 `latestRunByKey` 在生产侧**一个调用方都没有了**
  ——它们唯一的用户是用例脚手架（`task_seed_test.go` 的 `controlByKey` / `runControlRequest`、
  `task_engine_seam_test.go` 的 `currentTask`）。票答不了这两个「只剩用例在用的生产函数」该待在哪。
- **已做：** 把两个函数原样搬进 `internal/api/task_seed_test.go` 的「按**任务键**寻址（仅用例）」一节，
  生产侧因此一处都不再写 `task.RunFilter.Key`（`grep -rn 'RunFilter{' internal --exclude '*_test.go'` 可验）。
  一行逻辑都没改，只换了文件。
- **选项：** A 搬进用例文件（推荐，因为派工时明写「把重试这个读者撤彻底——留半个，03 就删不掉」，
  而搬完之后「生产不按键取数」是一条 grep 得出来的不变量）｜
  B 原样留在 `task_engine.go`，让票 03 连同列一起带走。
- **`/code-review` 两条反对，都记在这里：** ①规格的「删除」清单把「按键取最近那一条的取数函数」
  划给了删键那张票，搬家等于让票 03 改去 `_test.go` 里找；②`latestRunByKey` 是生产类型
  `*taskEngine` 上的方法，声明在 `_test.go` 里意味着这个类型的方法集在测试构建下与生产不同。
  两条都不改结论——派工那句话更靠前——但翻案成本仍是一次搬回去。
- **不处理会怎样：** 无论选哪边行为都一样。选 B 的话，`task_engine.go` 里长期留着两个没有生产调用方
  的导出面，读的人得自己 grep 一遍才知道它们只服务用例。
- **归谁裁：** 票 03
- **状态：** open

## D12 · 票 02 · 「没有可重试的运行」是复用「任务不存在」，还是自成一条

- **问：** 票面第四条要求「没有可重试的运行」回 404 而不是 500，但没说这一句与既有的
  `errTaskNotFound` / `"Task not found"` 是不是同一句。改成按**终态**取之后，零行有两种来路：
  这个任务 id 压根不存在，或者它存在、只是一次都还没跑完（正跑着第一次）。
- **已做：** 两种来路**分开答**。新开一条哨兵 `errNoRetryableRun`（`internal/api/task_engine.go`），
  并在一条终态运行都没挑出来时多问一次 `runStore.LoadTasks`：任务不在就是 `errTaskNotFound` +
  `"Task not found"`，在就是 `errNoRetryableRun` + `"No finished run to retry"`。两者都是 404。
  多的那次查询只落在失败路径上。
- **选项：** A 分开答，失败路径上多一次任务查询（推荐，因为两句话的意思不一样，而说错的那一句
  会把用户支去查一个并不存在的数据丢失）｜ B 只加哨兵、不多查，两种来路共用
  `"No finished run to retry"`（省一次查询，代价是对一个根本不存在的 id 说「它没跑完过」）｜
  C 复用 `errTaskNotFound`，两种来路都答「Task not found」（哨兵少一条、映射表不动，
  代价是对第二种说了假话——而第二种正是票面第四条要修的那个局面）。
- **不处理会怎样：** 留在代码里的是一条只有重试端点用的哨兵、一句新英文文案，以及失败路径上
  那一次 `LoadTasks`（前端不读文案，走的是自己那条 i18n 兜底）。翻案是删掉哨兵与那次查询。
- **归谁裁：** 用户
- **状态：** open

## D13 · 票 02 · 一条终态运行都没有的任务，重试从 202 变成 404

- **问：** 票面第三、四条合起来推出一个票面没有明写的行为变化：一个**只有**活动运行（或只有排队中）
  的任务，此前重试回 202（按「最近那一条」拿到那条在跑的、照它的入参再发起一次，落成排队），
  此后回 404——它一条跑完的运行都没有，没有可重放的东西。既有两条用例正是踩在这个场景上
  （`TestRetryOfAnActiveTaskQueues`、`TestRetryTaskErrorSemantics` 的「运行中的任务」一段），
  票答不了它们该改场景还是该改断言。
- **已做：** 认这个行为变化。两条用例改的是**场景**不是断言：各自先播一条终态运行再播那条活动运行，
  「重试撞上活动态只会排队、不会被起两遍」那句话原样守住。
- **选项：** A 没有终态运行就 404（推荐，因为票面的立论就是「重试的意思是那次跑完的、再跑一次」；
  而且界面上重试按钮本就只在 `!isActiveRunStatus(lastRun.status)` 时才画，用户点不到这一格）｜
  B 补一条兜底：没有终态运行时退回取最近那条活动运行（好处：老客户端行为不变；
  代价是把票要拆掉的那个不确定寻址又请回来了一半——退回去拿到的那一条仍然由序号说了算）。
- **不处理会怎样：** 留在代码里的是一条对**直接打 API** 的老客户端可见的行为变化
  （界面上没有入口能触发）。翻案是在 `snapshotForRetry` 的零行分支上加一次退回查询。
- **归谁裁：** 用户
- **状态：** open

## D14 · 票 02 · ADR 0007 的「尚未实施」状态行本票没有改

- **问：** `docs/adr/0007-task-key-retires-from-addressing.md` 顶上写着「状态：已裁决，尚未实施」，
  正文那句「分家落地之后仍剩一处按键寻址：重试端点 `POST /api/system/tasks/{taskKey}/retry`、
  `RunFilter.Key` 与 `latestRunFilterFor`、……」列的是这批票要拆的**整份清单**。本票拆掉了其中的
  重试端点那一处，另外几处分属票 01 与票 03。票答不了这行状态该由谁改。
- **已做：** 一个字没动。本票只落地清单里的一项，改成「已实施」是假的，改成「部分实施」又要
  在正文里逐项标注谁落地了——那份逐项状态在批次跑完之前只会是错的。
- **选项：** A 留给这批票的最后一张（推荐，因为 ADR 的状态说的是这条决定整体落没落地，
  而它由 01、02、03 三张合起来才成立）｜ B 本票就改成「部分实施」，正文逐项标注。
- **不处理会怎样：** 在票 03 收工之前，ADR 上写着「尚未实施」而重试端点其实已经改完了。
- **归谁裁：** 票 03
- **状态：** open

## D15 · 票 02 · 端点路径变了，`CHANGELOG.md` 本票没记

- **问：** `AGENTS.md`「Commit & Pull Request Guidelines」写着「For user-visible changes, update
  `CHANGELOG.md` in the same batch」，而本票改掉了一条对外端点的路径
  （`POST /api/system/tasks/{taskKey}/retry` → `{taskID}`），并新增了一句 404
  （见 D13：只有活动运行的任务从 202 变 404）。两处对**直接打 API** 的客户端都可见。
  但派工时明确交代「本票不需要动 `CHANGELOG.md`（票面没要求）」，票面十条验收里也确实没有这一条。
  两条指令指向相反的方向，票自己答不了。
- **已做：** 先按派工走、一个字没动，回报时把矛盾交了上去；**工头当场推翻了自己那句「不需要」**，
  判定要记、且记进 `v1.6.2 — 2026-09-09`。据此补了一条（「点了重试，重跑的却不是刚失败的那次」），
  照该节写法：先一句用户视角的改动，再在「说明」里写升级要不要动手——那里明写直接调这条 API 的
  脚本要改路径，且只有在跑 / 排队运行的任务返回从 202 变 404。
- **工头的理由（原样记）：** `AGENTS.md` 第 37 行是仓库常设规矩，票面没提不等于不适用；
  更硬的是同批**票 07** 今天刚立的先例——它同样是一次对外字段变更、同样只有本仓前端消费，
  仍记进了 `v1.6.2 — 2026-09-09` 那一节。而本票还带着 D13 那个 202→404，比纯改名更往外。
  工头明说自己不是最终裁决人，因此本条**仍留 open**，用户可以推翻。
- **选项：** A 本票就补一条，写明路径与那句新 404（**已采纳**——工头裁的，理由见上：
  仓库常设规矩 + 同批票 07 的先例）｜ B 整批收口时由一条记这批「按对象寻址」的合并条目
  （我原先推荐的那条：七张票改的是同一件事，01 与 03 还会继续动同一组端点，本票单记一条、
  收口时可能还要再合并一次）。
- **不处理会怎样：** 留在文件里的是 `v1.6.2 — 2026-09-09` 下多出的那一节。
  翻案是删掉它、留给收口时合并着写。
- **归谁裁：** 用户
- **状态：** open

## D21 · 票 03 · D11 的裁决：那两个按键取数的函数删了，用例的把手换成一张「启动登记表」

- **问：** D11 把 `latestRunFilterFor` / `latestRunByKey` 搬进了 `internal/api/task_seed_test.go`，
  归本票裁。`RunFilter.Key` 一删，这两个函数连编译都过不去，因此「留在哪」不再是问题；
  真问题是**用例还要不要以任务键为把手**——`lastPublishedTask` / `currentTask` / `pauseByKey`
  这一族约 250 个调用点全都传一个键，而契约与库上都没有它了。
- **已做：** 两个函数删掉，换成按**任务 id** 取数的 `latestRunFilterForTask` / `latestRunForKey`
  （都是普通函数，不再是 `*taskEngine` 上的方法——D11 记的第二条 review 反对因此自然消解）。
  用例的把手仍是键，桥是 `task_seed_test.go` 里的 `launchedTaskIdentities`：播种时自动登记，
  直接调生产启动点的装置显式登记一次（`rememberTaskKey`）。登记的键与身份**两样都是启动那一刻
  手里就有的**，不是从键反解身份。
- **选项：** A 登记表（**已采纳**，因为它把改动收在 2 个脚手架文件 + 约 15 处登记里，
  而调用点一个字不用改；且「谁登记谁负责」比反解诚实——外部库那两类的键带着会话 id，反解不出来）｜
  B 约 250 个调用点全部改成传 `TaskIdentity`，登记表不要（最彻底，也最贴「按对象寻址」这条立论，
  代价是 26 个用例文件的大面积改写，与本票的正题无关）。
- **不处理会怎样：** 无论选哪边行为都一样。选 A 留下的是一张测试侧的键→身份表，
  下一个读的人可能会问「键不是退场了吗」——注释里已写明它记的是「本次用例启动了什么」。
- **归谁裁：** 用户
- **状态：** open

## D22 · 票 03 · `task.Run.Key` 与 ctx 装饰的签名：票面没点名，掉列逼出来的

- **问：** 票面第 4 条只点名删 `RunStatus.Key` 与 `RunFilter.Key`，没提领域侧的 `task.Run.Key`。
  但列一掉，这个字段就没有了落盘去处：`internal/task/start.go` 的队列放行是**从库里读回**运行行
  再启动任务体的（`releaseQueuedLocked` → `beginLocked`），而 ctx 上那个任务键取的正是 `run.Key`。
  留着它等于「排过队的那些任务体，日志里静静地少掉任务键那一格」——恰好破坏 ADR 0007 要保的那半。
- **已做：** 删掉 `task.Run.Key`，并把 `Config.DecorateRunContext` 的签名从
  `func(ctx, Run)` 改成 `func(ctx, Run, RunSpec)`，键改从**运行声明**上取——它本来就是唯一来源。
- **选项：** A 删字段、装饰函数多收一个 RunSpec（**已采纳**，因为它让代码与 ADR 0007
  「键唯一的来源是启动点写下的 RunSpec.Key」逐字对上，且排队那条路自动正确）｜
  B 留着 `Run.Key`、只是不落盘，由 `beginLocked` 在调装饰函数前从 spec 补一次
  （改动更小，代价是领域结构体上多一个「读回来永远是空」的字段——正是本仓注释一再警告的形状）。
- **不处理会怎样：** 选 B 的话，任何一个从 `ListRuns` 拿到运行再读 `.Key` 的新代码都会拿到空串，
  而它不会报错。翻案是把字段加回去、装饰函数改回两个参数。
- **归谁裁：** 用户
- **状态：** open

## D23 · 票 03 · 前端还有两个按**任务键**认领推送的读者，ADR 与票面都没列

- **问：** ADR 0007 的删除清单是后端的六项，票面第 4、5 条也只说契约与快照。但
  `web/src/components/layout/useTaskBubbles.ts` 拿 `run.key` 当侧边栏气泡的身份，
  `web/src/pages/library/hooks/useExternalLibrary.ts` 拿 `progress.key === externalScanTaskKey`
  认领「这一帧是不是我这次外部库扫描」。字段一删这两处静默失效——气泡再也不出现、
  外部库扫完之后那一页不刷新，都是用户看得见的。票面说「用户这一侧不该有任何可见变化」，
  但没说这两处怎么办。
- **已做：** 气泡的身份改用 `task_id`（同一件事的历次推送更新同一个气泡，语义与从前一致）；
  外部库那处改判 `type` 加载荷里的 `params.session_id`——会话 id 由启动点作为重启入参落下，
  每一帧运行快照都带着它，比键更准（键本身就是「类型 + 会话 id + 库 id」拼出来的）。
- **选项：** A 各自改判（**已采纳**，两处都有比键更准的现成字段）｜
  B 让这两条端点改返回 `task_id`，前端按 id 认领（更统一，但要动两条对外响应体，超出本票）。
- **不处理会怎样：** 不改就是两个功能静默坏掉，因此这不是「不处理」的选项；
  记在这里是因为**票面与 ADR 的删除清单漏了前端这两个读者**，下一张动契约的票该照这条查一遍。
- **归谁裁：** 下一次结算
- **状态：** open

## D24 · 票 03 · 票 01 漏了内存 Store 替身：关键词串还判在键上

- **问：** 票 01 把生产的 `keywordClause` 从 `task_key || message_code || error` 换成了
  `scope_name || …`，但 `internal/task/store_memory_test.go` 那个内存 Store 替身没跟着换——
  它的 `matchesFilterLocked` 与任务清单那一支仍拿 `run.Key` 拼匹配串。两套口径分岔了，
  而领域包的用例是对着替身跑的。
- **已做：** 一并换成 `run.ScopeName`（生产那份的口径），顺手删掉替身里的 `filter.Key` 分支。
  改完 `go test ./internal/task` 全绿——说明领域包里没有任何用例真在靠「按键搜得到」这条行为。
- **选项：** A 本票顺手改（**已采纳**，`Run.Key` 一删这里连编译都过不去，绕不开）｜
  B 记一条留给 01 的收尾（票已合并，等于没人收）。
- **不处理会怎样：** 已经不可能不处理。留这条是为了记下**01 有一处漏改**，
  合并抽检时值得回头看一眼它还漏没漏别的。
- **归谁裁：** 下一次结算
- **状态：** open

## D25 · 票 03 · 外部库与 ComicInfo 三条端点的响应体里仍有 `task_key`

- **问：** 本票把**运行快照**上的键删干净了，但另有三处对外响应体仍显式返回一个叫 `task_key`
  的字段：`internal/api/external_controller.go` 的建会话与传输两条，
  `internal/api/comicinfo_controller.go` 的回写一条。它们不是 `RunStatus`，不在 ADR 0007 的
  删除清单里，也不在票面十三条里；`web/src/pages/library/types.ts` 与 `useExternalLibrary.ts`
  仍按它取值。改口径之后它们就是「契约上还剩三处任务键」。
- **已做：** 一个字没动，只把前端**用它来认领推送帧**的那半改掉（见 D23）——
  这三条响应体现在的作用退化成「我发起过这一步」的一个标记，键值本身不再被拿去比对。
- **选项：** A 原样留着（**已采纳**，票面没点名，动它要改两条对外端点与前端三处，
  且 ADR 0007 判的是「键退出**寻址**」，一个只当标记用的返回值不算寻址）｜
  B 一并改成返回 `task_id`，前端按 id 认领（更彻底，属于另一张票）。
- **不处理会怎样：** `grep -rn 'task_key' internal/ web/src` 之后仍会看到这三处，
  读的人要多问一句「这不是删干净了吗」。翻案是一次改两条端点加前端三处。
- **归谁裁：** 用户
- **状态：** open

## D26 · 票 03 · 用例按身份取数之后，外部库那两类分不出两次会话

- **问：** `/code-review` 的规格轴指出：`task_engine_seam_test.go` 的 `snapshotBelongsTo` 比的是
  **身份四要素**，而外部库扫描 / 传输的**任务键**带着会话 id、身份里没有它（ADR 0007 原话）。
  于是 `lastPublishedTask` / `publishedCountFor` / `belongsToKey` 这一族在外部库场景下**分不出
  同一个库的两次会话**，而调用处读起来仍像在指认某一个键。同一轴还指出一处口径过宽：
  「对外契约上不再有任务键」只对**运行快照**成立——外部库两条与 ComicInfo 一条响应体仍返回它（见 D25）。
- **已做：** 口径那处收紧了（`CONTEXT.md` 词条、`internal/taskstore/schema.go` 的掉列注释都改成
  「运行快照上」）。分辨力那处**没有改代码**，只在登记表的 doc 上写明「分辨力到身份为止」：
  身份是**任务**这一层，而同一个任务同一时刻最多只有一条活动运行——用例里从没有过
  「同一身份的两个会话同时在跑」这种摆法，现有用例因此一条都没有被削弱到。
- **选项：** A 写明边界、不动代码（**已采纳**，因为要真区分两次会话，用例就得按**运行 id** 取数，
  而运行 id 要等启动之后才知道，等于把这一族辅助函数全部改成两段式）｜
  B 登记表改记「键 -> 运行 id 列表」，`trySeedTask` 与各装置在启动后登记 `launched.Run.ID`
  （分得出会话，代价是生产启动点起的运行拿不到 id——`taskEngine.Run` 只回 error）。
- **不处理会怎样：** 将来若真写一条「同一个库的两次外部库会话」的用例，它会静默地对着另一次会话
  的快照断言。翻案是把登记表换成按运行 id 记。
- **归谁裁：** 下一次结算
- **状态：** open

## D27 · 票 03 · 侧边栏气泡那条深链的死参数顺手删了

- **问：** `web/src/components/SidebarTaskBubble.tsx` 的链接原本是
  `/ops?tab=tasks&task=<任务键>`。键一删这个参数就没有值可填了，而票面只说删字段、没说改 URL。
- **已做：** 改成 `/ops?tab=tasks`。先查了基线：`web/src/pages/Ops.tsx` 只读 `tab` 与 `run_id`
  两个参数，`task` 从来没有读者——它是「任务中心与系统日志合并到 /ops」那次改动之后留下的死参数，
  因此删掉没有任何行为损失。
- **选项：** A 删掉（**已采纳**，一个填不出值的死参数留着只会让下一个人去找它的读者）｜
  B 改填 `task_id`，顺手把 Ops 页那条深链补上（那是「点气泡直达那一条」的功能，票面没有）。
- **不处理会怎样：** 留着就是 `?task=undefined` 这类形状。选 B 是新功能，属于另一张票。
- **归谁裁：** 下一次结算
- **状态：** open

## D31 · 票 04 · 改名扫到哪：把类型名拼进自己名字的那几个函数

- **问：** 票面第 1 条只点名「运行快照结构体」。但 `internal/api` 里有几个标识符把旧类型名
  逐字拼在自己名字里：`taskEngine.listRunStatuses`、`taskEngine.runStatusFrom`、
  用例里的 `TestRunStatusTracksScrapeMetricsAndLabels`。它们不是那个结构体，却会让
  `grep RunStatus` 继续在 `internal/api` 里命中，也就继续答不出「这是快照还是状态枚举」。
- **已做：** 这三个跟着改（`listRunSnapshots` / `runSnapshotFrom` /
  `TestRunSnapshotTracksScrapeMetricsAndLabels`），理由与 D51 采纳的那条一样——按「是不是那个
  结构体本身」切一刀，会在同一个包里留下两个名字。`runSnapshotFrom` 里那个装返回值的局部变量
  原名 `status`，一并改成 `snap`：它与同一个复合字面量里的 `Status:` 字段正是票面立论说的
  「同一个函数里看见两个」。**没跟着改的三类**：`statusesFrom` / `firstStatusFor` /
  `latestStatusByKey`（名字里只有 `Status`，没有类型名）、`waitForRunStatus`（它等的是
  状态串，对上的是留下不改的 `task.RunStatus`）、`web/src/utils/runStatus.ts` 那一族
  `isActiveRunStatus` / `isLiveRunStatus` / `isTerminalRunStatus`（同理，判的是状态串）。
- **选项：** A 把逐字拼了类型名的一并改（**已采纳**，改完 `internal/api` 里 `grep '\bRunStatus\b'`
  只剩包限定的 `task.RunStatus`，全是那个状态枚举，读的人不必再分辨）｜
  B 严格按票面只改结构体，那三个留旧名（diff 更小，代价是包里长期并存两套叫法）。
- **不处理会怎样：** 选 B 的话，`statusesFrom` 这一族与 `listRunStatuses` 会一直混在同一个文件里，
  下一张动这批文件的票（05）要再撞一次同样的岔口。翻案是一次符号改名。
- **顺带一记：** 票面的四个数没有一个对得上实测。给命令不给数：
  `grep -rho --include='*.go' '\bRunStatus\b' $(grep -rln --include='*.go' '\bRunStatus\b' internal/api cmd | grep -v _test.go) | wc -l`
  量非用例侧，`grep -rn '\bRunSnapshot\b' web/src | wc -l` 量前端侧。按实测做的，不按票面那几个数。
- **归谁裁：** 下一次结算
- **状态：** open

## D32 · 票 04 · 状态文案的 i18n 键实为九条，第九条是「全部状态」

- **问：** 票面第 4 条写「八条运行状态文案的 i18n 键」。八条状态之外还有第九条
  `logs.taskStatus.all`（全部状态 / All statuses），它不是运行状态，是筛选框里的「不筛」选项。
  票面没说它算不算。
- **已做：** 九条一起搬到 `logs.runStatus.*`。理由是拼串处只有一个前缀——
  `web/src/components/tasks/TaskCenter.tsx` 里既有 `t('logs.runStatus.all')` 这样的定值键，
  也有 ``t(`logs.runStatus.${run.status}`)`` 这样的拼串；把 `.all` 留在旧前缀等于让同一个
  下拉框的九个选项挂在两个键族下。
- **选项：** A 九条一起搬（**已采纳**，同一个控件的键族不该被切开）｜
  B 只搬八条状态，`logs.taskStatus.all` 原地不动（更贴票面字面，代价是留下一个只剩一条词条的
  `logs.taskStatus.*` 键族，两份 locale 里都得留着）。
- **不处理会怎样：** 无论选哪边用户看到的文案一个字不变（两份 locale 的值都没动）。
  翻案是把两份 locale 与一处 `option` 里的四行键名改回去。
- **归谁裁：** 下一次结算
- **状态：** open

## D33 · 票 04 · 浏览器自定义事件改叫什么：票面只给了「与 SSE 事件名同形」

- **问：** 票面第 5 条要求那条浏览器内部事件「改名与 SSE 事件名同形」，但没给名字。
  同一处已有的兄弟事件叫 `manga-manager:run-push`，而后端的两个 SSE 前缀是
  `internal/api/task_engine.go` 的 `runSnapshotEventPrefix`（`run_snapshot:`）与
  `runLiveEventPrefix`。「同形」有两种读法：与兄弟事件同名，或与 SSE 前缀同名。
- **已做：** 取 `manga-manager:run-snapshot` / `manga-manager:run-snapshot-override`，
  对齐 `runSnapshotEventPrefix`。与兄弟事件同名这条读法直接不成立：两条事件的 `detail`
  不是一份东西——`run-push` 送的是整个信封（`RunPush`），这一条送的是信封里的
  `frame.run`（`RunSnapshot`），同名会让两边的监听方互相收到对方的形状。
- **选项：** A 取 SSE 前缀的名字（**已采纳**，它与载荷类型逐字同名，这一跳与上一跳因此讲同一个词）｜
  B 取 `manga-manager:run-progress`（更贴「进度」这个旧语义，代价是仓库里从此有第三个词
  指同一份载荷）。
- **不处理会怎样：** 无论选哪边行为一样，派发方与两个监听方都在 `web/src` 里、一次改到底。
  翻案是四个字符串字面量。
- **归谁裁：** 下一次结算
- **状态：** open

## D34 · 票 04 · 覆盖变体那条事件没有任何派发方

- **问：** `web/src/components/layout/useTaskBubbles.ts` 监听
  `manga-manager:run-snapshot-override`（改名前是 `manga-manager:task-progress-override`），
  但 `grep -rn 'run-snapshot-override' web/src` 只有这一处监听，全仓没有任何派发点。
  它上面那句注释说的「系列详情页在触发操作后乐观更新气泡」这条路已经不存在了。
- **已做：** 只改名，一行没删。票是纯改名票，删一条监听是行为改动。
- **选项：** A 原样改名留着（**已采纳**，本票不改行为；且留着的成本是一个 `useEffect`）｜
  B 连同 `handleOverride`、那段注释与 `RunSnapshotPayload` 里只有它用得到的字段一起删掉
  （代码少一截，但它是行为改动，得有用例证明确实没人派发）。
- **不处理会怎样：** 留着的是一个永远不触发的监听器加一段描述不存在的路径的注释，
  下一个读 `useTaskBubbles` 的人要花一次全仓 grep 才能确认它是死的。
- **归谁裁：** 用户
- **状态：** open

## D35 · 票 04 · 改名扫到文档：ADR 与 `AGENTS.md` 跟着改了，`docs/changelog/` 没有

- **问：** 票面九条只讲代码与契约，没讲文档。而 `AGENTS.md` 的重生契约那句拿
  `api.RunStatus` 当例子，`docs/adr/0007-task-key-retires-from-addressing.md` 两处写
  `RunStatus.Key`。`docs/changelog/` 里没有 `RunStatus`，有的是更早那一代的 `TaskStatus`
  （`grep -rn 'TaskStatus' docs/changelog`）——正是上一次同类改名留下的先例。
- **已做：** 改 `AGENTS.md` 与 ADR 0007，不动 `docs/changelog/`。依据两条：
  `docs/agents/doc-style.md` 的「不会腐坏的引用」说文档引用代码写符号名就是为了「重命名时它跟着改」；
  而上一次同类改名（`TaskStatus` → `RunStatus`）留下的先例正是——`AGENTS.md` 跟着改了，
  changelog 里的 `TaskStatus` 一个没动。changelog 的读者是用户、内容是版本史，改它等于改历史。
- **选项：** A 活文档跟着改、历史不动（**已采纳**）｜
  B ADR 也算历史、一并不动（代价是 ADR 0007 里的 `RunStatus.Key` 会指向一个仍然存在、
  却从来没有 `.Key` 字段的类型——`task.RunStatus` 那个状态枚举，比没改还容易误导）。
- **不处理会怎样：** 选 B 的话，`grep RunStatus.Key` 找不到任何代码，而 ADR 说它「已删除」，
  读的人分不出是删干净了还是文档烂了。翻案是三处字符串。
- **归谁裁：** 下一次结算
- **状态：** open

## D36 · 票 04 · 被推翻的那条关键决定只写进了提交信息，没有就地标注

- **问：** 本票推翻 `task-run-model` 规格的关键决定 16（那条逐字写着把 `TaskStatus` 改名为
  `RunStatus`）。票面第 8 条只要求「提交信息写明它推翻了关键决定 16 及原因」，没要求去动那份规格。
  于是那份规格里现在留着一条已经不成立的决定，而唯一记着它被推翻的地方是一条提交信息。
- **已做：** 照票面办——只写进提交信息，`.scratch/task-run-model/spec.md` 一个字没动。
  那是另一个 effort 的规格，改它伸出了本票。
- **选项：** A 只写提交信息（**已采纳**，票面明写，且改别的 effort 的规格是越界）｜
  B 在关键决定 16 下面补一行「本条已被 address-by-object 票 04 推翻，见该票」
  （代价是动了一份不归本票管的文件，好处是读那份规格的人当场就看得到）。
- **不处理会怎样：** 那份规格会一直说「`TaskStatus` → `RunStatus`」，而代码里叫 `RunSnapshot`。
  下一个照那份规格核对符号名的人会以为代码漂了。翻案是加两行。
- **归谁裁：** 用户
- **状态：** open

## D37 · 票 04 · `RunSnapshot` 这个名字在仓库里现在也有两个主人

- **问：** 票面指定的新名字 `RunSnapshot` 已经被占用了一半：`internal/task/control.go` 上有
  `func (e *Engine) RunSnapshot(ctx, runID) (Snapshot, error)`，取一条运行此刻的领域快照。
  改完之后 `internal/api` 里既有类型 `RunSnapshot`，又有 `…engine.RunSnapshot(…)` 这样的调用
  （`internal/api/task_queue_test.go` 与 `internal/api/task_retry_dispatch_test.go` 里的用例装置）。
  票面没预见到这一点——它预见到的是本包内两个**类型**同名。
- **已做：** 照票面用 `RunSnapshot`，`task.(*Engine).RunSnapshot` 一个字没动。两者的歧义
  比原来那对弱一档：一个是类型、一个是方法，语法位置不同（后者永远写成 `x.RunSnapshot(...)`），
  且分处两个包；实测那几处调用与类型引用没有落在同一个函数里。
- **选项：** A 原样（**已采纳**，票面指定了名字，且这一对分得开）｜
  B 把领域侧那个方法改名（`task.(*Engine).SnapshotOf` 之类），四个调用点全在用例里，
  改动比重命名类型小一个数量级——真要收干净该动的是它，不是本票刚落地的类型名。
- **不处理会怎样：** `grep -rn '\bRunSnapshot\b' internal` 会同时命中一个类型和一个方法，
  读的人要多看一眼接收者。翻案（选 B）是四处调用点加一处声明，不碰本票的成果。
- **归谁裁：** 用户
- **状态：** open

## D38 · 票 04 · 前端那条路的载荷名与接入函数名：`/code-review` 判本票口径没盖住

- **问：** D31 给自己定的口径是「名字里逐字拼了旧类型名的才跟着改」。前端有两个标识符
  不满足这条口径，却是同一处误名：`web/src/components/layout/useTaskBubbles.ts` 里
  接事件载荷的 `TaskProgressPayload`，与它的接入函数 `ingestProgress`——
  `web/src/components/Layout.tsx` 在解构时把后者别名成 `ingestTaskProgress` 再调用。
  两个名字都说「任务进度」，而载荷是一份**运行快照**。`/code-review` 的两轴都点了这一处：
  规格轴判它「落在 D31 自定口径之外」，标准轴判在调用点起别名是「遮住不一致而不是消除它」。
- **已做：** 载荷类型改 `RunSnapshotPayload`，hook 上的方法改 `ingestRunSnapshot`，
  `Layout.tsx` 里那个别名整个撤掉——两端从此是同一个名字。用例
  `web/src/components/layout/useTaskBubbles.test.ts` 三处调用跟着改。
- **选项：** A 两端都改成 `ingestRunSnapshot`、别名撤掉（**已采纳**，事件叫 `run-snapshot`、
  载荷类型叫 `RunSnapshot`，接它的函数再叫「任务进度」就是本票要消灭的那种歧义）｜
  B 原样留 `ingestProgress` 与 `TaskProgressPayload`，只改事件名字符串（最贴票面第 5 条的字面，
  代价是派发点那一行同时出现 `run-snapshot` 与 `TaskProgress` 两个词）。
- **不处理会怎样：** 选 B 行为一样，留下的是「事件改了名、接它的东西没改」。
  翻案是四个文件里的一族标识符改名，不碰行为。
- **归谁裁：** 下一次结算
- **状态：** open

## D39 · 票 04 · 改名把两处结构体 tag 的对齐撑歪了，`gofmt` 没跟着跑

- **问：** 新名 `RunSnapshot` 比 `RunStatus` 长两个字符，`internal/api/controller.go` 的
  `RunPush.Run` 与 `internal/api/controller_series.go` 的
  `SeriesFailedTaskSummary` 那一族字段的 tag 对齐因此不再是 `gofmt` 的产物。基线 `e1b0e09`
  上这两个文件是干净的，是本次引入的。`.golangci.yml` 的 `formatters` 里开着 `gofmt`，CI 必红。
- **已做：** `gofmt -w` 那两个文件，改完 `gofmt -l ./cmd ./internal` 为空，
  `golangci-lint run` 也过。核对过 `gofmt` 只动了那两行的空格，没有顺带重排别的地方。
- **选项：** A 跑 `gofmt -w`（**已采纳**，工具认的格式没有第二种写法）｜
  B 手工补空格（同一个结果，但下一次改名还会漏）。
- **不处理会怎样：** 已经处理了。留这条是因为**纯改名的批量替换会撑歪对齐，而门禁清单里
  没有 `gofmt`/`golangci-lint` 这一步**——票 05 动的是同一批文件，同一个坑就在那儿等着。
  建议把 `gofmt -l ./cmd ./internal` 加进这批票的门禁。
- **归谁裁：** 下一次结算
- **状态：** open

## D41 · 票 05 · 另有两个函数把旧类型名逐字拼在自己名字里，票面那三个没点到它们

- **问：** 票面第 3 条点名三个函数（「取最近若干类型的运行」`latestTaskByTypes`、「给进度补料」
  `enrichTaskProgress`、「算至今暂停了多久」`taskPausedSoFar`）。但 `internal/api` 里另有两个把
  `TaskLimits` 逐字拼进自己名字的函数：`taskLimitsFromDomain`（`internal/api/task_model.go`）
  与 `Controller.taskLimitsForPath`（`internal/api/controller_tasks.go`，调用点分布在
  `internal/api/controller_library.go` 与 `internal/api/controller_test.go`，条数用
  `git grep -n 'taskLimitsForPath' 90e0489` 数）。票面没列它们。
- **已做：** 一并改成 `runLimitsFromDomain` / `runLimitsForPath`，口径与 D31 采纳的那条相同
  （名字里逐字拼了旧类型名的跟着改）。改完
  `git grep -n 'TaskLimits\|taskLimits' -- . ':!.scratch' ':!docs/changelog'` 为空。
- **选项：** A 逐字拼了类型名的一并改（推荐，因为按「票面点名没点名」切一刀，会在同一条调用链上
  留下两个名字——`runSnapshotFrom` 里紧挨着的两行就会一个叫 `runLimitsFromDomain`、
  一个叫 `taskLimitsForPath`）｜ B 严格按票面只改那三个，这两个留旧名。
- **不处理会怎样：** 选 B 的话，包里留着两个名字说 `taskLimits`、返回值却是 `RunLimits` 的函数，
  `grep -i tasklimits` 不再是零。翻案是两处符号改名。
- **顺带一记（票面两个数都不准，给命令不给结论）：** `git grep -n '\bTaskResult\b' 90e0489 --
  'internal/**/*.go' 'cmd/**/*.go' | wc -l` 量得 125（票面写「约 124 处」），其中非用例侧
  再 `| grep -v '_test.go' | wc -l` 得 76；`TaskLimits` 同法 Go 侧 14，前端
  `git grep -n '\bTaskLimits\b' 90e0489 -- web/src | wc -l` 得 4，合计 18（票面写「约 17 处」）。
  两个都在同一量级，按实测做的。
- **归谁裁：** 下一次结算
- **状态：** open

## D42 · 票 05 · 改到哪一层为止：符号名与它自己那句 doc 改了，形参与邻居没改

- **问：** 票面只写「相关函数名跟着改」，没说这次改名要不要带上形参与散文。改完之后同一批位置
  仍留着旧概念：`enrichRunProgress(task *RunSnapshot)` 与 `runPausedSoFar(task RunSnapshot, …)`
  （`internal/api/task_model.go`）的形参仍叫 `task`，而本文件 import 着
  `manga-manager/internal/task`——形参把包名遮住了；`taskArchiveOpenRate(task *RunSnapshot)`
  与 `taskMetricValue(task *RunSnapshot, key string)`（`internal/api/storage_io_controller.go`）
  连函数名带形参都还叫 task；`taskFailure`（`internal/api/task_run.go`）造的是 `RunResult`。
  用 `grep -rn --include='*.go' 'task \*RunSnapshot\|task RunSnapshot' internal/api` 看得全。
  `/code-review` 的标准轴另点了一处：`latestRunByTypes(types ...string)` 收的是**任务**类型
  （内部转 `[]task.Type`），叫 `latestRunByTaskTypes` 更诚实。
- **已做：** 改到**符号名 + 被改的那个符号自己那句 doc** 为止，两头都停在这条线上。
  往前一步是 `/code-review` 判的硬违反：改了名而 doc 散文仍按「任务」讲，抵触 `AGENTS.md`
  的「注释与文档只写**当前的结果**」，其中 `RunSpec.Limits` 那句还与 ADR 0004（上限落进
  `run_limits`、**按运行一行**）正面相抵，`runPausedSoFar` 里的「**取消中**的任务」则与
  `CONTEXT.md` 把**取消中**定义为运行状态相抵。因此四句跟着改：`enrichRunProgress`
  与 `runPausedSoFar` 的首句、后者 doc 里那句「取消中」、以及 `RunSpec.Limits` 的字段 doc。
  往后一步（形参、`taskFailure`、`taskArchiveOpenRate`、`latestRunByTaskTypes`）没做——
  它们不是被本票改名的符号，扫起来没有边界。
- **选项：** A 停在「符号名 + 它自己那句 doc」（推荐，因为这条线画得出来：**本票动过的每一个名字，
  它正上方那句话必须跟着对**；而形参与邻居函数的扫法没有边界，`taskIsActive`、`taskFilters`
  会一路带出来，它们没有一个在票面上）｜ B 连形参与邻居函数一起改（读起来更顺，代价是这张纯改名票
  的 diff 里混进一批票面没有的位置，且「改到哪里为止」得另立一条口径）。
- **不处理会怎样：** 留在代码里的是「函数叫 run、形参叫 task」的几个函数，读的人在同一个函数里
  同时看到形参 `task` 与包 `task`。翻案是一次形参改名，不碰行为。
- **归谁裁：** 下一次结算
- **状态：** open

## D43 · 票 05 · 顺路撞见：`TaskRuntime` 是个死掉的导出类型，而且名字也按旧概念取

- **问：** `internal/api/controller.go` 的 `type TaskRuntime struct`（装 context、cancel、
  `taskcontrol.PauseGate` 与 StartedAt）**一个引用都没有**：
  `git grep -n 'TaskRuntime' -- . ':!.scratch' ':!docs/changelog'` 只命中它自己那一行的声明。
  它描述的是**运行时句柄**——那是挂在一次**运行**上的东西，不是挂在任务上的，因此它同时是
  「按旧概念取的名字」的一例。它是本票七条勾完之后，任务子域里我找得到的最后一处同类矛盾（后端侧）。
- **已做：** 一个字没动。删一个导出类型不是改名，它是对包导出面的改动，超出这张纯改名票；
  跟着改名又等于给一个没有引用的类型换名字。
- **选项：** A 原样留着（推荐，因为在这张票里它只有「删」或「改名」两条路，两条都伸出票面；
  而留着的代价是一个结构体声明）｜ B 删掉（它没有引用，`go build`/`go vet` 不会有话说，
  但导出面少一个类型是对外可见的改动，该有它自己的票）｜ C 改名为 `RunRuntime` 之类
  （代价：给一个死类型换名字，读的人下次仍要 grep 一遍才知道它是死的）。
- **不处理会怎样：** 留在代码里的是一个谁都不用、名字还指着旧概念的导出结构体。
  下一个读 `controller.go` 的人要跑一次全仓 grep 才敢下结论。
- **归谁裁：** 用户
- **状态：** open

## D44 · 票 05 · 前端还留着同一类矛盾，而本批十张票没有一张覆盖它

- **问：** 本批把后端与契约扫干净了（票 04 另扫了 i18n 键、浏览器事件与那个载荷类型），
  但 `web/src` 里仍有一族按**任务**取名、装的却是一次**运行**的东西：
  `TaskAction`（`web/src/components/tasks/TaskCenter.tsx`，四个动作里暂停 / 恢复 / 取消作用在
  运行上，只有重试作用在任务上）、同文件的 `TaskRow`（画的是一张运行卡片）、
  `TaskBubbleEntry`（`web/src/components/SidebarTaskBubble.tsx`，按 taskId 归并、字段却是
  status/current/total 这些运行的数）、`TaskWithParams` 与 `TaskMessageSource`
  与 `getTaskMessage`（`web/src/i18n/task.ts`，渲染的是运行帧上那句文案）。
  用 `grep -rn -E '^(export )?(interface|type|function) [A-Za-z]*Task' web/src --include='*.ts'
  --include='*.tsx' | grep -v '\.test\.'` 看得全。
- **已做：** 一个都没改。票面第 6 条只要求「前端**该类型**的引用跟着改」——指的是
  `TaskLimits`，本票已改完（`web/src/api/generated.ts` 与
  `web/src/components/tasks/TaskCenter.tsx`）。上面这一族不在任何一张票的票面上。
- **选项：** A 本批就此收口，前端这一族留到下一个 effort（推荐，因为它们大多是**组件与视图**的
  名字而不是契约类型，改名会连着改一批 props 与用例，值得单独一张票；而且其中至少
  `TaskBubbleEntry` 与 `TaskCenter` 是否该叫「任务」本身有得争——气泡确实按任务归并）｜
  B 趁契约这次重生一起扫掉（一次改完，代价是把一批与生成契约无关的前端改动塞进本票，
  且「谁该保留 Task 前缀」要在前端再判一遍词汇表）。
- **不处理会怎样：** 留在仓库里的是「后端与契约已按对象命名、前端组件层仍按旧概念命名」这一层
  落差：读的人从 `RunSnapshot` 一路读到 `TaskRow` 时要自己接上。
- **归谁裁：** 用户
- **状态：** open

## D45 · 票 05 · `golangci-lint` 的共享缓存记着已删除的兄弟工作树，排除规则因此整片失效

- **问：** 本票门禁里 `golangci-lint run` 退 1、报 22 条（19 errcheck + 3 staticcheck），
  而**每一条的路径都是 `../abo-04/...`**——那棵工作树在票 04 合并后已经删掉，
  `git worktree list` 只剩主仓与本树。根因不是代码：`.golangci.yml` 杀掉这批告警靠的是
  **要读源码行**的两种机制——`exclusions.rules` 里那条 `source: '\.Close\(\)'`，
  与 `internal/logger/context_test.go` 里那两行 `//nolint:staticcheck`。
  golangci-lint 自己的缓存（`golangci-lint cache status` 报的
  `/Users/nicoer/Library/Caches/golangci-lint`，1.1 MiB，跨工作树共享）按文件内容命中了
  abo-04 那次跑的结果，回放出来的 issue 带着 abo-04 的绝对路径；文件已不存在，
  于是日志里刷满 `[runner/exclusion_rules] Failed to get line … from line cache`，
  排除与 nolint 一条都没生效，本该被吃掉的告警全漏了出来。
- **已做：** 用一份隔离缓存重跑一次（`GOLANGCI_LINT_CACHE=<临时目录> golangci-lint run`），
  结果 `0 issues.`、退 0。共享缓存一个字节没动，仓库里没有留下任何残留。
- **选项：** A 每棵工作树用自己的 `GOLANGCI_LINT_CACHE`（推荐，因为它对症——问题正是
  「按内容命中、位置却按路径回放」，一棵树一份缓存就不会串；代价是每棵新树第一次 lint 慢一轮）｜
  B 撞上了就 `golangci-lint cache clean`（一条命令、缓存只有 1.1 MiB，但它是事后补救：
  下一次删掉工作树之后同一个坑还在）｜ C 不管，靠「工作树别删」绕开
  （站不住：合并完就该删，而这次正是删干净之后才露出来的）。
- **不处理会怎样：** 下一个在这台机器上跑门禁的人（包括收尾的工头）会看到 22 条红，
  路径指向一棵不存在的树。它长得跟一次真的 lint 回归一模一样，而**代码是干净的**——
  查清楚要一次 `.golangci.yml` 与 `//nolint` 的对读。此前九张票没撞上，
  是因为那时兄弟工作树还在，读得到那些行。
- **归谁裁：** 用户
- **状态：** open
