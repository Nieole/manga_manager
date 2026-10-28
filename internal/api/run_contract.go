// HTTP and SSE task observation contracts are independent of process assembly.

package api

import "time"

type RunSnapshot struct {
	// RunID 是这一条**运行**的标识：同一个任务在列表里可以出现多条，各是一次运行。
	// 暂停 / 恢复 / 取消按它寻址，重试从此不再抹掉上一次，因此它是必需的。
	RunID int64 `json:"run_id"`
	// TaskID 是这次运行属于哪个**任务**：任务中心的两层要靠它对上——一帧推过来的运行属于清单里
	// 哪一行、展开的历次运行里该不该多出这一条，都只有这个字段答得出。重试与禁用按它寻址。
	//
	// 契约上没有**任务键**（ADR 0007）：它认得出「哪件事」、认不出「哪一次」，而队列一开，
	// 同一个键此刻可以有两条仍会变化的运行。键留在日志行上，来源是启动点写下的 RunSpec.Key。
	TaskID  int64  `json:"task_id"`
	Type    string `json:"type"`
	Scope   string `json:"scope"`
	ScopeID *int64 `json:"scope_id,omitempty"`
	// Variant 是身份四要素的第四项（见 TaskIdentity），引擎用它把**重启函数**按（类型，变体）分发。
	// 它与另外三项一样进 JSON：四要素分两处走的话，任何按快照判身份的地方都要再从别处补一项。
	// 四项一起是身份行上那条唯一约束的四列。
	Variant   TaskVariant `json:"variant,omitempty"`
	ScopeName string      `json:"scope_name,omitempty"`
	// Trigger 是**发起方**：这次运行是被什么叫来的（见 task.Trigger）。它不改变运行怎么跑，
	// 只回答「这活是谁叫来的」——半夜转盘的那条究竟是定时守护还是用户自己点的。
	Trigger string `json:"trigger,omitempty"`
	Status  string `json:"status"`
	Message string `json:"message"`
	// MessageCode/MessageParams 承载可本地化的任务消息：后端只发稳定 i18n 键 + 占位参数，由前端按当前
	// 语言渲染，Go 里因此不出现面向用户的文案字面量。
	//
	// Message **没有任何来源**，恒为空：文案只有 i18n 码一种，连**中断**那句也是码。它留在契约里
	// 只为前端那条「码缺失时的兜底」还在——一句写死在 Go 里的文案没有任何地方能翻译它。
	MessageCode   string            `json:"message_code,omitempty"`
	MessageParams map[string]string `json:"message_params,omitempty"`
	Error         string            `json:"error,omitempty"`
	Current       int               `json:"current"`
	Total         int               `json:"total"`
	Percent       *float64          `json:"percent,omitempty"`
	// RatePerMinute 是这次运行**至今的平均速率**：已处理的条数折成每分钟多少条。
	//
	// **分母是这次运行真正在干活的那一段**——从开跑量到此刻（终态量到结束时刻），
	// 并把**暂停**逐段扣掉（见 enrichRunProgress）。它答的是「这次运行整体跑多快」，
	// 因此卡住时只缓慢下滑；「它此刻还在不在产出」由曲线上那个 RunSample.ThroughputPerMinute
	// 答，分母是墙上时间。两个数都对，但不是同一个数。
	RatePerMinute float64    `json:"rate_per_minute,omitempty"`
	EtaSeconds    *int64     `json:"eta_seconds,omitempty"`
	CanCancel     bool       `json:"can_cancel"`
	CanPause      bool       `json:"can_pause"`
	CanResume     bool       `json:"can_resume"`
	Retryable     bool       `json:"retryable"`
	PausedAt      *time.Time `json:"paused_at,omitempty"`
	// PauseReason 是这一次暂停原因（见 task.PauseReason）：用户按的是这条运行自己的暂停键，
	// 还是「全部暂停」。只在**已暂停**期间非空——它回答的是「谁把它按下的」，
	// 一条早已跑完的运行带着这句话只会误导。
	PauseReason string `json:"pause_reason,omitempty"`
	// ControlPausedMillis 是这个任务至今在**已暂停**里待过的累计毫秒数，由引擎在每次离开暂停
	// （恢复、取消、收尾）时把那一段折进来。它只有一个用途：从速率与 ETA 的分母里扣掉——
	// 暂停期间任务一条都没处理，把那段时长算成在干活会让两个数一路失真到终态。
	//
	// 它与重启入参里的 `paused_ms` 不是一回事：那个是**磁盘作业**为阅读让路而等掉的时长，
	// 期间任务本身仍在跑。累计只属于这一次**运行**——重试是新一次运行，分母从它自己的开始时刻重新计。
	// 不进 JSON：前端不显示它，它是运行行上的一列。
	ControlPausedMillis int64 `json:"-"`
	// CoalescedCount 是**合并**进这条**排队中**运行的额外发起次数：同一件事被反复发起时不新建
	// 运行，只把这个数加一。它要发出去，否则用户看到的是一条孤零零的排队，不知道它代表了几次发起。
	CoalescedCount int               `json:"coalesced_count,omitempty"`
	Phase          string            `json:"phase,omitempty"`
	CurrentItem    string            `json:"current_item,omitempty"`
	EffectiveLimit *RunLimits        `json:"effective_limit,omitempty"`
	Metrics        map[string]int64  `json:"metrics,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	Params         map[string]string `json:"params,omitempty"`
	// StartedAt 是这次运行**进入运行中**的时刻。**排队中**的运行还没开跑，因此它是 nil 而不是
	// 一个零值时刻——发零值出去的话，界面上那格「开始时间」会写着它公元 1 年就开始了。
	// 入队时刻不是开始时刻：排了一小时队的运行会被算成跑了一小时，速率与 ETA 一路失真。
	StartedAt  *time.Time `json:"started_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Sequence   int64      `json:"-"`
}

