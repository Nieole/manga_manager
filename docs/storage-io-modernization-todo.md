# 外接机械盘 IO 优化待办清单

更新日期：2026-05-25

本清单与 `docs/storage-io-north-star-plan.md` 对应，用于跟踪外接机械盘低冲击扫描、封面生成、缓存与诊断改造。

状态说明：

- `[x]` 已完成并验证。
- `[~]` 部分完成，仍有剩余拆项。
- `[ ]` 未完成。
- `[F]` 当前约束下冻结或暂缓。

## 阶段 1：存储介质策略与配置模型

- [x] 新增资源库存储策略。
  - [x] 设计 `storage_profile` 枚举：`auto`、`ssd`、`hdd_external`、`network`、`custom`。
  - [x] 在配置模型中保存全局默认策略，并支持按路径覆盖的 `storage_policies`。
  - [x] 支持运行时更新配置，不需要重启。
  - [x] 为旧配置提供兼容默认值 `auto`。
- [x] 新增资源库 IO 策略。
  - [x] `scan_concurrency`。
  - [x] `archive_open_concurrency`。
  - [x] `cover_concurrency`。
  - [x] `hash_concurrency`。
  - [x] `pause_background_when_reading`。
  - [x] `idle_only_heavy_tasks`。
  - [x] `disable_same_disk_page_cache`。
- [x] 外接机械盘低冲击默认值。
  - [x] 扫描归档打开并发默认为 1。
  - [x] 封面生成并发默认为 1。
  - [x] hash 并发默认为 1。
  - [x] 阅读时暂停后台重 IO 默认开启。
  - [x] 同盘页面磁盘缓存默认关闭。
- [x] 配置 UI。
  - [x] 设置页展示默认存储介质策略。
  - [x] 增加“外接机械盘低冲击模式”快捷选项。
  - [x] 展示当前策略下的关键并发限制。
  - [x] 保存后立即生效。
  - [x] 按单个资源库路径编辑 `storage_policies` 的高级 UI。
- [x] 阶段 1 验收。
  - [x] 单测覆盖配置默认值。
  - [x] 单测覆盖外接 HDD 策略映射。
  - [x] 前端构建通过。

## 阶段 2：统一存储 IO 调度器

- [x] 设计 `StorageWorkScheduler`。
  - [x] 定义 `volume_key` 或 library root 识别规则。
  - [x] 定义每个 `volume_key` 的 token 池。
  - [x] 支持按 `storage_profile` 设置 token 数。
  - [x] 支持 reader 优先级：等待中的 reader 会阻止新的后台重 IO 获取 token。
  - [x] 支持任务取消、暂停、恢复：取消跟随任务 ctx 释放 token，阅读活跃时后台任务按策略等待。
- [x] 定义任务类型与优先级。
  - [x] `reader`。
  - [x] `scan_fast`。
  - [x] `metadata_scan`。
  - [x] `cover_build`。
  - [x] `cache_write`。
  - [x] `identity_hash`。
- [x] 接入归档打开路径。
  - [x] 阅读页图打开归档前申请 reader token。
  - [x] 扫描打开归档前申请 scan token。
  - [x] 封面生成打开归档前申请 cover token。
  - [x] hash 前申请 hash token。
- [x] 阅读抢占。
  - [x] 阅读请求进入时标记同盘正在阅读。
  - [x] 同盘低优先级任务暂停或延后。
  - [x] 阅读空闲一段时间后恢复后台任务。
- [x] 阶段 2 验收。
  - [x] 外接 HDD 同盘后台重 IO 并发不超过 1。
  - [x] 扫描和封面重建同时触发时不会并发打开多个归档。
  - [x] 阅读请求等待 IO token 的耗时被记录。
  - [x] 取消任务能释放 token。

## 阶段 3：扫描低冲击分级

