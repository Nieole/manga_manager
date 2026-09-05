# 05 — 删掉旧接口，收口

**What to build:** 删除 `taskIOMetrics` 与它的三个方法；确认三个 `progress` 形参与
`AbsorbDiskWork` 字段已全部消失；`CHANGELOG.md` 记一条对外行为变更。

**这一票必须与票 02～04 落在同一个 PR 内。** 理由沿用 `diskwork` 那次：本仓接口里那条
「i18n 码版／字面量版」的疤，就是一次停在半路的迁移冻结进签名的产物。半途合入的代价不是难看，
是两套形状同时存在时「该用哪个」变成每个新取用点要判断的事，而判断错不会有编译错误。

**Blocked by:** 03, 04

**Status:** done

## 删除清单

| 要删的东西 | 在哪 | 由哪一票腾空 |
| --- | --- | --- |
| `taskIOMetrics` 类型 | `api` | 03 |
| 它的两个吸收方法与那份取帧指标的方法 | `api` | 03 |
| `RebuildOptions.AbsorbDiskWork` 字段与 `nil` 兜底 | `koreader` | 02 |
| 两处 `progress func(current, total int)` 形参 | `koreader` | 02 |
| 一处 `progress func(current, total int)` 形参 | `external` | 04 |
| 两处 `progress func(..., metrics taskIOMetrics)` 的第二形参 | `api` | 03 |

删完之后应当成立的三条，逐条核对：

1. 全仓**不再有**任何裸的 `progress func(current, total int)` 跨包形参。
2. `diskwork.Stats` 的取用点只剩两处：`diskwork` 自己，与 `taskrun` 的吸收；
   `scanner` 那份是本次范围外的合法取用，保留。
3. 手抄的**暂停闸门**调用只剩 `scanner` 的 8 处（`diskwork` 内部那一处是模块自己的，不算手抄）。
   本次从 21 处降到 8 处，**不归零**是设计如此，不是遗漏。

## CHANGELOG

记**一条**对外行为变更：

> KOReader 的**指纹**重建不再把被**暂停闸门**或**存储令牌**挡下的书计入「已哈希文件数」——
> 那些书一个字节都没读。此前该任务与文件身份重建对同一个指标给出不同答案，现在两处口径一致。

不记的：模块搬迁、类型改名、词汇表调整、用例下沉。它们对用户不可见。

## 收尾核对

- [x] 上表六项全部删净，全仓搜不到残留
- [x] 上述三条「删完之后应当成立的」逐条核对通过
- [x] `internal/taskrun` 的 package doc 边界描述与最终事实相符（谁用它、它不做什么）
- [x] `internal/api/doc.go`、`internal/koreader/doc.go`、`internal/external/doc.go` 里
      涉及进度上报与磁盘作业的描述已更新
- [x] `CONTEXT.md` 的**任务句柄**词条与最终接口相符；**进度句柄**已无残留引用
- [x] `CHANGELOG.md` 记了且只记了上述一条
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿（含 `-race`）
- [x] `go vet ./...`、`golangci-lint run` 无 issue、`gofmt -l` 干净、`check-doc-style.sh` 通过
- [x] `go run ./cmd/tsgen` 无漂移（本次不动任何契约结构体，应当天然无漂移）
