# 14: 应用会话连接与普通刷新

**What to build:** 连接、解码、重连及普通藏书刷新脱离导航 implementation，保留路由活跃范围、每实例单连接及身份隔离。

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

**验收基准：** [父规格](../spec.md)。本票遵守其中的跨功能约束和范围，不将历史实现偏差重新定义成业务规则。

**规格覆盖：** US 71、US 72、US 74、US 75、US 76、US 77、US 78、US 85、US 86、US 88、US 89。

- [x] 建立独立于导航视图的应用会话 module，实际接管现有 SSE 连接、解码、重连及普通藏书刷新；不是将原代码原样挪入另一个大 hook。
- [x] 路由集成明确连接活跃范围，保留主导航路线现状；独立阅读器和图谱不新增常驻连接，每个前端实例不因多个消费者重复建连。
- [x] 保留 CLOSED 后重连及 CONNECTING 时避免双连接，退出作用域正确清理连接和定时资源。
- [x] 普通用户保留藏书刷新，敏感运行内容仍由后端管理员过滤；身份或角色变化清理旧订阅和敏感投影，鉴权依旧属于现有身份归属。
- [x] 临时断网和会话过期不按显式登出处理，不删除离线 owner、内容和队列，也不将设备级偏好改成账户级。
- [x] 为未迁消费者保留单向兼容桥，来自同一连接，不能同时保留两份权威连接或导致一次事件重复刷新；后续消费者迁移后可删除桥。
- [x] 以会话 seam 与页面回归验证角色、身份切换、主导航与独立路线切换、断线恢复和卸载；保留页面局部状态。
- [ ] 执行前端测试、构建、适用 lint 及所需传输回归；本票不要求等待后台架构迁完。

## 实施与验证

- 会话入口为 `web/src/session/ApplicationSessionProvider.tsx`，由 App 的主导航路由显式装配；传输重连由 `useEventStream` 管理，藏书刷新与已解码运行订阅通过会话 interface 交付。
- Layout 保留页面状态与 Outlet 兼容刷新信号，气泡订阅会话；任务中心和外部传输仍由同一连接的单向桥兼容。身份或角色改变时整个旧作用域释放，关闭连接之后的滞留回调不再投递。
- TDD：新增会话界面测试先因缺少 module 失败；身份切换后旧连接仍投递的行为测试先失败，再加入 disposed/source 保护后通过。
- 前端全量 `npm run test`：83 文件、869 用例通过；会话及实际 App/AuthProvider/Layout/LibraryPage 路由回归 3 文件、8 用例通过，覆盖普通刷新、多个消费者、角色和身份变化、关闭连接滞留回调、断线恢复、卸载、阅读器/图谱范围及页面局部筛选保留。既有 offlineOwner、sessionExpiryOffline、offlineAuthGate 回归随全量通过。
- `npm run build`、`npm run lint`、`git diff --check` 通过。
- 所需后端传输回归尝试：`go test ./internal/api -run 'Test(SSE|ServerEventsFiltersTaskFramesByRole|RunEventsAreNotPushed)' -count=1`。受机器 Application Control policy 阻止 Scoop gcc 执行，未开始运行测试；未绕过系统策略。因此最后一项及 Status 保留未完成，等待集成环境完成该传输门禁后关闭。