- [x] 实现 `fast_scan`。
  - [x] 只遍历目录和文件属性。
  - [x] 比较 path、mtime、size。
  - [x] 标记新增文件。
  - [x] 标记删除文件。
  - [x] 标记变化文件。
  - [x] 未变化文件不打开归档。
- [x] 实现 `metadata_scan`。
  - [x] 只处理新增或变化文件。
  - [x] 打开归档读取页数。
  - [x] 读取 ComicInfo。
  - [x] 读取首图必要元信息。
  - [x] 只创建封面任务，不同步生成封面。
- [x] 实现 `identity_scan`。
  - [x] quick hash 按需执行。
  - [x] full hash 后台低优先级执行。
  - [x] KOReader 关闭时跳过 fingerprint。
- [x] 实现 `repair_scan`。
  - [x] 显式入口触发。
  - [x] 强制重建派生数据。
  - [x] 显示外接盘占用提示。
- [x] 阶段 3 验收。
  - [x] 未变化文件扫描不会 `OpenArchive`。
  - [x] 外接 HDD 增量扫描不会同步生成封面。
  - [x] KOReader 关闭时不计算 fingerprint/full hash。
  - [x] 扫描日志包含 `opened_archives` 与 `hashed_files`。

## 阶段 4：封面生成队列化与低速模式

- [x] 封面任务模型。
  - [x] 定义全量缩略图重建任务状态。
  - [x] 支持入队。
  - [x] 支持暂停。
  - [x] 支持全量缩略图重建取消。
  - [x] 支持失败重试。
- [x] 扫描与封面生成解耦。
  - [x] 扫描只登记缺失封面。
  - [x] 扫描只登记过期封面。
  - [x] 后台 worker 异步生成封面。
- [x] 按存储设备限流。
  - [x] 外接 HDD 默认 `cover_concurrency = 1`。
  - [x] 同一 `volume_key` 不并发打开多个归档。
  - [x] SSD 支持更高并发或不额外限制。
- [~] 阅读时暂停封面任务。
  - [x] 同盘阅读开始后暂停 cover worker 获取新的 IO token。
  - [x] 阅读空闲后恢复 cover worker。
  - [x] 前端任务中心显示 IO 等待与暂停原因摘要。
- [x] 缩略图写入策略。
  - [x] 缩略图默认写入应用数据目录。
  - [x] 避免写回漫画原盘目录。
  - [x] 应用数据目录位于慢速盘时给诊断提醒。
- [x] 阶段 4 验收。
  - [x] 重建封面期间后台归档打开并发受控。
  - [x] 阅读时封面任务能自动暂停。
  - [x] 任务中心可以取消封面重建。
  - [x] 封面完成后前端能看到更新。

## 阶段 5：缓存策略与同盘写入控制

- [x] 页面磁盘缓存按库控制。
  - [x] 读取资源库 `disable_same_disk_page_cache`。
  - [x] 外接 HDD 默认禁用同盘页面磁盘缓存。
  - [x] 高级设置允许手动开启或关闭同盘限制。
  - [x] 单资源库路径级 UI 待补。
- [x] cache/data 路径诊断。
  - [x] 判断应用数据目录所在 volume。
  - [x] 判断资源库所在 volume。
  - [x] 两者相同时给出慢盘写入提醒。
- [x] 缓存写入限速。
  - [x] 批量缩略图写入限速。
  - [x] 避免大量小文件同时落盘。
  - [x] 记录缓存写入耗时。
- [x] 阶段 5 验收。
  - [x] 外接 HDD 资源库默认不写页面磁盘缓存。
  - [x] 缩略图不会默认写回漫画目录。
  - [x] 诊断页能显示 cache/data 与资源库是否同盘。

## 阶段 6：后台任务用户体验

- [x] 任务中心增强。
  - [x] 显示任务访问的资源库或范围名称。
  - [x] 显示 `storage_profile`。
  - [x] 显示 `volume_key`。
  - [x] 显示任务累计 IO 等待耗时。
  - [x] 诊断页显示当前后台等待 token 数与暂停原因。
  - [x] 显示已打开归档数。
  - [x] 显示已 hash 文件数。
