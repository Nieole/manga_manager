# 挂账清单 · task-run-handle

跑票过程中冒出的非阻塞选择。流程不为它们停；由用户挑时机逐条处理。

D1–D8 已于 2026-08-31 逐条裁定，全部 resolved。D9 起由批次收尾后新开的跟进票留下，待裁定。

## D1 · 票 02 · `koreader.NewService` 从三参降为两参

- **问：** 磁盘作业改走**任务句柄**后，`Service` 不再持有 `*diskwork.Runner`，该形参与字段全无读取点，
  留着过不了 `unused`。票 02 没点名这一步，subagent 顺手删了（4 处调用点跟着改）。
- **选项：** A 收下，它是本票改动的直接后果（推荐，因为留着字段等于让 lint 红着过批次）｜
  B 补一张票单独记这次签名变更
- **不处理会怎样：** 已经落地了，不处理即默认收下；只是票据与实际改动范围对不齐一格。
- **状态：** resolved
- **已决（用户裁定）：** 选 A——收下，认定为「磁盘作业改走句柄」的直接后果，不补票。

## D2 · 票 02 · `newBackgroundTestEngine` 要不要收下 `diskWork` 形参

- **问：** code-review 提议给这个测试装置加 `diskWork` 形参，与 `newTaskEngine` 对齐；
  subagent 驳回了——32 个调用点全要改，而两处装置直接赋值 `e.diskWork` 已写在该函数 doc 里。
- **选项：** A 维持现状（推荐，因为收益不抵 churn，且 doc 已交代）｜
  B 批次收尾后单独做一次机械改名
- **不处理会怎样：** 测试装置有两种拿到 runner 的路子，新写用例时要多看一眼 doc。
- **状态：** resolved
- **已决（用户裁定）：** 单独开票日后做，见 `issues/06-test-engine-diskwork-param.md`。本批不动。

## D3 · 票 03 · `runRebuildFileIdentities` 的第三形参用 `koreader.TaskHandle` 还是 api 自己的窄接口

- **问：** 这个批循环要的正是「过闸门 / 发磁盘作业 / 报计数推进」三样，与 `koreader.TaskHandle` 逐字同形。
  直接收下那个接口，两处哈希回填的调用点就长得一模一样（同一个 `hashingFrameHandle` 适配器）；
  但接口的归属写着 koreader，而这个循环留在 api 里。
- **选项：** A 收下 `koreader.TaskHandle`（推荐，因为两处哈希回填从此逐字对称，且这个循环本就调
  `koreader.FingerprintQuickFile`）｜ B `api` 自己声明一份同形的三方法接口，代价是全仓多一份同形声明
- **不处理会怎样：** 已按 A 落地；只是 api 的一个私有批循环把自己的协作契约寄在 koreader 的接口上。
- **状态：** resolved
- **已决（用户裁定）：** 选 A——维持收下 `koreader.TaskHandle`，两处哈希回填保持逐字对称。

## D4 · 票 03 · `reportHashProgress` 收 IO 实况还是自己去句柄上取

- **问：** 实况已经全在句柄里，这个函数其实可以只收句柄、自己调 `IOMetrics()`。本票改成收
  `taskrun.IOMetrics`（而不是原来的 `taskIOMetrics`），是为了让用例仍能直接摆出一组实况值去守
  「整帧一次报出」——自己去取的话，用例得先造一个真跑过磁盘作业、且等待时长可预期的句柄。
- **选项：** A 收 `taskrun.IOMetrics`（推荐，用例造得起，且票 05 删 `taskIOMetrics` 时这一处不用再动）｜
  B 只收句柄，用例改用假调度器把实况喂进去
- **不处理会怎样：** 已按 A 落地；上报函数多一个本可推导的形参。
- **状态：** resolved
- **已决（用户裁定）：** 选 A——维持收 `taskrun.IOMetrics` 值，保住用例的可驱动性。

