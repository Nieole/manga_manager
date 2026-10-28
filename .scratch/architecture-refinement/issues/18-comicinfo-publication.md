# 18: ComicInfo 回写的安全发布

**What to build:** 临时归档与阅读并行构建，最终有界协调借用后安全发布并更新内容事实。

**Blocked by:** 17: 整书下载与读取句柄。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 41、98、99、100、101、102、103、104、105、196。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 发布前阻止新借用并等活跃读取，核对原文件变化，冲突或超时保留原文件。
- [x] 继续沿用保守 ComicInfo 合并和格式支持，池淘汰不当作已退出。
- [x] 成功后交接内容事实和缓存，后续失败保留已提交状态，不诱导盲重写。
- [x] 实际文件系统、活跃页/下载、取消及变更竞争验证，不另造任务准入。

## Comments

### 实施证据

- `parser.PrepareComicInfo` 使用实际共享源借用构建同目录暂存，保留保守 XML 合并、条目路径及 ZIP/CBZ 支持边界；返回前关闭自身源句柄。`PreparedComicInfo.Publish` 使用同一借用登记关闭新准入，最多等待五秒（调用者可更早取消），直到直接读、旧池/已淘汰页借用和整书流全部退出；不截断既有读取。
- 发布窗口中淘汰闲置池句柄，Windows 路径大小写别名同时处理；核对文件身份、mtime、大小和 SHA-256 后实际 `os.Rename` 覆盖。取消、超时、竞争发布和源变化均保持原文件，由本次 staging 的 Close 清理自身临时文件，不清理其他操作的暂存。
- `bookcontent.Publisher` 负责完整构建、发布及事实/缓存交接。数据库按书 ID 与路径刷新实际 size/mtime 并清除过期内容 hash，保留身份、阅读态和路径指纹；生产 runtime 接通共享来源/页目录缓存失效。来源缓存重建以实际文件 stat 补充字节事实，事实库更新失败也不继续命中过期来源版本。
- diskjobs 继续经既有任务准入、LibraryIDs 和磁盘作业运行；成功替换计入 written，后续事实更新失败单独计入 warnings 并留下明确“file committed; refresh facts before retrying”运行告警，不计为未写入失败。HTTP 页清单、页图与下载将发布占用映射为 503。
- TDD 两轮首先因缺少 PrepareComicInfo/Publisher 编译失败，随后真实文件回归通过。Windows 实际验证替换成功、已淘汰借用继续读页、新借用被拒、超时/取消/外部源变化保留原文及 staging 清理；完整 ReadHandle 下载读完前发布等待，既有字节不被截断。
- 实际通过：完整 parser（4.121s）；完整 bookcontent `-race`（7.460s）；完整 diskjobs `-race`（14.647s）及普通完整包（3.084s）；HTTP 发布占用、真实数据库触发器造成的提交后失败、下载/协议和 ComicInfo 运行回归（6.919s）。初轮 parser 定向保守合并与发布回归亦实际通过（1.417s）。
- 完整 parser 的两次 `-race` 启动遭 Windows Application Control 拦截，未计通过；随后普通完整 parser 实际通过。合并后首次完整 API/runtime 启动同样被拦，单独重试的最终结果另行记录。最终 lint 为 `0 issues.`，此前 build 与 gofmt 门禁实际通过。前端源未变，告警沿用运行事件消费者展示。
- 已合入集成 `f197115`（包含 22 资料库 reservation），沿用原 Engine 访问接口。原始日志保存在本工作树 `.tmp/ticket18-*.log`，清理前应保留；本票已完成集成验收并关闭。
- 合并后完整 API 实际通过（113.652s）。runtime 完整包两次被 Windows Application Control 阻止启动，未计通过。
- 已提交范围由每次文件提交后立即写入运行的 `committed_book_ids`/`written` 保存；终态参数另列 warnings，实际事实库触发器失败保持 written=1/failed=0/warnings=1。告警事件带 book_id 与已提交、先刷新事实再重试的明确文字，既有 TaskCenter Warn 消费者展示其 detail；ComicInfo 无自动重启/重试能力。新增范围与 ListRunEvents 断言的普通二进制启动被拦，最终实际结果另录。
- 新增精确范围/告警事件断言的最终 `-race` 二进制同样遭 Application Control 阻止启动，在该工作树未实际执行；此最终新增断言随后已在集成工作树实际复验通过，见集成验收。本票不将此前启动拦截记作通过。该断言扩展前同一真实触发器失败路径及最终完整 API 已实际通过，不能替代新增断言本身的执行证据。
### 集成验收

- 已由合并提交 `4d23f99` 纳入 `codex/architecture-refinement`，无冲突。
- 主树使用 Go 1.26.10 实际执行最终 `TestComicInfoCommitted`、`TestPublicationWindow`、`TestWriteComicInfo`，覆盖新增的真实 Run 已提交范围与 `ListRunEvents` 告警断言，并联合 22 删除准入与 17 整书下载/真实退出回归：普通程序两次遭 Application Control 拦截，随后 `-race` 变体实际通过（17.221s），未把启动拦截计为通过。
- 最终完整 runtime/parser 实际通过（8.019s/3.742s）；完整 diskjobs `-race` 通过（16.263s），bookcontent 完整 `-race` 首次被拦、重跑实际通过（7.973s）。gofmt 无输出，独立缓存 lint 为 `0 issues.`。原始输出保留在主工作树 `.tmp/merge-wave8-18-*.log`；没有前端或 SQL/TS 生成源变化，未重复无关全仓门禁。
