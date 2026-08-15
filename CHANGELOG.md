# Changelog

本文件记录项目各版本的功能新增、改动与修复。

---

### 🏷️ v1.4.1 — 2026-07-30（工具链与依赖升级）

> 纯维护版本，无功能变更、无数据库改动。v1.4.0 的产物是这批升级**之前**的代码。
> 从 v1.4.0 升级无需任何额外步骤（v1.4.0 本身的两条升级须知见下一段）。

#### CI
- **七个 GitHub Action 全部升到原生 node24。** GitHub 已弃用 Actions runner 上的 node20，
  此前有**四个** action 还在 node20（GitHub 的弃用告警只点了其中两个，另外两个是逐个查
  各自 `action.yml` 的 `runs.using` 才发现的），靠 `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24`
  被强制降级运行——CI 能过，但等 node20 真从 runner 上移除时会集体失效。
  - `golangci-lint-action` v7.0.1 → v9.3.0 · `upload-artifact` v5.0.0 → v7.0.1
    · `download-artifact` v6.0.0 → v8.0.1 · `action-gh-release` v2.6.2 → v3.0.2
    · `setup-node` v6.5.0 → v7.0.0 · `setup-go` v6.5.0 → v7.0.0
  - 逐条核对过跨大版本的破坏性变更对本仓用法的影响。其中 `download-artifact` v8 把
    「下载哈希不匹配」从警告改为**报错**——对发布流水线是净收益（我们本来就产出
    `SHA256SUMS.txt` 供自建者校验）。
  - `FORCE_JAVASCRIPT_ACTIONS_TO_NODE24` **保留不删**：所有 action 现已原生 node24，
    它因此变成安全网而非必需项——将来某次依赖升级若又引入 node20 的 action，
    它能让其继续跑，而不是等 runner 移除那天 CI 突然全红。已补注释写清这一点。

#### 依赖
- **Go 依赖 7 项**：`modernc.org/sqlite` 1.51.0 → 1.55.0（内嵌 SQLite 版本随之变化，
  因此单独验过迁移、FTS5 gram 索引、部分唯一索引与 RAR 夹具）、`golang.org/x/crypto`
  0.39.0 → 0.54.0（bcrypt 在鉴权路径上）、`x/image` 0.43.0 → 0.44.0、
  `go-chi/chi` 5.3.0 → 5.3.1、`rardecode` 2.2.3 → 2.3.0、`gen2brain/avif` 0.4.4 → 0.6.0。
- **前端依赖 21 项**，含四个大版本：`eslint` 9 → 10、`@vitejs/plugin-react` 5 → 6、
  `vite` 7 → 8、`@types/node` 25 → 26，以及 `@yui540/comimi` 0.5 → 0.18（阅读器的
  Comimi 主题）。前端高危漏洞从 **7 个降到 2 个**。
  - **TypeScript 暂留在 6**：`typescript-eslint` 目前没有任何版本支持 TS 7
    （最新 8.65.0 的 peer 仍是 `typescript <6.1.0`），强行升级会让 `npm ci` 直接
    ERESOLVE 失败。等上游支持后再单独升。
  - 剩下的 2 个高危漏洞是同一条——react-router 的 RSC 模式 CSRF 绕过
    ([GHSA-qwww-vcr4-c8h2](https://github.com/advisories/GHSA-qwww-vcr4-c8h2))。
    **对本仓不可达**（我们用的是最朴素的 `BrowserRouter`，没有 RSC 与 server actions），
    且**没有修复版可升**（通告范围 7.12.0–8.2.0，而 npm 上最新就是 7.18.2；
    `npm audit` 建议的「修复」是降到 7.11.0）。
- ⚠️ **待人工确认**：`comimi` 0.5 → 0.18 的运行时行为（手势、默认设置、菜单）无法用类型
  检查覆盖，建议在阅读器的 Comimi 主题上过一眼。它是**可选**主题，默认主题不受影响。

#### 修复
- eslint 10 默认启用了 `preserve-caught-error` 与 `no-useless-assignment`，报出 4 处真问题：
  登录 / 初始化 / 改密三处 `throw new Error(...)` 把原始的 axios 错误整个丢掉了
  （状态码与响应体这些排查要用的信息全没了），改为挂到 `cause` 上；
  另有一处读不到的变量初值。

---

### 🏷️ v1.4.0 — 2026-07-30（全量代码审计的逐批修复）

> 对整个代码库做了一次系统性体检并逐批修复，共 62 个提交。每条修复都配了回归测试，
> 高危项做了**反向验证**（临时回退修复、确认用例变红）；部分方案在对抗式复核后被判为
> 「不做」并在下面写明理由。
>
> **升级须知（两条，务必先读）：**
>
> 1. **不可回滚到 v1.4.0 之前的二进制。** `metadata_review_fields.status` 这一列已被删除
>    ——它是死数据（唯一写入点硬编码 `'pending'`，全仓无 UPDATE，且读路径只查 pending
>    审核，写进别的值也永远读不出来）。旧版二进制的 SELECT/INSERT 显式带这一列，
>    启动后会报 `no such column`。升级前请备份数据库。
> 2. **首次启动后 franchise 合集的链接会失效一次。** 这类系统合集改用稳定自然键后，
>    存量的无键行会被换成带键新行（id 变化）。如果你在 Mihon/OPDS 客户端里按 id 收藏过
>    franchise 合集、或在站内存了合集链接，需要重新添加一次；之后就不会再变了。
>    **手工创建的合集完全不受影响。**
>
> **对外行为变更：**
>
> - 元数据审核：对已处于终态的记录重复 apply/reject 由 200 变为 **409**。
> - 多用户站点新注册的 KOReader 设备在被管理员指派之前，其同步进度不计入任何用户
>   （此前会被静默记到 id 最小的管理员名下）。
> - 普通用户不再看到资料库增删改与系统设置的入口（后端一直是管理员专属，前端此前照样
>   渲染，点了只会吃 403）。
> - 批量应用元数据的响应新增 `partial` 桶。
> - 设置页「保存本分区」不再连带提交其他分区未保存的草稿；反过来保存后也不再抹掉它们。
>
> 下面按主题列出本次的全部改动。

## 更早的版本

- [2026-08](docs/changelog/2026-08.md) — 1 条
- [2026-07](docs/changelog/2026-07.md) — 95 条
- [2026-06](docs/changelog/2026-06.md) — 3 条
- [2026-05](docs/changelog/2026-05.md) — 58 条
- [2026-04](docs/changelog/2026-04.md) — 42 条
