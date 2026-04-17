# Changelog

本文件记录项目各版本的功能新增、改动与修复。

---

## [Unreleased] — 2026-04-10

> 本次集中改动对应 [面向公开发布的系统性改进路线图] 中的 **P0 发布阻塞** 全部五项、**P1 产品完善**（设置页信息架构、日志与任务中心、库管理交互、元数据工作流）以及 **P2 体验增强**（仪表板产品化、文案与视觉统一）。下方各节标题中的 `[Pn]` 标记对应路线图条目。

### 📌 增量记录 — 2026-04-17（前端主题化）

#### 主题系统与本地持久化 `[P2: 文案与视觉统一 / UI 个性化]`
- **新增前端主题系统**：引入 `ThemeProvider` 与主题注册表，主题切换在前端本地生效，不依赖后端配置。
- **主题配置本地保存**：当前主题使用浏览器 `localStorage` 持久化，刷新页面后自动恢复；存储键为 `manga_manager_theme`。
- **内置 5 套主题**：
  - `Midnight`：默认深色主题，延续当前系统识别度；
  - `Paper`：浅色纸张风格；
  - `Forest`：低饱和墨绿主题；
  - `Amber`：暖色书房主题；
  - `Graphite`：中性工业风主题。
- **阅读器跟随应用主题**：`BookReader` 不引入独立主题状态，默认跟随应用当前主题；现有阅读模式、方向、缩放和图像处理偏好保持本地独立存储。

#### 前端颜色体系重构 `[P2: 文案与视觉统一]`
- **全局颜色 token 化**：`index.css` 新增主题变量层，统一管理 app 背景、surface、sidebar、主强调色、hover 色、阴影和灰阶文本色。
- **Tailwind 颜色映射切换为 CSS Variables**：`tailwind.config.js` 中的 `komga*`、`gray`、`slate`、`black`、`white` 颜色改为从主题变量读取，原有大量 Tailwind 类无需全量重写即可响应主题切换。
- **Modal 与通用按钮跟随主题**：
  - `ModalShell` 的遮罩、面板渐变与阴影改为读取主题变量；
  - `modalStyles` 中主按钮 hover 色不再写死为紫色，统一使用主题主色 hover token。

#### 设置页主题入口 `[P2: UI 个性化]`
- **Settings 新增“外观 / 主题”分组**：
  - 使用主题卡片而不是下拉框展示内置主题；
  - 每个主题展示名称、风格说明和颜色预览；
  - 明确提示“主题仅保存在当前浏览器”。
- **当前主题即时切换**：用户在设置页点击主题卡后立即生效，无需额外保存配置。

#### 关键界面接入主题 `[P2: 视觉统一]`
- 以下关键界面已接入主题色：
  - `Settings`
  - `Collections`
  - `BookReader`
  - `ModalShell`
  - `ErrorBoundary`
  - `DirectoryPicker`
- 若干主交互按钮的 hover 行为从固定 `purple-*` 调整为 `komgaPrimaryHover`，避免切换主题后仍残留默认紫色交互反馈。

### 📌 增量记录 — 2026-04-17（前端导航与阅读体验收敛）

#### 导航与空间利用 `[P2: 桌面端效率提升]`
- **桌面端左侧导航支持折叠**：`Layout` 新增桌面端折叠态，侧栏可在完整列表与图标窄栏之间切换。
- **折叠状态本地持久化**：侧栏折叠状态保存到浏览器本地，刷新后自动恢复。
- **折叠态保留主导航与资源库入口**：在窄栏模式下继续显示主导航图标和资源库入口，不会完全丢失切换能力。

#### 阅读器移动端交互 `[P2: 阅读体验进一步打磨]`
- **移动端阅读页隐藏翻页箭头**：`BookReader` 在移动端不再显示左右箭头提示。
- **移动端阅读页隐藏左右半透明遮罩**：翻页热区仍保留，但移动端不再显示悬浮遮罩层，减少对内容的干扰。
- **桌面端保留显式翻页提示**：桌面端继续保留左右翻页提示，仅在悬停时显示。

#### 页面与面板交互收敛 `[P1: 页面密度与操作层级优化]`
- **外部资源库面板支持折叠/展开**：资源库页的外部资源库区域新增折叠交互，默认降低页面首屏占用。
- **系列详情卷册操作图标优化**：卷册卡片的完成/未完成切换图标由编辑铅笔调整为更明确的完成态图标，降低语义歧义。
- **KOReader 设置页样式统一**：服务配置、立即操作按钮、错误提示和 Sync Key 展示统一到当前主题色体系，减少独立的蓝色强调样式。

