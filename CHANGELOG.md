# Changelog

本文件记录项目各版本的功能新增、改动与修复。

---

## [Unreleased] — 2026-04-10

> 本次集中改动对应 [面向公开发布的系统性改进路线图] 中的 **P0 发布阻塞** 全部五项、**P1 产品完善**（设置页信息架构、日志与任务中心、库管理交互、元数据工作流）以及 **P2 体验增强**（仪表板产品化、文案与视觉统一）。下方各节标题中的 `[Pn]` 标记对应路线图条目。

### ⚡ 重大变更 (Breaking Changes)

#### LLM 配置结构重构 `[P0: 修正 OpenAI/兼容 LLM 配置模型]`
- **`config.yaml` 中 `llm.endpoint` 字段已废弃**，拆分为 `base_url` + `request_path` 两个独立字段。
  - `base_url`：仅包含协议和主机部分（如 `https://api.openai.com`）。
  - `request_path`：仅包含 API 路径（如 `/v1/responses`）。
  - 旧的 `endpoint` 字段仍被保留用于向后兼容，启动时会自动拆分并迁移。
- 新增 `llm.api_mode` 字段，用于显式声明 OpenAI 协议模式：
  - `"responses"` — 使用 OpenAI Responses API（默认）。
  - `"chat_completions"` — 使用传统 Chat Completions API。
- 原 `provider: "openai-legacy"` 在规范化阶段自动映射为 `provider: "openai"` + `api_mode: "chat_completions"`。

#### 默认扫描格式变更 `[P0: 统一文件格式能力声明]`
- 默认扫描格式从 `zip,cbz,rar,cbr,pdf` 调整为 `zip,cbz,rar,cbr`，移除了 PDF。
- 数据库 `schema.sql` 中的 `scan_formats` 列默认值同步更新。

---

### 🚀 新功能 (Features)

#### 后端 — 配置管理 `[P0: 配置校验 + LLM 配置模型]`

- **配置自动规范化**：新增 `NormalizeConfig()` 和 `normalizeLLMConfig()` 函数，在配置加载及默认配置创建后统一补全缺失字段并执行迁移逻辑，包括：
  - 自动设置 `server.port`、`database.path`、`cache.dir` 等系统级默认值。
  - `endpoint` → `base_url` + `request_path` 自动拆分（`splitEndpoint`）。
  - 根据 `request_path` 自动推断 `api_mode`（`inferAPIModeFromRequestPath`）。
  - `BuildLLMEndpoint()` 函数：从 `base_url` + `request_path` 拼装最终请求地址。
- **配置校验（Validation）**：新增 `ValidateConfig()` 函数，返回 `ValidationResult` 结构体（含 `valid` 布尔值及 `issues` 列表），前端可据此高亮问题字段并阻止保存。
- **系统能力查询 API**：新增 `GET /api/system/capabilities` 端点，返回：
  - `supported_scan_formats` — 支持的扫描格式列表。
  - `default_scan_formats` — 默认扫描格式 CSV。
  - `default_scan_interval` — 默认扫描间隔。
  - `supported_llm_providers` — 支持的 LLM 提供商列表。
  - `supported_llm_api_modes` — 支持的 API 模式列表。
- **配置响应增强**：`GET /api/system/config` 返回结构由单纯配置对象升级为包含 `config`、`validation`、`capabilities` 三个字段的 `SystemConfigResponse` 信封。

#### 后端 — 任务管理增强 `[P0: 增强后台任务的用户反馈闭环]`

- **任务模型扩展**：`TaskStatus` 结构体新增字段：
  - `scope` / `scope_id` — 任务作用域及关联实体 ID（library / series / system），由 `inferTaskScope()` 自动推断。
  - `error` — 失败任务的详细错误信息。
  - `can_cancel` — 标记任务是否可取消（预留）。
  - `retryable` — 标记任务是否允许重试。
  - `params` — 任务启动参数快照（如 `provider`），用于重试时复现。
  - `started_at`、`finished_at` — 任务开始与结束时间。
