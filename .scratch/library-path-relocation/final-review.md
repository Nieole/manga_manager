# 最终代码审查与处置

审查固定范围：`9bb77a3f1ed789b83a331b7d1643302cbadcad63...3453c5f9a91ab4abadc2fa3d8a0bd88edce5388e`。两个审查轴独立报告；同一实现者在 `codex/relocation-final-fixes` 处理全部意见，产品提交为 `7ca17536f7f6975cd152ae562b26dd394b18dea8`，回调语义保真补充为 `6516c4e649570d0122b487bd12e0c4806a306a09`、`35fb65b565194c97f0ace16579826d124e133429`；最终锁纪律补充为 `8b8a62e7d42c5765c2d33b3529c2dba6c1d56d0d`。以下保留固定提交的审查结果，位置仅指审查快照。

## Standards
Reviewed `git diff 9bb77a3f1ed789b83a331b7d1643302cbadcad63...HEAD` at pinned HEAD `3453c5f9a91ab4abadc2fa3d8a0bd88edce5388e`, the supplied commit/file lists, and applicable repository standards. Tooling-enforced findings are excluded.

## Documented-standard violations

- **[P3] Restore the displaced cancellation contract comment** — `internal/task/control.go:319–342`. The inserted `disableCancellation` now sits immediately below `// cancelLocked 是取消的两条分支。调用方持锁；返回的 launch 必须在锁外调用。`. That contract describes `cancelLocked`, whereas `disableCancellation` acquires its own mutex and returns an error. Move this comment back onto `cancelLocked`; document the new method separately if needed. Rule: `docs/agents/doc-style.md`, symbol documentation must sit next to its declaration and describe concurrency requirements/error semantics; `AGENTS.md` requires each documentation layer to answer its own question.

## Judgment calls: smell baseline

- **[P3] Possible Duplicated Code: path intersection** — `internal/api/relocation_access.go:46–50, 88–91, 146–150` repeat the same bidirectional containment predicate: `_, inside := relocation.MapPath(lib.Path, lib.Path, candidate)`, `_, contains := relocation.MapPath(candidate, candidate, lib.Path)`, `if inside == nil || contains == nil`. Preview conflicts, root-edit admission and commit access must share this interpretation. Extract a named intersection helper and use it at all three sites, so a path-boundary fix cannot leave one admission path behind.

- **[P3] Possible Duplicated Code: durable operation polling** — `web/src/components/layout/RelocationModal.tsx:139–162` and `RestorationModal.tsx:93–112` each implement `const operationID = operation?.id || plan?.operation_id`, an active/timeout lifecycle, the same operation fetch, the same three nonterminal statuses and completion callback. Extract the common polling lifecycle into a hook, leaving purpose validation and optional live-progress fetching explicit at the callers. This would keep future retry/cleanup changes consistent without merging their different confirmation workflows.

**Count:** 1 documented-standard violation; 2 nonblocking heuristic findings. Worst documented issue: the misplaced concurrency contract. No repository changes or test runs were made.

### Standards 处置

| 原报告类别 | 结论 | 处置与依据 |
| --- | --- | --- |
| P3 documented-standard violation | 已修复 | `cancelLocked` 的持锁和锁外启动契约紧贴自身声明；`disableCancellation` 独立说明与取消请求共锁、已取消时的错误。 |
| P3 heuristic: path intersection duplication | 已处理 | `relocationPathsIntersect` 统一预览、根编辑准入及提交访问范围的双向边界判定。沿用真实 Controller 的路径、冲突、并发及恢复回归。 |
| P3 heuristic: operation polling duplication | 已处理 | `useRelocationOperation` 统一持久操作轮询、清理、非终态及完成通知。调用方保留 restoration 目的与资料库校验；relocation 显式选择加载 live progress，实时进度失败不掩盖持久结果。沿用两套 Modal 的公开请求与用户交互测试。 |

本轴：1 条文档规范问题、2 条启发式建议，全部处理；未把启发式建议升级为规范违规。