#### 前端主题兼容修正 `[P2: 文案与视觉统一]`
- **主题变量透明度写法修正**：`ModalShell` 和全局背景中的 `rgba(var(...))` 写法调整为 `rgb(var(...) / alpha)`，避免部分浏览器下 CSS 变量透明度解析不稳定。

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
  - `path_fingerprint_no_ext`
  用于当前 KOReader 匹配方案中的二进制哈希和路径索引。
- **当前匹配方案说明**：KOReader 现仅保留两种正式匹配模式：
  - `binary_hash`
  - `file_path`
  其中 `file_path` 模式只比较“文件名 + 向上最多两层路径”，并支持可选的“忽略扩展名”规则。
- **进度投影回本地阅读状态**：当 KOReader 文档成功命中本地书籍时，会把 `percentage` 按 `page_count` 投影回 `books.last_read_page/last_read_at`，并写入 `reading_activity`。
- **防止进度回退**：服务端采用“更远进度优先”，较小的 `percentage` 不会覆盖已有较大进度。

#### 扫描器与维护任务 `[P1: 资源识别与恢复能力]`
- **扫描时自动生成指纹**：扫描器在入库/更新书籍后会同步写入 KOReader 文档匹配指纹，不再依赖纯人工维护。
- **新增系统维护任务**：
  - `POST /api/system/koreader/rebuild-hashes`：按当前匹配模式重建 KOReader 索引
  - `POST /api/system/koreader/reconcile`：重关联未匹配的 KOReader 同步记录
- **任务中心集成**：新增任务类型
  - `rebuild_book_hashes`
  - `reconcile_koreader_progress`
  并支持任务列表展示、错误提示与重试。

#### 设置页与管理入口 `[P1: 配置与可运维性]`
- 设置页新增 **KOReader Sync** 分组，支持：
  - 启用/关闭同步服务
  - 配置同步路径

### 📌 增量记录 — 2026-04-15（KOReader 多账号与服务端生成密钥）

#### 多账号模型 `[P1: KOReader 服务端能力增强]`
- **KOReader 账号从单账号升级为多账号**：新增 `koreader_accounts` 表，账号字段包含：
  - `username`
  - `sync_key`
  - `enabled`
  - `created_at`
  - `updated_at`
- **旧单账号自动迁移**：数据库迁移时会尝试把历史 `koreader_settings` 中的单账号数据迁入 `koreader_accounts`，避免已有接入用户被直接清空。
- **按账号鉴权**：KOReader 协议层不再读取单一账号配置，而是按 `x-auth-user` 查找对应账号，再校验其 `x-auth-key`。

#### 服务端生成 Sync Key `[P1: 账号管理与接入流程]`
- **Sync Key 改为由服务端生成**：新增随机 16 字节十六进制生成逻辑，每次创建账号或轮换密钥时由服务端生成新的 32 位小写十六进制 Sync Key。
- **账号管理接口落地**：
  - `GET /api/system/koreader/accounts`
  - `POST /api/system/koreader/accounts`
  - `POST /api/system/koreader/accounts/{id}/rotate-key`
  - `POST /api/system/koreader/accounts/{id}/toggle`
  - `DELETE /api/system/koreader/accounts/{id}`
- **设备自助注册不再作为主路径**：`POST /koreader/users/create` 现在返回“请从管理后台创建账号”的兼容拒绝响应，以避免与“服务端生成 Sync Key”模型冲突。

#### 系统状态与进度投影 `[P1: 多账号运行态可见性]`
- `GET /api/system/koreader` 现在返回服务级统计，而不是单账号字段，新增：
  - `account_count`
  - `enabled_account_count`
- KOReader 进度仍按账号分别保存在 `koreader_progress` 中，但对本地 `books.last_read_page/last_read_at` 的投影仍然保持**全局合并**，继续采用“更远进度优先”规则。
- 删除 KOReader 账号时，会同步清理该账号关联的 `koreader_progress` 与 `koreader_sync_events`，避免遗留脏数据。

#### 设置页重构 `[P1: 管理界面]`
- KOReader 设置页从“单账号表单”改为“服务配置 + 账号列表”：
  - 服务配置仍负责开关、路径和匹配模式
  - 账号列表负责创建、查看 Sync Key、复制、轮换、启停和删除
