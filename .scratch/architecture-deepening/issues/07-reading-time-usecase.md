# 07: 阅读时长的完整用例

**What to build:** 时长写入与活动归属移入阅读状态，保留活跃判断、可见性、切书结算和软统计失败语义。

**Blocked by:** 01: 在线阅读位置的完整用例

**Status:** done

**验收基准：** [父规格](../spec.md)。本票遵守其中的跨功能约束和范围，不将历史实现偏差重新定义成业务规则。

**规格覆盖：** US 26、US 45、US 46、US 49、US 52。

- [x] 现有阅读时长上报接入阅读状态完整用例，主体、书、累加及活动写入有明确归属；不只移动一个存储调用。
- [x] 保留前端活跃输入、页面可见性、空闲停止、心跳及切书结算语义，不将隐藏或空闲时间算入阅读。
- [x] 保留时长为尽力而为软统计的处理，发送成功但响应丢失时不因盲目重试重复累计；旧书异步失败不能污染新书。
- [x] 保留非法或非正秒数、单次上限、缺失书及兼容主体的合理结果，不增加没有事实依据的离线时长或跨日分摊。
- [x] 时长与所需活动聚合保持既有必要事务关系；存储失败可观察，且不与已成功的阅读位置保存混淆。
- [x] 用完整功能及真实 SQLite 验证用户隔离、累加、故障和聚合，前端验证可见性、切书、卸载与失败不重复；保留既有上报兼容。
- [x] 删除相应请求处理中的规则副本，完成相关后端与前端测试、构建和生成检查。

## Comments

2026-10-25 实施与验收：

- `reading.Service.RecordTime` 是完整时长用例：显式主体和书、无效增量空操作、单次 3600 秒封顶、缺书兼容、接收日归属、累计与每日活动的共同事务及可观察告警。HTTP 只解析身份/请求并映射响应，不再维护规则副本。
- `Queries.AccumulateReadingTime` 仅提供计数持久化；原存储入口保留给已有存储 fixture。用例不调用位置或读态规则，不添加离线时间推断。
- 红绿证据：首个 public 用例测试因缺失 RecordTime/TimeReport 编译失败，新增用例后通过；真实关闭数据库的 HTTP 回归先发现旧处理返回 200，迁移后返回 500。真实 SQLite 触发器确认活动失败回滚本次时长、保留已保存位置，另覆盖主体隔离、累计、日期与秒数上限。
- 前端沿用现有 hook，以消费者边界验证可见性、90 秒空闲停止、输入恢复、心跳、切书/卸载结算、隐藏/pagehide beacon、异步旧书失败与丢失响应不回填/不重传；无须改写已正确的计时逻辑。
- 门禁通过：Go 1.26.10 `go test ./...`；`gofmt -l ./cmd ./internal` 空输出；独立缓存 `golangci-lint run --allow-parallel-runners` 为 `0 issues.`；前端全量 93 文件/907 测试、`npm run build`、`npm run lint`；`go run ./cmd/tsgen` 无生成漂移；`git diff --check`。
- 首轮全量 Go 仅扫描器 `TestScanAccountsArchiveFailuresAndKeepsScanning/归档打不开` 的 TempDir 清理报告目录非空；复跑完整 `go test ./...` 已通过。未绕过系统执行限制。
公共 fixture 修复（独立 test 提交）：

- 多树重复出现的清理失败已定位：`TestScanAccountsArchiveFailuresAndKeepsScanning` 创建无应用发起方的 standalone Scanner，`CoverBatch.attach` 以后台上下文自行排空封面；fixture 没有在 `newSlowDiskTestConfig` 的缓存临时目录删除和 store 关闭前等待 `selfDrained`。
- 在 NewScanner 与缓存 TempDir 创建后注册 LIFO `t.Cleanup`，以独立 5 秒上下文等待现有 `waitForCoverQueue`；超时明确报告失败。只修该公共 fixture，不修改生产扫描/封面生命周期，不新增调用镜像测试。
- 红色证据为多个工作树的同一 TempDir 目录非空失败；修复后原测试 `go test ./internal/scanner -run TestScanAccountsArchiveFailuresAndKeepsScanning -count=1` 与完整 `go test ./internal/scanner -count=1` 通过，格式空输出与 lint `0 issues.`。
- 生产资源停止契约由票 21/24 继续核验；这次 fixture 修复不构成相关生产生命周期完成证据。