- **任务列表过滤**：`GET /api/system/tasks` 支持 `status`、`scope`、`type`、`q`（关键词搜索）、`limit` 查询参数。
- **任务重试 API**：新增 `POST /api/system/tasks/{taskKey}/retry` 端点，支持重新启动已完成 / 失败的任务（`scan_library`、`scan_series`、`cleanup_library`、`rebuild_index`、`rebuild_thumbnails`、`scrape`、`ai_grouping`）。
- **刮削任务重构**：`batchScrapeAllSeries` 和 `scrapeLibrary` 拆分出可复用的 `launchBatchScrapeAllSeriesTask()` / `launchLibraryScrapeTask()` 内部方法，同时记录 `params` 以便 `retryScrapeTask()` 使用。
- **失败任务错误跟踪**：新增 `failTaskWithError()` 方法，失败任务会同时保留 `message` 与 `error` 两个独立描述。

#### 后端 — 日志系统增强 `[P1: 改善日志与任务中心]`

- **日志响应结构变更**：`GET /api/system/logs` 返回结构从纯日志数组改为 `{ items: [...], summary: { total, by_level } }` 格式。
- **日志关键词搜索**：支持 `q` 查询参数，在 `raw` 和 `msg` 字段中进行不区分大小写的全文搜索。
- **日志计数统计**：返回的 `summary` 包含筛选后的总条目数及按级别（ERROR / WARN / INFO）的分布。

#### 后端 — 扫描器改进 `[P0: 统一文件格式能力声明]`

- **统一格式判断**：`scanner.go` 中的硬编码扩展名判断替换为 `config.IsSupportedArchiveExtension()` 调用，与全局配置保持一致。
- **文件监听器格式同步**：`watcher.go` 中的 `formats` 切片不再硬编码，而是从 `config.SupportedScanFormats` 动态生成。

#### 后端 — 元数据提供者 `[P0: 修正 OpenAI/兼容 LLM 配置模型]`

- **`NewAIProvider` 签名变更**：从 `(provider, endpoint, model, apiKey, timeout)` 改为 `(provider, apiMode, baseURL, requestPath, model, apiKey, timeout)`，内部通过 `config.BuildLLMEndpoint()` 组装最终端点。
- 自动识别 `openai-legacy` 提供者并降级为 `openai` + `chat_completions` 模式。

#### 前端 — 仪表盘 (Dashboard) `[P0: 降低首次使用门槛]` `[P2: 仪表板更产品化]`

- **首次使用引导页**：当用户没有任何资源库时，展示 Onboarding 卡片引导用户：
  1. 添加资源库。
  2. 检查系统设定。
  3. 开始首次扫描。
- **失败任务通知区**：仪表盘顶部展示最近最多 3 个失败的后台任务，包含错误信息。
- **库列表与任务数据预加载**：页面加载时并行拉取 `/api/libraries` 和 `/api/system/tasks` 数据。

#### 前端 — 设置页 (Settings) 重构 `[P0: 配置校验]` `[P1: 重构设置页信息架构]`

- **全面重写**：Settings 页面逻辑和 UI 大幅重构。
- **配置信封解析**：从新的 `SystemConfigResponse` 结构中提取 `config`、`validation`、`capabilities`。
- **实时校验反馈**：保存时若后端返回 422，直接解析 `validation` 并在对应字段下方渲染错误提示。
- **LLM 配置 UI 改进**：
  - Provider 选择改为从后端 `capabilities.supported_llm_providers` 动态渲染。
  - 新增 API Mode 选择器（`responses` / `chat_completions`）。
  - `base_url` 和 `request_path` 分离输入。
- **样式统一**：提取 `sectionClassName` 和 `inputClassName` 常量，组件风格统一。

#### 前端 — 日志页 (Logs) 重构 `[P1: 改善日志与任务中心]`

- **双面板布局**：日志页不再仅展示日志条目，新增**后台任务面板**，左右分栏展示：
  - 左侧：系统日志（支持级别过滤、关键词搜索）。
  - 右侧：后台任务列表（支持状态、作用域过滤及关键词搜索）。
