# 09 — 符号全量改名与契约重生

**What to build:** 纯机械。把代码符号对齐已经落地的词汇表，并重生前端契约。零行为变化。

**Blocked by:** 08

**Status:** ready-for-agent

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

## 验收

- [ ] 四组符号全量改名，全仓无旧名残留
- [ ] `TaskRuntime` 未被改名
- [ ] 契约生成器的目标随之更新，生成产物已重生且与仓库一致（CI 检查漂移）
- [ ] SSE 事件名两侧同步改，前端监听端已跟上
- [ ] 无任何行为变化；全量用例仅改名不改断言即继续绿
- [ ] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [ ] `cd web && npm run test && npm run lint && npm run build` 全绿
- [ ] `go vet ./...`、`golangci-lint run` 无 issue、`gofmt -l` 干净、`check-doc-style.sh` 通过