## D5 · 票 03 · `runGlobalScan` 里 `Checkpoint` 之后那句冗余的 `ctx.Err()`

- **问：** `Handle.Checkpoint` 的 doc 明写「闸门在未暂停时返回上下文错误，因此它同时是取消检查——
  任务体不需要另写一次 `ctx.Err()`」，而 `runGlobalScan` 的循环顶部在改走句柄之后仍连着写了两句。
  两条 code-review 都提到了它，判定一致：删掉是安全的，但本票承诺零行为变化，删它属于顺手。
- **选项：** A 留着，交给票 05 收尾时与其余「手抄闸门」残留一起清（推荐，本票不夹带）｜
  B 当场删掉，反正等价
- **不处理会怎样：** 多一句永远为假的判断；读的人会以为闸门不管取消。
- **状态：** resolved
- **已决（用户裁定）：** 当场删掉。`runGlobalScan` 里 `Checkpoint` 之后那句 `ctx.Err()` 已移除。

## D6 · 票 04 · `external.ScanSession` 的窄接口容不容 nil

- **问：** 旧形参 `progress func(current, total int)` 是可选的（两处用例传 nil，循环里带 `!= nil` 判断）。
  换成接口之后有两条路：留着那个判断（同 `scanner.ScanObserver`——它的 doc 明写「传 nil 表示这次扫描
  不属于任何任务」），或者要求非 nil（同 `koreader.TaskHandle`）。
- **选项：** A 要求非 nil，两处用例各写一个丢弃型手写假体（推荐，因为改完之后全仓再没有传 nil 的调用点，
  留着判断就是死分支；且接口值的 nil 判断有「带类型的 nil 不为 nil」这个坑）｜
  B 容 nil 并照 `ScanObserver` 的口径写进 doc，代价是那条分支无人走
- **不处理会怎样：** 已按 A 落地；两个包各多一个两行的丢弃型假体。
- **状态：** resolved
- **已决（用户裁定）：** 选 A——维持要求非 nil，不留死分支。

  - **补记（code-review）：** 选 A 使票 04 自己那条「`prepare_transfer_test.go` 若变红说明改动溢出了」
    被触及——该文件必须跟着改一行。两条审查一致判定这是机械替换、断言与「传输规划」语义零变化，
    不算语义溢出。若日后翻成 B，`scanner.ScanObserver` 的 doc 与 CONTEXT.md 的**扫描观察者**词条
    是现成的措辞来源；分歧点在于 `external.ScanSession` 的「不属于任何任务」只出现在用例里，
    而 `ScanObserver` 的那一路是真实生产路径（守护扫描、watcher 派生扫描、建库首扫）。

## D7 · 票 05 · `AGENTS.md` 的 `internal/*` 清单漏了四个包

- **问：** code-review 指出 Project Structure 那句「supporting services」的包清单与实际不符：
  缺 `taskrun`（本批新建）、`diskwork`、`proposal`（上一批新建）、`runtimecfg`。doc-style 的归属表
  把「结构」判给 `AGENTS.md`，而本票是这一批最后一次机会。
- **选项：** A 只补本批自己造成的那一个 `taskrun`，其余三个另记（推荐，本票不夹带别批的账）｜
  B 一次补齐四个，顺带核对整句是否还有别的漂移
- **不处理会怎样：** 已按 A 落地；清单仍漏三个包，新来的 agent 读 `AGENTS.md` 会以为
  `diskwork`、`proposal`、`runtimecfg` 不存在。
- **状态：** resolved
- **已决（用户裁定）：** 一次补齐。`AGENTS.md` 已补 `diskwork` / `proposal` / `runtimecfg`，并核对整句其余引用无漂移。

## D8 · 票 05 · IO 实况两条通道的键名要不要互相推导

- **问：** `taskIOFrameMetrics`（帧指标）与 `taskIOMetricsParams`（任务参数）各自把
  `hashed_files` / `io_wait_ms` / `paused_ms` 拼了一遍。code-review 记了一条 Duplicated Code，
  提议让参数那条从帧那份 map 转写。
