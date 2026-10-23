# 01: 在线阅读位置的完整用例

**What to build:** 接通阅读状态 module、必要持久化及在线阅读入口，支持有效回退、拒旧、同页新阅读刷新与可识别重试。

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

**验收基准：** [父规格](../spec.md)。本票遵守其中的跨功能约束和范围，不将历史实现偏差重新定义成业务规则。

**规格覆盖：** US 2、US 3、US 5、US 6、US 8、US 10、US 12、US 13、US 15、US 18、US 19、US 20、US 26、US 27、US 28、US 45、US 46、US 48、US 108、US 110。

- [x] 通过现有在线阅读入口实际调用独立阅读状态 module；建立本用例需要的窄依赖和最小持久化，不只新增无人调用的 interface。
- [x] 按明确的用户与书处理位置；有效新报告允许前进或回退，较旧报告不能覆盖较新的阅读变更；新旧比较与写入在并发下仍成立。
- [x] 有可信发生时间时按该时间比较和保存，没有时间的兼容输入使用服务端接收时间；保留时区归一化及现有响应兼容性。
- [x] 同页发生较新的有效阅读时刷新最近阅读时间；前端保留可识别事实的稳定依据，重复发送同一事实不制造新阅读，不能仅以页码相同判定永久跳过。
- [x] 保留合法页码、未知页数与无用户兼容路径；按用户隔离读取与写入，不回退到另一用户或全局遗留进度。
- [x] 进度与派生系列聚合保持必要事务一致性；缓存失效与活动后续动作由完整用例接管，不向 HTTP 返回步骤配方。
- [x] 准确区分状态写入失败和活动统计告警；进度已提交时不声称未保存，需要的兼容统计独立尝试且告警可观察。
- [x] 新持久化可升级已有数据库，不重建书、系列、用户身份，不猜测补造旧事件；为后续清除依据和可识别重试留出本功能内部的扩展位置。
- [ ] 通过完整用例和真实临时 SQLite 验证回退、拒旧、同页新时间、可识别重复、用户隔离、并发、聚合回滚及统计失败；保留在线入口回归。
- [ ] 删除本用例被替代的旧规则；完成对应后端门禁、涉及前端的测试构建和必要生成检查，记录实际验证结果。

## Comments

已实现在线入口接入 `reading.RecordPosition`。阅读规则由 reading 持有，database 的 `MutateReadingPosition` 事务 seam 负责原子读取、决定回调与进度/聚合/事实收据落库；活动两路独立尝试，告警通过结果及日志可观察。前端保留发生时间，网络失败转离线队列不重造事实时间。

实际验证：
- `go test ./internal/reading ./internal/database` 通过。新增真实 SQLite 覆盖回退拒旧、同页新事实/重复事实、并发、隔离、夹页、未知页数、聚合回滚、活动失败、旧库升级和 UTC+8 发生时间；`gofmt -l ./cmd ./internal` 无输出。
- `npm run test`：82 文件、868 测试通过；`npm run build`、`npm run lint` 通过。
- CI Go 1.26.10 工具链下 `golangci-lint run ./internal/reading/... ./internal/database/...` 返回 0、`0 issues.`。
- HTTP 回归已新增，但 API/全仓 Go 测试受到 Windows Application Control 拦截 gcc.exe 的环境限制；`CGO_ENABLED=0` 也因既有 chai2010/webp 必需 CGO 而无法编译。全仓 lint 在 CI Go 工具链下同样因 CGO preprocessing failed 类型加载失败（exit 1，末尾 `0 issues.` 不算通过）。不能声称这些门禁已通过，剩余两项暂不勾选、Status 暂不关闭。
- 仅增加 `schema_handwritten.sql` 手写表，未修改 sqlc 输入或 tsgen 的契约目标，无需重新生成。
