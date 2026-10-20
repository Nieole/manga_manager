# 03 验证记录

## 审查修复

- 策略明细增加 `PolicyBefore` 与 `PolicyAfter`，仅包含存储档位和完整目录 IO 策略。迁移映射展示原显式策略与映射后的策略；目标冲突展示既有目标值与拟迁入值；其他资料库影响展示实际生效的前后值。
- 检查运行写终态时，迁移领域维护的 SQLite 触发器在同一条运行更新事务中保存计划终态。关联严格限定资料库作用域的 `relocation_check` 及服务端 `plan_id` 标签，不覆盖已检查等既有终态。
- 运行标签在准入互斥边界内先于运行发布与执行保存，触发器可关联尚未由 HTTP 返回路径绑定的计划。即使终态随后被清历史删除，计划事实仍保留；无法绑定且对应运行已不存在时明确返回持久化失败。
- 取消回归先取消排队检查、立即经清历史 HTTP 接口删除运行，再重启并首次查询计划。另一条回归把取消、运行退出与清历史全部放在 `SetRun` 之前。两条都不以先查询计划作为持久化前提。

## 后端验证

用户允许在保留智能应用控制的条件下重试宿主测试；以下命令使用 Go 1.26.5、共享构建缓存和仓库临时目录执行。

- `go test ./internal/api -run Relocation -count=1` 通过，包含真实 Controller、SQLite、临时文件和运行历史清理回归。
- `go run ./cmd/tsgen` 成功生成 20 个类型，`web/src/api/generated.ts` 无漂移。
- `go test ./...` 全部通过。
- `gofmt -l ./cmd ./internal` 与 `git diff --check` 输出为空。

## 前端验证

完整策略对比交互先在缺少前后策略卡片的界面上失败，再在实现后通过。对同为 `custom` 的策略，断言四项不同并发值与三个不同开关均可见，避免仅显示档位掩盖真实差异。

合并后的前端门禁均通过：`npm run lint` 无警告，`npm run test` 为 78 个文件、832 项测试通过，`npm run build` 成功。`gofmt -l ./cmd ./internal` 与 `git diff --check` 输出为空。


## 并发检查与验收

- `go test -race ./internal/api -run Relocation -count=1` 通过，覆盖本票新增的迁移预览、取消、清理历史、访问协调与旧路径失效场景。
- Go 1.26.5 下完整 `golangci-lint run` 退出码 0，输出 `0 issues.`。
- 完整 `go test -race ./...` 属于扩展验证；执行时 `cmd/server` 的测试程序被 Windows 智能应用控制阻止，不能宣称完整 race 通过。其他包的最终结果另行记录；此阻断不覆盖已经通过的普通全量及本票定向 race。
- 最新真实 UI 策略实拍见 `screenshots/preview-policy.png`；检查与结果展示保持只读，未提供迁移提交入口。

验收证据对应 `internal/api/relocation_preview_test.go`、`internal/api/library_access_test.go`、`web/src/components/layout/RelocationModal.test.tsx` 与 `RelocationEntry.test.tsx`。服务端领域计划和分页明细持久化，不依赖运行历史；前后端权限、路径边界、跨类型占用、文件分类、完整 IO 策略、取消与关闭重开均有行为验证。