- 状态卡从展示单一账号改为展示：
  - 已启用账号数 / 总账号数
  - 最近同步时间
  - 最近错误
- 设备接入文案同步更新为：
  - 自定义同步服务地址从服务配置读取
  - 用户名和 Sync Key 从后台账号列表复制

#### 测试与验证 `[工程质量]`
- 新增 / 更新测试覆盖：
  - 创建 KOReader 账号并生成 Sync Key
  - 轮换 Sync Key
  - 启停账号
  - 多账号下按用户名鉴权
  - 现有 binary_hash / file_path 进度同步回归
- 验证通过：
  - `GOCACHE=/Users/nicoer/dev/manga_manager/.gocache GOTMPDIR=/Users/nicoer/dev/manga_manager/.tmp go test ./...`
  - `npm run build` in `web/`

### 📌 增量记录 — 2026-04-15（KOReader 最近错误修复）

#### 系统事件与错误提示分离 `[P1: 可观测性修正]`
- **修复“轮换 Sync Key 后显示最近错误”的问题**：`latest_error` 的查询逻辑此前把所有 `status != ok` 的事件都当成错误，包括：
  - `account_rotated`
  - `account_enabled`
  - `account_disabled`
- 现在 `latest_error` 只会读取**非系统事件**中的失败记录，系统管理操作不再污染账号卡片或服务状态中的“最近错误”提示。
- 新增回归测试，覆盖“轮换/启停账号后 `latest_error` 仍为空”的场景。

### 📌 增量记录 — 2026-04-15（KOReader 原始密钥与登录排障）

#### 原始 Sync Key 语义修正 `[P0: 鉴权兼容性修复]`
- **修复“新生成账号仍然 Unauthorized”的问题**：多账号版本初版误把服务端保存的 `sync_key` 直接与请求头 `x-auth-key` 比较。
- 现已改为符合 KOReader 客户端行为：
  - 后台展示和复制的是**原始 Sync Key**
  - KOReader 设备使用该原始值配置账号
  - 客户端请求时会发送 `MD5(原始 Sync Key)` 到 `x-auth-key`
  - 服务端鉴权时按 `MD5(保存的原始 Sync Key)` 与请求头比较
- 设置页文案同步修正，明确说明：
  - 设备侧直接填写原始 Sync Key
  - 不需要手动对密钥做 MD5

#### 登录排障日志增强 `[P1: 可观测性与故障定位]`
- 为 KOReader 协议入口新增结构化日志，覆盖：
  - `/koreader/users/auth`
  - `/koreader/syncs/progress`
  - `/koreader/syncs/progress/{document}`
- 新增日志字段：
  - `username`
  - `client_ip`
  - `user_agent`
  - `accept`
  - `document`
  - `device`
  - `device_id`
  - `client_key_prefix`（仅前缀，不记录完整密钥）
- 服务层鉴权分支现在会明确记录失败原因：
  - 缺失凭据
  - 账号不存在
  - 账号停用
  - 账号未保存原始密钥
  - 客户端 header 与预期 MD5 不匹配
  - 鉴权成功
- 失败事件仍会写入 `koreader_sync_events`，同时补充一条结构化 `slog.Warn`，便于直接从服务日志定位问题。

#### 测试与验证 `[工程质量]`
- 新增 / 更新测试覆盖：
  - 新生成账号 + 客户端发送 MD5 后的 `x-auth-key` 可以成功登录
  - 服务层按“原始 Sync Key -> MD5 请求头”语义鉴权
- 验证通过：
  - `GOCACHE=/Users/nicoer/dev/manga_manager/.gocache GOTMPDIR=/Users/nicoer/dev/manga_manager/.tmp go test ./internal/api ./internal/koreader ./internal/database`
  - `npm run build` in `web/`

### 📌 增量记录 — 2026-04-15（KOReader 协议兼容修复）