- **选项：** A 各写各的（推荐，因为两条通道的键集本就不同——参数那条多 `storage_profile` 与
  `volume_key` 且滤空值——而「存储 IO 面板按哪几个参数名读」是面板契约，写成推导出来的会让它
  从签名上消失）｜ B 参数那条遍历帧那份 map 转字符串，再补两个键
- **不处理会怎样：** 已按 A 落地；三个键名在同一个文件里相邻出现两次。
- **状态：** resolved
- **已决（用户裁定）：** 选 A——各写各的，面板契约留在签名上。

  - **同时驳回（同一条审查）：** 形参名 `handleIO` 说的是来处不是内容，且
    `taskIOFrameMetrics` / `taskIOMetricsParams` 词序不对称。`handleIO` 是票 02～04 在四处
    立下的名字，只改本票这一处会把批次内的一致性打散；改成 `taskIOMetricsFrame` 又会与
    `taskrun.Frame` 撞脸——它返回的只是那一帧里的 `Metrics`。日后若做全批统一改名，
    这两条一起做。

## D9 · 票 06 · 两处从 Controller 取配置的装置里，引擎与 Controller 谁先造

- **问：** 装置改成构造期收下**磁盘作业**入口之后，`newComicInfoRig` 与 `newMaintenanceRig` 里的 runner
  取的是 `c.currentConfig`——它得等 Controller 存在才造得出，而引擎又要在构造期收下这个 runner。
  于是两处装置的顺序翻了过来：先造 Controller 与 runner，再造引擎，最后补一句 `c.taskEngine = e`。
- **选项：** A 调顺序，runner 仍取 `c.currentConfig`（推荐，配置快照的来源与生产的 `newControllerCore`
  逐字同形，且「`c.diskWork` 与引擎拿到的是同一个 runner」一眼看得出）｜
  B 两处改取 `manager.Snapshot`（同 `newKOReaderTaskRigWithMode`），Controller 就能一次性用复合字面量
  造齐，代价是配置来源与生产不再同形
- **不处理会怎样：** 已按 A 落地；两处装置各多一句 `c.taskEngine = e`，Controller 不再一句造齐。
- **状态：** resolved
- **已决（用户裁定）：** 选 A——维持现状，配置来源与生产的 `newControllerCore` 保持同形。

  - **同时驳回（同一条审查）：** 调用点上的裸 `nil` 不自明，提议保留单参版、另给要读盘的装置一个
    `…WithDiskWork`。这正是本票要消灭的东西——两条拿到 runner 的路子并存，「这个引擎的句柄能不能
    发起**磁盘作业**」又要读 doc 才知道。裸 `nil` 的含义由形参名与该函数 doc 回答，一处即可。

## D10 · 票 06 · 三处装置里「调度器必须新建」这条约束各写了一遍

- **问：** `newComicInfoRig` / `newMaintenanceRig` / `newKOReaderTaskRigWithMode` 都是
  `diskwork.NewRunner(<配置快照>, storageio.NewScheduler())` 加一句措辞各异的「调度器**必须**新建而不能用
  包级实例：后者按卷计数，用例之间会经它互相污染」。code-review 记了一条 Duplicated Code，
  并按「唯一归属」判该约束只该有一处。三份在基线 `4b2e801` 上就已存在，本票只是重排了其中两份的行文。
- **选项：** A 各写各的（推荐，本票只做形参化，不夹带装置的重新分层；且约束就近写在做决定的那一行上，
  读 rig 的人不必跳走）｜ B 往 seam 文件抽一个 `newTestDiskWork(cfg func() config.Config)`，
  构造与那条约束一起收口，代价是三处 rig 各多一次跳转
- **不处理会怎样：** 三处装置各留一份同义注释；将来改「调度器该不该共用」要改三个地方。
- **状态：** resolved
- **已决（用户裁定）：** 选 A——各写各的，约束就近写在做决定的那一行上。