- [x] 全局低冲击运行模式。
  - [x] 增加“后台维护低冲击运行”开关。
  - [x] 增加“仅空闲时运行”选项，并接入后台重 IO 调度。
  - [x] 增加“暂停全部后台 IO”操作。
- [~] 高风险任务提示。
  - [x] repair scan / 强制全量读取前提示会长时间占用磁盘。
  - [x] 全量重建封面前提示会占用外接盘。
  - [x] full hash / 文件身份重建前提示会产生大量顺序/随机读取。
  - [~] 提供“低速后台执行”和“立即执行”路径：当前默认走低冲击执行，立即绕过限流未开放。
- [x] 阶段 6 验收。
  - [x] 用户可以一键暂停扫描、封面、hash 获取新的后台 IO token。
  - [x] 用户可以看到全局后台 IO 暂停状态与活跃 token。
  - [x] 用户可以选择低冲击执行全量重建。

## 阶段 7：诊断与基准

- [~] 后端指标。
  - [x] 每个任务记录 `volume_key`。
  - [x] 每个任务记录 `storage_profile`。
  - [x] 扫描任务记录 `opened_archives`。
  - [x] 扫描与文件身份任务记录 `hashed_files`。
  - [x] 扫描与文件身份任务记录 `io_wait_ms`。
  - [x] 每个任务记录 `paused_ms`。
  - [x] 阅读请求记录是否等待后台 IO。
- [~] 性能面板。
  - [x] 展示当前活跃 IO token 与 reader 等待状态。
  - [x] 展示当前后台等待 token 数与暂停原因。
  - [x] 展示每个资源库重 IO 并发。
  - [x] 展示最近扫描的归档打开速率。
  - [x] 展示最近封面任务的归档打开速率。
  - [x] 展示阅读等待 IO token 总耗时。
- [~] 基准脚本。
  - [x] 新增 `cmd/storageiobench` 采集工具。
  - [x] 新增 `scripts/storage-io-baseline.ps1` 一键基线采集脚本。
  - [x] 新增外接 HDD 基线采集指南。
  - [x] 新增基线结果模板。
  - [x] 支持扫描与阅读并发基线采集入口。
  - [x] 支持封面重建读写压力模拟基线采集入口。
  - [x] 支持 SSD 与 HDD 策略对比所需的 `label/profile/notes` 报告字段。
  - [x] 报告输出并发、缓存位置和阅读延迟策略建议。
  - [x] 生成结果默认保存到 `docs/performance-baselines/`。
  - [ ] 外接 HDD 样本库扫描基线。
  - [ ] 外接 HDD 封面重建基线。
  - [ ] 扫描与阅读并发基线。
  - [ ] SSD 与 HDD 策略对比。
- [~] 阶段 7 验收。
  - [x] 诊断页能判断当前磁盘压力来源。
  - [ ] 低冲击模式下阅读延迟下降有数据证明。
  - [ ] 基线结果保存到 `docs/performance-baselines/`。

## 第一批建议执行清单

- [x] 增加 `storage_profile` 与 `io_policy` 配置结构。
- [x] 为外接 HDD 策略设置保守默认值。
- [x] 新增统一 `StorageWorkScheduler` 基础实现。
- [x] 接入封面重建 worker 的并发限制。
- [x] 接入扫描打开归档的并发限制。
- [x] 阅读时暂停同盘封面/hash 任务。
- [x] 外接 HDD 默认禁用页面磁盘缓存。
- [x] 增加任务日志字段：已补 `volume_key`、`storage_profile`、扫描任务 `opened_archives`、扫描/文件身份任务 `hashed_files`、`io_wait_ms`、`paused_ms` 与 `thumbnail_write_ms`。
- [x] 增加后端单测覆盖配置默认值和调度器 token 行为。
- [x] 更新 CHANGELOG。
