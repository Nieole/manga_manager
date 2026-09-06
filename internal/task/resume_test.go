// **可续跑**白名单的契约：重启转**中断**之后谁被挑出来自己接着跑、谁停在中断等人裁决，
// 以及恢复出来的那一条与原来那条的关系。全程纯内存，一次盘都不读。

package task

import (
	"context"
	"testing"
)

// comicInfoSpec 是一份**改磁盘内容**的运行声明：ComicInfo 回写每本书都是一次原子替换。
// 它可重试，但无人看着时不该自己重跑——白名单外的那一半用它举例。
func comicInfoSpec(seriesID int64) RunSpec {
	return RunSpec{
		Identity:  Identity{Type: "write_comicinfo", Scope: ScopeSeries, ScopeID: seriesID, Variant: VariantPrimary},
		Trigger:   TriggerManual,
		Total:     10,
		CanCancel: true,
	}
}

// scanWhitelist 是只放行资料库扫描的白名单，开关开着。
func scanWhitelist() ResumePolicy {
	return NewResumePolicy(ResumeKey{Type: "scan_library", Variant: VariantPrimary})
}

// resumeIDs 取一批待续跑快照的运行 id，供断言挑中了哪几条。
func resumeIDs(snapshots []Snapshot) []int64 {
	ids := make([]int64, 0, len(snapshots))
	for _, snapshot := range snapshots {
		ids = append(ids, snapshot.Run.ID)
	}
	return ids
}

// 白名单是一份**允许清单**：列进来的挑出来自己接着跑，没列的照样转**中断**、但一条都不发起。
// 判错方向的代价不对称——错放进一个改磁盘内容的类型，无人值守的机器开机就自己动了文件。
func TestRestartResumesWhitelistedTypesOnly(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	h.resume = scanWhitelist()
	scan := h.start(t, libraryScanSpec(1), idleBody)
	comicInfo := h.start(t, comicInfoSpec(9), idleBody)

	outcome, err := h.engine.MarkInterrupted(context.Background())
	if err != nil {
		t.Fatalf("批量转中断失败: %v", err)
	}

	if outcome.Marked != 2 {
		t.Fatalf("转中断的条数为 %d, want 2", outcome.Marked)
	}
	for _, id := range []int64{scan.ID, comicInfo.ID} {
		if got := h.load(t, id).Status; got != StatusInterrupted {
			t.Fatalf("运行 %d 的状态为 %q, want interrupted —— 白名单只决定谁重排队，不决定谁转中断", id, got)
		}
	}
	if got := resumeIDs(outcome.Resume); len(got) != 1 || got[0] != scan.ID {
		t.Fatalf("待续跑的是 %v, want [%d]（只有资料库扫描在白名单里）", got, scan.ID)
	}
}

// 用户按下暂停或取消之后断电，重启不该把它们又叫起来：对这两条运行，用户最后一次表态是「停下」。
// 它们照样转**中断**——那一笔记的是「上一次断在哪」，与要不要自己接着跑是两件事。
func TestPausedAndCancellingRunsAreNotResumed(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	h.resume = scanWhitelist()
	paused := h.start(t, libraryScanSpec(1), idleBody)
	cancelling := h.start(t, libraryScanSpec(2), idleBody)
	if err := h.engine.Pause(paused.ID); err != nil {
		t.Fatalf("暂停失败: %v", err)
	}
	if err := h.engine.Cancel(cancelling.ID); err != nil {
		t.Fatalf("取消失败: %v", err)
	}
	if got := h.load(t, cancelling.ID).Status; got != StatusCancelling {
		t.Fatalf("取消之后的状态为 %q, want cancelling（任务体没在跑，收不了尾）", got)
	}

	outcome, err := h.engine.MarkInterrupted(context.Background())
	if err != nil {
		t.Fatalf("批量转中断失败: %v", err)
	}

	if outcome.Marked != 2 {
		t.Fatalf("转中断的条数为 %d, want 2 —— 暂停与取消中的运行照样要记这一笔", outcome.Marked)
	}
	if got := resumeIDs(outcome.Resume); len(got) != 0 {
		t.Fatalf("待续跑的是 %v, want 空 —— 用户按下的那一下是「停」，重启不该把它推翻", got)
	}
}

// 全局开关关掉之后一条都不续跑，而转**中断**照旧：开关关的是「自己接着跑」，不是「记不记这一笔」。
func TestResumeSwitchOffMarksEverythingAndResumesNothing(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	h.resume = scanWhitelist()
	h.resume.Disabled = true
	scan := h.start(t, libraryScanSpec(1), idleBody)

	outcome, err := h.engine.MarkInterrupted(context.Background())
	if err != nil {
		t.Fatalf("批量转中断失败: %v", err)
	}

	if outcome.Marked != 1 {
		t.Fatalf("转中断的条数为 %d, want 1 —— 关掉续跑不该连中断也不记", outcome.Marked)
	}
	if got := h.load(t, scan.ID).Status; got != StatusInterrupted {
		t.Fatalf("运行的状态为 %q, want interrupted", got)
	}
	if len(outcome.Resume) != 0 {
		t.Fatalf("开关关着却挑出了 %d 条待续跑", len(outcome.Resume))
	}
}

