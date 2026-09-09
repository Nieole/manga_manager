# 03 — 合并时端点如实回话

**What to build:** 用户点「强制重扫」，界面回一句「已发起」，然后什么都没有按他要的方式发生。
那个库已经有一条扫描**排队中**时（守护 tick 或监听扫描在长扫期间排下的很常见），这次发起会
**合并**进那一条：被丢掉的是本次的声明**与任务体**，跑的仍是先排上那份普通扫描。

引擎其实知道——它交回的 `Launched` 上带着「本次被合并了」这一格，doc 也写明「本次交出的任务体
不会执行」。是资料库扫描的启动点把它丢掉了，端点因此无条件回「已发起」。

**语义一个字不改。** 合并仍是合并，只是不再骗人。

**Blocked by:** None — can start immediately

**Status:** done

- [x] 资料库扫描的启动点不再丢弃引擎交回的 `Launched`
  —— 端点 `scanLibrary` 改走 `startLibraryScanRun`：它**本来**就交回 `Launched`（文件监听那条路
  一直在用它），端点此前只是在两个函数里挑错了那一个。薄壳 `launchLibraryScanTask` 里那个 `_`
  **保留**：建库首扫、守护 tick、重启函数这三个调用方发起完就不过问结果，改签名只会逼三个
  不需要这个值的调用点一起改。票面「是启动点把它丢掉了」这句因果与代码对不上，详见挂账 D21。
- [x] 手动发起、本次带**强制**、且本次发起被**合并**时，端点回一句说明：已并入正在排着的那次扫描，那次不是强制的
  —— 判据抽成 `scanCoalescedNoticeCode`，响应体为 `{"status":"Scan coalesced","message":<按 locale 的那句>}`。
  另加两行前端（`Layout.tsx`）读回这句话：那个端点的响应体本来被整个丢掉、toast 是写死的，
  只改后端的话用户看到的一个字都不变，详见挂账 D22。
- [x] 新增两条 i18n 词条（中英各一）
  —— `library.scan.force_coalesced`，落在 `internal/api/messages.go` 两张表里；
  既有的 `TestAPITextLocalization` 已经强制两张表 key 一致，缺一侧当场变红。
- [x] 在**真路由**上守它：摆出「一条扫描停在活动态 + 一条排队 + 再来一次强制」的局面，断言 HTTP 响应说的是被合并而不是「已发起」（前例：既有的自动扫描装置，它注入的 `blockingScanStore` 已经能让扫描停在可控点并由用例放行）
  —— 走 `SetupRoutes` + `httptest.Server` 的真路由，因此路径匹配、路径参数与 CSRF 都真的过了一遍；
  建立首个管理员之前鉴权闸门对所有非公开端点一律 401，用例为此先 setup 登录。活动那一格由
  `seedTask` 占（它同样是一个真的在跑、停在可控点上的任务体），`blockingScanStore` 仍然要——
  它拦住下一条用例里新建的那条排队运行，否则断言会跟一条正在收尾的运行赛跑。
- [x] 守边界：队列里**没有**排队运行时，强制重扫照常新建一条排队运行并写下本次的声明，强制完整保住（新用例）
- [x] 守边界：守护扫描与监听扫描被合并时**不**回这句话——它们没有人在等答案
  —— 断言落在判据 `scanCoalescedNoticeCode` 上而不是 HTTP 响应上：自动发起那两条路不经过任何
  handler，没有响应可断。「它们被合并时安安静静」由既有的两条用例各守一半。
- [x] **不做**：不把强制立成**变体**；不让本次发起顶替排队那条的声明与任务体；不给**运行句柄**加读回自己入参的面
  —— 三条都没做，且第二条有反向断言：合并之后排队那条的**发起方**仍是 scheduled、它自己的
  `force` 仍是 false、`CoalescedCount` 加到 1。`RunSpec` 与 `runhandle` 一个字没动。
- [x] `CHANGELOG.md` 记一条
- [x] 后端与前端门禁全绿
  —— `go build` / `go vet` / `check-doc-style.sh` / `vitest`(75 文件 819 例) / `npm run build` /
  `npm run lint` 全 0。`go test ./...` 唯一一条红是基点自带的 `TestStorageIOCoverRateComesFromTheCoverRun`
  （`cover=0.000000`，命中率随负载浮动，记在 `address-by-object` 的 D63），与本票无关。
