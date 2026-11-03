---
id: mm-architecture
type: architecture-design
status: active
title: 系统架构与依赖边界
parent: manga-manager
references: [mm-domain-language, mm-adr-0001, mm-adr-0003, mm-adr-0004, mm-adr-0005, mm-adr-0006, mm-adr-0007, mm-adr-0008, mm-adr-0009]
---

## 系统形状

单进程 Go 服务使用 chi 接收 HTTP，在 SQLite 中保存索引及用户状态，并内嵌 React/Vite 前端。归档文件仍是藏书内容来源，封面和页图缓存是派生数据；可选元数据源、LLM 与超分引擎在各自适配边界外。

`cmd/server` 持有配置加载、数据库迁移、HTTP 中间件、静态资源与进程信号。`internal/api` 适配 Web、OPDS、Mihon、KOReader 与 SSE；它通过 `application/runtime` 装配和启动应用能力，而不是持有第二套业务规则或调度器。

以下归属由实际导入、完整用例与集成测试核对。禁止反向依赖的表述是既有设计约束，不宣称编译器自动检查了每条边。

## 功能归属与依赖

- `reading` 持有阅读事实及用户投影；`organize` 持有藏书编排，阅读列表的继续阅读依赖 `reading`。两者使用 `database`，不依赖 HTTP 或应用总对象。
- `seriesmeta` 持有各来源的系列元数据写入策略；`proposal` 组合 `metadata` 候选与 `seriesmeta` 完成裁决。`scanner` 通过 `seriesmeta` 导入内嵌元数据，并使用 `parser`、`images` 和 `koreader` 的指纹能力。
- `koreader` 持有设备账号、协议记录与指纹匹配，匹配后的书位置交给 `reading`；`external` 管传输会话，不成为扫描资料库。
- `application` 组合扫描、维护、外部磁盘作业、智能生成及迁移的完整协作链，使用 `task.Engine` 的统一发起口。`application/runtime` 构造这些能力和重启注册表，并拥有恢复、后台输入和停止等待。
- `relocation` 持有持久映射、补偿、配置协调与保护；应用迁移编排负责独占访问、监听、阅读缓存与外部会话交接。它不是一个与 IO、任务和数据库无关的纯领域包。

## 基础能力的实际边

- `taskstore → task`：SQLite 实现领域持有的 Store 端口。`task` 使用 `runhandle`、`diskwork`、`taskcontrol`，不得反向依赖 SQL、`database` 或 HTTP。
- `database → taskstore`：统一迁移入口安装任务表；这是实际存在的迁移依赖，不是任务引擎绕道访问数据库。
- `runhandle → diskwork → storageio`：运行资格、一次后台磁盘作业与卷级调度不同；`taskcontrol` 提供协作式暂停。运行槽位、卷令牌和暂停闸门不得合并成一个概念。
- `config` 管配置值、快照和更新；`runtimecfg` 依赖它并应用 `parser`、`images` 与 `logger` 的资源效果。所有组装能力共享同一个 `config.Manager`。
- `booksort` 共享书排序，`logger` 提供结构化日志；这些支撑能力不拥有阅读或编排用例。

这些跨兄弟模块的边集中记录于本节点；叶规格只解释自己的责任、公开契约与禁止越界。

## 浏览器边界

`web/src/App.tsx` 决定路由和会话订阅范围。`auth` 持有身份；`session` 管一条作用域内的 SSE、协议解码及刷新水位；`features` 持有功能查询与投影；页面持有筛选、选择、弹窗和布局。阅读器持有当前书、跨书下载及本机离线状态的不同生命周期，不能把它们归入全站会话。

客户端只消费 API 与推送契约，不承担后端规则。生成契约由 `cmd/tsgen` 维护，SQL 绑定由 sqlc 维护；生成方式与验证命令仍归 `AGENTS.md`。

## 决策与规格状态

领域概念只在 `mm-domain-language` 定义；任务身份、准入、寻址、重启、存储分包、用户名和迁移的理由归本节点引用的 ADR，不另复制论证。

`mm-adr-0008` 是已裁决但未完整实施的决定，不能把逐次有损合并事件描述成现有保证。ADR 0002 已被 ADR 0005 取代，仅保留原历史文件。`.scratch/` 中的实施规划和验收记录作为输入，不提升为长期节点。

已发现的文档漂移：`internal/api/doc.go` 与 `internal/scanner/doc.go` 的部分归属仍指向 API；当前代码的应用编排归 `application`，藏书编排归 `organize`，提案裁决归 `proposal`。本图谱按实现记录，不据旧注释恢复旧边界。
