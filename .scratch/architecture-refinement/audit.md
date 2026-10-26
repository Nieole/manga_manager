# 全项目审查线索

阶段：审查线索与证据范围。当前批准需求与实施范围以 [规格](spec.md) 为准，已确认取舍见 [设计](design.md)；以下静态候选不是运行缺陷或验收通过证明。

## 覆盖与限制

后端已清单覆盖领域、应用、手写数据库、基础设施、协议与命令目录；对核心生产文件完整阅读或阅读主要入口和关键路径，生成代码查看生成源与消费者约束。前端已完成生产 TS/TSX 清单、导出符号及网络/存储/订阅副作用索引，重点阅读会话、鉴权、离线归属、设置、资料库与系列、阅读器、任务投影、审核、统计和导航路径。API 已核对类型与方法清单，并阅读装配、资料库管理、读态、搜索、配置及系列入口。

这是跨大多数主要功能的架构审查，不等同于逐行覆盖所有文件。全部 SQL/迁移分支、上游适配分支、扫描失败路径、归档格式细节及所有前端页面尚未逐项验证；本轮未运行测试，不将静态推断写成运行故障。

## 设计候选

| 方向 | 当前证据 | 待讨论的收益与约束 |
| --- | --- | --- |
| 藏书事实与读者视图 | reading.Queries.ProjectBooks 原地覆盖 database.Book 的阅读字段 | 显式表示已绑定主体的查询视图，减少漏做投影的调用知识；保留 SQL 分页前的主体筛选 |
| 事务能力 | reading 与 organize 的部分 ExecTx 回调暴露完整 database.Queries | 按原子不变量收窄事务 seam；不统一引入泛型 CRUD 仓储 |
| 提案并发裁决 | proposal.Service.Apply 的部分应用分支保留并发拒绝后可能写入的旧取舍 | 是否要求部分应用也在事务内确认仍待裁决，须明确裁决 |
| 派生作品群恢复 | franchiseRebuilder 以内存 pending 合并，失败日志不构成持久恢复 | 明确允许陈旧多久、谁负责重试与恢复；保留稳定身份及差集更新 |
| 资料库管理 | api.createLibrary/updateLibrary/deleteLibrary 仍组合校验、落盘、监听、运行及缓存 | 建立完整应用用例，并明确数据库失败与已发生外部效果的结果 |
| 应用装配与宿主 | api.assembleRuntime 构建恢复 runtime，能力再回填 Controller | 区分装配根、运行生命周期与 HTTP 能力；保持恢复先于开放及停机排空顺序 |
| 迁移恢复过程 | relocation.Service 包含状态、SQL、配置 journal、核验和进度 | 区分持久过程、决策及存储适配；不可丢失路径已提交、补偿、独占保留约束 |
| 扫描摄入与安全清理 | scanner 承担发现、归档解释、身份重连、入库、清理和封面交接；watcher 持协调状态 | 依据身份与清理不变量组织内部模块；改名启发式与单一运行准入不能隐式改变 |
| 书身份与设备匹配 | scanner 与 maintenance 使用 koreader 指纹能力 | 区分书身份、内容证据与设备文档键，讨论中立能力归属；保留算法兼容性 |
| 外部传输边界 | external.Manager.CreateSession 当前仅检查当前资料库根重叠 | 与 CONTEXT.md 的外部库词义对照，裁决另一资料库是否允许作为外部路径 |
| 文件操作与资源 | external.CopyFile 整本复制无 ctx；归档回写涉及池、暂存及替换 | 定义取消粒度、读写访问与失败结果；活动句柄影响仍需测试核实 |
| 配置与进程资源 | config.Manager 发布边界及 runtimecfg.Apply 涉及 parser/images/logger 全局状态 | 以完整热更新和资源生命周期分析；单例与技术辅助本身不是领域缺陷 |
| 元数据集成能力 | Provider/AIProvider、配置工厂、出站预算与各供应商 DTO | 按实际消费能力审视依赖，保留来源差异与技术适配 |
| 人工元数据编辑 | seriesmeta 已持来源规则，人工编辑事务与统计后续仍由 API 组合 | 为完整编辑用例确定应用 owner，保留已批准的来源语义与事务契约 |
| 查询与契约 | API 搜索、系列上下文、仪表盘及 task HTTP/SSE 投影 | 区分查询模型、传输契约与领域模型；tsgen 名称和外部 JSON 兼容需要显式维护 |
| 旧写入口与宽 Store | database.Store 暴露全量 Querier/DB，部分旧写方法未见生产调用 | 核实测试与迁移用途后收窄可绕过新规则的入口；装配根获取 DB 有合理用途 |
| 任务声明语义 | Args/Labels 已存在，部分扫描环境仍混入 Args | ADR 0008 尚待完整实施，须单独确认本轮范围；日志、事件、采样保留不同含义 |
| 前端离线生命周期 | AuthProvider 依赖 pages/book-reader/offlineReader | 明确身份、离线归属、缓存与补传的应用能力，保持断网与显式换人的差异 |
| 前端配置和编辑模型 | SettingsProvider 共装配多类功能；SeriesEditForm 使用 Partial<Series> | 审视独立状态生命周期与读模型/编辑意图，避免全站大对象及持久化形状渗透 |
| 前端裁决投影 | AIGroupingReviews.settledReviewStatus 复刻后端收尾规则 | 讨论动作返回权威候选与整单汇总，前端合并投影并保持局部视图状态 |
| 查询与写后失效 | useLibrarySeries.refreshLoadedSeries 与 useSeriesContext.reload 同时取数和发布跨功能失效 | 区分重试查询、已提交写入、部分成功及推送恢复，由动作结果表达实际影响 |
| 功能接口与 UI 类型 | task-observation 导入 TaskCenter/SidebarTaskBubble 类型，external-library 导入页面类型 | 明确功能投影及交互模型的所有者，让呈现消费功能接口 |
| 查询降级语义 | Stats/Dashboard 部分失败降为零或空，合集查询有忽略错误保留旧值的分支 | 按功能区分无数据、未加载、失败和陈旧，不强推全站通用状态框架 |
| 前端资料库完整用例 | Layout 持有新增、编辑、删除与导航刷新 | 与后端资料库用例协同设计完整结果，导航壳消费功能能力 |
| 账户与会话管理 | auth_controller 的首管理员初始化、改密、角色修改、删账户组合 Store 操作与会话效果；users.UpdateUserProfile 将最后管理员校验留给调用者 | 审视账户不变量的原子保证、初始化与旧数据归属、会话撤销和鉴权缓存；首建与最后管理员并发场景尚未运行复现，不直接定性为漏洞 |

前端纯展示组件、语言文本、CSS/主题、手势数学、Service Worker 全部实现及每个设置子页面未逐项深读。异步保护的不同实现形状只作为进一步核查线索；没有复现 A→B→A 等场景前不认定已有回归失效。

## 当前应保留的分工

task/taskstore 的领域与落盘分包有 ADR 依据；seriesmeta 的来源差异、reading 的位置与统计故障分离、storageio 与 diskwork 的不同职责均有业务或技术依据。传输 DTO、parser 格式模型、缓存、日志及合理辅助函数不统一搬入领域层。当前未发现足以直接拆多 bounded context 的独立词义冲突。
