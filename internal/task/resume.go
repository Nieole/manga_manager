// **可续跑**这一半：白名单、它的全局开关，与重启转**中断**之后由它挑出「谁自己接着跑」。
// 真正把那几条重新发起的是装配方——任务体是个闭包，落不了盘，本包只交出该续跑的那几条。

package task

import "context"

// ResumeKey 是**可续跑**白名单的键：类型加**变体**。
//
// 变体也判：同一个类型下的两个变体跑法不同，其中一个可续跑不等于另一个也可以。作用域与作用域 id
// 不进这个键——续跑与「哪个库」无关，它问的是「这类活在无人看着时自己再跑一次会不会出事」。
type ResumeKey struct {
	Type    Type
	Variant Variant
}

// ResumePolicy 是**可续跑**白名单与它的全局开关。白名单的内容属于装配方：本包不认识具体有哪些类型。
//
// 它是一份**允许清单**，没列进来的一律停在**中断**等人裁决。默认拒绝是刻意的，因为两个方向的
// 代价不对称：漏掉一个可续跑的类型，用户重启后多点一次重试；错放进一个改磁盘内容的类型，
// 无人值守的机器开机就自己动了文件。
//
// **可续跑比可重试严格，两者不得合成一个标志**（规格关键决定 7）：可重试只要求「能再发起一次」，
// 可续跑还要求「在无人看着时再发起一次也不会造成意外」——外部库传输与 ComicInfo 回写
// 可重试但不可续跑。
type ResumePolicy struct {
	// Disabled 是全局开关的反面：零值即**开着**，与「默认开」一致。关掉之后一条都不续跑，
	// 而仍会变化的运行照样全部转**中断**——开关关的是「自己接着跑」，不是「记不记这一笔」。
	Disabled bool
	// Types 是白名单本身，为空即一条都不续跑。
	Types map[ResumeKey]struct{}
}

// NewResumePolicy 收一组白名单键建一份策略，全局开关开着。
func NewResumePolicy(keys ...ResumeKey) ResumePolicy {
	types := make(map[ResumeKey]struct{}, len(keys))
	for _, key := range keys {
		types[key] = struct{}{}
	}
	return ResumePolicy{Types: types}
}

// Allows 判断这个身份的运行在重启之后该不该自动重排队。
func (p ResumePolicy) Allows(id Identity) bool {
	if p.Disabled {
		return false
	}
	_, ok := p.Types[ResumeKey{Type: id.Type, Variant: id.Variant}]
	return ok
}

// Interruption 是一次重启转写的结果：转了多少条，以及其中哪几条该自己接着跑。
//
// 两者一起交出去是因为**只有这一批**才该被续跑：库里那些更早的**中断**运行是上几轮重启留下的，
// 人已经看过并选择了不重试，按状态再查一遍会把它们一并叫醒。
type Interruption struct {
	// Marked 是转成**中断**的条数。
	Marked int
	// Resume 是白名单挑出来、该由装配方重新发起的那些运行，每个任务至多一条。
	// 快照而不是运行行：**重启函数**要读回的原始入参在侧数据里。
	//
	// 它只可能来自重启前**正在跑或排着队**的运行：用户按下暂停或取消的那几条同样转成中断，
	// 但不在这里（见 markInterrupted）。
	Resume []Snapshot
}

// resumePolicy 读此刻的白名单与全局开关；装配期没交出来就一条都不续跑。
//
// 在这里也写一份白名单等于让「哪些类型可续跑」有两个答案，而其中一个还认不出具体的类型名。
func (e *Engine) resumePolicy() ResumePolicy {
	if e.resume == nil {
		return ResumePolicy{}
	}
	return e.resume()
}

// resumable 从刚转成**中断**的这批运行里挑出该自己接着跑的，配上侧数据交出去。
//
// 判据要的是**身份**，而运行行上只有任务 id：类型与**变体**在任务那一行上，因此先一次性批量
// 取回身份，不是每条一次查询。
//
// **每个任务至多交出一条**：重启之前同一个任务可能既有一条活动运行、又有一条排着的，而它们跑的
// 是同一件事。两条都重新发起的话，后一条必然当场被**合并**掉，用户看到的是一条凭空带着合并计数
// 的恢复运行。
//
// 「续不续」只由 ResumePolicy.Allows 一处回答，全局开关关着也照样走一遍这段（一次批量取身份的
// 查询，发生在开机那一刻）：在这里抢答一次等于把同一条判据写两遍。
func (e *Engine) resumable(ctx context.Context, policy ResumePolicy, marked []Run) ([]Snapshot, error) {
	if len(marked) == 0 {
		return nil, nil
	}
	taskIDs := make([]int64, 0, len(marked))
	for _, run := range marked {
		taskIDs = append(taskIDs, run.TaskID)
	}
	owners, err := e.store.LoadTasks(ctx, taskIDs)
	if err != nil {
		return nil, err
	}

	picked := make(map[int64]bool, len(marked))
	runs := make([]Run, 0, len(marked))
	for _, run := range marked {
		owner, ok := owners[run.TaskID]
		if !ok || picked[run.TaskID] || !policy.Allows(owner.Identity) {
			continue
		}
		picked[run.TaskID] = true
		runs = append(runs, run)
	}
	if len(runs) == 0 {
		return nil, nil
	}
	return e.snapshotsOf(ctx, runs)
}
