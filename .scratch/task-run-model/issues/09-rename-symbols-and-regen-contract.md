# 09 — 符号全量改名与契约重生

**What to build:** 纯机械。把代码符号对齐已经落地的词汇表，并重生前端契约。零行为变化。

**Blocked by:** 08

**Status:** done

## 改什么

| 今天 | 改成 |
| --- | --- |
| `TaskStatus` | `RunStatus` |
| `TaskSpec` | `RunSpec` |
| `taskrun` 包与 `Handle` | **运行句柄** |
| SSE 事件名 `task_progress` | `run_snapshot` |

**`TaskRuntime` 不改**：它指的是引擎内部登记的上下文 + **暂停闸门** + 取消函数，刻意从不交给
任务体，这个名字继续归它。

## 爆炸半径已量过，因此不走类型别名

- `TaskStatus` 非测试引用 114 处（含测试 172），**只在 `api` 一个包**，外加数据库 1 处与
  契约生成器 1 处
- `taskrun.` 127 处，同样**只在 `api`**——`koreader` 与 `external` 早已各自声明窄接口
- 前端 63 处，其中大部分在生成契约里

单包内的机械改动，编译器能全量兜住。**不要引入过渡期的类型别名**：本仓接口里那条
「i18n 码版／字面量版」的疤，就是一次停在半路的迁移冻结进签名的产物。

## 落地记录

**符号落点**：`taskrun` 包改名为 `internal/runhandle`，类型仍是 `Handle`，调用点因此读作
`runhandle.Handle`；`api.TaskStatus` → `api.RunStatus`（连同 `listTaskStatuses`、
`taskStatusFrom` 与前端那批 `isActiveTaskStatus` / `ACTIVE_TASK_STATUSES` / `utils/taskStatus.ts`）；
`api.TaskSpec` → `api.RunSpec`；SSE 前缀 `task_progress:` → `run_snapshot:`，前端 `Layout` 的
监听端同步改。`api.TaskRuntime` 与 `task.taskRuntime` 都未动。

**并进本票的挂账 D34**：身份表从 `task_identities` 改回 `tasks`。改名走 `taskstore` 自己的
`renameLegacyIdentityTable`（ALTER TABLE RENAME，SQLite 一并改写 `runs` 上那条外键），
排在建表语句之前；作用域索引随之改名，过渡期那条 `idx_task_identities_scope` 丢弃。
`internal/database` 丢弃旧表那一句因此改成**认形状不认名字**（旧表有 `key` 列、身份表没有）——
只认名字的话，改名落地后的每一次启动都会把身份表连同它的全部运行一起丢掉。

升级路径由 `TestMigrateRenamesIdentityTableToTasks` 守：两个起始 `user_version` 各一遍，
外加**旧任务表与身份表并存**那种真实形状一遍（新旧引擎并存期的库长这样），每种都连跑两次。

**没做的**：i18n 键 `logs.taskStatus.*`、前端内部的 `manga-manager:task-progress` 自定义事件、
任务体形参名 `tp`、api 侧其余 `Task*` 前缀的类型与函数名——都不属于票据点名的那四组符号，
分别记作 D35、D36、D37、D39；`api.RunStatus` 与 `task.RunStatus` 同名一事记作 D38，
跨这次改名回滚到票 08 会丢掉任务清单一事记作 D40。

## 验收

- [x] 四组符号全量改名，全仓无旧名残留
- [x] `TaskRuntime` 未被改名
- [x] 契约生成器的目标随之更新，生成产物已重生且与仓库一致（CI 检查漂移）
- [x] SSE 事件名两侧同步改，前端监听端已跟上
- [x] 无任何行为变化；全量用例仅改名不改断言即继续绿
      → **对改名那部分成立**：既有断言一行未改。**身份表改名那部分蓄意偏离**：它本来就是一段
        存量库迁移，因此 `migration_test.go` 的 `TestMigrateDropsLegacyTasksTable` 改了断言
        （表名腾给身份表之后，「旧表没了」只能认形状），并新增 `TestMigrateRenamesIdentityTableToTasks`；
        `taskstore_schema_test.go` 同理。这是用户明确并进本票的 D34，见「落地记录」。
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [x] `cd web && npm run test && npm run lint && npm run build` 全绿
- [x] `go vet ./...`、`golangci-lint run` 无 issue、`gofmt -l` 干净、`check-doc-style.sh` 通过