- **任务重试按钮**：对可重试的失败任务，页面上提供一键重试按钮。
- **日志复制功能**：每条日志旁新增复制按钮，可将原始日志文本复制到剪贴板。
- **日志级别统计栏**：页面顶部展示 ERROR / WARN / INFO 各级别的条目计数。
- **UI 全面中文化**：所有文案从英文替换为中文。

#### 前端 — 系列搜索弹窗 (SeriesSearchModal) 增强 `[P1: 完善元数据工作流]`

- **双栏预览布局**：从单栏列表改为左右分栏——左侧为搜索结果列表，右侧为选中条目与当前系列的**逐字段对比**。
- **元数据差异对比**：预览面板按字段（标题、简介、作者、出版商等）展示「当前值 → 即将更新值」，并标注变更计数。
- **选中状态高亮**：选中条目显示 `已选中` 徽章和边框高亮。
- **弹窗标题改进**：从"选择最佳匹配条目"改为"预览并选择元数据来源"，增加來源提示文案。

#### 前端 — 侧边栏 / Layout `[P0: 统一格式能力声明]` `[P1: 优化库管理与目录选择交互]`

- **最近路径记忆**：新增资源库时，自动记录最近使用的路径到 `localStorage`，`DirectoryPicker` 可利用这些历史路径提供快速选择。
- **扫描格式动态获取**：从 `GET /api/system/capabilities` 获取默认扫描格式，替代前端硬编码。
- **任务进度条改进**：进度条根据 `status` 字段（`completed` / `failed`）判断是否自动消失，不再仅依赖 `current >= total`。
- **错误信息提取改进**：新增 `extractErrorMessage()` 工具函数，可从 Axios 错误响应中提取校验 issue 或 error 消息。
- **全局事件绑定**：Dashboard 的 Onboarding 卡片可通过 `manga-manager:open-add-library` 自定义事件触发侧边栏添加资源库弹窗。

---

### 🐛 修复 (Bug Fixes)

- 修复任务完成后 `error` 字段未被清空的问题（仅在 `status != "failed"` 时重置）。
- `scrape_controller.go` 响应体不再返回 `total` 字段（该信息已由 task SSE 推送），避免前端混淆。

---

### 🧹 重构 (Refactoring)

- `scrapeLibrary` / `batchScrapeAllSeries` HTTP handler 与后台任务启动逻辑解耦，分为 handler（处理 HTTP 请求/响应）和 `launch*Task()`（可复用的启动方法）。
- `completeTask` 和 `failTask` 路径合并为统一的状态更新逻辑。
- `metadata.NewAIProvider` 工厂方法参数从拼装完成的 endpoint 改为结构化的 `baseURL` + `requestPath`，内部统一调用 `config.BuildLLMEndpoint()`。

---

### 🧪 测试 (Tests)

- 新增 `collection_controller_test.go`：覆盖收藏集 API 的边界场景。
- 新增 `opds_controller_test.go`：覆盖 OPDS 协议端点的边界场景。
- 扩展 `controller_test.go`：覆盖任务管理、API 统计等新端点。
- 扩展 `scrape_controller_test.go`：覆盖刮削控制器的边界场景。
- 扩展 `log_controller_test.go`：覆盖新的日志响应结构和搜索功能。

---

### 📦 其他

- `.gitignore` 更新。
- `config.yaml` 示例文件更新以反映新的 LLM 配置字段。
- `web/src/components/layout/constants.ts` 更新默认扫描格式常量。
- `web/src/pages/series-detail/types.ts` 新增 `VolumeCount` 字段。
- `web/src/pages/SeriesDetail.tsx` 新增 `VolumeCount` 相关展示逻辑。

### 📌 增量记录 — 2026-04-13

#### 任务中心与失败恢复 `[P1: 改善日志与任务中心]`
- **任务筛选 API 完整化**：`GET /api/system/tasks` 新增 `status`、`scope`、`type`、`q`、`limit` 过滤参数，前端可以按状态、作用域和关键字做组合筛选。
- **任务重试能力落地**：新增 `POST /api/system/tasks/{taskKey}/retry`，已支持以下任务类型的重试：
  - `scan_library`
  - `scan_series`
  - `cleanup_library`
  - `rebuild_index`
  - `rebuild_thumbnails`
  - `scrape`
  - `ai_grouping`
