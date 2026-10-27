# 16: 页读取的共享内容能力

**What to build:** 页清单与实际页读取集中来源、加工、缓存和归档借用，阅读器及封面等真实消费者接入。

**Blocked by:** None (can start immediately)。

**Status:** done

**规格覆盖：** [父规格](../spec.md) US 90、91、92、94、95、96、97、102。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 完整内容 interface 隐藏来源定位、池借还与加工缓存资格，HTTP 只映射请求响应。
- [x] 共享借用 seam 能识别旧消费者的活跃借用，为后续发布协调提供真实依据。
- [x] 透传、实际加工、尺寸/解压/内存保护和来源变更缓存键保持。
- [x] 真实归档、HTTP 与读取消费者验证；独立字节结果及时归还借用。

## Comments

### 实施证据

- `bookcontent.Service` 统一来源与页目录缓存、完整页查询、单飞加工、存储配额和派生图缓存资格；`Source`、`Manifest`、`PageOptions`、`PageResult` 不携带 HTTP 请求或状态码。HTTP 页图、页清单和 Mihon 实际调用新入口，只处理输入、响应与现有故障/观测映射。
- `bookcontent.ArchiveReader` 统一页目录、指定页和已选条目读取。scanner 自动缩略图与指定页封面消费同一能力，保留现有故障注入、封面运行和磁盘作业边界；物化字节返回前归还归档，图像加工在归还后进行。
- `parser.ActiveArchiveBorrows` 覆盖 `OpenArchive`、格式专用直开和 `GetArchiveFromPool`。池内底层打开不重复计数；淘汰、旧池关闭/替换不隐藏仍活跃的借用，闲置缓存句柄不算借用，归还幂等，Windows 大小写路径别名共用观察。
- 资料库访问继续复用现有 engine 读准入与版本，来源缓存发布规则集中在 `Service.AcquireRead`，不增加任务准入锁。本票提供活跃观察，不实现后续发布阻新借用/排空流程。
- 原图透传、实际加工、AI 缺席/加工失败回退及缓存资格保持；使用原有图片尺寸、解码/解压和字节预算保护。变体身份包含来源路径/mtime/size、尺寸档位和全部加工参数，亦区分 AI 引擎路径。旧 HTTP 规则副本已移除。
- TDD：先确认缺失借用观察和完整内容接口的测试编译失败，再实现通过。真实 ZIP/RAR 验证旧借用、淘汰/池替换、独立字节与文件重命名；真实归档验证派生缓存、来源/选项变化和不缓存透传/回退。
- Go 1.26.10 本轮实际通过：`go test ./internal/api ./internal/scanner ./internal/images -count=1`（106.810s、20.762s、6.492s）；完整 `bookcontent` 与 `parser` 包；最终 `go test -race ./internal/parser ./internal/bookcontent -count=1`（4.535s、6.214s）；最终 HTTP/封面消费者定向回归（6.787s、5.578s）。
- 本轮实际通过：阅读器数据、页图缓存、图像尺寸及选项切换消费者 4 文件/32 测试；`gofmt -l ./cmd ./internal` 无输出，`golangci-lint run --allow-parallel-runners` 返回 `0 issues.`。前端源码未改变。本轮未运行真实外部 AI 引擎端到端，验证范围为现有软件加工与回退语义。
- 原始输出保存在本工作树 `.tmp/ticket16-*.log`；集成负责人清理工作树前应保留所需输出。本票已通过集成分支实际联合验证并关闭。
- 合并最新集成 `c2ca015` 后实际通过：`-race` 完整 `bookcontent`/`parser`（2.319s/1.652s）；HTTP、阅读器图像、Mihon/OPDS 与封面消费者定向回归（API 11.024s、scanner 4.837s）。两条原始日志与退出码均为成功；本轮未重跑无变化的全仓测试。
### 集成验收

- 05 由 `488e6ec`、16 由 `6b5e8f7` 合入 `codex/architecture-refinement`，两次合并均无冲突。
- Go 1.26.10：完整 `bookcontent`/`parser` 的 `-race` 测试实际通过（5.132s/5.687s）；reading/database 的原子写入、位置、时长、标记及同步回归通过；API 页图、来源缓存、封面、进度/离线与迁移兼容回归通过（48.889s），scanner 封面消费者通过（4.641s）。
- 迁移/旧阅读态 fixture 的真实消费另行执行：database、KOReader、scanner、application/relocating、application/scanning 五包的对应回归实际通过；未将初轮筛选中无匹配测试的包计作行为通过。
- 前端阅读器、进度/离线、时长、页缓存及图像选项消费者 7 文件、73 测试通过；gofmt 无输出，独立缓存 lint 为 `0 issues.`。未更改 SQL/TS 生成源，未重复无关生成或全仓门禁。原始输出保存在主工作树 `.tmp/merge-wave5-*.log`。
