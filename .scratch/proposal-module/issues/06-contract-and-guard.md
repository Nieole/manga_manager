# 06 — 收缩：删旧接口，立守卫

**What to build:** expand–contract 的收缩步。所有调用点都已迁移、所有用例都已改打模块接口之后，把 Controller 上的旧裁决方法与哨兵错误整体删除，并立一条守卫，让「绕过模块直接查提案表」这件事不再依赖任何人记得住。

**这一步必须与前面的迁移落在同一个 PR 内。** 仓库现有接口里那条「i18n 码版 / 字面量版」的分裂轴，就是一次停在半路的迁移冻结进签名的产物——生产走一支、测试走另一支，两套词汇永久共存。用同样的方式收尾只会再造一条相同的疤。

**Blocked by:** 02, 03, 04, 05

**Status:** done

- [x] Controller 上的入队、应用、拒绝、按系列加载等旧裁决方法整体删除
- [x] 四个哨兵错误（无变更、全被锁、此前已拒绝、非 pending）删除；模块不导出哨兵供调用方 `errors.Is`
- [x] 全仓不再有任何代码路径能绕过模块修改提案状态
- [x] `internal/api` 内不再出现任何提案表的查询调用
- [x] 上一条由一条源码扫描守卫用例守住，按符号名扫描。守卫**不开白名单**——第一条白名单会让它变成一份需要维护的例外清单
- [x] 守卫用例的 doc 写明它为何存在：编译器在这里拦不住（api 仍握着全量存储接口），因此它有真实对象，不适用「结构性成立后守卫失去对象就该删」的先例
- [x] 无任何遗留的兼容垫片或过渡别名
- [x] `CHANGELOG.md` 记本批次的架构改动，以及两条用户可见变化：
  - 批量处理时被别人抢先的条目不再报成失败，单独成桶且不置为错误色（票 04）
  - 打开有多条待裁决提案的系列时不再随提案条数发起额外查询（票 01）
  - 除这两条外若发现还有行为变化，那是回到 spec 讨论的信号，不是一条顺手写进 CHANGELOG 的注脚
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...`、`cd web && npm run test`、`cd web && npm run build` 全绿

**落地说明：**

「按系列加载」`loadSeriesMetadataReview` **保留**——它在票 05 之后已经不是裁决方法，只剩视图
装配（`proposal.SeriesProposals` → `metadataReviewResponse`），且被系列详情上下文与
`/metadata-review` 两个入口共用。删掉等于把同一段映射抄两遍，与票 05「`*View` 结构体与映射
函数留在 api」相抵触。

守卫的符号名从 sqlc 生成物反推：每个查询常量以 `-- name: 方法名 :kind` 开头、常量体就是那条
SQL，取 SQL 里提到提案表的那些。写死清单会腐坏——新加一条打提案表的查询，守卫仍按旧名字扫，
看着绿其实没在守。守卫连**用例文件**一起扫：一个直接读提案行的断言与一次绕过模块的生产调用，
在这条边界上是同一回事。为此三处断言改走模块或 handler（批量拒绝改断言桶 + 系列未被写入 +
离开收件箱，比原先的 `status == "rejected"` 更贴用户可见的性质）。

`metadataDefaultConfidence` 随入队方法一起删除，其用例搬进 `internal/proposal` 打
`defaultConfidence`；`metadataSourceURL` **不删**——它唯一的调用方是刮削搜索预览，与提案无关，
已从提案 handler 文件挪进 `scrape_controller.go` 并改名 `scrapeResultSourceURL`。

spec 里「同批修改 CONTEXT.md 的**提案**词条」这条没被任何一张票认领，本票一并落地。

**⚠ 发现第三条行为变化，按本票的规则应回 spec 讨论：** 票 03 把单条应用的加载迁进模块时，
顺带改了故障路径的分类。base 上 `GetMetadataReview` / `GetSeries` 的**任何**错误都折成 404
（「提案不存在」/「系列不存在」），现在只有 `sql.ErrNoRows` 回 404，其余数据库故障回 500；
另有两句 500 文案被并进 `Failed to apply metadata review`。新行为更诚实（数据库抖动不该被说成
「这条东西没了」），且票 03 的验收清单只钉了「409 语义不变」、没覆盖这里，因此没被拦下。
已如实记进 CHANGELOG，**但接受还是回滚由裁决方定**。

**遗留（不属本票）：**
- `ListMetadataReviewFields`（逐条版）与 `ListMetadataReviewsBySeries` 现已全仓零调用方，但它们
  是 sqlc 生成物，清掉要改 `sql/query.sql` 并重跑 `sqlc generate`（按 AGENTS.md 只能在
  PowerShell 里跑）。守卫不依赖它们是否存在。
- `scrapeResultSourceURL` 与 `proposal.resolveSourceURL` 是跨接缝的同源副本，两边会各自漂移。
- 守卫的符号名只从 sqlc 生成物反推：将来若在 `internal/database` 手写一个打提案表的 Store 方法，
  它扫不到。今天没有那条路，注释里写明了。
- **终态**在 CONTEXT.md 里只为后台任务定义（明列四种），`internal/proposal` 却也用它指提案的
  已应用/已拒绝。要么给提案补一条词条，要么换词。