// **排队中**的运行在重启时同样进中断，其中可续跑的照样恢复：它没开跑过，但那一次发起是真的。
func TestQueuedRunsAreResumedToo(t *testing.T) {
	h := newTestEngine(t, registerOnly, 1)
	h.resume = scanWhitelist()
	h.start(t, libraryScanSpec(1), idleBody)
	queued := h.start(t, libraryScanSpec(2), idleBody)
	if got := h.load(t, queued.ID).Status; got != StatusQueued {
		t.Fatalf("第二条运行的状态为 %q, want queued（槽位只有一个）", got)
	}

	outcome, err := h.engine.MarkInterrupted(context.Background())
	if err != nil {
		t.Fatalf("批量转中断失败: %v", err)
	}

	ids := resumeIDs(outcome.Resume)
	if len(ids) != 2 {
		t.Fatalf("待续跑的是 %v, want 两条（活动的与排队的各一条）", ids)
	}
}

// 同一个任务重启前可能既有一条活动运行、又有一条排着的，两条跑的是同一件事：至多恢复一条。
// 两条都发起的话，后一条必然当场被**合并**掉，用户看到的是一条凭空带着合并计数的恢复运行。
func TestResumeYieldsAtMostOneRunPerTask(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	h.resume = scanWhitelist()
	active := h.start(t, libraryScanSpec(1), idleBody)
	queued := h.start(t, libraryScanSpec(1), idleBody)
	if active.ID == queued.ID || queued.Status != StatusQueued {
		t.Fatalf("同一个任务的第二次发起落成了 %+v, want 一条新的排队运行", queued)
	}

	outcome, err := h.engine.MarkInterrupted(context.Background())
	if err != nil {
		t.Fatalf("批量转中断失败: %v", err)
	}

	if outcome.Marked != 2 {
		t.Fatalf("转中断的条数为 %d, want 2", outcome.Marked)
	}
	if got := resumeIDs(outcome.Resume); len(got) != 1 || got[0] != active.ID {
		t.Fatalf("待续跑的是 %v, want [%d]（同一个任务只发一次）", got, active.ID)
	}
}

// 待续跑的那份快照必须带着侧数据：**重启函数**要读回这次运行是拿什么参数发起的，
// 读丢了不会有编译错误，后果是恢复出来的那一次静默换了跑法。
func TestResumeCarriesTheOriginalArgs(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	h.resume = scanWhitelist()
	spec := libraryScanSpec(1)
	spec.Args = map[string]string{"force": "true"}
	h.start(t, spec, idleBody)

	outcome, err := h.engine.MarkInterrupted(context.Background())
	if err != nil {
		t.Fatalf("批量转中断失败: %v", err)
	}

	if len(outcome.Resume) != 1 {
		t.Fatalf("待续跑的有 %d 条, want 1", len(outcome.Resume))
	}
	if got := outcome.Resume[0].Side.Args["force"]; got != "true" {
		t.Fatalf("待续跑的快照里 force = %q, want true —— 重启函数要读的入参没跟着交出来", got)
	}
}

// 恢复出来的是**新一次运行**，原来那条留在**中断**：用户回头要看得到「上一次断在哪」。
// 重新发起这一步属于装配方（任务体是个闭包，落不了盘），这里照它的做法走一遍启动入口。
func TestResumeStartsANewRunAndKeepsTheInterruptedOne(t *testing.T) {
	h := newTestEngine(t, registerOnly, 0)
	h.resume = scanWhitelist()
	original := h.start(t, libraryScanSpec(1), idleBody)

	outcome, err := h.engine.MarkInterrupted(context.Background())
	if err != nil {
		t.Fatalf("批量转中断失败: %v", err)
	}
	if len(outcome.Resume) != 1 {
		t.Fatalf("待续跑的有 %d 条, want 1", len(outcome.Resume))
	}
	resumeSpec := libraryScanSpec(1)
	resumeSpec.Trigger = TriggerResumed
	resumed := h.start(t, resumeSpec, idleBody)

	if resumed.ID == original.ID {
		t.Fatal("恢复改写了原来那条运行 —— 它该留在中断态，恢复是新一次运行")
	}
	if got := h.load(t, original.ID).Status; got != StatusInterrupted {
		t.Fatalf("原来那条运行的状态为 %q, want interrupted", got)
	}
	if got := h.load(t, resumed.ID).Trigger; got != TriggerResumed {
		t.Fatalf("恢复出来的运行发起方为 %q, want resumed", got)
	}
	if got := h.load(t, resumed.ID).NthRun; got != original.NthRun+1 {
		t.Fatalf("恢复出来的是第 %d 次运行, want 第 %d 次", got, original.NthRun+1)
	}
}

// 恢复走的是同一个启动入口，因此**槽位上限照样管着它们**：一次重启恢复出两条，
// 槽位只有一个时第二条留在**排队中**等放行，而不是绕过队列直接开跑。
func TestResumedRunsGoThroughTheQueue(t *testing.T) {
	h := newTestEngine(t, registerOnly, 1)
	h.resume = scanWhitelist()
	h.start(t, libraryScanSpec(1), idleBody)
	h.start(t, libraryScanSpec(2), idleBody)

	outcome, err := h.engine.MarkInterrupted(context.Background())
	if err != nil {
		t.Fatalf("批量转中断失败: %v", err)
	}
	if len(outcome.Resume) != 2 {
		t.Fatalf("待续跑的有 %d 条, want 2", len(outcome.Resume))
	}

	// 待续跑的顺序是序号升序，也就是它们当初被发起的顺序：先排上的先恢复。
	statuses := make([]RunStatus, 0, len(outcome.Resume))
	for i := range outcome.Resume {
		spec := libraryScanSpec(int64(i + 1))
		spec.Trigger = TriggerResumed
		statuses = append(statuses, h.load(t, h.start(t, spec, idleBody).ID).Status)
	}
	if statuses[0] != StatusRunning || statuses[1] != StatusQueued {
		t.Fatalf("恢复出来的两条状态为 %v, want [running queued] —— 槽位只有一个，第二条该排队", statuses)
	}
}
