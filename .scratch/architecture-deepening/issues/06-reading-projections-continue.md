# 06: 共享阅读状态查询与继续阅读

**What to build:** 列表、详情、继续阅读和读取协议复用每用户读态判断，未知页数与已读状态解释一致。

**Blocked by:** 01: 在线阅读位置的完整用例；03: 批量读态与持久化未读保护；04: 批量已读活动与未知页数统计

**Status:** done

**验收基准：** [父规格](../spec.md)。本票遵守其中的跨功能约束和范围，不将历史实现偏差重新定义成业务规则。

**规格覆盖：** US 26、US 28、US 34、US 51、US 52。

- [x] 系列列表、书列表、详情及继续阅读实际使用阅读状态的共享主体投影与读态判定；调用方不再各自重建相同规则。
- [x] 用户无本人记录时不继承其他用户或旧全局进度；无用户兼容路径保留明确行为，读取不修改阅读时间或生成活动。
- [x] 有效回退、同页新时间、已读标记、未读清除及未知页数在各视图含义一致，内部哨兵不被当成真实页数。
- [x] 保留书的阅读顺序、上一书和下一书关系，以及全部已读或缺少可继续位置时的既有回退结果。
- [x] 读取协议继续使用已有身份、分页与响应形状；只迁移共同阅读判定，不重写归档、图片或协议目录。
- [x] 通过功能 interface 和真实 SQLite 对照同一主体在列表、详情、继续阅读及相关协议的可见结果，覆盖无记录、回退、清除、未知页数与跨用户。
- [x] 移除重复投影和继续阅读判定；规则测试不要求初始化完整 Controller，保留相关 HTTP、协议和页面回归并完成门禁。


## Comments

### 实施与验收记录（2026-10-25）

- `reading.Queries` 通过窄 `ProjectionStore` 提供书详情、系列阅读视图及上下本；`ProjectBooks` 统一覆盖页码、时间与 `IsRead`，无本人记录时清空三者。读取失败返回错误，不能把全局数据当成用户数据返回。
- `reading.Summarize` 复用 `InspectBook`，统一第一本未完成书、最近阅读与统计；保留自然阅读顺序、时间相同取顺序靠前书、全部已读时 `next_unread_book_id=0` 及空系列回退。
- 系列搜索的主体选择归 `reading.CatalogueQueries`；继续使用 SQLite 的主体聚合、筛选、计数及 offset/cursor 分页，不在分页之后逐系列查书重算。最近系列和 Web/Mihon/OPDS 的最近阅读共用主体选择。
- HTTP 移除投影与 continue 配方；Web 书列表/详情/系列上下文/continue/上下本、Mihon 和 OPDS 书列表均用共同读取功能。协议身份、分页和响应形状保留。未触及写路径、SQL 或生成前端契约。
- TDD：公共查询功能测试先因 `NewQueries` 尚不存在失败，随后真实 SQLite 测试通过；上下本 HTTP 回归先实际暴露全局进度泄漏，再通过共同主体投影修复。
- 真实 SQLite 的功能 seam 覆盖全局兼容、主体隔离、回退、清除、未知页数完成、自然上下本、全部已读回退和按主体筛选分页。HTTP/协议矩阵覆盖已知 20 页与未知 0 页、无记录/前进/回退/同页新时间/完成/清除、本人与他人；检查活动、时长、位置与阅读时间均未因读取改变。
- 页面读取消费者回归：`npm run test -- src/reading src/pages/series-detail/hooks/useSeriesContinue.test.ts src/pages/series-detail/hooks/useSeriesContext.test.ts src/pages/book-reader/useReaderSiblings.test.tsx src/pages/book-reader/useReaderBookData.test.tsx`，7 files / 27 tests 通过。无前端代码改动。
- Go 工具链为 `go1.26.10`；`gofmt -l ./cmd ./internal` 与 `git diff --check` 均空；独立 `lint-06` 缓存下 `golangci-lint run --allow-parallel-runners` 报 `0 issues.`。首次 lint 因其他 runner 的共享锁未启动，随后使用工具自身支持的并行 runner 参数。
- 门禁过程中，缺少忽略的 embed `web/dist` 时全量测试未启动相关包，复制主树产物后首轮 `go test ./...` 全部通过。之后更新测试再跑全量时 scanner 包曾一次失败（工具输出中段被截断，未保留具体测试名）；`go test ./internal/scanner -count=1` 独立复跑通过，最终全量结果见交付记录。
- 工作树依赖注意：`C:/Users/saber/dev/manga_manager-06/web/node_modules` 与误建的 `C:/Users/saber/dev/manga_manager-06/web/web/node_modules` 均为 junction，目标均为 `C:/Users/saber/dev/manga_manager/web/node_modules`。误建链接清理曾被自动审批以 `blocked by policy` 拒绝，未尝试绕过。保留两个链接；禁止对其执行 npm ci、删除或递归清理此工作树，后续由主代理统一处置。

- 最终门禁：`go test ./...` 退出 0，完整输出保存在本工作树忽略文件 `.tmp/go-test-final.log`；其中 reading、api、database、scanner 及其余所有包通过。该固定门禁基于 `021676b` 加本票实现。实现提交 `a1792aa` 后，通过 merge 提交 `783a281` 纳入集成最新 `bf1628d`；唯一冲突为 `controller_progress.go` 中双方各自删除的旧 helper，按双方删除结果解决，保留 02 的 `SyncPositions` 适配与规则。

- 合并后复核：`golangci-lint run --allow-parallel-runners` 再次 `0 issues.`，gofmt 与 diff-check 空；前述页面命令增加 `src/pages/book-reader/offlineReader.test.ts` 后 8 files / 53 tests 通过。

- 合并后 `go test ./internal/reading ./internal/api` 退出 0（reading 4.898s、api 75.415s），完整输出保存在 `.tmp/go-test-after-merge.log`。实现与 merge 已提交，全部验收项满足，票据标记 done。