- **任务内部模型增强**：`TaskStatus` 新增 `retryable` 和 `params` 字段。
  - `retryable` 用于前端判断是否显示重试按钮。
  - `params` 用于保存任务启动时的关键参数快照（例如刮削任务的 provider），以便重试时复现原始行为。
- **后台任务启动逻辑抽取**：扫描、清理、重建索引、重建缩略图、AI 分组、批量刮削等任务拆出可复用的 `launch*Task()` 内部方法，使 HTTP handler 和任务重试共用同一套执行路径。
- **日志页升级为任务中心**：
  - 任务面板增加状态筛选、作用域筛选和关键字搜索。
  - 对可重试且当前非运行中的任务，提供一键重试按钮。
  - 任务中心不再只展示“最近 8 条”，而是支持按条件取回更完整的任务集合。

#### 阅读器恢复体验 `[P2: 阅读器体验进一步打磨]`
- **阅读器加载失败回退**：`BookReader` 新增 `loadError` 状态。
  - 当页面列表为空，或 `/api/pages/:bookId`、`/api/book-info/:bookId` 请求失败时，阅读器会显示明确的错误卡片，而不是停留在黑屏或加载态。
  - 错误卡片提供“重试加载”和“返回系列”两个直接动作。
- **阅读帮助入口**：阅读器顶部新增帮助按钮。
  - 增加键盘快捷帮助和移动端提示。
  - 支持通过 `H` / `?` 快速切换帮助面板。
- **返回逻辑收口**：提取 `handleBackToSeries()`，统一处理从阅读器回到系列页 / 卷视图的导航逻辑，避免不同按钮各自维护回跳规则。

#### 系列页锁定字段可见性 `[P1: 完善元数据工作流]`
- `SeriesHeader` 现在会在系列页头部直接展示**已锁定字段数量与字段列表**。
- 用户在执行刮削前，不需要先打开编辑弹窗，也能知道哪些字段不会被自动覆盖。
- 这项改动与前一轮的“元数据预览对比”配合后，系列页的信息结构更完整：
  - 先在页头确认哪些字段被锁定；
  - 再在搜索弹窗中比较当前值与候选值；
  - 最后决定是否应用。

#### Changelog 维护 `[工程记录]`
- 补充记录本次及前两次改造的详细内容，覆盖：
  - 配置模型重构与能力声明统一
  - 设置页、日志页、任务中心、仪表盘与首次引导
  - 元数据预览流程与最近使用目录
  - 任务重试、阅读器错误恢复与锁定字段提示

### 📌 增量记录 — 2026-04-13（第二次续改）

#### 任务中心与页面联动 `[P1: 改善日志与任务中心]`
- **任务列表支持 `scope_id` 精确过滤**：`GET /api/system/tasks` 新增 `scope_id` 查询参数，前端可以直接按具体资源库或系列拉取关联任务，不再依赖模糊关键词搜索。
- **日志页任务跳转**：任务中心中的每条任务都新增“打开关联页面”动作：
  - `scope=series` 时跳转到对应系列详情页；
  - `scope=library` 时跳转到对应资源库页；
  - `scope=system` 时回到系统设定页。
- **系列页失败任务面板**：`SeriesDetail` 现在会主动拉取与当前系列关联的失败任务，并在页面下方展示。
  - 展示失败消息、错误详情、更新时间；
  - 对可重试任务提供原地重试按钮。

#### 阅读器恢复体验补全 `[P2: 阅读器体验进一步打磨]`
- **阅读器帮助面板**：`BookReader` 顶栏新增帮助按钮，支持通过 `H` / `?` 快速打开。
- **阅读器空页面防御**：当归档可打开但没有任何可读页面时，阅读器会明确提示“当前书籍没有可读取的页面”，不再停留在空白阅读区。
- **阅读器回退动作统一**：返回系列的导航逻辑抽成统一函数，错误态、顶部返回按钮和其他回退入口共用同一套规则。