#### KOReader 鉴权与协议兼容 `[P0: 修正 KOReader 自定义同步协议实现]`
- **修复设备登录 `Unauthorized`**：服务端不再把 `x-auth-key` 当作明文密码做二次哈希，改为按 KOReader 客户端实际发送的 **32 位 MD5 Sync Key** 直接存储和校验。
- **切断旧错误密钥语义**：旧实现里保存的非 KOReader 兼容 key 不再被视为有效配置；管理员需要重新填写正确的 Sync Key。
- **KOReader 专用响应对齐**：
  - `GET /koreader/healthcheck` / `GET /koreader/healthstatus` 现在返回 `{"state":"OK"}`
  - `GET /koreader/users/auth` 成功时返回包含 `state` / `authorized` 的兼容响应
  - `PUT /koreader/syncs/progress` 与 `GET /koreader/syncs/progress/{document}` 的成功响应统一带 `state: OK`
- **支持 KOReader vendor JSON**：当客户端发送 `Accept: application/vnd.koreader.v1+json` 时，接口会返回对应 `Content-Type`，否则仍返回普通 JSON。

#### KOReader 设置与状态展示 `[P1: 配置可见性与排障能力]`
- **设置字段语义切换**：系统管理接口与设置页从旧的 `password` 语义切换为 `sync_key`，明确表示这里填写的是 **KOReader Sync Key (MD5)**，不是原始密码。
- **新增 Sync Key 格式校验**：保存 KOReader 配置时，`sync_key` 必须是 32 位小写十六进制字符串，否则返回字段级校验错误 `koreader.sync_key`。
- **状态接口增强**：`GET /api/system/koreader` 新增：
  - `has_valid_sync_key`
  - `latest_error`
  用于区分“已设置但格式无效”和“真正可用”的同步配置。
- **设置页状态卡改进**：
  - 显示 `Sync Key 已配置 / 格式无效 / 未设置`
  - 显示最近一次 KOReader 协议错误
  - 文案明确说明设备端应填写的是 32 位 MD5 Sync Key

#### 日志与事件记录 `[P1: 排障闭环]`
- KOReader 认证失败会写入 `koreader_sync_events`，状态区分为：
  - `auth_failed_invalid_key`
  - `auth_failed_forbidden`
- `GET /api/system/koreader` 会读取最近一次非成功事件，作为设置页排障提示来源。

#### 测试补充 `[工程质量]`
- 新增 / 更新 KOReader 相关测试，覆盖：
  - 合法 `sync_key` 登录成功
  - 非法 `sync_key` 配置保存被拒绝
  - KOReader vendor JSON `Accept` 头返回正确的 `Content-Type`
  - 旧格式无效存储 key 被识别为不可用
- 验证通过：
  - `GOCACHE=/Users/nicoer/dev/manga_manager/.gocache GOTMPDIR=/Users/nicoer/dev/manga_manager/.tmp go test ./...`
  - `npm run build` in `web/`
  - 配置用户名与同步密钥
  - 控制是否允许首次注册
  - 查看当前模式下的索引进度、已匹配/未匹配同步记录数

### 📌 增量记录 — 2026-04-15（KOReader 索引重建与模式感知文案）

#### KOReader 索引重建修正 `[P1: 资源识别与恢复能力]`
- **索引重建不再截断在 10000 条**：`RebuildBookIdentities` 和 `ReconcileProgress` 从一次性 `LIMIT 10000` 改为分页批处理。
  - `limit` 现在表示批次大小，当前任务入口默认按 `500` 条一批循环执行。
  - 重建流程会持续迭代直到所有缺失索引的书籍都处理完成。
  - 未匹配进度重关联同样改为全量分页扫描，不再只处理前一段记录。
- **数据库层新增分页/计数接口**：
  - `CountBooksMissingIdentity`
  - `CountUnmatchedKOReaderProgress`
  - `ListBooksMissingIdentityBatch`
  - `ListUnmatchedKOReaderProgressBatch`
  用于支持全量迭代和更准确的任务总量显示。
- **索引更新改为按字段选择性写入**：`UpdateBookIdentity` 现在只更新本次模式需要写入的字段，不会把未参与本轮重建的字段清空。

#### KOReader 匹配模式驱动索引构建 `[P1: 匹配模型一致性]`
- **二进制哈希模式与路径模式的索引构建正式分离**：
  - `binary_hash` 模式下只计算并写入 `file_hash`
  - `file_path` 模式下只计算并写入 `path_fingerprint` / `path_fingerprint_no_ext`
- **路径模式不再额外计算二进制哈希**，避免在仅使用路径匹配时仍进行不必要的大文件读取和哈希开销。
- **索引覆盖统计改为模式感知**：
  - `GET /api/system/koreader` 中的 `stats.hashed_books` 现在表示“当前匹配模式下已完成索引的书籍数”
  - 不再要求 `file_hash`、`path_fingerprint`、`path_fingerprint_no_ext` 三类字段同时存在才算完成

