# 01 — 任务面板只报真正管住这个任务的那一项

**What to build:** 任务面板上那块「有效 worker 数 / 归档打开并发 / 存储档位 / 卷」的徽章，今天有
五个任务在报一个与自己无关的数——它们拿**扫描档位**和**全局默认存储策略**算出「假如现在扫一次库
会起几个 worker」，而它们根本不是扫描。这五处一并撤掉；剩下两个真扫描调用点改调扫描器**导出的
那一份**公式，api 自己重抄的那份连同它那个死掉的 `force` 形参一起消失。

**用户侧可观察的变化**：文件身份重建、低优先级哈希回填、KOReader 书籍指纹重建、KOReader 匹配
刷新、缩略图重建这五个任务的详情里不再出现那块并发徽章。资料库扫描与重扫两处逐字不变。

**不动 `TaskLimits` 结构体**——没有字段增删，因此 `cmd/tsgen` 不必重跑，前端一行不改
（`TaskLimitBadges` 本就是 `{limit && ...}`，`hasInlineTelemetry` 本就把 `effective_limit`
的有无算进去）。引擎侧也已就绪：`TaskSpec.Limits` 的零值语义就是「这个任务没有上限可报」，
`Run` 里那句 `if spec.Limits != (TaskLimits{})` 是它的全部实现。

**Blocked by:** 无

**Status:** done

## 为什么是这五个

逐个核过七个调用点，判据是「有没有一个并发上限真的管得住它」：

| 调用点 | 它是什么 | 结论 |
| --- | --- | --- |
| 资料库扫描与重扫（`controller_library.go` 两处） | 扫描，传真实库路径 | **保留**，本来就是给它设计的 |
| `launchRebuildFileIdentitiesTask` | 单线程顺序循环逐本算指纹 | 撤 |
| `launchLowPriorityBookHashBackfillTask` | 同上 | 撤 |
| `launchRebuildBookHashesTask` | 同上（它的读盘今天还不取令牌，见票 `.scratch/diskwork/issues/07`——那条与本票正交，修完也不改变本票的判断：顺序循环仍然不受任何并发上限约束） | 撤 |
| `launchRefreshKOReaderMatchingTask` | 同上（它的第一阶段就是那次重建） | 撤 |
| `launchRebuildThumbnailsTask` | 清缓存后**顺序**扫每个库 | 撤 |

前四个是 `for _, book := range books { ... }` 的顺序循环，一次只有一本书在读盘——任何 ≥1 的
并发上限都约束不到它们，报出来的数字没有对应的实物。

缩略图重建是唯一需要说明的一个：它确实驱动扫描，但 `runGlobalScan` **顺序**扫每个库，每个库用
**自己**的存储策略与 worker 数，而它启动时钉死的那份数字取自 `path=""` 即全局默认策略。只有在
没有任何按库策略条目时它才碰巧正确，而「碰巧正确」正是本票要消的东西。引擎在 `TaskSpec` 时刻
快照 `Limits`，没有运行期更新通路，因此「逐库更新」不在本票范围内——那要给引擎加一个只有一个
用户的能力。

## 顺带消掉的两处

1. **`force` 形参整个是死的。** `taskLimitsForPath(path string, force bool)` 里先
   `if profile == ScanProfileRepair { force = true }`，紧接着 `_ = force`。它是从
   `scanner.scanOptions` 抄过来的，但那边 `force` 只影响 `ScanOptions.Force`，而 worker 数只看
   `opts.Profile`——所以这个形参对结果从来没有影响。删形参时把那句 `if` 一并删掉。