#### 系列页恢复提示增强 `[P1: 完善元数据工作流]`
- 系列页除了显示锁定字段之外，还会直接暴露关联失败任务，形成“看到问题 -> 就地重试”的闭环。

### 📌 增量记录 — 2026-04-13（KOReader Sync 服务端）

#### KOReader 阅读进度同步 `[P0/P1: 服务端同步能力]`
- **新增 KOReader 服务端接口**：
  - `POST /koreader/users/create`
  - `GET /koreader/users/auth`
  - `PUT /koreader/syncs/progress`
  - `GET /koreader/syncs/progress/{document}`
  - `GET /koreader/healthcheck`
  - `GET /koreader/healthstatus`
  - `GET /koreader/robots.txt`
- **单用户同步模型落地**：当前版本以单用户服务为目标，不引入完整账号系统；同步认证使用 KOReader 头部 `x-auth-user` / `x-auth-key`。
- **同步记录持久化**：新增 `koreader_settings`、`koreader_progress`、`koreader_sync_events` 三张表，分别保存同步账号、文档进度和同步审计事件。
- **书籍身份指纹**：`books` 表新增：
  - `file_hash`
  - `path_fingerprint`
  - `filename_fingerprint`
  用于 Binary / 路径 / 文件名三类 KOReader 文档匹配。
- **双轨以上匹配策略**：服务端优先使用二进制哈希匹配书籍，同时保留路径和文件名指纹兜底，支持历史未匹配进度后续重关联。
- **进度投影回本地阅读状态**：当 KOReader 文档成功命中本地书籍时，会把 `percentage` 按 `page_count` 投影回 `books.last_read_page/last_read_at`，并写入 `reading_activity`。
- **防止进度回退**：服务端采用“更远进度优先”，较小的 `percentage` 不会覆盖已有较大进度。

#### 扫描器与维护任务 `[P1: 资源识别与恢复能力]`
- **扫描时自动生成指纹**：扫描器在入库/更新书籍后会同步写入 KOReader 文档匹配指纹，不再依赖纯人工维护。
- **新增系统维护任务**：
  - `POST /api/system/koreader/rebuild-hashes`：重建书籍同步指纹
  - `POST /api/system/koreader/reconcile`：重关联未匹配的 KOReader 同步记录
- **任务中心集成**：新增任务类型
  - `rebuild_book_hashes`
  - `reconcile_koreader_progress`
  并支持任务列表展示、错误提示与重试。

#### 设置页与管理入口 `[P1: 配置与可运维性]`
- 设置页新增 **KOReader Sync** 分组，支持：
  - 启用/关闭同步服务
  - 配置同步路径
  - 配置用户名与同步密钥
  - 控制是否允许首次注册
  - 查看书籍指纹进度、已匹配/未匹配同步记录数
  - 一键触发“重建书籍指纹”和“重关联未匹配记录”
- `config.yaml` 示例文件新增 `koreader` 配置段：
  - `enabled`
  - `base_path`
  - `allow_registration`

#### 测试 `[回归保障]`
- 为 KOReader 设置保存、鉴权、进度写入与进度拉取补充控制器测试。
- 已验证：
  - `GOCACHE=/Users/nicoer/dev/manga_manager/.gocache GOTMPDIR=/Users/nicoer/dev/manga_manager/.tmp go test ./internal/api ./internal/config ./internal/scanner ./internal/database`
  - `npm run build`（`web/`）

### 📌 增量记录 — 2026-04-13（资源库级 KOReader 开关）