## Spec
Reviewed pinned `3453c5f9a91ab4abadc2fa3d8a0bd88edce5388e` against `9bb77a3f1ed789b83a331b7d1643302cbadcad63`, using `.scratch/library-path-relocation/spec.md`, ADR 0009, supplied commit/file lists, validation-03 through validation-08 and validation-combined. Runtime implementation inspected; no tests rerun or repository files modified.

- **[P2] Preserve independent nested libraries' watches.** `internal/api/relocation_controller.go:118-120` unconditionally calls `UnwatchLibrary(op.SourceRoot)`. That existing watcher method removes every registered descendant watch, including those serving independent nested libraries. The preview permits unchanged nested libraries (`TestRelocationPreviewAllowsUnchangedNestedLibrariesAndSelfMapping`). Relocating A from `source` to `target` while B remains at `source/unrelated-nested` therefore removes B's watches, even when A uses mode `none` and only B uses `watch`. Only A's target is registered afterward; B silently stops receiving filesystem events until another registration/restart. Decision 13 requires **“其他不冲突资料库继续运行。”** Decision 5 also says **“已有嵌套资料库不能一律视为非法，必须判断具体映射与策略影响。”** Preserve shared watches or restore the other libraries' registrations inside the effects/recovery boundary. Static finding; existing nested tests use mode `none`.

- **[P2] Show persisted selected settings when reopening a completed restoration.** `web/src/components/layout/RestorationModal.tsx:99-107` loads the durable operation but only calls `setOperation(data)`. Mode/interval/formats are initialized to `none`/60/empty and populated solely from `data.protection` (`:19-21,57-58`). Completed restoration deletes protection, so a fresh modal shows completion beside disabled “selected settings” that do not describe the operation: selecting `interval`/29/`cbz` later appears as `none`/60/empty. The operation already returns persisted `scan_mode`, `scan_interval`, `scan_formats` (`internal/relocation/commit.go:20-22,103`), but the UI never restores them. Story 43 requires **“关闭弹窗后仍可查看检查与迁移结果”** and story 49 requires **“迁移结果列出数量、异常、保护状态及后续操作”**. Hydrate completed-operation settings or display them directly from its immutable result. Static confirmation; root's separate real UI observation showed a completed reopened `none`/60/`cbz` restoration with empty formats. The `interval`/29 example is inferred, not browser-verified.

No scope creep established. Identity preservation, durable protection, explicit release and recovery have substantial recorded coverage. Raw diagnostic task errors are administrator-only through both HTTP and SSE, so they do not establish decision 21's forbidden nonadministrator path channel.

### Spec 处置

| 原报告类别 | 结论 | 处置与依据 |
| --- | --- | --- |
| P2 independent nested watches | 已修复 | `FileWatcher.UnwatchLibrary` 仅释放当前库不再共享的目录监听，保留其他已注册根覆盖的 watch，不先拆再补。Windows 原根表示不同的删除按原生路径等价处理。真实 Controller 提交、真实 SQLite 与 fsnotify 的 `TestRelocationKeepsIndependentNestedLibraryWatching` 覆盖 source/target 嵌套位置、父库 none/watch、显式原根和 DB 路径分隔符/大小写差异；观察独立库继续接收新文件事件、迁移库不再接收旧根事件。先 RED 再 GREEN，迁移相关 API 与 watcher race 均通过。 |
| P2 completed restoration settings | 已修复 | 已消费操作的所选设置直接取不可变 `RelocationOperation`，包括 Go `omitempty` 表示的空格式选择；不回写新检查的编辑状态。真实 LocaleProvider 和公开 API 请求替身验证 completed、paths_committed、pending 的 interval/29/cbz，以及 pending 的空格式，控件锁定且无隐式修改请求。先 RED 再 GREEN。 |

本轴：2 条 P2，全部修复。管理员任务诊断路径的怀疑经权限边界核对排除，未引入无依据的全局任务错误改写。

门禁与执行边界见 [validation-final.md](validation-final.md)。真实页面证据由集成工作区补充至截图说明；既有检查、迁移及保护恢复行为的证据仍保留。