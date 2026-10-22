# 最终审查修复验收

审查固定集成提交为 `3453c5f9a91ab4abadc2fa3d8a0bd88edce5388e`；全部 Standards 与 Spec 意见由同一实现分支处理，产品提交为 `7ca17536f7f6975cd152ae562b26dd394b18dea8` 与 `6516c4e649570d0122b487bd12e0c4806a306a09`、`35fb65b565194c97f0ace16579826d124e133429`；最终锁纪律补充为 `8b8a62e7d42c5765c2d33b3529c2dba6c1d56d0d`。两轴原报告及处置见 [final-review.md](final-review.md)。本次没有改动 SQL 或生成响应契约，不需重新运行 sqlc 或 tsgen；其整批证据见前序各票验收。

## 行为与回归

- 真 Controller HTTP 检查和提交、SQLite、临时配置、原生目录及 fsnotify 回归 `TestRelocationKeepsIndependentNestedLibraryWatching`：父库 none/watch 与独立库位于 source/target 的组合继续接收新文件事件；迁移库不再收到原根事件。Windows DB 原根使用正斜杠及不同大小写，管理员明确输入等价原生路径。修复前 source/none 在 10 秒内无独立库事件；第一次共享监听修复后，Windows 别名变体再现旧 owner 仍收到旧根事件；按原生等价根注销后，两种失败均消失。保留共享物理监听，不存在先拆再注册的空窗。
- 真实 LocaleProvider、现有公共 HTTP 请求替身与 DOM 用户界面：已完成且 protection 已删除的恢复结果重新打开后显示 interval/29/cbz；pending、paths_committed 同样展示持久选择，控件锁定且无隐式修改。空格式选择经过 Go `omitempty` 后仍显示为空，不回退到历史 protection 的 cbz。新结果仅用于展示，不覆盖新检查的可编辑选择。先观察错误默认值 RED，再通过修复 GREEN。
- 原有两个 Modal 的交互与异步测试覆盖公共轮询 hook 的完成、恢复、目的校验及关闭清理；恢复 release 请求保留最新完成回调的既有语义。

## 门禁

Go 验证使用进程级 Go 1.26.5、GOTOOLCHAIN=local、CGO/gcc 15.2；共享主工作区 `.gocache`，临时目录及 lint 缓存独立。未改变全局 Go 环境。

| 命令 | 实际结果 |
| --- | --- |
| `go test ./...` | 最后整批命令退出 1，仅 `internal/koreader` 测试可执行文件被 Smart App Control 阻止；其余包通过，包括 API 122.849s、scanner 19.165s。 |
| `go test ./internal/koreader` | 正常单包重试退出 0，5.082s。整批各包均获得通过证据，不宣称最后整条全包命令退出 0。 |
| `go test -race ./internal/api -run 'Relocation\|Restoration' -count=1` | 退出 0，187.719s，含新增真实监听回归。终版锁纪律补充后，`TestRelocationKeepsIndependentNestedLibraryWatching` 的 race 再验退出 0，41.306s。 |
| `go test -race ./internal/scanner -run 'Watch\|Cleanup' -count=1` | 终版锁外注销纪律补充后退出 0，21.098s。 |
| `gofmt -l ./cmd ./internal` | 输出为空。 |
| `golangci-lint run --allow-parallel-runners` | 退出 0，`0 issues.`。 |
| `npm run test -- --run` | `6516c4e` 退出 0，81 个测试文件、866 项通过；最后 `35fb65b` 仅轮询作用域依赖增量，两个 Modal 与真实语言回归再验 3 文件、31 项退出 0。 |
| `npm run lint` | 退出 0。 |
| `npm run build` | 本树自行构建退出 0，包含 TypeScript 检查与 Vite 产物。 |
| `scripts/check-doc-style.sh` | 通过；最终补充文档与 UI 回归后再次核验。 |

新增恢复 UI 修复不改变 Go 源代码，已通过项目未因纯 UI 增量重复执行。最终自查保留 fsnotify 原有锁外物理注销纪律，避免 Windows reader 与事件消费者因持有 fw.mu 互等；共享 watch 从不进入注销列表。此增量后补跑新增 Controller/fsnotify 回归的 race、scanner Watch/Cleanup race、完整 lint 与 gofmt，未重复无关包。早期整批运行另出现过 external 的 SAC 启动阻断及 scanner 临时目录收尾非空失败；最后整批 external 与 scanner 均通过，未为此改动无关生产代码。更早各票的执行及重试边界仍以对应验收记录为准。

原始执行日志位于实现工作树 `.tmp/final-*.log`，并复制到工作树外 `C:/Users/saber/dev/relocation-notes/final-gates`；不提交运行缓存或构建产物。真实 UI 截图来源及演示数据说明见 [screenshots/README.md](screenshots/README.md)。

## 环境与收尾边界

Smart App Control 保持开启；仅按用户授权正常重试被阻止的 Go 程序，未添加排除、降低安全设置或重启系统。Windows 沙盒可选组件此前已启用但仍待用户方便时手动重启，本次验收不依赖沙盒。

实现工作树清理尚未完成：自动审批先拒绝批量 worktree/junction 清理，再拒绝已核对单一目录的无递归 junction 移除，仅返回 `blocked by policy`，未给出更细原因。已停止删除并保留工作树、分支及证据，不尝试用其他代码绕过；主工作区的 node_modules 保留。
