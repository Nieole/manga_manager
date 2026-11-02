# 33: 离线归属与补传功能

**What to build:** 鉴权、阅读器和离线书架通过完整离线功能管理设备数据、下载及补传生命周期。

**Blocked by:** None (can start immediately)。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 165、166、167、168。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [ ] 功能持有 owner、缓存字节、书目、下载与队列契约，消费者不依赖页面目录。
- [ ] 断网/过期保留、显式登出、换人隔离及首次认领按既有语义。
- [ ] 原时间、稳定事实、lost-ack 重试与逐本回退不改；不补造历史或全站任务。
- [ ] 真实功能消费者与离线回归验证跨书、身份和网络变化。

## Comments

### 实施与恢复验证（2026-11-02，待独立合并验收）

- 保留中断前全部工作区改动，完成 `features/offline` 的 owner、书目、缓存字节、下载、队列及消费者生命周期归属。AuthProvider 的登出顺序交给 `endOfflineSession`，OfflineShelf 消费 `useOfflineShelf`，BookReader 消费功能所属 `useReaderOffline`；功能不再导入页面类型，旧页面生产实现已退出。没有改缓存/owner 键、代次围栏、下载令牌、稳定事实、原时间或逐本回退规则，没有全站通用 store。
- 新增 `readerOfflineConsumer.test.tsx`，仅替换浏览器 Cache Storage/HTTP，真实调用功能 hook 与存储：下载跨书完成仍归原书；重连补传两本书的原时间/事实；换人后旧响应不能删掉新用户同书队列。此前新增的真实书架 lost-ack/回退消费者与 auth 登出失败回收回归完整保留。旧离线删除、下载、归属、事实与阅读器切换回归继续运行。
- 当前实现树 HEAD 与集成引用均为 `b913455f22f1b7eff431ffec2e555d1ea41987c1`，无需引入额外集成差异。执行环境的上级约束禁止提交、索引变更及 merge；因此全部修改未提交，工单状态保持开放，交独立集成人员提交/合入。
- 新读者消费者：`npm run test -- --maxWorkers=2 src/features/offline/readerOfflineConsumer.test.tsx`，1 文件 / 2 测试通过，`.tmp/33-retry-reader-consumer.log`。
- 全量验证按运行环境分区：`npm run test -- --pool=threads --maxWorkers=2 --exclude src/utils/heatmap.test.ts`，103 文件 / 942 测试通过（`.tmp/33-retry-web-threads-except-heatmap.log`）；`npm run test -- --maxWorkers=1 src/utils/heatmap.test.ts`，1 文件 / 16 测试通过（`.tmp/33-retry-heatmap-fork.log`）。合计 104 文件 / 958 测试实际通过。
- 初次完整 forks 命令 `npm run test -- --maxWorkers=2` 被 Windows worker 启动失败影响（58 文件 / 383 测试通过，46 worker 错误；`.tmp/33-retry-web-full.log`），不计通过；改用 threads 的完整运行是 103 文件通过、heatmap 时区动态切换一项失败（957/958，`.tmp/33-retry-web-full-threads.log`），不计完整通过。heatmap 修改进程 TZ 在 worker thread 内不生效，故保持源码不变，单独用默认 fork 实际通过全部 16 项；最终全量分区无跳过测试。中断前 `.tmp/33-*.log` 原始记录均保留。
- `npm run lint` exit 0（`.tmp/33-retry-web-lint.log`）；`npm run build` exit 0（`.tmp/33-retry-web-build.log`），本树自行生成 dist；`git diff --check` 无输出，旧页面离线生产路径及功能反向导入页面扫描无匹配。依赖沿用已设置的 ignored node_modules；未改动主树、其他工人的设置/locale/useRequestLifetime/ApplicationSessionProvider 文件。

### 独立审查（2026-11-02，未提交/未合入）

- 独立读取 AGENTS、spec/design/execution、工单及 issue-tracker 规则，审查全部 tracked diff 和 5 个 untracked 功能文件，并逐项比较迁出实现。真实生产消费者为 AuthProvider/offlineAccess、BookReader、OfflineShelf 及阅读器设置组件；未发现本票新增的架构阻塞。缓存/owner 键、代次、令牌、原时间和稳定事实/回退实现保持原样，书架配方迁入功能 hook，无功能反向页面依赖或旧离线页面入口残留。
- 独立检查发现 untracked `useReaderOffline.ts` 带有迁移前已有的行尾空白（普通 `git diff --check` 不覆盖 untracked），仅删除该空白行；无行为重写。最终 tracked + 全部 untracked whitespace 检查通过：`.tmp/33-independent-review-diff-check-final.log`；正确收窄到功能反向依赖和旧离线入口的扫描通过：`.tmp/33-independent-review-ownership.log`。初次过宽扫描误命中合法 UI imports 的日志保留，不作为失败的架构证据。
- 修整后使用仓库 `.node-version` 的 Node v24.16.0 独立执行 11 文件 / 86 项真实功能、书架、鉴权、删除/下载/归属/事实和阅读器切换消费者测试，全部通过；lint 与 build 均 exit 0。日志分别为 `.tmp/33-independent-review-consumers-final.log`、`.tmp/33-independent-review-lint-final.log`、`.tmp/33-independent-review-build-final.log`。此前 npm 不在 PATH 的启动失败及 v24.21.0 通过记录均保留。
- 已检查全部 `.tmp/33-retry-*.log`：103 文件/942 项 threads 与 heatmap 单独 fork 1 文件/16 项的分区通过证据支持合计 104 文件/958 项；原完整 forks worker 错误及 threads TZ 失败仍明确为未通过。独立审查未重跑全量，不冒称全量独立验证。
- 上级指令禁止提交、索引变更及合并；因此未提交、未合入、未关闭，queue/execution 完成计数不变。主树保持 clean、两树 HEAD 仍为 b913455。父集成人员须提交本票 scope、串行合入主树后运行前端全量分区测试/lint/build，再按规则勾选验收、关闭工单并更新 queue/execution；完整待提交路径见 `.tmp/33-independent-review-scope.txt`。所有源改动与旧/新日志保留，其他工作树未触碰。
