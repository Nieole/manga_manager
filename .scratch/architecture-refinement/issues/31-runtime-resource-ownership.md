# 31: 资源热切换与关闭所有权

**What to build:** 内容及进程资源在明确所有者下安全替换，旧使用者正常归还，配置热更新不打断阅读。

**Blocked by:** 16: 页读取的共享内容能力；30: 配置发布与生效范围。

**Status:** ready-for-agent

**规格覆盖：** [父规格](../spec.md) US 140、141、144、145、153、154。全部验收同时遵守父规格的兼容、测试及范围外约束，不重新定义已批准业务规则。

**切片类型：** 纵向切片。

- [x] 消费内容借用 seam 与配置生效分类，明确池、处理器、预算和日志的创建/发布/关闭。
- [x] 资源切换不强杀旧借用，正常归还后释放；保留合理进程单实例。
- [x] 当前应用运行时实际使用资源所有者，停止输入与使用者先于最终资源关闭。
- [x] 真实借用、热重载、故障及停机回归验证，不以改字段位置作为完成。

## Comments

### Implementation handoff — 2026-10-30

- `runtimecfg.Resources` is owned by the application runtime. Startup, settings/file publication and relocation compensation use its serialized apply capability; final close rejects retained late callbacks. Config.Manager remains the only configuration source. The executable retains the process log file across application construction and shutdown; Resources owns live log-level publication, archive-pool retirement and image-budget retirement.
- Failed log-level validation previously changed the archive pool and image limits before returning an error. A public regression captured pool/AI limits changing from 3/2 to 9/7 on rejection; validation now precedes every resource mutation. Resource initialization failure prevents watcher/background inputs opening. A stopped runtime previously accepted a settings publication and reopened its pool; the complete configuration consumer now rejects it without publishing settings.
- Existing archive reference counting and image permit-generation returns are retained. A real ZIP borrow and `bookcontent.Service.Page` survive a budget/pool update; retirement leaves the borrowed archive usable and the last return permits actual file deletion. Public image processing with a controlled codec proves active encoding can return its original permit after publication and retirement. Another real encoding barrier first reproduced AI-limit updates creating a second occupied software budget; only changed budgets are now replaced.
- Runtime shutdown signals input stop, seals engine admission/cancels active work, joins the watcher, joins admitted background work, closes scanner workers, then retires resources. `Engine.Stopping` describes the admission/cancellation boundary, separately from body exit. The handoff regression now waits for that boundary instead of assuming Close ran after 20ms; ten repeats passed. A real admitted archive reader proves cancellation does not retire its pool until the body has returned. This ordering also allows watcher-owned joins from ticket28 to observe already-cancelled active work; integrated watcher join verification remains required after that ticket merges.
- Actual gates: runtimecfg/runtime full ordinary 0.071s/2.465s; complete race 1.083s/8.158s; final generation regression race images/runtimecfg/runtime 23.890s/1.108s/11.552s; final software-budget race 1.194s. Images/parser/bookcontent/task full race 18.059s/1.255s/1.531s/1.904s; resource HTTP consumers 6.044s; complete API 104.233s. Admitted-reader/shutdown boundary race repeated three times passed 3.686s. `go build ./...`, Go lint (0 issues), gofmt and baseline whitespace/marker checks passed. Raw red/green and gate logs are in this worktree `.tmp/ticket31-*.log`.
- Frontend, schema and generated transport contracts are unchanged. No external AI executable, device, remote multi-platform run or independent performance benchmark is claimed. Status remains ready-for-agent pending integration verification and closure.