#### KOReader 任务文案与前端状态同步 `[P1: 配置与可运维性]`
- **后端任务消息改为模式感知**：
  - `rebuild_book_hashes` 在二进制模式下显示“二进制哈希索引”
  - `rebuild_book_hashes` 在路径模式下显示“路径索引”或“路径索引（忽略扩展名）”
  - `refresh_koreader_matching` 中的阶段消息与完成消息同步使用当前模式名称
- **任务参数快照补全**：`rebuild_book_hashes` 和 `reconcile_koreader_progress` 现在都会记录：
  - `match_mode`
  - `path_ignore_extension`
  便于日志页、仪表板和重试逻辑基于实际模式展示准确说明。
- **前端任务中心/仪表板/设置页文案更新**：
  - 日志页 `Logs` 中的 KOReader 任务标题和操作建议会根据当前模式动态显示“二进制哈希索引”或“路径索引”
  - 仪表板中的运行中/失败任务卡片同步显示模式感知的 KOReader 任务名称
  - 设置页中的“匹配索引进度”“重建匹配索引”按钮与变更提示，统一改为显示当前实际索引类型

#### 测试补充 `[Tests]`
- 新增 `internal/koreader/service_test.go`，覆盖：
  - 小批次参数下索引重建仍会分页处理完整批量数据
  - `file_path` 模式下只构建路径索引，不写入 `file_hash`
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

### 📌 增量记录 — 2026-04-13（KOReader 匹配模式重构）

#### 匹配方案重构 `[P0: KOReader 匹配规则收敛]`
- **直接切换到新方案，不做旧逻辑兼容**：KOReader 匹配现在只保留两种明确模式：
  - `binary_hash`
  - `file_path`
- **移除旧的隐式多路匹配**：不再采用“哈希优先，路径/文件名兜底”的混合逻辑，也不再使用文件名作为正式匹配输入。
- **路径模式规则固定**：`file_path` 模式只比较“文件名 + 向上最多两层路径”，更高层目录全部忽略。
- **路径模式新增扩展名开关**：可配置是否忽略扩展名；若不忽略，则按保留扩展名的路径片段精确匹配。
- **新增配置字段**：
  - `koreader.match_mode`
  - `koreader.path_ignore_extension`
- **新增路径索引字段**：
  - `books.path_fingerprint_no_ext`
- **保留但停用旧字段语义**：旧的 `filename_fingerprint` 不再参与 KOReader 新匹配方案。

#### 扫描器与重建任务 `[P1: 匹配索引维护]`
- 扫描器现在会为每本书同时生成：
  - `file_hash`
  - `path_fingerprint`
  - `path_fingerprint_no_ext`
- 原“重建书籍指纹”任务语义调整为 **重建 KOReader 匹配索引**。
- 重关联任务严格按当前 `match_mode` 执行，不再跨模式尝试。

#### 设置页与状态展示 `[P1: 可配置与可解释性]`
- 设置页 KOReader 区块新增匹配模式选择器和“忽略扩展名”开关。
- 设置页、仪表板、资源库页都会明确展示当前使用的是：
  - 二进制哈希匹配
  - 或文件路径匹配（并说明只比较向上两层路径）
- **未匹配记录排障视图**：设置页新增最近未匹配 KOReader 记录列表，展示原始文档标识、当前归一化匹配键、设备信息、时间与建议动作。
- **匹配规则变更维护入口**：当保存了新的匹配模式或扩展名规则后，设置页会提示执行维护操作；新增一键“应用匹配规则变更”，会顺序触发：
  - 重建 KOReader 匹配索引
  - 重关联未匹配记录

#### 系统接口 `[排障与维护]`
- 新增 `GET /api/system/koreader/unmatched`
- 新增 `POST /api/system/koreader/apply-matching`
- 任务中心新增任务类型 `refresh_koreader_matching`

#### 测试 `[回归保障]`
- 新增并通过以下场景测试：
  - 二进制哈希模式进度同步
  - 文件路径模式精确匹配
  - 文件路径模式忽略扩展名匹配

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

### 📌 增量记录 — 2026-04-16（外部资源库任务链路修复）

