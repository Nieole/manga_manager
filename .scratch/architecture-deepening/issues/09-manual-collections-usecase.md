# 09: 人工合集的完整用例

**What to build:** 人工成员和名称、来源所有权集中到藏书编排，页面及现有协议访问相同合集身份。

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

**验收基准：** [父规格](../spec.md)。本票遵守其中的跨功能约束和范围，不将历史实现偏差重新定义成业务规则。

**规格覆盖：** US 2、US 3、US 5、US 6、US 12、US 53、US 58、US 60、US 65、US 68、US 70、US 108。

- [x] 以人工合集创建、读取、编辑、成员增删和删除建立藏书编排 module 的真实用例，页面和协议入口实际接入。
- [x] 名称检查、成员约束和来源所有权由完整用例持有，不让 HTTP 在调用前后另执行必须步骤，不公开任意策略组合。
- [x] 人工合集以及已落地的智能书架快照、AI 分组合集仍可人工维护；作品群来源继续拒绝人工编辑与成员修改。
- [x] 复用现有 SQLite、事务与数据身份，只接受本功能所需的数据能力；不反向依赖请求控制或应用总对象。
- [x] 现有 OPDS、Mihon 及站内链接继续访问相同合集身份，现有界面交互与授权保持兼容。
- [x] 保留尚未迁移来源的兼容接入，名称与所有权只建立一份权威规则；不要求先搬动其他编排功能才能交付本票。
- [ ] 通过完整用例和真实 SQLite 验证名称冲突、来源可编辑性、成员变更、事务失败、协议身份与普通用户授权；规则测试无需整个 Controller。
- [ ] 删除本票替代的人工规则副本，完成相关后端、前端及必要生成检查。


## 实施与验证

人工 CRUD 与成员变更已由 `internal/organize.Collections` 持有；来源所有权、名称与成员限制、事务及成员更新时间共同执行。快照和 AI 兼容入口共用名称检查。HTTP 仅解析、授权与响应映射；协议测试通过完整用例建立相同身份。

真实 SQLite 验证通过：名称清理及冲突、重命名保留身份、ASCII NOCASE 与 Unicode 边界、快照/AI 人工维护、作品群拒绝修改、重复成员计数、外键失败回滚、更新时间失败回滚及删除。补充真实路由普通用户只读回归，保留并加强 OPDS/Mihon 身份回归。

通过（Go 1.26.10）：`go test ./internal/organize ./internal/database`、`golangci-lint run ./internal/organize/...`（0 issues）、`gofmt -l ./cmd ./internal`（无输出）、`git diff --check`。

全量 `go test ./...` 及 API/协议/授权测试受 Windows Application Control 拦截 gcc，尚未完成行为验证。全量 lint 同因 cgo preprocessing failed 无法完成，不能把其尾部 0 issues 当成成功。日志：`C:/Users/saber/.codex/tmp/architecture-deepening/collections-go-test.log`。本票无 SQL、响应契约和前端实现变更，无生成变更；当前保留 ready-for-agent，待环境恢复完成余下门禁后再标记 done。
