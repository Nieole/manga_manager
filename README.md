# Manga Manager 📚

[![CI](https://github.com/Nieole/manga_manager/actions/workflows/ci.yml/badge.svg)](https://github.com/Nieole/manga_manager/actions/workflows/ci.yml)
[![Release](https://github.com/Nieole/manga_manager/actions/workflows/release.yml/badge.svg)](https://github.com/Nieole/manga_manager/actions/workflows/release.yml)

Manga Manager 是一款自托管的本地漫画 / 画集管理与阅读服务器,基于 `Go` + `React (Vite + TailwindCSS)` 构建。

它不依赖任何云端数据库,完全围绕本地文件扫描工作:把硬盘上散落的压缩包(`zip` / `cbz` / `rar` / `cbr`)整理为带封面、元数据、阅读进度的系列书库,并通过浏览器、OPDS 客户端、Mihon、KOReader 等多种入口阅读。内置多用户账户体系,可为不同成员分别记录阅读进度。后端编译为单个二进制,解压即用;数据库、缩略图、配置与日志都落在本地 `data/` 目录,不污染系统环境。

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
- 基于 Go 协程工作池并发扫描、解压与解析;并发度受 IO 策略约束,可在 SSD / 机械硬盘 / 网络盘等场景下按需调优。
- 支持 `zip` / `cbz` / `rar` / `cbr` 压缩包,并自动解析包内 `ComicInfo.xml`(`rar` / `cbr` 为只读)。
- 使用内嵌的 `SQLite`(`modernc.org/sqlite`,驱动本身 CGO-free)作为索引库,免安装、零外部数据库依赖。
- 自动截取卷册封面并生成缩略图(固定 400px 宽),支持 `WebP`(默认)/ `AVIF` 编码以控制体积。
- 提供可配置的 IO 策略(扫描 / 解压 / 封面 / 哈希并发度、阅读时降载、同盘缓存控制),避免后台任务与阅读争抢磁盘。
- **四档扫描档位**(`fast` / `metadata` / `identity` / `repair`):决定是否打开压缩包、抽取 `ComicInfo`、以及是否计算文件哈希。默认为 `metadata`,不计算内容哈希。
- **可选的实时文件夹监听**:按库开启(扫描模式设为 `watch`)后,借助 `fsnotify` 递归监听并在防抖后触发增量扫描与失踪记录清理。
- **重复文件检测与安全清理**:按内容哈希分组识别重复卷册,可将原文件移入 `data/trash`(永不硬删除源文件);该能力需先执行 `identity` / `repair` 扫描以生成文件哈希。
- **元数据写回**:编辑后的 `ComicInfo.xml` 可原子写回 `zip` / `cbz` 压缩包(`rar` / `cbr` 不支持写回)。
- 内置解压安全护栏(单条目 256 MiB 上限)与批量误删保护(库根不可达或超过 50% 系列疑似丢失时中止清理)。

### 👥 多用户与账户
- 内置**多用户账户体系**:服务端会话 Cookie(`mm_session`)+ CSRF 令牌 + `bcrypt` 口令。角色分**管理员**(全部权限)与**普通用户**(只读浏览 + 记录本人进度 / 书签 / 短评)。
- 首次通过浏览器访问会**强制创建管理员**;管理员新建的账号首次登录需**强制改密**。
- **每用户独立**记录阅读进度、书签、系列短评与深度统计(连读天数、阅读时长、周期分布);首个管理员创建时会自动迁移历史的全局进度 / 活动 / 无主 KOReader 账户。
- 管理员保护:不能删除自己,也不能降级 / 删除最后一位管理员。

### 🔍 全文搜索
- 内置 SQLite FTS5(trigram)全文索引,按**系列 / 卷册的名称与标题**跨库检索;FTS 不可用时自动降级为子串扫描。
- 搜索结果附带封面与归一化到 `0~1` 的相关度评分,并提供 OpenSearch 描述端点。
- 作者 / 标签作为**结构化筛选项**(经 Mihon 端筛选),而非全文检索目标。

### 🤖 元数据刮削与 AI
- **多刮削源**:`Bangumi`(默认)、`AniList`、`MangaDex`(三者无需密钥即可使用),以及 `MyAnimeList`、`Comic Vine`(需在设置中填入 MAL Client ID / Comic Vine API Key 后才出现在可用源中)。所有 HTTP 源共享指数退避重试并遵守 `Retry-After`(429 / 5xx)。
- **LLM 刮削**:对接兼容的大模型后端 —— `Ollama`、OpenAI 的 `/v1/responses` 与 `/v1/chat/completions` 均可,`Provider / Endpoint / Model / API Key / Timeout` 全部可在设置页动态配置,并内置连通性测试(带 SSRF 防护)。
- **批量刮削**:可对全库或单个库发起批量刮削,作为后台任务运行(支持暂停 / 取消 / 重试,进度经 SSE 推送);按库刮削会自动跳过已有简介 / 出版社的系列。
- **元数据审核队列**:字段级 "现值 vs 拟改" 差异对比、按来源与字段的置信度评分、逐条或批量应用 / 驳回;同一系列内相同候选自动去重。`Bangumi` 会写入可点击的 `bgm.tv` 外链,其余来源的源地址随审核条目保留。
- **字段锁定与来源溯源**:可锁定单个字段使其永不被刮削覆盖;每个已应用字段记录来源、地址、置信度与时间。
- **AI 智能分组**:借助 LLM 对**尚未归入任何合集的系列**给出主题合集建议(单次约 50 个系列、归为 3~5 个合集),经审核流后落库为合集。
- **AI 阅读推荐**:首页基于常读标签 + 候选系列,请 LLM 给出带理由的推荐,按语言缓存 24 小时。
- **统一审核收件箱**:聚合待处理的元数据与 AI 分组审核并给出汇总。

### 🗂️ 合集、智能筛选与关系图谱
- 手动**合集**(增删改、批量加入 / 移除系列)。
- **智能合集 / 智能筛选**:按评分区间、阅读进度、加入天数、阅读状态、标签、作者、状态、首字母等条件动态计算成员(每个条件单值、以 AND 组合);可将当前结果**快照**冻结为静态合集。
- **阅读列表(Reading List)**:增删、排序、逐项进度回填。
- **系列关联关系**与**关系图谱**:力导向可视化图谱,支持系列级与全库级(超大书库会截断展示,服务端约 4000 条关系 / 客户端约 200 节点);相关系列还会按连通分量自动生成 "Franchise" 系统合集。
- **标签管理**:重命名 / 合并 / 删除(作者侧目前仅支持列表与搜索,不含改名 / 合并 / 删除)。
- **自定义系列字段**:按系列维护键值对(整表替换保存)。
- **批量编辑**:跨多个系列批量增删标签、设置状态 / 出版社、批量收藏与已读 / 未读。

### 🖼️ 画质重建:超分辨率(可选)
- 集成 `waifu2x-ncnn-vulkan` 与 `realcugan-ncnn-vulkan`,在**阅读时按需**对页面放大降噪(非预先烘焙),结果内存 + 磁盘缓存。
- 支持多级放大(1/2/4/8)、降噪强度调节与轻度重编码(默认 `WebP`,可选 `PNG` / `JPG`)。
- 两套引擎相互隔离、可在阅读器滤镜下拉中**按页切换**;并发数受上限约束,引擎失败时优雅回退 `Lanczos3`。
- 属可选能力:需自行下载对应引擎并在设置中填入可执行文件**绝对路径**(路径校验仅检查存在性;非绝对路径会在阅读时被忽略并回退到 `PATH` / `./bin`)。配置改动可热重载。

### 📱 响应式沉浸阅读
- 移动优先(Mobile First)的 TailwindCSS 断点设计,从桌面大屏到手机均保持合理排布。
- **瀑布流(Webtoon,虚拟滚动)** 与 **翻页(Paged)** 双模式切换;`LTR / RTL` 方向仅作用于翻页模式(瀑布流恒为纵向)。另有**双页对开**布局。
- 提供 "原始 / 等宽 / 等高 / 适屏" 多档画幅,页面预加载(可调档数)并预取下一册,超窗口图像自动释放以控制内存占用。
- 翻页模式支持**自由缩放 / 平移**(捏合、双击 2.5x、Ctrl+滚轮、拖拽,至多 4x)。
- **阅读书签(带备注)**、**护眼暖色叠层**、**双阅读器主题**(base / comimi)、**自动裁边**、**多档重采样滤镜**。
- 每会话可覆盖**网络传输格式 / 画质**(`webp` / `jpeg` + 质量档;`AVIF` 仅用于封面缩略图,不作阅读直出)。
- **离线缓存**:可将整册下载到本地(Cache Storage + Service Worker)离线阅读,并提供缓存 / 删除入口。

### 🔌 多端协议与同步
- **OPDS v1.2**:导航 / 获取 / PSE 分页流式,含续读、最近添加、合集、阅读列表、智能合集与 OpenSearch;兼容 Chunky、Panels、Tachiyomi/Mihon 等常见 OPDS 客户端(以协议兼容为准)。
- **Mihon 扩展 API**:系列 / 卷册浏览、标签 / 作者 / 状态筛选、智能合集、阅读列表与进度写回。
- **KOReader 进度同步**:兼容 kosync 协议,支持**二进制哈希**与**文件路径指纹**(相对路径,可选忽略扩展名)两种匹配模式;使用按账户的独立同步密钥,并可将进度并入对应站点用户。设备自助注册默认关闭(`allow_registration`),可在设置中开启;管理员可管理 KOReader 账户(新建 / 轮换密钥 / 启停 / 删除),并对未匹配进度进行校准 / 重建标识。
- 三类协议均可**按需单独启停**;`OPDS` / `Mihon` 默认关闭,启用后使用 HTTP Basic(站点用户名 + 口令)鉴权。

### ⚙️ 运维与可观测性
- `slog` + `Lumberjack` 结构化日志:关键请求与异常落 `data/*.log`,按文件大小滚动(单文件 10MB、最多保留 5 个备份),旧文件 gzip 压缩并按保留期(28 天)清理。
- **可视化日志看板**:面向已登录管理员在浏览器中查看并高亮 `error / warn / info` 日志、按任务过滤(按需拉取,无需登录主机)。
- **SSE 实时事件**:扫描 / 刮削等后台任务进度通过 Server-Sent Events 推送到前端任务中心,带心跳与背压保护。
- **后台任务中心**:任务支持暂停 / 继续 / 取消 / 重试,并持久化以便重启后重建。
- **健康检查**:在诊断面板中按需统计空页、缺封面、缺元数据、重复哈希(及缺失 / 重复快速哈希、未匹配 KOReader)等问题项,并提供一键修复(重扫 / 刮削 / 重建标识 / KOReader 校准)。
- **健康探针**:`GET /api/health` 返回服务与数据库连通状态(数据库不可达时返回 `503`),可用于反向代理 / 编排器的存活 / 就绪检查。

> 🔐 **身份认证与安全**
>
> - 认证为**默认强制、无需额外开关**:一旦创建首个管理员,所有 Web UI 与 `/api` 接口(OPDS / Mihon 协议端点除外,见下)均要求登录会话。唯一例外是**全新数据库尚未完成初始化**的短暂窗口(此时尚无任何账户,接口处于直通状态),因此请在首次启动后**立即完成管理员创建**。
> - 服务默认监听 `0.0.0.0:8080`,且**不内置 HTTPS,也没有登录失败限流 / 锁定**。对公网暴露时请置于反向代理(Nginx / Caddy 等)之后并启用 TLS(HTTPS 下会话 Cookie 会自动带 `Secure`)。
> - 默认注入安全响应头(`X-Content-Type-Options: nosniff`、`X-Frame-Options: DENY`、`Referrer-Policy: no-referrer`、`Permissions-Policy`);当 CORS 来源配置为通配符时,自动关闭带凭据的跨域请求。
> - OPDS / Mihon 协议端点默认关闭,启用后使用 **HTTP Basic**(站点用户名 + 口令)鉴权;KOReader 同步使用**独立的按账户同步密钥**。

---

## 📦 部署与使用

得益于 Go 的交叉编译,Manga Manager 可在 Windows / Linux / macOS 上运行。

### 0. GitHub Actions 自动构建与发布

仓库内置两条工作流:

- `CI`:在 `push main` 与 `pull_request` 时于 `ubuntu` + `windows` 双平台矩阵执行前端 `lint`、前端测试(`vitest`)、前端构建、`go vet`、`go test`、`go test -race`(ubuntu),以及 `sqlc` 与 `tsgen` 的生成代码漂移校验和服务端编译检查。
- `Release`:在推送 `v*` 标签时,由各原生 runner 分别构建 `Linux AMD64`、`Windows AMD64`、`macOS ARM64` 三平台二进制并上传到 GitHub Release(也可在 `Actions → Release → Run workflow` 手动触发)。

```bash
git tag v0.1.0
git push origin v0.1.0
```

### 1. 从源码构建

需要本地具备 `Go` 与 `Node.js` 环境;`build.sh` 还需要 `zig` 作为交叉编译的 C/C++ 工具链(用于 Linux musl 静态与 Windows mingw 目标,macOS ARM64 原生编译)。所有目标均以 `CGO_ENABLED=1` 构建(`WebP` 编码依赖 CGO)。在项目根目录运行:

```bash
# 先用 Vite 打包前端到 dist,再交叉编译三端二进制到 build/
./build.sh
```

Windows 下可使用 `build.ps1`(仅产出 Windows AMD64 单个二进制,使用原生 CGO)。

### 2. 运行二进制

在 `build/` 中选择对应平台的可执行文件运行(以 macOS ARM64 为例):

```bash
./manga-manager-mac-arm64
```

首次运行会自动生成 `data/` 目录,用于存放缩略图、数据库、配置与日志。

### 3. 打开控制台

浏览器访问 [http://localhost:8080](http://localhost:8080)。首次访问会引导**创建管理员账户**;登录后进入**设置 ⚙️**,添加包含漫画文件的绝对目录并执行**全局扫描**即可。

### 4. 命令行参数与环境变量

```bash
./manga-manager-mac-arm64 -config /path/to/config.yaml -data-dir /path/to/data
./manga-manager-mac-arm64 -version
```

- `-config`(或环境变量 `MANGA_MANAGER_CONFIG`):配置文件路径,默认 `config.yaml`。
- `-data-dir`(或环境变量 `MANGA_MANAGER_DATA_DIR`):数据目录,默认 `data`。
- 配置文件支持基于 `fsnotify` 的热重载,可参考仓库内 `config.example.yaml`(该模板并非穷举所有配置项)。

---

## 🛠️ 启用超分辨率(可选)

1. 自行从 GitHub 下载 `waifu2x-ncnn-vulkan` 或 `realcugan-ncnn-vulkan` 的官方 Release 并解压到本地。
2. 在 **设置 → 智能扫描与处理引擎设定** 中,把两个可执行文件的**绝对路径**分别填入并保存。
3. 进入任意画集阅读,在阅读器滤镜下拉中选择对应引擎,并按需调整放大倍率(1/2/4/8)、降噪强度与输出格式(`WebP` / `PNG` / `JPG`)。

---

## 🧱 技术栈

| 层 | 技术 |
| --- | --- |
| 后端 | Go 1.25.0、`chi/v5` v5.3.0 路由、`sqlc` 预编译查询、`modernc.org/sqlite` v1.51.0(SQLite 驱动 CGO-free;整体二进制因 WebP 编码仍以 CGO 构建) |
| 搜索 | SQLite FTS5(trigram)全文索引(系列 / 卷册名称与标题) |
| 图像 | `chai2010/webp`、`gen2brain/avif`、`nfnt/resize`、`disintegration/imaging`、`x/image/webp`(解码);可选 waifu2x / Real-CUGAN |
| 解析 | zip / cbz、rar / cbr、ComicInfo.xml(读取 + zip / cbz 写回) |
| 前端 | React 19、Vite 7、TailwindCSS 4、`@xyflow/react` 12、`react-virtuoso` 4、`react-router` 7、`axios` 1;i18n(中 / 英),响应式阅读器 |
| 协议 | OPDS v1.2(含 PSE)、Mihon 扩展 API、KOReader kosync、SSE |
| 鉴权 | 服务端会话 Cookie + CSRF(Web)、HTTP Basic(OPDS / Mihon)、按账户同步密钥(KOReader) |

---

## 🤝 开发

```bash
# 前端开发
cd web && npm run dev

# 前端构建 / 检查 / 测试
cd web && npm run build
cd web && npm run lint
cd web && npm run test   # vitest

# 后端测试
go test ./...

# 重新生成前端接口契约(CI 会做漂移校验)
go run ./cmd/tsgen
```

修改 `sql/query.sql` 或 `internal/database/schema.sql` 后,需运行 `sqlc generate` 重新生成 Go 绑定。更多约定见 [`AGENTS.md`](AGENTS.md),版本变更见 [`CHANGELOG.md`](CHANGELOG.md)。

---
*Developed with ❤️ via AI-Pair.*