#### 外部资源库管理与同步能力落地 `[P1: 外部资源管理]`
- **新增类似 Calibre 设备管理的外部资源库工作流**：本地资源库现在可以把一个外部目录作为“外部资源库”来管理，用于识别哪些资源已经存在，以及把缺失资源批量同步过去。
- **支持指定外部资源库位置并即时扫描**：
  - 在资源库页选择一个外部目录后，系统会创建一个外部资源会话；
  - 后端会递归扫描该目录中的归档资源，并生成当前会话的外部资源状态快照。
- **支持与当前主资源库做内容对比**：
  - 对比逻辑基于主资源库中的相对路径与文件名；
  - 会把扫描结果映射回当前资源库中的系列与书籍，计算“已命中 / 总数”。
- **资源库页直接展示外部存在性信息**：
  - 当前页增加外部存在性摘要；
  - 每个系列卡片可直接看到该系列在外部资源库中的存在情况、命中数量和进度条；
  - 用户无需进入系列详情页即可判断哪些资源已同步、哪些仍缺失。
- **支持批量传输到外部资源库**：
  - 资源库页可多选系列；
  - 提交后仅同步外部资源库中缺失的书籍，不覆盖已存在文件；
  - 传输目标目录保持主资源库中的原始相对路径结构。
- **任务系统接入外部扫描与传输**：
  - 新增外部资源库扫描任务
  - 新增外部资源库传输任务
  - 可在底部全局任务浮层和任务中心查看进度与结果

#### 外部资源库扫描/传输稳定性修复 `[P1: 外部资源管理]`
- **故障根因确认**：外部资源库扫描和传输任务在批量处理时，会对 `c.updateTask()` 进行高频调用；每次调用都会向 SSE Broker 写入一条 `task_progress` 消息。
- **SSE 消息洪水问题**：
  - 服务端 `c.messages` 通道容量有限；
  - 外部扫描/传输在处理大量文件且速度很快时，会在短时间内塞入大量中间态消息；
  - 当客户端消费速度跟不上时，后续关键消息可能无法及时送达，包括：
    - 最终 `completed` 任务状态
    - 任务结束后的 `refresh` 页面刷新通知
- **表面症状统一解释**：此前反复出现的两个问题，其实来自同一底层并发通信故障：
  - 扫描进度条停留在中间状态，不会收口；
  - 传输完成后，本地资源库页中的外部存在性标记不刷新。

#### 修复策略 `[后端通信收敛]`
- **扫描任务增加节流**：在外部资源扫描的进度回调中加入时间节流控制，不再为每一个文件都推送一次 SSE 进度；中间态更新收敛为固定时间窗内最多一次。
- **传输任务增加节流**：外部资源传输循环中的 `updateTask()` 调用同样改为节流发送，避免“开始复制”和“复制完成”两类中间态把消息通道挤满。
- **优先保证尾部关键消息送达**：在中间态消息被显著削减后，最终的：
  - `c.finishTask(...)`
  - `c.PublishEvent("refresh")`
  能稳定进入消息通道并送达前端。

#### 任务链路重构 `[前后端状态统一]`
- **外部扫描/传输接口显式返回 `task_key`**：
  - `POST /api/libraries/{libraryId}/external-libraries/session` 现在返回 `{ session, task_key }`
  - `POST /api/libraries/{libraryId}/external-libraries/session/{sessionId}/transfer` 现在返回 `task_key`
- **前端改为按 `task_key` 跟踪任务**：资源库页不再通过任务类型、作用域或 `session_id` 猜测“哪一个任务属于当前页面”，而是显式记录：
  - `externalScanTaskKey`
  - `externalTransferTaskKey`
- **扫描进度条补收口机制**：若扫描会话已经从 `scanning` 进入 `ready/failed`，但底部全局任务浮层还未收到最终完成态，前端会按同一个 `task_key` 主动补一条最终状态，用于收口进度条并自动隐藏。
- **全局任务浮层增强**：`Layout` 中的全局 `taskProgress` 结构新增 `key` / `params` 字段，并支持通过内部覆盖事件按 `task_key` 精确修正当前展示中的任务状态。

#### 动态接口缓存修复 `[状态刷新可靠性]`
- **外部资源会话状态接口禁缓存**：
  - `GET /api/libraries/{libraryId}/external-libraries/session/{sessionId}`
  - `GET /api/libraries/{libraryId}/external-libraries/session/{sessionId}/series`
  均显式返回 `Cache-Control: no-store` 与 `Pragma: no-cache`。
