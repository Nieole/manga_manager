# Manga Manager 📚

[![CI](https://github.com/Nieole/manga_manager/actions/workflows/ci.yml/badge.svg)](https://github.com/Nieole/manga_manager/actions/workflows/ci.yml)
[![Release](https://github.com/Nieole/manga_manager/actions/workflows/release.yml/badge.svg)](https://github.com/Nieole/manga_manager/actions/workflows/release.yml)

Manga Manager 是一款自托管的本地漫画 / 画集管理与阅读服务器,基于 `Go` + `React (Vite + TailwindCSS)` 构建。

它不依赖任何云端数据库,完全围绕本地文件扫描工作:把硬盘上散落的压缩包(`zip` / `cbz` / `rar` / `cbr`)整理为带封面、元数据、阅读进度的系列书库,并通过浏览器、OPDS 客户端、Mihon、KOReader 等多种入口阅读。后端编译为单个二进制,解压即用;数据库、缩略图、配置与日志都落在本地 `data/` 目录,不污染系统环境。

---

## 🖼️ 截图预览

以下截图按 "总览 → 书库 → 系列 → 阅读 → 运维配置" 的主线组织,便于快速理解核心能力与日常使用路径。

### 1. 仪表板总览

仪表板把馆藏规模、阅读活跃度、最近阅读和后台任务集中到一个入口。

![仪表板总览](images/overview-dashboard.png)

### 2. 资源库浏览

资源库页面展示系列网格、筛选与排序,以及每个系列的阅读进度与封面状态。

![资源库浏览](images/collection-library.png)

### 3. 系列详情与元数据

系列详情页聚合卷册、元数据、阅读进度和运维动作,体现 "系列维度管理" 而不仅是文件浏览。

![系列详情与元数据](images/series-detail.png)

### 4. 移动端阅读器

阅读器支持移动端沉浸式阅读,这张图重点展示跨端可用性与阅读界面完成度。

![移动端阅读器](images/reader-mobile.png)

### 5. 系统设定与运维能力

设置页与同步配置页展示了系统面向长期运行的另一面:配置校验、KOReader 同步与维护工具。

![系统设定与运维能力](images/system-settings.png)

---

## 🌟 核心特性

### 🗃️ 本地扫描与内嵌数据库
- 基于 Go 协程工作池并发扫描、解压与解析,充分利用多核读取画册。
- 支持 `zip` / `cbz` / `rar` / `cbr` 压缩包,并自动解析包内 `ComicInfo.xml` 元数据。
- 使用 CGO-free 的嵌入式 `SQLite`(`modernc.org/sqlite`)作为索引库,免安装、零外部依赖。
- 自动截取卷册封面并生成缩略图,支持 `WebP` / `AVIF` 编码以控制体积。
- 提供可配置的 IO 策略(扫描 / 解压 / 封面 / 哈希并发度、阅读时降载、同盘缓存控制),便于在不同磁盘与机械硬盘场景下调优。

### 🔍 全文搜索
- 内置 `bleve` 全文索引,跨系列与卷册按标题、作者、标签等检索。
- 搜索结果实时回填封面与相关度评分(归一化到 0~1),并提供 OpenSearch 描述端点。

### 🤖 元数据刮削:Bangumi 与 LLM
- **Bangumi** 在线刮削:按标题匹配候选条目,抓取评分、简介、标签等。
- **LLM 刮削**:对接任意兼容的大模型后端 —— `Ollama`、OpenAI 兼容的 `/v1/responses` 与 `/v1/chat/completions` 接口均可,`Endpoint / Model / API Key` 全部可在设置页动态配置。
- 刮削结果进入**元数据审核队列**:带置信度评分、来源链接与字段级差异对比,可逐条或批量应用 / 驳回,相同来源自动去重。
- **AI 智能分组**:借助 LLM 对零散卷册给出合集 / 系列归组建议,同样走审核流再落库。

### 🗂️ 合集与智能筛选
- 手动**合集**与基于条件的**智能合集 / 智能筛选**(评分、阅读进度、加入时间等),动态计算成员。
- 阅读列表(Reading List)与系列关联关系管理。

### 🖼️ 画质重建:超分辨率(可选)
- 集成 `waifu2x-ncnn-vulkan` 与 `realcugan-ncnn-vulkan`,在阅读时按需对页面放大降噪。
- 支持多级放大、降噪强度调节与轻度重编码(WebP 直出),两套引擎相互隔离、可热切换。
- 属可选能力:需自行下载对应引擎并在设置中填入可执行文件绝对路径。

### 📱 响应式沉浸阅读
- 移动优先(Mobile First)的 TailwindCSS 断点设计,从桌面大屏到手机均保持合理排布。
- **瀑布流(Webtoon)** 与 **翻页(Paged)** 双模式切换;支持 `LTR / RTL` 翻页方向。
- 提供 "原始 / 等宽 / 等高 / 适屏" 多档画幅,并带页面预加载以平滑翻页。

### 🔌 多端协议与同步
- **OPDS v1.2**:接入 Chunky、Panels、Tachiyomi/Mihon 的 OPDS 源等通用阅读客户端。
- **Mihon**:提供 Mihon 扩展可消费的接口。
- **KOReader 进度同步**:兼容 KOReader 的进度同步协议,支持二进制哈希 / 文件名等匹配模式,与电纸书阅读进度互通。

### ⚙️ 运维与可观测性
- `slog` + `Lumberjack` 结构化日志:关键请求与异常落 `data/*.log`,带尺寸截断与按时间滚动压缩。
- **可视化日志看板**:在网页端实时查看并高亮 `error / warn / info` 日志,无需登录服务器。
- **SSE 实时事件**:扫描 / 刮削等后台任务进度通过 Server-Sent Events 推送到前端任务中心,带心跳与背压保护。
- 内置健康检查:统计空页、缺封面、缺元数据、重复哈希等问题项,辅助库维护。

> ⚠️ **安全提示**:服务默认监听 `0.0.0.0:8080` 且**不带身份认证**。请仅在受信任的内网使用,或自行在前置反向代理(Nginx / Caddy 等)上配置访问控制后再对外暴露。

---

## 📦 部署与使用

得益于 Go 的交叉编译,Manga Manager 可在 Windows / Linux / macOS 上运行。

### 0. GitHub Actions 自动构建与发布

仓库内置两条工作流:

- `CI`:在 `push main` 与 `pull_request` 时执行前端构建、后端测试与服务端编译检查。
- `Release`:在推送 `v*` 标签时自动构建 `Linux AMD64`、`Windows AMD64`、`macOS ARM64` 三平台二进制并上传到 GitHub Release(也可在 `Actions → Release → Run workflow` 手动触发)。

```bash
git tag v0.1.0
git push origin v0.1.0
```

### 1. 从源码构建

需要本地具备 `Go` 与 `Node.js` 环境。在项目根目录运行:

```bash
# 先用 Vite 打包前端到 dist,再交叉编译三端二进制到 build/
./build.sh
```

Windows 下可使用 `build.ps1`。

### 2. 运行二进制

在 `build/` 中选择对应平台的可执行文件运行(以 macOS ARM64 为例):

```bash
./manga-manager-mac-arm64
```

首次运行会自动生成 `data/` 目录,用于存放缩略图、数据库、配置与日志。

### 3. 打开控制台

浏览器访问 [http://localhost:8080](http://localhost:8080),进入**设置 ⚙️**,添加包含漫画文件的绝对目录并执行**全局扫描**即可。

---

## 🛠️ 启用超分辨率(可选)

1. 自行从 GitHub 下载 `waifu2x-ncnn-vulkan` 或 `realcugan-ncnn-vulkan` 的官方 Release 并解压到本地。
2. 在 **设置 → 智能扫描与处理引擎设定** 中,把两个可执行文件的**绝对路径**分别填入并保存。
3. 进入任意画集阅读,在右上角启用对应滤镜即可。

---

## 🧱 技术栈

| 层 | 技术 |
| --- | --- |
| 后端 | Go 1.25、chi 路由、`sqlc` 预编译查询、`modernc.org/sqlite`(CGO-free) |
| 搜索 | bleve 全文索引 |
| 图像 | `chai2010/webp`、`gen2brain/avif`、`disintegration/imaging`;可选 waifu2x / Real-CUGAN |
| 解析 | zip / cbz、rar / cbr、ComicInfo.xml |
| 前端 | React + Vite + TailwindCSS,i18n(中 / 英),响应式阅读器 |
| 协议 | OPDS v1.2、Mihon、KOReader 进度同步、SSE |

---

## 🤝 开发

```bash
# 前端开发
cd web && npm run dev

# 前端构建 / 检查
cd web && npm run build
cd web && npm run lint

# 后端测试
go test ./...
```

修改 `sql/query.sql` 或 `internal/database/schema.sql` 后,需运行 `sqlc generate` 重新生成 Go 绑定。更多约定见 [`AGENTS.md`](AGENTS.md),版本变更见 [`CHANGELOG.md`](CHANGELOG.md)。

---
*Developed with ❤️ via AI-Pair.*
