# 资料库迁移弹窗 GPU 占用回归

## 结论与范围

迁移弹窗的全屏背景模糊与后台任务气泡的持续动画叠加，使浏览器持续处理整个视口。迁移与恢复自动处理弹窗使用普通遮罩，进度刷新、任务动画和明细分页保留，其他弹窗默认外观保留。

本地修复分支：`codex/fix-relocation-gpu`。生产改动仅为 `ModalShell` 的可选 `blurBackdrop` 与两个长任务弹窗的调用。

## 复现边界

- 用户现场：另一台电脑检查约 50 万条目的资料库时，弹窗开启 GPU 约 70–80%，关闭约 20–30%，重开立即升高；本次没有访问该电脑或真实数据库。
- 本机：Windows、AMD Radeon 780M。独立有界面 Chrome，视口 2560×1215，使用实际 `RelocationModal`、`ModalShell`、`SidebarTaskBubble`、语言与主题组件。
- 隔离数据仅模拟接口契约：5000 系列、500000 本书的摘要，检查处于 checking；后台气泡按任务引擎的 200ms 投递间隔更新，弹窗保留实际的约 1s 轮询。未构造或读取 50 万条真实数据库记录，也未将它们渲染到 DOM。
- 模拟接口由独立测试页面内的 axios adapter 提供，不连接后端、不发起迁移或取消请求。
- GPU 数值为该独立 Chrome GPU 进程在 Windows 3D / high-priority 3D 引擎上的占用之和；不是整机任务管理器总占用，也不是用户另一台电脑的复测数值。每个状态稳定 1.5s 后采样 8 次，间隔 1s。浏览器关闭遮挡降频，仅用于保证所有对照都有实际绘制。

## 反馈循环与隔离

初始回归命令 `node .tmp/relocation-gpu/profile.mjs regression` 在未改动产品源码时退出 1：`GPU regression: opening relocation dialog adds >=10 percentage points`。关窗均值 1.90%，开窗 28.39%，重复开窗 28.44%。未通过修改生产代码制造 RED。

可维护回归脚本归档为 `profile.mjs` 与 `measure-gpu.ps1`；真实调用位置仍是迁移组件，断言 GPU 差值，不用 jsdom 的 class 字符串断言代替性能结果。它在本机修复后重新运行通过；阈值用于捕捉此次约 26 个百分点的增长，不是跨硬件的通用性能标准。

| 单一变量 | Chrome GPU 均值 |
| --- | ---: |
| 原版关窗 | 1.92% |
| 原版开窗 | 28.69% |
| 仅移除全部背景模糊 | 1.48% |
| 恢复原版 | 28.73% |
| 仅关闭动画 | 2.85% |
| 再恢复原版 | 28.66% |
| 仅暂停弹窗进度轮询，保留后台气泡更新与动画 | 28.98% |

进一步隔离只去掉全屏遮罩的模糊、保留气泡本身的局部模糊及动画：2.04%；恢复全屏模糊后 28.67%。去掉全部 72 张背景卡片的最小场景仍有开窗 27.66%、关窗约 2%，无需大量卡片或数据库记录才可复现。最小场景的最后一段曾因动态性能计数器样本无效中止，未记为通过；采样器只接受目标进程的有效样本并要求足够数量，修复后完整对照及回归另行通过。

原始对照、RED、GREEN 和图片存放于 `evidence/`。未向产品加入调试代码或日志。临时浏览器配置、依赖及运行产物位于忽略的 `.tmp/relocation-gpu/`。

## 运行回归

在仓库根目录执行（需要 Windows GPU 性能计数器、已安装 Chrome，以及 Node.js）：

```powershell
npm install --prefix .tmp/relocation-gpu/runtime --no-audit --no-fund --package-lock=false playwright-core@1.64.1
npm --prefix web run dev -- --host 127.0.0.1 --port 18084 --strictPort
```

保持开发服务器运行，在另一终端执行：

```powershell
node .scratch/relocation-gpu/profile.mjs regression
```

脚本只向 web 临时放入两个独立测试入口文件，遇到同名且内容不同的文件会拒绝覆盖；结束后清理它自己创建的入口，关闭独立浏览器。它不改变产品设置或真实数据。

可用 `minimal`、`isolation`、`probes` 模式复核隔离实验；这些模式只报告对照，只有 `regression` 的差值超阈值会退出失败。全屏模糊在产品中被关闭后，原版效果已不再默认存在，历史 RED 和隔离结果以已保存证据为准。

## 修复验收

| 检查 | 结果 |
| --- | --- |
| 修改前实际组件的 GPU 回归 | RED，退出 1：1.90% → 28.39%，重复开窗 28.44%。 |
| 修复后归档脚本 `node .scratch/relocation-gpu/profile.mjs regression` | GREEN，退出 0：2.08% → 2.13%；重复关窗 2.04%、重开 2.22%。未选择诊断 CSS 覆盖，使用实际产品调用。 |
| 前端全量 Vitest | 81 文件、866 项通过。 |
| ESLint | 退出 0。 |
| TypeScript 与生产前端构建 | `npm run build` 退出 0，主工作区产物已刷新。 |
| 差异空白检查 | 保留既有已跟踪 CRLF 文件，使用单次 `core.whitespace` 的 `cr-at-eol` 规则通过；未改全局 Git 配置。 |

后端源码、SQL 与生成契约均未修改，因此未重复 Go、sqlc 或 tsgen 门禁。未修改浏览器硬件加速设置、安全策略或真实资料库数据。未推送或发布这个修复。模拟入口已从 web 移除，独立测量浏览器已关闭；复现工具与证据明确保留于此目录，缓存和完整日志位于 `.tmp/relocation-gpu/`。另一台电脑的原现场仍需在升级修复版本后复测，本地数值不代替该现场测量。
