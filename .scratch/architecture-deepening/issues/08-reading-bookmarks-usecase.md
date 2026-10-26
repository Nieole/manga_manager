# 08: 书签的完整用例

**What to build:** 书签增删查通过阅读状态执行，保留用户隔离、页码约束、已有身份及页面行为。

**Blocked by:** 01: 在线阅读位置的完整用例

**Status:** ready-for-agent

**验收基准：** [父规格](../spec.md)。本票遵守其中的跨功能约束和范围，不将历史实现偏差重新定义成业务规则。

**规格覆盖：** US 26、US 28、US 50、US 52。

- [x] 通过阅读状态完整用例完成书签列表、新建更新和删除，HTTP 仅解析、提取主体和转换结果。
- [x] 保留书签身份、备注处理、合法页码及未知页数约束，不因归属迁移重新创建已有书签。
- [x] 书签的查询、更新和删除均按主体与书限定，不允许用另一用户的书签标识越权操作。
- [x] 保留缺失书、缺失书签与存储失败的既有可观察结果，书签操作不刷新阅读位置或伪造阅读活动。
- [x] 使用功能 interface、真实 SQLite 和既有 HTTP/页面先例验证完整 CRUD、跨用户隔离、边界页码和故障。
- [x] 删除本用例旧规则，不迁移图片或离线字节管理；完成相关后端及受影响前端门禁。

## Comments

- 2026-10-26：`reading.Bookmarks.List/Save/Delete` 拥有完整书签用例；仅依赖 `BookmarkStore` 的书与书签能力，不扩大位置/时间写服务的依赖。HTTP 保留解析、会话主体提取、状态码与响应转换，删除原页码夹取 helper、备注处理及直接书签存储配方。
- 公共用例的真实临时 SQLite 回归覆盖完整 CRUD、页序、同页重试与既有记录的 ID/创建时间、Unicode 备注裁剪与清空、已知页数边界、未知页数、用户零兼容、跨用户与跨书隔离、缺书和缺书签。读取公开读态与统计结果验证书签不改变位置、最近阅读时间、时长或活动；真实 SQLite trigger/表不可用/连接关闭分别验证创建、更新、删除、列表及书查询故障，失败不破坏已保存备注。
- 红绿证据：完整 CRUD 测试先因缺少 `NewBookmarks/BookmarkInput` 编译失败，再由最小用例通过；页码与备注测试先实际得到 -1/0/500 和未裁剪备注而失败，再由夹取/裁剪规则通过；缺失对象测试先缺 `ErrBookmarkNotFound` 失败，再由错误结果通过。已有存储隔离和故障约束的公共用例回归保持通过。
- HTTP 在迁移前固定空数组、身份重试、备注清空、解析错误、缺书/缺书签及存储错误的既有状态码/文案；迁移后同组回归通过。补充请求体伪造 `user_id/book_id/id`、跨用户/跨书删除，验证会话和路由身份有效且私人备注保持完整。页面使用真实 `BookmarkPanel` 与 `useReaderBookmarks`，只替换 API 边界，验证加载、新建、同页更新与按返回 ID 删除。
- 验证基线：已将集成分支合入至 `19c7f29`。`GOTOOLCHAIN=go1.26.10`、共享 `GOCACHE`、本树 `GOTMPDIR` 下 `go test ./...` 通过；最终增补的 HTTP 用例后 `go test ./internal/reading ./internal/api -run 'Test(Bookmark|ReadingBookmarks)' -count=1` 再次通过。`gofmt -l ./cmd ./internal` 输出为空；独立 `GOLANGCI_LINT_CACHE` 下 `golangci-lint run --allow-parallel-runners` 为 `0 issues.`。前端物理 `npm ci --no-audit --no-fund` 后 `npm run test` 通过 94 文件/910 测试，`npm run build`、`npm run lint` 均通过。`git diff --check` 通过。本票未变更 SQL、生成契约、归档/图片/离线字节管理，无生成漂移工作。
