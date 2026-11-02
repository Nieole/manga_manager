# 43: 阅读与目录 HTTP 依赖迁移

**What to build:** 阅读、目录和查询路由使用功能级窄能力处理器，保留读取、评价等实际消费者及外部协议。

**Blocked by:** 07: 退出旧读者模型兼容面；42: 独立装配根与新宿主扩展。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 4、5、6、7、8、9、10、13、18、28、30、152、163。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 广泛宿主迁移批次。

- [x] 迁移本家庭处理器及路由真实调用，不再依赖全量 Controller 服务镜像或 Store。
- [x] 读取与目录结果只经已完成能力，评价复用完整命令；合理 HTTP 辅助仍留适配层。
- [x] 旧宿主兼容面仍服务未迁家庭，已迁路由不再走旧配方。
- [x] HTTP、生成契约及页面消费者回归通过，批次可独立保持 CI 绿色。

## Comments


### 2026-11-02 实现与验收证据（保持开放，待父树集成）

- 消费基线 `da6811b`，工作树 `C:/Users/saber/dev/manga-refinement-43` / `codex/refinement-43`；恢复时保留全部已有 tracked/untracked 实现与 `.tmp/t43-*.log`，未提交或操作索引。
- `bootstrap.Open` 创建 `ReaderCapabilities` 并通过 `ReadingDependencies` 注入 Host。`ReaderHTTP` 接收阅读查询/位置/标记/时长/书签、目录搜索、系列上下文、评价完整用例以及只读 `ReaderQueryStore`；不持有 Controller、Runtime、全量 Store 或另建应用。统计与推荐仍为原宿主共享实例；Engine、Manager、鉴权、SSE 不复制。
- 阅读、系列目录/详情、全局搜索、统计/推荐、个人评价及 OPDS/Mihon 路由均直接挂载 ReaderHTTP 方法。`controller_series.go` 仅迁走读处理器，编辑区不动；`controller_library.go` 仅迁走 `getSeriesByLibrary`，其他资料库实现留给45。页清单/资源字节及其错误诊断仍由45的内容传输家庭拥有，协议通过窄资源回调复用。
- 迁移不改变排序/分页/DTO/状态码/协议资源链接/匿名与主体规则；现有 reading、catalogsearch、seriescontext、seriesreview owner 保留权威。协议合集/智能书架/阅读清单仅消费现有窄查询回调，44接续其拥有者绑定，不在本票复制配方。

#### Red → green 与实际门禁

- 中断前保留的 `.tmp/t43-red.log` 是独立 ReaderHTTP 消费者缺少入口的编译红灯；`.tmp/t43-green{1,2,3,4}.log` 记录逐次修正与最终消费者通过（早期错误输出不当成功）。
- 恢复后完整 API 首跑真实发现 `TestDuplicateBooksRequiresAdmin` 回归：扁平 reader 通配路由遮蔽旧 `/books` 子路由，见 `.tmp/t43-api-full.log`。修复为在同一 chi 子树内组合功能路由；`.tmp/t43-route-green2.log` 显示原失败消费者及 ReaderHTTP 消费者通过，不改测试权限或预期。
- 完整 `go test ./internal/api ./internal/bootstrap -count=1`：API 121.787s、bootstrap 0.589s 通过（`.tmp/t43-full-green.log`）。
- 定向 `go test -race ./internal/api ./internal/bootstrap`：HTTP 阅读/书签/离线批次/目录/评价/搜索/统计/推荐/OPDS/Mihon/协议鉴权及单 Engine/Manager/cache 消费者实际通过，API 113.558s、bootstrap 5.331s（`.tmp/t43-race.log`）。
- 新增生产 `bootstrap.Open` → Host 回归实际验证 session 与 Basic 共享已提交位置、协议写后同用户看板缓存失效、评分完整保存/删除、目录与 duplicates 路由共存、OPDS scoped resource URL 和未认证 Basic 401；完整 bootstrap race 13.614s（`.tmp/t43-bound-race.log`）。初次新测试误期待到末页即显式标记已读，按既有位置/标记分离契约修正测试，未改变生产规则（`.tmp/t43-bound.log`、`t43-bound2.log`）。
- `GOTOOLCHAIN=go1.26.10`；共享主树 `.gocache`、本树 GOTMPDIR/lint cache。`go build ./...` 通过（`.tmp/t43-build.log`）；最终 `golangci-lint run` 0 issues（`.tmp/t43-lint-final.log`），`gofmt -l ./cmd ./internal` 无输出（`.tmp/t43-gofmt.log`）。首次 lint 的4个未使用新兼容转发已移除；未删除仍有调用的旧构造。
- `go run ./cmd/tsgen` 26 types，`web/src/api/generated.ts` 无差异（`.tmp/t43-tsgen.log`）。没有 SQL/schema 或 DTO 变化。
- 前端全量 106 文件 / 990 项通过，ESLint 与 TypeScript/Vite build 通过（`.tmp/t43-web{,-lint,-build}.log`）。当前 shell 无 npm，用既有 Node v24.15.0 直接执行 node_modules 下 Vitest/ESLint/tsc/Vite；依赖为忽略的本地 hardlink 副本，未改主树。初始忽略 web/dist 从主树副本提供，最后已本树重新构建。

#### 47 收缩退出清单 / 集成交接

- `reader_http_compat.go` 的 Controller 方法仅向 ReaderHTTP 正向转发，供旧单处理器 fixture/嵌入调用；生产迁移路由不经这些方法。47迁移调用者后删除转发、`newReaderHTTP` 的旧构造 fallback 与 `Controller.reader` 访问器，保留只装配一次的 reader capabilities。
- `Host` 总装暂经兼容 Controller 组合同一鉴权/资源/编排窄回调；44替换合集/智能书架/阅读清单查询绑定，45替换内容传输与 run DTO 投影绑定，46替换配置快照/协议开关/Basic 中间件绑定，47移除最终兼容总装，而不是引入第二套缓存或服务。
- 共改点：`controller.go` 的功能路由注册、`host.go` 的 ReadingDependencies/Bind、`bootstrap.go` 的阅读 owner 创建；`controller_series.go` 只涉及读 handler 区；`controller_library.go` 只涉及只读系列列表区。父树合并44–46时需保留同一 chi 子树组合，不能再次注册同一路径的独立 Mount。
- 未运行真实设备/外部供应商或其他平台；未触碰已由集成修复负责的 scanner cleanup/restoration race。工单保持 `ready-for-agent` 与验收框开放，待父树整合后最终核对。

- 最终移动兼容构造到 `reader_http_compat.go` 后重验：完整 API 135.717s、bootstrap 0.638s；完整 build 通过（`.tmp/t43-final-{tests,build}.log`），lint 0 issues（`.tmp/t43-lint-final2.log`）、`git diff --check` 无输出（`.tmp/t43-diff-check.log`）。
