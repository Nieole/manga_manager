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
