# 02: 统一离线批量与逐本补传

**What to build:** 两种传输走同一规则，保留发生时间和原活动日期，前端队列重试保持事实身份与用户归属。

**Blocked by:** 01: 在线阅读位置的完整用例

**Status:** done

**验收基准：** [父规格](../spec.md)。本票遵守其中的跨功能约束和范围，不将历史实现偏差重新定义成业务规则。

**规格覆盖：** US 16、US 17、US 20、US 21、US 35、US 37、US 38、US 39、US 40、US 86。

- [x] 离线批量同步与逐本回退均实际调用已建立的位置规则；前端可继续使用现有批量优先、必要时逐本回退的传输方式。
- [x] 两种传输保留相同发生时间、新旧胜负和同页语义；验证较旧 A 先回退补传后，真实较新 B 不会被 A 的接收时间错误挡住。
- [x] 队列保留可识别事实身份或等价发生时间；请求在途时新的队列项不会被旧响应删除或改写，身份切换仍遵守离线 owner 规则。
- [x] 保留逐项处理、输入顺序、逐项结果及缺失书和非法项的既有容错；单项失败不会让其他独立项失去结果。
- [x] 有效补传活动按可证明的原阅读日期归属；重复传输不改为联网日期，被拒旧的位置不更新最近阅读，也不据此补造额外历史活动。
- [x] 本地日历和时间归一化一致；没有完整轨迹的队列不能凭最后位置补出多日活动，已有历史统计不猜测重算。
- [x] 用同一真实 SQLite 行为矩阵对比批量和回退的页码、最近阅读时间、活动日期与重复结果；前端覆盖批量不可用、回退失败、晚响应及账户切换。
- [x] 移除两条输入中被替代的规则副本，保留协议和队列兼容；完成对应后端、前端及生成物门禁。
## Comments

### 实施与验证（2026-10-25）

实现提交 `f9936ae`，合并集成分支（含 05 的可信接收时间）提交 `15fad94`。`reading.SyncPositions` 按输入顺序逐项调用 `RecordPosition`，统一位置、拒旧、同页新阅读、事实重试、活动与失效规则；HTTP adapter 只转换请求和逐项结果，不接收内部 `ReceivedAt`。删除批量路径的直写、接收日活动、冲突与节流副本及无人读取的 `progressWriteCache`。

前端新队列项持久保留 `factId`，两种传输与重试携带同一个身份和原 `updatedAt`；旧队列仍按发生时间兼容。响应只移除与发送事实匹配的当前队列项，失败不会把快照写回；owner 或身份代次改变后停止剩余回退发送，保护新账户队列。会话过期仍保留归属与本机数据。

红绿证据：新增 `SyncPositions` 用例先因接口缺失失败；真实 SQLite 的 HTTP 矩阵先复现批量同页新事实被跳过；队列消费者先复现批量/回退晚响应删除新项、切换 owner 后继续补传，以及同 tick 新事实缺乏身份；修复后均通过。额外验证夹取后的重复结果保持 `skipped_unchanged`，单项事务失败保留前后独立结果及必要失效。原有依赖 `time.Now` 不同 tick 的身份重试测试改为确定递增时钟，继续保留显式时间等时拒旧规则。

已通过：

- `go test ./internal/api ./internal/reading -count=1`（合并前）；合并后 `go test ./...` 中 API、reading、database、koreader 等所有实际启动的包均通过。
- `npm run test`：92 文件、900 测试；`npm run build` 与 `npm run lint` 通过。
- 合并后离线队列、owner、会话过期定向回归：3 文件、36 测试通过；新增失败晚响应场景后 `offlineReader.test.ts` 的 26 测试通过。
- `gofmt -l ./cmd ./internal` 输出为空；独立缓存的 `golangci-lint run` 为 `0 issues.`；`go run ./cmd/tsgen` 无产物漂移；`git diff --check` 通过。

未完成的环境门禁：完整 `go test ./...` 退出 1，仅 `internal/application/relocating`、`internal/images`、`internal/metadata`、`internal/runtimecfg` 的测试 exe 被 Windows Application Control 阻止执行，提示 `An Application Control policy has blocked this file.`；没有把这些阻断记为通过，也未改名、换路径或绕过策略。故最后一条仍未勾选，状态保留 `ready-for-agent`，待集成环境完整门禁验收。
### 同刻无发生时间事实的幂等修正（2026-10-25）

在集成 `b8d7ca0` 上通过公共 `RecordPosition` 与 `SyncPositions`、真实 SQLite 和固定 `WithClock` 确认：同一接收 tick 的两个不同 `fact_id`、相同页码、均不携带发生时间的报告，第二条被跳过且没有接收回执；次日重试第二个身份会被当作新阅读，将最近阅读与活动改到次日。

最小修正由 `reading.RecordPosition` 统一完成：是否为新接收只取决于没有 `OccurredAt`，事实身份只用于回执去重；同页等时的额外拒绝只针对显式发生时间。无时间的新身份因此按事务顺序接受并持久保存回执，次日重试不再产生新阅读日期。未改写或合成时间；无身份兼容报告的两秒节流、显式发生时间的严格拒旧、持久清除保护，以及 `ReceivedAt`/`OccurredAt` 互斥保持原规则。

红绿证据：新增 `TestReceivedFactsAtSameTickRetainRetryIdentity`，修复前两入口均报告 `first=true second=false next-day retry=true`，活动日期出现两天；修复后两入口均为首条与第二条接受、次日重试拒重，最近阅读仍为原接收时刻，活动仅原一天。`go test ./internal/reading ./internal/koreader -count=1` 通过；仅变更公共位置规则和此回归，没有重跑无关全量或改变既有环境门禁结论。

本轮变更门禁：HTTP 离线/在线/匹配 KOReader 与 bulk 定向回归通过；独立缓存的 `golangci-lint run` 为 `0 issues.`，`gofmt -l ./cmd ./internal` 为空，`git diff --check` 通过。提交前已同步最新集成分支。

## 集成补验

无时间新身份幂等修复 9f62352 已合入 ad86c67，reading/koreader 与相关 HTTP 回归通过。随后该集成上的完整 go test ./... 退出 0（API 133.670s、reading 7.855s、koreader 9.370s、scanner 19.635s；post-untimed-id-go-test.log），先前受策略阻碍的全仓包均已实际通过。07完整门禁也覆盖此修复，相关前端及生成门禁已有成功证据，本票全部验收项满足并关闭。