// RunLiveSummary 是**实况汇总**：槽位占用与全局暂停那几个数，一条运行都不带。
//
// 它有自己的名字，因为它走两条路：整份实况帧里带一份（见 RunLive），推送通道上还单发一种
// 轻量帧（见 RunPush）。两条路同形是刻意的——实况区因此不必自己从运行列表里数，
// 而浏览器手上那份列表是推来的帧攒出来的，丢过一帧就少一条，少一条数出来的占用不会报错。
type RunLiveSummary struct {
	// Active 是**活动态**运行数，也就是占着运行槽位的那些；Queued 是**排队中**的条数。
	Active int `json:"active"`
	Queued int `json:"queued"`
	// Slots 是运行槽位上限，界面据此画出「槽位 n/N」。它恒为一个真的会被撞上的数——
	// 领域引擎把小于 1 的配置值改写成默认值，因此 0 不会发出去。
	Slots int `json:"slots"`
	// Paused 回答「有没有运行被暂停」，PausedAll 回答「全部暂停的闸门还关着吗」。
	//
	// 两个都要发，因为它们答的不是同一个问题：闸门关着而被暂停的那几条已经被取消或跑完时，
	// 前者是 false 而后者仍是 true，此时队列还被拦着——只按前者决定「全部恢复」的可按性，
	// 那个按钮会灰在唯一能重新放开队列的位置上。
	Paused    bool `json:"paused"`
	PausedAll bool `json:"paused_all"`
}

// RunLive 是任务中心**实况区**的一帧：此刻仍会变化的那些运行，加上它们的**实况汇总**。
//
// 它不受列表筛选影响。实况区答的是「我的盘现在在干什么」——那是个全局问题，
// 而顶部那对全部暂停 / 全部恢复同样作用于全体运行，按筛选给出的答案会与按钮动到的那批对不上。
type RunLive struct {
	// 内嵌而不是抄一遍那五个字段：抄一遍之后，推来的汇总与拉回来的整份实况就成了两种形状，
	// 前端要写两套读法。JSON 里内嵌是摊平的，因此对外的形状与抄一遍完全相同。
	RunLiveSummary
	// Runs 是常驻置顶的那批运行：**活动态**与**排队中**，仍会变化的都在里面。
	Runs []RunSnapshot `json:"runs"`
}