2. **两份公式合成一份。** `api.taskLimitsForPath` 与 `scanner.scanWorkerCount` 是同一段公式的
   两份实现，逐条对照：

   | | `scanner.scanWorkerCount` | `api.taskLimitsForPath` |
   | --- | --- | --- |
   | 起点 | `policy.IOPolicy.ScanConcurrency` | 同 |
   | 收窄一 | `opts.Profile.opensArchive()` → 与归档打开并发取正最小 | `profile != ScanProfileFast` → 同 |
   | 收窄二 | `computesQuickHash() \|\| computesFullHash(cfg)` → 与哈希并发取正最小 | `profile == Identity \|\| profile == Repair` → 同 |
   | 收尾 | 与 `cfg.Scanner.Workers` 取小，下限 1 | 同 |

   **两者当前逐条等价，但那是巧合。** `computesFullHash` 忽略它的 `cfg` 形参（`golangci-lint`
   的 `unusedparams` 一直在点它），于是那个条件恰好塌成 `Identity || Repair`。没有任何东西守着
   这条等价：`computesFullHash` 哪天真的看一眼 `cfg`（它的形参摆在那里就是为了这个），扫描器起
   的 worker 数变了，面板照旧报旧数，两处都不报错、没有用例会红。

   建议的导出形状（`scanWorkerCount` 今天是 `*Scanner` 的方法却从不用接收者，提成自由函数即可）：

   ```go
   // scanner
   func WorkerCount(cfg config.Config, rootPath string, opts ScanOptions) int
   ```

   api 侧随之只剩两步，档位仍走它已经在用的 `scanner.NormalizeScanProfile`：

   ```go
   profile := scanner.NormalizeScanProfile(cfg.Scanner.ScanProfile)
   effective := scanner.WorkerCount(cfg, path, scanner.ScanOptions{Profile: profile})
   ```

## 会变红的既有用例（这是好事，逐条确认而不是逐条改绿）

- `api.TestKOReaderTasksCarryMatchConfigMetadata` 的 `wantLimit` 列今天是
  `指纹重建 true / 进度对账 false / 匹配刷新 true`。三者本票之后全为 `false`，这一列随之整个
  塌成常量——**删掉这一列**，改为三条统一断言「KOReader 任务不报并发上限」。
- `api` 里守文件身份重建的那条用例有一句
  `if first.EffectiveLimit == nil { t.Fatal("首帧没带上并发上限 —— 任务面板上会缺一块") }`。
  它的断言与失败文案**双双反转**：缺的那一块正是本票要撤的。
- `api.TestScanTaskEffectiveLimitsUseExternalHDDPolicy` 与那条直接断言 `taskLimitsForPath`
  的用例都**保持绿**，它们守的是两个真扫描调用点。
- `api.task_run_test.go` 里那两条（任务声明带上限 / 不带上限）是引擎层的，与本票无关，保持绿。

## 验收

- [x] 五个任务的 `TaskSpec` 不再带 `Limits`；资料库扫描与重扫两处逐字不变
- [x] `taskLimitsForPath` 的 `force` 形参与那句 `if profile == ScanProfileRepair { force = true }`
      一并消失
- [x] 全仓只剩一份「起点 → 两次收窄 → 与配置 worker 数取小」的公式，api 调用它而不是重抄
- [x] `TaskLimits` 结构体无字段增删；`go run ./cmd/tsgen` 无漂移；`web/` 一行不改
- [x] 上述四组用例按「会变红的既有用例」一节各自处置；新增/改写的用例**不得只压外置硬盘档**
      ——那一档四项并发全为一，两份公式在它上面恒等，是唯一测不出分歧的一档
- [x] `CHANGELOG.md` 记一条对外行为变更：五个任务不再显示并发徽章，因为那个数从来不管它们
- [x] `GOCACHE="$(pwd)/.gocache" GOTMPDIR="$(pwd)/.tmp" go test ./...` 全绿
- [x] `go vet ./...`、`golangci-lint run` 无 issue、`gofmt -l` 干净、`check-doc-style.sh` 通过

## 出处

从 `.scratch/diskwork/` 票 06（删除旧接口、收口）的收尾核对中查出。那条 spec 从未认领这段公式
（它只收后台的八处**磁盘作业**，而这里算的是 worker 数），因此单独立票。票 06 的 comment 1 记了
同一件事的简版。

一条相关但正交的事实：`scan_concurrency` 现在只剩「扫描起几个 worker」这一个用途了——磁盘作业的
上限已由 `diskwork.concurrencyLimit` 按工种裁定，那张表只认归档打开／哈希／封面三项。