- **前端请求增加时间戳参数**：资源库页在拉取外部会话状态和系列存在性状态时追加 `_ts` 参数，避免浏览器或中间层复用旧响应。

#### 资源库页 UI 与交互完善 `[外部资源可视化]`
- **当前页外部存在性摘要**：资源库页新增“当前页外部存在情况”摘要卡，按当前分页结果展示：
  - 已完整存在
  - 部分存在
  - 尚未同步
- **系列卡片新增独立外部状态区**：
  - 直接显示状态标签（已完整存在 / 部分存在 / 尚未同步）
  - 显示 `命中数 / 总数`
  - 显示命中比例进度条
- **状态展示移出封面叠层**：外部状态不再覆盖评分、操作按钮和封面底部信息，而是放到卡片正文中的独立区块，减少视觉冲突。
- **资源库跨页多选**：
  - 资源库页的批量选择状态在翻页时不再清空，支持跨页累计选择系列；
  - 仅在切换资源库、切换筛选条件、切换排序方式或手动退出批量模式时清空选择；
  - 顶部工具栏新增“全选本页 / 取消本页”；
  - 底部批量操作栏会同时显示总选择数与当前页命中数，便于跨页整理后统一执行传输、收藏或加入合集。

#### 任务完成后的页面刷新 `[存在性数据闭环]`
- 资源库页在收到外部扫描/传输任务的完成态后，会显式重拉：
  - 当前外部会话状态
  - 当前页系列的外部存在性状态
- 同时，收到全局 `refresh` 时，如果当前页面存在外部资源会话，也会顺带重取外部存在性数据，确保“系列列表刷新”和“外部命中状态刷新”保持一致。

#### 用户可见结果 `[UI 恢复闭环]`
- 扫描外部资源库时，底部全局进度条不再长时间卡在中途。
- 传输到外部资源库完成后，资源库页中的“外部存在性”状态可随 `refresh` 正常重取并更新。
- 同时降低了高频任务更新对前端 React 渲染造成的额外压力，使外部资源库页面在大批量文件操作时更平稳。

#### 验证说明 `[回归保障]`
- 本次修复聚焦于 `internal/api/external_controller.go` 中的外部扫描与传输任务推进逻辑。
- `internal/api/controller_test.go` 已补充对外部资源扫描/传输响应体中 `task_key` 的校验。
- 修复后的核心目标不是增加更多前端补丁，而是从后端消息流层面保证：
  - 进度中间态适度发送；
  - 任务完成态和 `refresh` 刷新信号稳定送达。

### 📌 增量记录 — 2026-04-17（弹出框视觉系统统一）

#### 弹出框视觉骨架统一 `[P1: 用户交互与视觉一致性]`
- **新增统一弹出框基座**：前端新增通用 `ModalShell`，统一管理遮罩、圆角、边框、阴影、头部标题区、正文滚动区、底部操作区和进入动画。
- **新增共享样式令牌**：补充一组弹窗内通用按钮、输入框、选择器、分区块和标签样式，避免不同弹窗继续各自维护一套深色样式。
- **交互行为同步收敛**：
  - 打开弹窗时锁定页面滚动；
  - 支持 `Esc` 关闭；
  - 遮罩点击关闭与关闭按钮风格保持一致；
  - 大型弹窗统一为“面板内滚动”，避免整页与弹窗双滚动混杂。

#### 主要业务弹窗统一换壳 `[P1: 用户交互与视觉一致性]`
- **已统一到同一套弹窗视觉语言的模块**：
  - 添加到合集
  - 添加/编辑资源库
  - 全局搜索
  - 系列元数据编辑
  - 元数据来源预览与应用
  - 合集页“新建合集”
- **视觉效果调整方向**：
  - 靠拢当前首页、仪表板和设置页的深色卡片风格；
  - 强化头部层级、正文留白和页脚操作区；
  - 取消此前各弹窗在边框、圆角、按钮样式和阴影上的不一致表现。

#### 原生浏览器弹窗清理 `[P1: 用户交互与视觉一致性]`
- **新增统一确认弹窗组件**：新增 `ConfirmDialog`，用于替换系统中残留的原生 `confirm()`。
- **系统内原生确认框已统一替换**：
  - 删除合集
  - 强制全量扫描资源库
  - 批量刮削缺失元数据
  - 清理失效资源
  - AI 智能分组
  - 删除资源库
  - 传输到外部资源库确认
