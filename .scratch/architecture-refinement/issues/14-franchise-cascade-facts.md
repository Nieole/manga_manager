# 14: 级联变化与作品群修复覆盖

**What to build:** 删系列、删资料库及影响派生结果的事实变化都能触发同一持久修复责任。

**Blocked by:** 13: 关系编辑与作品群持久修复。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 63、64、67、68、89。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 覆盖实际写入与级联删除来源，不只监听显式关系编辑的内存通知。
- [x] 事实变更与 dirty 依据保持原子保证，不在协议/页面复制触发规则。
- [x] 使用新增修复版本能力，避免旧尝试覆盖最新删除或更名结果。
- [x] 真实 SQLite 与实际删除/编辑入口验证跨库图、稳定身份和重启修复。

## Comments


### 实施与验证

- SQLite 在 `BEFORE DELETE ON series` 中于外键移走关系/成员之前确认作品群归属，同事务递增 13 的全局期望版本。生成 DeleteSeries、DeleteLibrary 外键级联、scanner 的 DeleteMissingSeries 均覆盖，不依赖 HTTP 或内存事件。
- `AFTER UPDATE OF name,title` 仅在事实值确实变化且系列参与关系/作品群时标记。人工保存、提案应用与扫描 upsert 共用此持久规则；无变化、无关联系列及摘要/状态/出版社等无关变更不产生额外更新责任。批量编辑来源按当前能力只写状态/出版社/标签，不改变作品群命名事实。作品群继续沿用目录 name 命名规则。
- 首次安装触发器同事务请求一次全局核对，避免既有 current 状态掩盖先前未标记的级联变化；后续迁移不反复置脏。成员 series_id 索引使资格判断不对全部成员扫描。
- 三个实际行为 red：删系列、人工改标题、安装覆盖后漏掉旧级联核对均已复现并修复；初次 Application Control 拦截不计为行为失败。原始输出保留 `.tmp/ticket14-red*.log`、`ticket14-green*.log`。
- 真实 SQLite 回归覆盖跨库删除、数据库关闭重开修复、稳定作品群身份、事实/标记失败同回滚、旧失败晚到、新改名在旧重建返回前提交、无关与未变事实、升级安装幂等。HTTP 回归覆盖完整人工编辑、提案应用及删库入口；scanner 回归覆盖实际摄入填标题与安全清理。
- 初轮 Go 1.26.10 的 database、organize、scanner、api、seriesediting 五包整包通过；organize/api race 通过，scanner race 一次被系统拦截后单包重跑通过；gofmt 输出为空，Go lint 为 `0 issues.`。新增升级核对回归随后通过，最新合并后的适用门禁另行记录。
- 只增数据库迁移/索引与回归，不修改前端。PowerShell `sqlc generate` 退出 0，生成产物无差异。状态暂留 ready-for-agent，待集成验证后关闭。


### 合并与最终适用门禁

- 已合入集成分支 `770c495`（含 03 和 09），本票新查询测试适配明确读者主体；09 的实际 Selection owner 已验证更新状态/出版社/标签不会误标作品群。
- 合并后的 Go 1.26.10 六包整包全部通过：database、organize、scanner、api、application/seriesediting、application/runtime。organize/scanner/api 的相关 race 全部通过；输出见 `.tmp/ticket14-packages-final.log` 与 `.tmp/ticket14-race-final.log`。
- 最终 PowerShell sqlc 退出 0、生成产物无漂移；gofmt 输出为空，Go lint 为 `0 issues.`。原始记录为 `.tmp/ticket14-sqlc-final.log`、`ticket14-format-final.log`、`ticket14-lint-final.log`。