#### 资源库配置 `[P1: 库管理交互 + 同步范围控制]`
- **新增资源库级 KOReader 开关**：`libraries` 表新增 `koreader_sync_enabled` 字段，默认开启，保持现有“全库可同步”的行为不变。
- **创建/编辑资源库弹窗支持配置**：添加资源库和编辑资源库时，可以直接决定该资源库是否参与 KOReader 阅读进度同步。
- **旧请求兼容**：若旧客户端或脚本调用创建/更新资源库接口时未传 `koreader_sync_enabled`，后端会按“开启”处理，避免无意中把已有同步范围关掉。
- **同步匹配范围收敛**：KOReader 文档匹配现在只会命中 `koreader_sync_enabled = true` 的资源库；被关闭的资源库不会再接收新的 KOReader 进度投影。
- **侧边栏可见反馈**：资源库列表直接显示 “KOReader Sync 开启/关闭”，无需打开编辑弹窗才能确认状态。
- **库页状态提示**：资源库主页顶部新增当前库的 KOReader 状态提示条，会明确说明该库是否参与同步，以及系统级 KOReader 服务是否已启用。
- **直接编辑当前资源库**：库页提示条提供“编辑当前资源库”按钮，通过全局事件直接唤起对应资源库的编辑弹窗。
- **仪表板全局概览**：仪表板新增 KOReader Sync 覆盖范围卡片，展示：
  - 启用同步的资源库数量
  - 关闭同步的资源库数量
  - 已匹配同步记录数
  - 待重关联记录数

### 📌 增量记录 — 2026-04-13（第三次续改）

#### 任务上下文增强 `[P1: 改善日志与任务中心]`
- **任务列表新增 `scope_id` 精确过滤**：后端任务接口支持按具体系列或资源库 ID 拉取任务，前端不再需要使用文本模糊匹配。
- **任务对象新增 `scope_name` 展示字段**：常见任务在创建时会记录目标名称，例如资源库名、系列名或“系统”“全库”等上下文名，前端可以直接显示更可读的任务对象。
- **仪表板失败任务卡片可跳转**：Dashboard 中的失败任务不再只是只读文本，点击后可直接进入关联系列、资源库或日志页。
- **日志页任务卡片增加类型标签与动作建议**：任务中心会显示任务类型中文标签、目标名称，以及基于任务类型的建议动作提示。
- **系列页失败任务卡片增加上下文名**：系列页中的失败任务面板会显示任务类型和目标名称，减少排错时的判断成本。

### 📌 增量记录 — 2026-04-13（第四次续改）

#### 任务看板可读性增强 `[P1: 改善日志与任务中心]`
- **日志页新增任务统计卡片**：在原有日志统计之外，增加运行中任务、失败任务、已完成任务三类任务统计，减少用户在长列表里手动判断状态分布的成本。
- **日志页任务按时间分组**：任务中心现在会按“今天 / 昨天 / 更早”分组展示任务，并同时显示相对时间和绝对时间。
- **日志页任务卡片信息量提升**：
  - 展示任务类型中文标签和目标上下文；
  - 展示基于任务类型的建议动作；
  - 保留原有重试和打开关联页面能力。

#### 仪表板恢复入口增强 `[P2: 仪表板更产品化]`
- **仪表板新增运行中任务面板**：除失败任务外，也会展示当前正在执行的后台任务，方便用户在首页快速了解系统活动。
- **仪表板失败任务区增加统一入口**：失败任务区新增“打开任务中心”按钮，便于用户从首页快速进入更完整的恢复界面。

### 📌 增量记录 — 2026-04-13（第五次续改）

#### 任务生命周期收口 `[P1: 改善日志与任务中心]`
- **任务清理 API**：新增 `DELETE /api/system/tasks`，支持通过 `status` 和 `scope` 过滤条件批量清理任务记录。
- **任务保留上限**：任务内存池加入基础保留策略，最多保留最近 `200` 条任务，避免任务中心无限膨胀。
- **日志页任务清理入口**：任务中心新增“清理已完成任务”和“清理失败任务”两个快捷动作，操作后会立即刷新列表。

### 📌 增量记录 — 2026-04-13（第六次续改）

#### 任务详情下钻 `[P1: 改善日志与任务中心]`
- **日志页任务卡片支持展开详情**：任务中心中的任务卡片可展开查看更多上下文，而不是只显示一行摘要。
- **新增任务详情内容**：
  - 开始时间与结束时间
  - 参数快照（如 `force`、`provider` 等任务启动参数）
  - 错误详情原文
  - 更明确的进度展示（当前值 / 总数 / 百分比）
- 这项改动不引入新接口，只消费现有任务对象中的 `params`、`started_at`、`finished_at` 和 `error` 字段。