- **原生提示框已统一改为页面内反馈**：
  - 原有 `alert()` 成功/失败提示改为项目现有 toast 风格；
  - `Layout` 也新增统一 toast，用于承接资源库管理相关的成功/失败反馈。

#### 用户可见结果 `[界面一致性]`
- 主要弹出框现在与站内页面卡片风格一致，不再出现“某些是自定义面板、某些还是浏览器原生框”的割裂感。
- 资源库操作和外部资源库传输确认不再跳出浏览器默认对话框，整体视觉和交互体验更连续。
- 后续新增确认类操作可直接复用统一确认弹窗，避免再次引入原生浏览器弹窗。

### 📌 增量记录 — 2026-04-18（设置页结构重构）

#### 设置页信息架构重构 `[P1: 重构设置页信息架构]`
- **设置页由单页堆叠改为多子页面结构**：`/settings` 现在作为设置壳层路由，内部拆分为：
  - `概览`
  - `外观`
  - `库与扫描`
  - `图片与缓存`
  - `AI / 元数据`
  - `KOReader`
  - `维护工具`
- **新增左侧设置导航**：桌面端改为左侧设置导航，移动端提供顶部分类切换，不再把所有配置和操作挤在同一页里。
- **概览页新增状态总览**：设置首页改为摘要面板，展示配置健康状态、已绑定目录数、KOReader 状态和主题摘要，并提供进入对应设置页的入口。

#### 设置数据流重构 `[P1: 设置页可维护性]`
- **新增共享设置状态容器**：将原 `Settings.tsx` 中的大量请求、保存、校验、KOReader 管理逻辑抽离到统一的 `SettingsContext`，由各子页面按需读取。
- **子页面独立保存模型落地**：
  - `外观` 继续本地即时生效；
  - `库与扫描`、`图片与缓存`、`AI / 元数据` 分页独立保存；
  - `KOReader` 保持服务配置单独保存，账号与维护动作继续即时执行；
  - `维护工具` 页面仅保留即时后台任务入口，不再混入持久配置。
- **当前已绑定目录归位**：原来放在设置页底部的“当前已绑定目录”已并入 `库与扫描` 页面，更符合信息归类。

#### 未保存修改拦截 `[P1: 设置页交互改进]`
- **设置页内部切换拦截**：当前设置分组有未保存修改时，切换到其他设置分类会弹统一确认框。
- **离开设置页拦截**：从设置页跳转到仪表板、资源库、日志等其他路由时，同样会拦截未保存修改并提示确认。
- **浏览器刷新/关闭保护**：保留 `beforeunload` 提示，避免刷新页面时直接丢失当前分组的编辑内容。

#### 浏览器路由兼容修复 `[Bug Fix]`
- **修复设置页进入即崩溃的问题**：此前尝试直接使用 `useBlocker` 做未保存拦截，但当前项目仍运行在 `BrowserRouter` 下，导致打开设置页时报错：
  - `useBlocker must be used within a data router`
- **改为兼容 `BrowserRouter` 的导航拦截方案**：设置页现在通过底层导航上下文实现自定义阻塞，不再要求整个项目迁移到 data router。

#### 请求循环修复 `[Bug Fix]`
- **修复设置页持续刷新和反复请求后端的问题**：
  - 根因是 `SettingsContext` 中 `fetchConfig` / `fetchKOReader` 的 `useCallback` 依赖链包含会在请求完成后再次变化的状态；
  - 这些 callback 变化又触发初始化 `useEffect` 重新执行，形成请求循环。
- **修复方式**：
  - 将初始化请求函数改为稳定引用；
  - 使用 `ref` 保存最新 `config` 与 `koreaderStatus`；
  - 避免在初始化加载链路中因为状态变化反复重建 callback。
- **用户可见结果**：
  - 进入设置页后不再持续刷新；
  - 不再反复请求 `/api/system/config`、`/api/system/koreader` 等接口。

#### 验证说明 `[回归保障]`
- 设置页重构后已通过前端构建验证：`npm run build`
- 当前重构重点放在：
  - 结构拆分
  - 保存边界收口
  - 未保存修改保护
  - 请求稳定性修复
- 尚未在本轮中继续做更细的按页懒加载请求优化；当前目标是先保证设置页稳定、清晰且不再失控刷新。