// RunPush 是推送通道上的一帧信封：一个序号、上一帧的序号，加上这一帧真正的载荷。
//
// 两个序号都取自引擎那个单调计数器，**不是第二套编号**：Sequence 就是这一帧对应的那条运行行上
// 的序号（汇总帧取的是同一个计数器发的下一个），Prev 是本通道**上一帧**的 Sequence。
//
// 要 Prev 是因为光靠 Sequence 判不出漏帧：逐条目进度按水位节流，被吞掉的那些照样取了号，
// 于是送达的序号本来就不连续。Prev 把送出去的那些串成一条链——前端手上的上一号对不上这一帧的
// Prev，就是中间掉了东西，整份重拉一次。投递通道是非阻塞的，buffer 满就丢弃，
// 而丢掉的若恰好是终态，界面会一直停在过期的进度上等下一次轮询。
type RunPush struct {
	Sequence int64 `json:"sequence"`
	Prev     int64 `json:"prev"`
	// Run 与 Live 恰有一个非空：前者是一帧全量运行快照，后者是一帧**实况汇总**。
	// 快照始终是全量的，不发增量——增量的合并逻辑要前后端各维护一套，而合并错一个字段不会报错，
	// 只会让界面安静地停在错值上。
	Run  *RunSnapshot    `json:"run,omitempty"`
	Live *RunLiveSummary `json:"live,omitempty"`
}

// TaskSummary 是**任务清单**上的一行：一个任务的身份、它的长期属性，与它最近一次运行。
//
// 历次运行不在这里：一屏几十个任务、每个几十次运行，一次全带回来是几千条。
// 展开某一行时按 task_id 单独取（见 taskFilters.TaskID）。
type TaskSummary struct {
	TaskID  int64       `json:"task_id"`
	Type    string      `json:"type"`
	Scope   string      `json:"scope"`
	ScopeID *int64      `json:"scope_id,omitempty"`
	Variant TaskVariant `json:"variant,omitempty"`
	// ScopeName 取自最近一次运行：显示名是**过渡期**字段，落在运行上而不是身份上
	// （见 task.Run.ScopeName）。一次都没跑过的任务因此没有显示名，界面回落到作用域加 id。
	ScopeName string `json:"scope_name,omitempty"`
	// Disabled / FailStreak / BackoffUntil 是**退避**与禁用那一组长期属性。
	// 连败为 0 时界面整格不显示，而不是画一个「连败 0」。
	Disabled      bool       `json:"disabled"`
	FailStreak    int        `json:"fail_streak"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	BackoffUntil  *time.Time `json:"backoff_until,omitempty"`
	// StallReason 是这个任务此刻**停发**的原因（人工禁用 / 连败到阈值 / 退避未到期），
	// 空串表示自动发起照常。界面按它标红并写明为什么。
	//
	// 它是**派生**出来的一个封闭枚举，不是上面那三个字段的重复：判定要那三个数、还要三个阈值
	// 与此刻的时间，而阈值在设置里可改。前端自己拿三个字段推一遍就等于把策略抄到浏览器里，
	// 改完设置之后两边立刻对不上。
	StallReason string `json:"stall_reason,omitempty"`
	// LastRun 是「上次跑成什么样」。一次运行都没有的任务为 nil。
	LastRun *RunSnapshot `json:"last_run,omitempty"`
}

type RunLimits struct {
	ScanProfile                string `json:"scan_profile,omitempty"`
	ScannerWorkersConfigured   int    `json:"scanner_workers_configured,omitempty"`
	ScannerWorkersEffective    int    `json:"scanner_workers_effective,omitempty"`
	StorageProfile             string `json:"storage_profile,omitempty"`
	VolumeKey                  string `json:"volume_key,omitempty"`
	ScanConcurrency            int    `json:"scan_concurrency,omitempty"`
	ArchiveOpenConcurrency     int    `json:"archive_open_concurrency,omitempty"`
	CoverConcurrency           int    `json:"cover_concurrency,omitempty"`
	HashConcurrency            int    `json:"hash_concurrency,omitempty"`
	PauseBackgroundWhenReading bool   `json:"pause_background_when_reading"`
	IdleOnlyHeavyTasks         bool   `json:"idle_only_heavy_tasks"`
	DisableSameDiskPageCache   bool   `json:"disable_same_disk_page_cache"`
}
